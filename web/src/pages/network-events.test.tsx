// @vitest-environment jsdom

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import type {
  ConnectionEventsResponse,
  ConnectionHostsResponse,
  ConnectionSeriesResponse,
  ConnectionTelemetryNodesResponse,
  NetworkEventHostsResponse,
  ServiceUsageResponse
} from "../types";
import {
  ActivityPanel,
  ConnectionTelemetryPanel,
  ServiceAuditPanel,
  bucketOffsetMinutes,
  formatDurationMs,
  formatRatio,
  resolveSeriesBucket,
  seriesSpanMillis,
  validConnectionSort
} from "./network-events";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const DAY_MS = 24 * 60 * 60 * 1000;

describe("seriesSpanMillis", () => {
  it("returns the covered span for a bounded range", () => {
    expect(seriesSpanMillis("2026-07-25T00:00:00Z", "2026-07-26T00:00:00Z")).toBe(DAY_MS);
  });

  it("returns null when either bound is missing, unparseable, or inverted", () => {
    expect(seriesSpanMillis(undefined, "2026-07-26T00:00:00Z")).toBeNull();
    expect(seriesSpanMillis("2026-07-25T00:00:00Z", undefined)).toBeNull();
    expect(seriesSpanMillis("not a time", "2026-07-26T00:00:00Z")).toBeNull();
    expect(seriesSpanMillis("2026-07-26T00:00:00Z", "2026-07-25T00:00:00Z")).toBeNull();
  });
});

describe("resolveSeriesBucket", () => {
  it("derives hour buckets up to 48 hours and day buckets beyond, matching the server", () => {
    expect(resolveSeriesBucket(null, 2 * DAY_MS)).toEqual({ bucket: "hour", hourAllowed: true });
    expect(resolveSeriesBucket(null, 3 * DAY_MS)).toEqual({ bucket: "day", hourAllowed: true });
  });

  it("honours an explicit choice while the range still supports it", () => {
    expect(resolveSeriesBucket("day", DAY_MS)).toEqual({ bucket: "day", hourAllowed: true });
    expect(resolveSeriesBucket("hour", 5 * DAY_MS)).toEqual({ bucket: "hour", hourAllowed: true });
    expect(resolveSeriesBucket("nonsense", DAY_MS)).toEqual({ bucket: "hour", hourAllowed: true });
  });

  it("withdraws hourly past a week and for an unbounded range", () => {
    expect(resolveSeriesBucket("hour", 8 * DAY_MS)).toEqual({ bucket: "day", hourAllowed: false });
    expect(resolveSeriesBucket("hour", null)).toEqual({ bucket: "day", hourAllowed: false });
  });
});

describe("bucketOffsetMinutes", () => {
  it("sends local midnight for day buckets and nothing for hour buckets", () => {
    // getTimezoneOffset is minutes to add to local to reach UTC; the server adds
    // offset_minutes to UTC to reach local, so the sign flips.
    const utcPlus8 = { getTimezoneOffset: () => -480 } as Date;
    expect(bucketOffsetMinutes("day", utcPlus8)).toBe(480);
    expect(bucketOffsetMinutes("hour", utcPlus8)).toBe(0);
  });
});

describe("validConnectionSort", () => {
  it("narrows the URL to the two dimensions the server accepts", () => {
    expect(validConnectionSort("connections")).toBe("connections");
    expect(validConnectionSort("bytes")).toBe("bytes");
    expect(validConnectionSort("duration")).toBe("bytes");
    expect(validConnectionSort(null)).toBe("bytes");
  });
});

describe("formatRatio", () => {
  it("prints 100% only for a genuinely complete window", () => {
    expect(formatRatio(1)).toBe("100%");
    // An estimate that lost a fraction of a percent must never read as complete.
    expect(formatRatio(0.9999)).toBe("99.9%");
    expect(formatRatio(0.925)).toBe("92.5%");
    expect(formatRatio(0)).toBe("0%");
    expect(formatRatio(Number.NaN)).toBe("n/a");
  });
});

describe("formatDurationMs", () => {
  it("keeps the two largest units so a cell stays readable", () => {
    expect(formatDurationMs(7_200_000)).toBe("2 hours");
    expect(formatDurationMs(90_000)).toBe("1 minute 30 seconds");
    expect(formatDurationMs(500)).toBe("<1 second");
    expect(formatDurationMs(0)).toBe("0 seconds");
  });
});

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <AdminApiProvider token="">{children}</AdminApiProvider>
    </QueryClientProvider>
  );
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

const scope = { start: "2026-07-25T00:00:00Z", end: "2026-07-26T00:00:00Z" };

const usage: ServiceUsageResponse = {
  start: scope.start,
  end: scope.end,
  group: "service",
  rows: [
    { key: "youtube", label: "YouTube", category: "media", connections: 400, hosts: 12 },
    { key: "github", label: "GitHub", category: "development", connections: 100, hosts: 1 }
  ],
  other: { key: "other", label: "Other", category: "", connections: 20, hosts: 4 },
  total_connections: 520,
  total_hosts: 17,
  truncated: false,
  catalog_version: "2026-07-01"
};

const hosts: NetworkEventHostsResponse = {
  hosts: [
    {
      host: "i.ytimg.com",
      service: "youtube",
      service_label: "YouTube",
      category: "media",
      source: "catalog",
      connections: 240,
      last_seen: "2026-07-25T02:40:00Z"
    }
  ],
  total: 12,
  limit: 50,
  offset: 0,
  truncated: false
};

describe("ActivityPanel", () => {
  it("asks for a bounded range instead of charting all time", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    render(wrapper(
      <ActivityPanel
        scope={{}}
        scopeKey={{ case: "unbounded" }}
        bucket="day"
        hourAllowed={false}
        onBucketChange={() => {}}
        onTimeRangeChange={() => {}}
      />
    ));
    expect(screen.getByText("Activity needs a bounded time range")).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("renders a failed series query as an error, never as an empty chart", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("series unavailable", { status: 422 })));
    render(wrapper(
      <ActivityPanel
        scope={scope}
        scopeKey={{ case: "error" }}
        bucket="hour"
        hourAllowed
        onBucketChange={() => {}}
        onTimeRangeChange={() => {}}
      />
    ));
    await waitFor(() => expect(screen.getByText("series unavailable")).toBeTruthy());
  });

  it("disables hourly when the range is too wide for it", () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 500 })));
    render(wrapper(
      <ActivityPanel
        scope={scope}
        scopeKey={{ case: "wide" }}
        bucket="day"
        hourAllowed={false}
        onBucketChange={() => {}}
        onTimeRangeChange={() => {}}
      />
    ));
    expect(screen.getByRole("button", { name: "Hourly" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Daily" }).getAttribute("aria-pressed")).toBe("true");
  });
});

describe("ServiceAuditPanel", () => {
  it("ranks services by connections and reports what fell outside the top rows", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(usage)));
    render(wrapper(
      <ServiceAuditPanel
        scope={scope}
        scopeKey={{ case: "rows" }}
        breakdown="service"
        onBreakdownChange={() => {}}
        service=""
        onServiceChange={() => {}}
      />
    ));
    await waitFor(() => expect(screen.getByRole("button", { name: /YouTube/ })).toBeTruthy());
    const items = screen.getAllByRole("listitem").map((item) => item.textContent);
    expect(items).toEqual(["YouTube12 hosts40077%", "GitHub1 host10019%"]);
    expect(screen.getByText(/520 connections across 17 hosts/)).toBeTruthy();
    expect(screen.getByText(/20 fall outside the top 10/)).toBeTruthy();
  });

  it("drills into a service and lists its hosts with connection counts", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: string) =>
      Promise.resolve(jsonResponse(input.includes("/hosts") ? hosts : usage))
    ));
    render(wrapper(
      <ServiceAuditPanel
        scope={scope}
        scopeKey={{ case: "hosts" }}
        breakdown="service"
        onBreakdownChange={() => {}}
        service="youtube"
        onServiceChange={() => {}}
      />
    ));
    await waitFor(() => expect(screen.getByText("i.ytimg.com")).toBeTruthy());
    expect(screen.getByRole("heading", { name: "Hosts in YouTube" })).toBeTruthy();
    expect(screen.getByText("240")).toBeTruthy();
    expect(screen.getByText("Showing 1 of 12 hosts.")).toBeTruthy();
  });

  it("renders a failed breakdown as an error, never as an empty ranking", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("services unavailable", { status: 422 })));
    render(wrapper(
      <ServiceAuditPanel
        scope={scope}
        scopeKey={{ case: "failed" }}
        breakdown="service"
        onBreakdownChange={() => {}}
        service=""
        onServiceChange={() => {}}
      />
    ));
    await waitFor(() => expect(screen.getByText("services unavailable")).toBeTruthy());
    expect(screen.queryByText("No classified connections in this range")).toBeNull();
  });
});

const telemetryNodes: ConnectionTelemetryNodesResponse = {
  nodes: [{ node_name: "tokyo", listen_address: "127.0.0.1", listen_port: 9091 }]
};

const connectionHosts: ConnectionHostsResponse = {
  sort: "bytes",
  hosts: [
    {
      host: "www.youtube.com",
      connections_opened: 900,
      connections_closed: 880,
      uplink_bytes: 805_306_368,
      downlink_bytes: 2_415_919_104,
      total_bytes: 3_221_225_472,
      duration_ms_total: 5_400_000
    },
    {
      host: "speed.cloudflare.com",
      connections_opened: 304,
      connections_closed: 300,
      uplink_bytes: 268_435_456,
      downlink_bytes: 805_306_368,
      total_bytes: 1_073_741_824,
      duration_ms_total: 1_800_000
    }
  ],
  totals: {
    connections_opened: 1204,
    connections_closed: 1180,
    uplink_bytes: 1_073_741_824,
    downlink_bytes: 3_221_225_472,
    total_bytes: 4_294_967_296,
    duration_ms_total: 7_200_000
  },
  distinct_hosts: 7,
  limit: 2,
  truncated: false,
  coverage: {
    connections_observed: 1204,
    connections_attributed: 900,
    connections_unattributed: 304,
    connections_orphaned: 12,
    stream_resets: 2,
    dropped_buckets: 0,
    bytes_observed: 5_368_709_120,
    bytes_attributed: 4_966_055_018,
    attribution_ratio: 0.925,
    reports: 12
  }
};

const connectionPoint = (bucketStart: string, totalBytes: number) => ({
  bucket_start: bucketStart,
  connections_opened: 10,
  connections_closed: 10,
  uplink_bytes: Math.round(totalBytes / 4),
  downlink_bytes: totalBytes - Math.round(totalBytes / 4),
  total_bytes: totalBytes,
  duration_ms_total: 60_000
});

const connectionSeries: ConnectionSeriesResponse = {
  bucket: "hour",
  offset_minutes: 0,
  start: scope.start,
  end: scope.end,
  points: [
    connectionPoint("2026-07-25T00:00:00Z", 1_000),
    connectionPoint("2026-07-25T01:00:00Z", 3_000),
    connectionPoint("2026-07-25T02:00:00Z", 2_000)
  ],
  totals: connectionHosts.totals,
  coverage: connectionHosts.coverage
};

const connectionRows: ConnectionEventsResponse = {
  events: [
    {
      node_name: "tokyo",
      // Single-user Shadowsocks never populates `user`; the row is kept anyway.
      user_name: "",
      auth_name: "",
      source_ip: "100.64.2.5",
      target_host: "speed.cloudflare.com",
      target_port: 443,
      domain: "",
      network: "tcp",
      ip_version: 4,
      protocol: "tcp",
      inbound: "ss-8388",
      inbound_type: "shadowsocks",
      rule: "final",
      outbound: "direct",
      outbound_type: "direct",
      chain: "ss-8388>direct",
      connections_opened: 6,
      connections_closed: 6,
      uplink_bytes: 268_435_456,
      downlink_bytes: 805_306_368,
      duration_ms_total: 7_200_000,
      bucket_start: "2026-07-25T02:00:00Z",
      window_start: "2026-07-25T02:00:00Z",
      window_end: "2026-07-25T02:05:00Z"
    }
  ],
  total: 34,
  limit: 25,
  offset: 0
};

/** Routes by path so a test can fail one endpoint without faking the others. */
function connectionApi(overrides: Partial<Record<"nodes" | "hosts" | "series" | "events", () => Response>> = {}) {
  const defaults = {
    nodes: () => jsonResponse(telemetryNodes),
    hosts: () => jsonResponse(connectionHosts),
    series: () => jsonResponse(connectionSeries),
    events: () => jsonResponse(connectionRows)
  };
  return vi.fn().mockImplementation((input: string) => {
    for (const key of ["nodes", "hosts", "series"] as const) {
      if (input.includes(`/connection-events/${key}`)) {
        return Promise.resolve((overrides[key] ?? defaults[key])());
      }
    }
    if (input.includes("/connection-events")) return Promise.resolve((overrides.events ?? defaults.events)());
    return Promise.reject(new Error(`unexpected request: ${input}`));
  });
}

function renderConnectionPanel(props: {
  key: string;
  nodeFilter?: string;
  host?: string;
  sort?: "bytes" | "connections";
  ignoredFilters?: string[];
  onHostChange?: (host: string) => void;
}) {
  return render(wrapper(
    <ConnectionTelemetryPanel
      scope={scope}
      scopeKey={{ case: props.key }}
      bucket="hour"
      fleetNodeCount={5}
      nodeFilter={props.nodeFilter ?? ""}
      ignoredFilters={props.ignoredFilters ?? []}
      host={props.host ?? ""}
      onHostChange={props.onHostChange ?? (() => {})}
      sort={props.sort ?? "bytes"}
      onSortChange={() => {}}
    />
  ));
}

describe("ConnectionTelemetryPanel", () => {
  it("explains itself instead of querying anything when no node is opted in", async () => {
    const fetchMock = connectionApi({ nodes: () => jsonResponse({ nodes: [] }) });
    vi.stubGlobal("fetch", fetchMock);
    renderConnectionPanel({ key: "none" });

    await waitFor(() => expect(screen.getByText("No node streams connection telemetry")).toBeTruthy());
    expect(screen.getByText(/opt-in per node and is currently switched off everywhere/)).toBeTruthy();
    // No ranking, no table, no empty state an operator could read as a bug.
    expect(screen.queryByText("No connections recorded in this range")).toBeNull();
    // Only /nodes was asked; the byte endpoints never fire for a dark fleet.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("names the streaming nodes when the page is filtered to one that is not", async () => {
    const fetchMock = connectionApi();
    vi.stubGlobal("fetch", fetchMock);
    renderConnectionPanel({ key: "other-node", nodeFilter: "paris" });

    await waitFor(() => expect(screen.getByText("paris does not stream connection telemetry")).toBeTruthy());
    expect(screen.getByText(/Only tokyo streams it today/)).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("renders a failed node lookup as an error, never as the disabled explanation", async () => {
    vi.stubGlobal("fetch", connectionApi({ nodes: () => new Response("nodes unavailable", { status: 422 }) }));
    renderConnectionPanel({ key: "nodes-error" });

    await waitFor(() => expect(screen.getByText("nodes unavailable")).toBeTruthy());
    expect(screen.queryByText("No node streams connection telemetry")).toBeNull();
  });

  it("states the partial node coverage and pins the byte total to its coverage figure", async () => {
    vi.stubGlobal("fetch", connectionApi());
    renderConnectionPanel({ key: "ranking" });

    await waitFor(() => expect(screen.getByText("4.0 GB")).toBeTruthy());
    // Unmistakably not fleet-wide.
    expect(screen.getByText(/Estimated bytes per destination, from 1 of 5 nodes \(tokyo\)/)).toBeTruthy();
    expect(screen.getByText(/Every other section on this page covers the whole fleet/)).toBeTruthy();
    // The coverage figure travels with the estimate, and never reads as billing.
    expect(screen.getByText(/Estimate, not a ledger: 92\.5% of 5\.0 GB observed bytes carried a user, across 12 report windows/)).toBeTruthy();
    expect(screen.getByText(/Per-user billing reads the traffic counters, never this/)).toBeTruthy();
    expect(screen.getByText(/Coverage measures the whole stream on tokyo for this range/)).toBeTruthy();
    expect(screen.getByText("2 stream resets")).toBeTruthy();

    const rows = screen.getAllByRole("listitem").map((item) => item.textContent);
    expect(rows).toEqual(["www.youtube.com900 conn3.0 GB75%", "speed.cloudflare.com304 conn1.0 GB25%"]);
    expect(screen.getByText(/Top 2 of 7 destinations, ranked by estimated bytes/)).toBeTruthy();
    // The sparkline reads the server's buckets verbatim.
    expect(screen.getByRole("img", { name: "Estimated bytes per hour, oldest bucket first" })).toBeTruthy();
  });

  it("drills into one destination with the columns log events cannot answer", async () => {
    vi.stubGlobal("fetch", connectionApi());
    renderConnectionPanel({ key: "drill", host: "speed.cloudflare.com" });

    await waitFor(() => expect(screen.getByText("ss-8388>direct")).toBeTruthy());
    expect(screen.getByRole("heading", { name: "Connections to speed.cloudflare.com" })).toBeTruthy();
    expect(screen.getByText("Unattributed")).toBeTruthy();
    expect(screen.getByText("2 hours")).toBeTruthy();
    expect(screen.getByText(/Showing 1 of 34 aggregated rows/)).toBeTruthy();
  });

  it("renders a failed host ranking as an error, never as a zeroed estimate", async () => {
    vi.stubGlobal("fetch", connectionApi({ hosts: () => new Response("hosts unavailable", { status: 422 }) }));
    renderConnectionPanel({ key: "hosts-error" });

    await waitFor(() => expect(screen.getByText("hosts unavailable")).toBeTruthy());
    expect(screen.queryByText("0 B")).toBeNull();
    expect(screen.queryByText("No connections recorded in this range")).toBeNull();
  });

  it("names the filters this source cannot honour instead of silently ignoring them", async () => {
    vi.stubGlobal("fetch", connectionApi());
    renderConnectionPanel({ key: "ignored", ignoredFilters: ["search", "action"] });

    await waitFor(() => expect(screen.getByText(/The search and action filters above do not narrow this section/)).toBeTruthy());
  });
});
