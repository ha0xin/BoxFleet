// @vitest-environment jsdom

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import type { NetworkEventHostsResponse, ServiceUsageResponse } from "../types";
import {
  ActivityPanel,
  ServiceAuditPanel,
  bucketOffsetMinutes,
  resolveSeriesBucket,
  seriesSpanMillis
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
