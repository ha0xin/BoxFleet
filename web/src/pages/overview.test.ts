import { describe, expect, it } from "vitest";

import type { NetworkEventSeriesResponse, TrafficPoint, TrafficSeriesResponse } from "../types";
import { networkEventTrend, nodeTrendValues, trafficTrendTotal, trafficTrendValues } from "./overview";

function point(overrides: Partial<TrafficPoint> = {}): TrafficPoint {
  return {
    bucket_start: "2026-07-20T00:00:00Z",
    uplink_raw_bytes: 0,
    uplink_billable_bytes: 0,
    downlink_raw_bytes: 0,
    downlink_billable_bytes: 0,
    ...overrides
  };
}

function trafficResponse(series: TrafficSeriesResponse["series"]): TrafficSeriesResponse {
  return {
    bucket: "day",
    offset_minutes: -480,
    start: "2026-07-20T00:00:00Z",
    end: "2026-07-26T00:00:00Z",
    group: series[0]?.key === "total" ? "total" : "node",
    series,
    truncated: false
  };
}

function eventResponse(series: NetworkEventSeriesResponse["series"]): NetworkEventSeriesResponse {
  return {
    bucket: "day",
    offset_minutes: -480,
    start: "2026-07-20T00:00:00Z",
    end: "2026-07-26T00:00:00Z",
    group: "total",
    series,
    actions: [{ action: "connect", count: 12 }],
    truncated: false
  };
}

describe("trafficTrendValues", () => {
  it("sums both directions of billable bytes per bucket, in server order", () => {
    const response = trafficResponse([
      {
        key: "total",
        label: "All traffic",
        points: [
          point({ uplink_billable_bytes: 1, downlink_billable_bytes: 2, uplink_raw_bytes: 900 }),
          point({ bucket_start: "2026-07-21T00:00:00Z", uplink_billable_bytes: 0, downlink_billable_bytes: 0 }),
          point({ bucket_start: "2026-07-22T00:00:00Z", uplink_billable_bytes: 10, downlink_billable_bytes: 5 })
        ],
        totals: {
          uplink_raw_bytes: 900,
          uplink_billable_bytes: 11,
          downlink_raw_bytes: 0,
          downlink_billable_bytes: 7
        }
      }
    ]);

    expect(trafficTrendValues(response)).toEqual([3, 0, 15]);
    expect(trafficTrendTotal(response)).toBe(18);
  });

  it("reports nothing when the series is absent", () => {
    expect(trafficTrendValues(undefined)).toEqual([]);
    expect(trafficTrendValues(trafficResponse([]))).toEqual([]);
    expect(trafficTrendTotal(undefined)).toBeNull();
    expect(trafficTrendTotal(trafficResponse([]))).toBeNull();
  });
});

describe("nodeTrendValues", () => {
  it("keys the trend by node name so rows can look themselves up", () => {
    const trends = nodeTrendValues(
      trafficResponse([
        {
          key: "edge-1",
          label: "edge-1",
          points: [point({ downlink_billable_bytes: 4 }), point({ bucket_start: "2026-07-21T00:00:00Z", uplink_billable_bytes: 6 })],
          totals: { uplink_raw_bytes: 0, uplink_billable_bytes: 6, downlink_raw_bytes: 0, downlink_billable_bytes: 4 }
        },
        {
          key: "edge-2",
          label: "edge-2",
          points: [point(), point({ bucket_start: "2026-07-21T00:00:00Z" })],
          totals: { uplink_raw_bytes: 0, uplink_billable_bytes: 0, downlink_raw_bytes: 0, downlink_billable_bytes: 0 }
        }
      ])
    );

    expect(trends.get("edge-1")).toEqual([4, 6]);
    expect(trends.get("edge-2")).toEqual([0, 0]);
    expect(trends.get("edge-3")).toBeUndefined();
  });

  it("is empty until the series resolves", () => {
    expect(nodeTrendValues(undefined).size).toBe(0);
  });
});

describe("networkEventTrend", () => {
  it("takes counts and the window total from the total series", () => {
    const trend = networkEventTrend(
      eventResponse([
        {
          key: "total",
          label: "All events",
          points: [
            { bucket_start: "2026-07-20T00:00:00Z", count: 3 },
            { bucket_start: "2026-07-21T00:00:00Z", count: 0 },
            { bucket_start: "2026-07-22T00:00:00Z", count: 9 }
          ],
          total: 12
        }
      ])
    );

    expect(trend.values).toEqual([3, 0, 9]);
    expect(trend.total).toBe(12);
  });

  it("reports a null total rather than zero when the series is absent", () => {
    expect(networkEventTrend(undefined)).toEqual({ values: [], total: null });
    expect(networkEventTrend(eventResponse([]))).toEqual({ values: [], total: null });
  });
});
