// @vitest-environment jsdom

import type { ReactNode } from "react";
import { Sidebar } from "@cloudflare/kumo";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { PublishStatusProvider } from "@/publish/publish-status";
import type { AdminNode, AdminNodesResponse, AdminRelease } from "../types";
import { NodesPage, nodeQueryParams, nodeUpdateStatus } from "./nodes";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const baseFilters = { search: "", status: "all", sort: "name", direction: "asc" } as const;

describe("nodeQueryParams", () => {
  it("omits the status parameter for the unfiltered facet", () => {
    expect(nodeQueryParams(baseFilters, 10, 0)).toEqual({
      limit: 10,
      offset: 0,
      search: "",
      status: undefined,
      deleted: undefined,
      sort: "name",
      direction: "asc"
    });
  });

  it("forwards a real node status verbatim", () => {
    const params = nodeQueryParams({ ...baseFilters, status: "degraded" }, 25, 50);
    expect(params.status).toBe("degraded");
    expect(params.deleted).toBeUndefined();
    expect(params).toMatchObject({ limit: 25, offset: 50 });
  });

  // No node ever has status "deleted"; sending it would filter every row out.
  it("translates the deleted facet into the server's own parameter", () => {
    const params = nodeQueryParams({ ...baseFilters, status: "deleted" }, 10, 0);
    expect(params.deleted).toBe("true");
    expect(params.status).toBeUndefined();
  });

  it("passes search and sort through unchanged", () => {
    const params = nodeQueryParams(
      { search: "edge-1", status: "active", sort: "last_seen_at", direction: "desc" },
      50,
      100
    );
    expect(params).toEqual({
      limit: 50,
      offset: 100,
      search: "edge-1",
      status: "active",
      deleted: undefined,
      sort: "last_seen_at",
      direction: "desc"
    });
  });
});

const release: AdminRelease = {
  repo: "boxfleet/boxfleet",
  boxfleet_version: "v1.4.0",
  agent_version: "v1.4.0",
  sing_box_version: "v1.13.14",
  updates_enabled: true
};

function node(overrides: Partial<AdminNode> = {}): AdminNode {
  return {
    id: "node-1",
    name: "edge-1",
    public_host: "edge-1.example.com",
    api_base_url: "",
    status: "active",
    sing_box_version: "v1.13.14",
    last_seen_at: new Date().toISOString(),
    agent_version: "v1.4.0",
    capabilities: ["operations.v1", "update.agent.v1", "update.sing_box.v1"],
    has_active_token: true,
    deleted_at: "",
    ...overrides
  };
}

describe("nodeUpdateStatus", () => {
  it("reports up to date when both components match the release", () => {
    expect(nodeUpdateStatus(node(), release)).toMatchObject({ label: "Up to date", available: false });
  });

  it("offers only the components the node can actually update", () => {
    const stale = node({ agent_version: "v1.3.0", capabilities: ["operations.v1", "update.agent.v1"] });
    expect(nodeUpdateStatus(stale, release)).toMatchObject({
      label: "Update available",
      available: true,
      canUpdateAgent: true,
      canUpdateSingBox: false
    });
  });

  it("demands a manual upgrade when the agent predates the operations protocol", () => {
    const legacy = node({ agent_version: "v1.0.0", capabilities: [] });
    expect(nodeUpdateStatus(legacy, release)).toMatchObject({
      label: "Manual agent upgrade required",
      available: false
    });
  });

  it("distinguishes a queued operation on an offline node from a running one", () => {
    const offline = node({
      last_seen_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(),
      active_operation: {
        id: "op-1",
        node_id: "node-1",
        kind: "update",
        status: "queued",
        phase: "queued",
        payload: {},
        result: {},
        idempotency_key: "k",
        required_capabilities: [],
        attempt: 0,
        cancel_requested: false,
        requested_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      }
    });
    expect(nodeUpdateStatus(offline, release).label).toBe("Queued — waiting for node");
  });

  it("reports unavailable when updates are disabled or the token was revoked", () => {
    expect(nodeUpdateStatus(node(), { ...release, updates_enabled: false }).label).toBe("Unavailable");
    expect(nodeUpdateStatus(node({ has_active_token: false }), release).label).toBe("Unavailable");
  });
});

function json(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

/**
 * Stubs the three requests the page issues and records the node-list URLs, which
 * is what proves the query string — not component state — drives the request.
 */
function stubFetch(nodesReply: () => Response) {
  const nodeUrls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const [path] = url.split("?");
      if (path === "/api/admin/nodes") {
        nodeUrls.push(url);
        return nodesReply();
      }
      if (path === "/api/admin/release") return json(release);
      if (path === "/api/admin/node-update-campaigns/current") return json(null);
      return json({});
    })
  );
  return nodeUrls;
}

function renderPage(initialEntry: string) {
  // jsdom ships no matchMedia, and Kumo's Sidebar.Provider reads it on mount.
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view: ReactNode = (
    <MemoryRouter initialEntries={[initialEntry]}>
      <QueryClientProvider client={client}>
        <AdminApiProvider token="">
          {/* AppPageHeader carries the sidebar trigger and the publish affordance. */}
          <Sidebar.Provider>
            <PublishStatusProvider>
              <NodesPage />
            </PublishStatusProvider>
          </Sidebar.Provider>
        </AdminApiProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
  return render(view);
}

function nodesPage(overrides: Partial<AdminNodesResponse> = {}): AdminNodesResponse {
  return { nodes: [], total: 0, limit: 10, offset: 0, ...overrides };
}

describe("NodesPage", () => {
  // A failed query rendering as "no nodes" has already shipped as a bug here.
  it("renders a failed node query as an error, never as an empty table", async () => {
    stubFetch(() => new Response("database is locked", { status: 500 }));
    renderPage("/nodes");

    await waitFor(() => {
      expect(screen.getByText("database is locked")).toBeTruthy();
    });
    expect(screen.queryByText("No nodes match this filter.")).toBeNull();
  });

  it("builds the request from the query string alone, so a link is refresh-safe", async () => {
    const urls = stubFetch(() => json(nodesPage({ total: 120, limit: 25, offset: 25 })));
    renderPage("/nodes?status=deleted&sort=last_seen_at&direction=desc&search=edge&page=2&limit=25");

    await waitFor(() => {
      expect(urls.length).toBeGreaterThan(0);
    });
    const params = new URL(urls[0], "http://localhost").searchParams;
    expect(params.get("limit")).toBe("25");
    expect(params.get("offset")).toBe("25");
    expect(params.get("search")).toBe("edge");
    expect(params.get("sort")).toBe("last_seen_at");
    expect(params.get("direction")).toBe("desc");
    expect(params.get("deleted")).toBe("true");
    expect(params.has("status")).toBe(false);
  });

  it("restores an unparseable facet to its default instead of discarding the URL", async () => {
    const urls = stubFetch(() => json(nodesPage({ total: 3 })));
    renderPage("/nodes?status=nonsense&search=edge");

    await waitFor(() => {
      expect(urls.length).toBeGreaterThan(0);
    });
    const params = new URL(urls[0], "http://localhost").searchParams;
    expect(params.has("status")).toBe(false);
    expect(params.has("deleted")).toBe(false);
    expect(params.get("search")).toBe("edge");
  });

  // The clamp waits for a response: a fresh deep link must not be rewritten to
  // page 1 by the total of a request that has not landed yet.
  it("clamps a page that outlives its rows and refetches from the last page", async () => {
    const urls = stubFetch(() => json(nodesPage({ total: 5, limit: 10, offset: 0 })));
    renderPage("/nodes?page=3");

    await waitFor(() => {
      expect(urls.length).toBe(2);
    });
    expect(new URL(urls[0], "http://localhost").searchParams.get("offset")).toBe("20");
    expect(new URL(urls[1], "http://localhost").searchParams.get("offset")).toBe("0");
  });
});
