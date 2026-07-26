import { describe, expect, it } from "vitest";

import type { TrafficSeries } from "../types";
import { queryString } from "@/admin/query";
import {
  dayBucketOffsetMinutes,
  directedBytes,
  filtersFromSearchParams,
  formatShare,
  peakBucket,
  resolveBucket,
  resolveTimeRange,
  trafficChartSeries,
  trafficSeriesScope,
  trafficUserRows
} from "./traffic";

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

function point(bucketStart: string, uplink: number, downlink: number, multiplier = 1) {
  return {
    bucket_start: bucketStart,
    uplink_raw_bytes: uplink,
    uplink_billable_bytes: uplink * multiplier,
    downlink_raw_bytes: downlink,
    downlink_billable_bytes: downlink * multiplier
  };
}

function series(key: string, points: ReturnType<typeof point>[]): TrafficSeries {
  return {
    key,
    label: key,
    points,
    totals: points.reduce(
      (sum, entry) => ({
        uplink_raw_bytes: sum.uplink_raw_bytes + entry.uplink_raw_bytes,
        uplink_billable_bytes: sum.uplink_billable_bytes + entry.uplink_billable_bytes,
        downlink_raw_bytes: sum.downlink_raw_bytes + entry.downlink_raw_bytes,
        downlink_billable_bytes: sum.downlink_billable_bytes + entry.downlink_billable_bytes
      }),
      { uplink_raw_bytes: 0, uplink_billable_bytes: 0, downlink_raw_bytes: 0, downlink_billable_bytes: 0 }
    )
  };
}

describe("filtersFromSearchParams", () => {
  it("falls back to the defaults for missing or unknown values", () => {
    expect(filtersFromSearchParams(new URLSearchParams("range=year&bucket=minute&metric=bogus"))).toEqual({
      range: "7d",
      bucket: "",
      metric: "billable",
      node: "all",
      user: "all"
    });
  });

  it("reads every supported value back out of the URL", () => {
    expect(filtersFromSearchParams(new URLSearchParams("range=custom&bucket=hour&metric=raw&node=edge-1&user=alice"))).toEqual({
      range: "custom",
      bucket: "hour",
      metric: "raw",
      node: "edge-1",
      user: "alice"
    });
  });
});

describe("resolveTimeRange", () => {
  const now = new Date("2026-07-26T12:00:00Z");

  it("anchors relative presets on the supplied instant", () => {
    const range = resolveTimeRange({ range: "24h", bucket: "", metric: "billable", node: "all", user: "all" }, null, null, now);
    expect(range.end).toBe(now.toISOString());
    expect(Date.parse(range.end) - Date.parse(range.start)).toBe(DAY_MS);
    expect(range.label).toBe("Last 24 hours");
  });

  it("uses the custom window when both bounds parse and end is after start", () => {
    const range = resolveTimeRange(
      { range: "custom", bucket: "", metric: "billable", node: "all", user: "all" },
      "2026-07-01T00:00:00Z",
      "2026-07-08T00:00:00Z",
      now
    );
    expect(range.start).toBe("2026-07-01T00:00:00.000Z");
    expect(range.end).toBe("2026-07-08T00:00:00.000Z");
  });

  it("falls back to the default preset when a custom window is unusable", () => {
    // start and end are required by the endpoint, so an inverted or missing
    // custom window must never reach the request.
    const inverted = resolveTimeRange(
      { range: "custom", bucket: "", metric: "billable", node: "all", user: "all" },
      "2026-07-08T00:00:00Z",
      "2026-07-01T00:00:00Z",
      now
    );
    expect(Date.parse(inverted.end) - Date.parse(inverted.start)).toBe(7 * DAY_MS);
    expect(inverted.label).toBe("Last 7 days");

    const missing = resolveTimeRange(
      { range: "custom", bucket: "", metric: "billable", node: "all", user: "all" },
      null,
      null,
      now
    );
    expect(Date.parse(missing.end) - Date.parse(missing.start)).toBe(7 * DAY_MS);
  });
});

describe("resolveBucket", () => {
  it("derives hour buckets for short windows and day buckets beyond 48 hours", () => {
    expect(resolveBucket("", 12 * HOUR_MS)).toBe("hour");
    expect(resolveBucket("", 48 * HOUR_MS)).toBe("hour");
    expect(resolveBucket("", 49 * HOUR_MS)).toBe("day");
  });

  it("honours an explicit granularity inside the hourly ceiling", () => {
    expect(resolveBucket("hour", 7 * DAY_MS)).toBe("hour");
    expect(resolveBucket("day", HOUR_MS)).toBe("day");
  });

  it("degrades an hourly request past the 7 day ceiling instead of erroring", () => {
    expect(resolveBucket("hour", 7 * DAY_MS + 1)).toBe("day");
    expect(resolveBucket("hour", 30 * DAY_MS)).toBe("day");
  });
});

describe("dayBucketOffsetMinutes", () => {
  it("sends nothing for hour buckets, which are UTC aligned", () => {
    expect(dayBucketOffsetMinutes("hour", "2026-07-26T00:00:00Z")).toBe(0);
  });

  it("sends the offset the server adds to UTC to reach local time", () => {
    const instant = "2026-07-26T00:00:00Z";
    const offset = dayBucketOffsetMinutes("day", instant);
    const shifted = new Date(Date.parse(instant) + offset * 60_000);
    const local = new Date(instant);
    expect(shifted.getUTCFullYear()).toBe(local.getFullYear());
    expect(shifted.getUTCDate()).toBe(local.getDate());
    expect(shifted.getUTCHours()).toBe(local.getHours());
    expect(shifted.getUTCMinutes()).toBe(local.getMinutes());
  });
});

describe("trafficSeriesScope", () => {
  const base = {
    start: "2026-07-19T00:00:00.000Z",
    end: "2026-07-26T00:00:00.000Z",
    offsetMinutes: -480,
    node: "all",
    user: "all",
    group: "total" as const
  };

  it("omits offset_minutes for hour buckets and drops the all sentinels", () => {
    expect(queryString(trafficSeriesScope({ ...base, bucket: "hour" }))).toBe(
      "?start=2026-07-19T00%3A00%3A00.000Z&end=2026-07-26T00%3A00%3A00.000Z&bucket=hour&group=total"
    );
  });

  it("sends offset_minutes for day buckets", () => {
    expect(queryString(trafficSeriesScope({ ...base, bucket: "day" }))).toContain("offset_minutes=-480");
  });

  it("forwards explicit node and user filters and the group limit", () => {
    const query = queryString(
      trafficSeriesScope({ ...base, bucket: "day", node: "edge-1", user: "alice", group: "user", limit: 25 })
    );
    expect(query).toContain("node=edge-1");
    expect(query).toContain("user=alice");
    expect(query).toContain("group=user");
    expect(query).toContain("limit=25");
  });
});

describe("directedBytes", () => {
  const volume = {
    uplink_raw_bytes: 100,
    uplink_billable_bytes: 150,
    downlink_raw_bytes: 400,
    downlink_billable_bytes: 600
  };

  it("selects the billable columns by default and the raw columns on request", () => {
    expect(directedBytes(volume, "billable")).toEqual({ uplink: 150, downlink: 600, total: 750 });
    expect(directedBytes(volume, "raw")).toEqual({ uplink: 100, downlink: 400, total: 500 });
  });
});

describe("trafficChartSeries", () => {
  const source = series("total", [
    point("2026-07-24T00:00:00Z", 1, 4),
    point("2026-07-25T00:00:00Z", 0, 0),
    point("2026-07-26T00:00:00Z", 2, 8)
  ]);

  it("plots downlink first and converts bucket starts to epoch milliseconds", () => {
    const [downlink, uplink] = trafficChartSeries(source, "raw");
    expect(downlink.key).toBe("downlink");
    expect(uplink.key).toBe("uplink");
    expect(downlink.points).toEqual([
      [Date.parse("2026-07-24T00:00:00Z"), 4],
      [Date.parse("2026-07-25T00:00:00Z"), 0],
      [Date.parse("2026-07-26T00:00:00Z"), 8]
    ]);
  });

  it("keeps every server bucket, including the zero-filled ones", () => {
    // The server owns bucketing and zero-fill; dropping an empty bucket here
    // would silently re-bucket the chart.
    for (const entry of trafficChartSeries(source, "billable")) {
      expect(entry.points).toHaveLength(source.points.length);
    }
  });

  it("renders nothing without a series", () => {
    expect(trafficChartSeries(null, "billable")).toEqual([]);
  });
});

describe("trafficUserRows", () => {
  it("reads window totals from the series totals and builds a trend per bucket", () => {
    const rows = trafficUserRows(
      [series("alice", [point("2026-07-25T00:00:00Z", 1, 3, 2), point("2026-07-26T00:00:00Z", 2, 6, 2)])],
      "billable"
    );
    expect(rows).toEqual([
      { key: "alice", label: "alice", uplink: 6, downlink: 18, total: 24, trend: [8, 16] }
    ]);
  });

  it("recomputes against the raw columns when the metered view is selected", () => {
    const rows = trafficUserRows(
      [series("alice", [point("2026-07-25T00:00:00Z", 1, 3, 2), point("2026-07-26T00:00:00Z", 2, 6, 2)])],
      "raw"
    );
    expect(rows[0]).toMatchObject({ uplink: 3, downlink: 9, total: 12, trend: [4, 8] });
  });
});

describe("peakBucket", () => {
  it("reports the heaviest bucket for the selected metric", () => {
    const source = series("total", [
      point("2026-07-24T00:00:00Z", 1, 1, 10),
      point("2026-07-25T00:00:00Z", 5, 5),
      point("2026-07-26T00:00:00Z", 2, 2)
    ]);
    expect(peakBucket(source, "billable")).toEqual({ bucketStart: "2026-07-24T00:00:00Z", value: 20 });
    expect(peakBucket(source, "raw")).toEqual({ bucketStart: "2026-07-25T00:00:00Z", value: 10 });
  });

  it("reports nothing without a series", () => {
    expect(peakBucket(null, "billable")).toBeNull();
  });
});

describe("formatShare", () => {
  it("formats shares with one decimal below ten percent and none above", () => {
    expect(formatShare(250, 1000)).toBe("25%");
    expect(formatShare(50, 1000)).toBe("5.0%");
    expect(formatShare(1, 100_000)).toBe("<0.1%");
    expect(formatShare(0, 1000)).toBe("0.0%");
    expect(formatShare(5, 0)).toBe("0%");
  });
});
