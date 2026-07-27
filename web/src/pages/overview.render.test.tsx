// @vitest-environment jsdom

import { Sidebar } from "@cloudflare/kumo";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { PublishStatusProvider } from "@/publish/publish-status";
import type { Overview } from "../types";
import { OverviewPage } from "./overview";

const overview: Overview = {
  nodes: [
    {
      id: "node-1",
      name: "edge-1",
      public_host: "edge-1.example.com",
      api_base_url: "",
      status: "active",
      sing_box_version: "sing-box version 1.13.13",
      last_seen_at: "2026-07-26T09:00:00Z",
      current_version: "7",
      target_version: "7",
      deleted_at: ""
    },
    {
      id: "node-2",
      name: "edge-2",
      public_host: "edge-2.example.com",
      api_base_url: "",
      status: "active",
      sing_box_version: "sing-box version 1.13.13",
      last_seen_at: "2026-07-26T09:00:00Z",
      current_version: "7",
      target_version: "7",
      deleted_at: ""
    }
  ],
  users: [],
  traffic: [],
  system_logs: [],
  system_log_note: "",
  release: { repo: "", boxfleet_version: "1.0.0", agent_version: "1.0.0", sing_box_version: "1.13.13" }
};

function volume(uplink: number, downlink: number) {
  return {
    uplink_raw_bytes: uplink,
    uplink_billable_bytes: uplink,
    downlink_raw_bytes: downlink,
    downlink_billable_bytes: downlink
  };
}

function trafficPoints(values: Array<[number, number]>) {
  return values.map(([uplink, downlink], index) => ({
    bucket_start: `2026-07-2${index}T00:00:00Z`,
    ...volume(uplink, downlink)
  }));
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
}

function stubSeriesFetch() {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/admin/traffic/series") && url.includes("group=node")) {
      return jsonResponse({
        bucket: "day",
        offset_minutes: 0,
        start: "2026-07-20T00:00:00Z",
        end: "2026-07-22T00:00:00Z",
        group: "node",
        series: [
          {
            key: "edge-1",
            label: "edge-1",
            points: trafficPoints([[1, 2], [3, 4], [5, 6]]),
            totals: volume(9, 12)
          }
        ],
        truncated: false
      });
    }
    if (url.includes("/api/admin/traffic/series")) {
      return jsonResponse({
        bucket: "day",
        offset_minutes: 0,
        start: "2026-07-20T00:00:00Z",
        end: "2026-07-22T00:00:00Z",
        group: "total",
        series: [
          {
            key: "total",
            label: "All traffic",
            points: trafficPoints([[100, 900], [0, 0], [24, 0]]),
            totals: volume(124, 900)
          }
        ],
        truncated: false
      });
    }
    if (url.includes("/api/admin/network-events/series")) {
      return jsonResponse({
        bucket: "day",
        offset_minutes: 0,
        start: "2026-07-20T00:00:00Z",
        end: "2026-07-22T00:00:00Z",
        group: "total",
        series: [
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
        ],
        actions: [{ action: "connect", count: 12 }],
        truncated: false
      });
    }
    throw new Error(`unexpected request: ${url}`);
  });
}

function renderOverview() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <AdminApiProvider token="devtoken">
          <PublishStatusProvider>
            {/* The page header renders Kumo's mobile sidebar trigger. */}
            <Sidebar.Provider>
              <OverviewPage overview={overview} />
            </Sidebar.Provider>
          </PublishStatusProvider>
        </AdminApiProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

beforeEach(() => {
  // jsdom ships no matchMedia; Kumo's sidebar reads it during render.
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false
  }));
});

afterEach(() => {
  // Vitest runs without globals, so Testing Library's auto-cleanup never registers.
  cleanup();
  vi.unstubAllGlobals();
});

describe("OverviewPage", () => {
  it("renders trend lines from the series endpoints", async () => {
    vi.stubGlobal("fetch", stubSeriesFetch());
    renderOverview();

    await waitFor(() => {
      expect(screen.getByRole("img", { name: "Billable traffic, last 7 days" })).toBeTruthy();
    });
    expect(screen.getByRole("img", { name: "Network events, last 7 days" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "Billable traffic for edge-1, last 7 days" })).toBeTruthy();
    // edge-2 returned no series, so its row carries no invented trend.
    expect(screen.queryByRole("img", { name: "Billable traffic for edge-2, last 7 days" })).toBeNull();
    expect(screen.getByText("12")).toBeTruthy();
  });

  it("passes the query signal to the polling series requests", async () => {
    const fetchMock = stubSeriesFetch();
    vi.stubGlobal("fetch", fetchMock);
    renderOverview();

    // Both traffic groups plus the network-event series.
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(3));
    // The stub only declares the `input` argument, so reach past its arity.
    const inits = fetchMock.mock.calls.map((call) => (call as unknown as [unknown, RequestInit?])[1]);
    expect(inits.every((init) => init?.signal instanceof AbortSignal)).toBe(true);
  });

  it("renders without trend lines when the series endpoints fail", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("boom", { status: 500 })));
    renderOverview();

    await waitFor(() => {
      expect(screen.getByText("Network events")).toBeTruthy();
    });
    expect(screen.queryAllByRole("img")).toEqual([]);
    expect(screen.getByText("—")).toBeTruthy();
  });
});
