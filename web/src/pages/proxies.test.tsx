// @vitest-environment jsdom

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import type { AdminProxiesResponse, AdminProxy } from "../types";
import { ProxiesPage, endpoint, multiplier, proxyPageParams } from "./proxies";

// The shared header pulls in the sidebar and publish-status contexts, neither of
// which says anything about proxy paging. Stub it down to the slots this page
// fills so the test exercises the table and its URL state, not app chrome.
vi.mock("@/components/app-page-header", () => ({
  AppPageHeader: ({ title, actions }: { title: string; actions?: ReactNode }) => (
    <header>
      <h1>{title}</h1>
      {actions}
    </header>
  )
}));

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const baseFilters = { search: "", status: "all", sort: "node_name", direction: "asc" } as const;

function proxy(overrides: Partial<AdminProxy> = {}): AdminProxy {
  return {
    id: "px_1",
    node_name: "lhr-1",
    name: "reality-443",
    protocol: "vless_reality",
    listen: "::",
    listen_port: 443,
    transport: "tcp",
    enabled: true,
    traffic_multiplier: 1,
    direct_publish: true,
    short_id: "",
    settings_json: "{}",
    inbound_rules_json: "[]",
    outbound_rules_json: "[]",
    route_rules_json: "[]",
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-20T00:00:00Z",
    deleted_at: "",
    ...overrides
  };
}

function listing(body: Partial<AdminProxiesResponse>): AdminProxiesResponse {
  return { proxies: [], total: 0, limit: 10, offset: 0, ...body };
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" }
  });
}

/** Renders the page at `search`, exposing the router's URL for assertions. */
function renderPage(search: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [{ path: "/proxies", element: <ProxiesPage /> }],
    { initialEntries: [`/proxies${search}`] }
  );
  render(
    <QueryClientProvider client={client}>
      <AdminApiProvider token="">
        <RouterProvider router={router} />
      </AdminApiProvider>
    </QueryClientProvider>
  );
  return { search: () => router.state.location.search };
}

/** Query params of the most recent listing request. */
function lastRequest(fetchMock: ReturnType<typeof vi.fn>): URLSearchParams {
  const calls = fetchMock.mock.calls
    .map((call) => String(call[0]))
    .filter((url) => url.startsWith("/api/admin/proxies?"));
  const last = calls[calls.length - 1];
  if (last === undefined) throw new Error("no proxy listing request was made");
  return new URLSearchParams(last.slice(last.indexOf("?")));
}

describe("proxyPageParams", () => {
  it("splits the single status facet into the server's enabled and deleted filters", () => {
    expect(proxyPageParams({ ...baseFilters, status: "enabled" }, 10, 0).enabled).toBe("true");
    expect(proxyPageParams({ ...baseFilters, status: "disabled" }, 10, 0).enabled).toBe("false");
    // "Deleted" swaps the base set rather than narrowing the live one, so it
    // must not also pin `enabled` — a disabled proxy is still restorable.
    expect(proxyPageParams({ ...baseFilters, status: "deleted" }, 10, 0)).toMatchObject({
      enabled: undefined,
      deleted: "true"
    });
    expect(proxyPageParams(baseFilters, 10, 0)).toMatchObject({ enabled: undefined, deleted: undefined });
  });

  it("passes paging and sort through unchanged", () => {
    expect(proxyPageParams({ ...baseFilters, sort: "updated_at", direction: "desc" }, 25, 50)).toMatchObject({
      limit: 25,
      offset: 50,
      sort: "updated_at",
      direction: "desc"
    });
  });
});

describe("endpoint", () => {
  it("renders a wildcard listen address as *", () => {
    expect(endpoint(proxy({ listen: "::" }))).toBe("*:443");
    expect(endpoint(proxy({ listen: "0.0.0.0" }))).toBe("*:443");
    expect(endpoint(proxy({ listen: "10.0.0.5", listen_port: 8443 }))).toBe("10.0.0.5:8443");
  });
});

describe("multiplier", () => {
  it("drops the decimal for whole multipliers", () => {
    expect(multiplier(1)).toBe("1x");
    expect(multiplier(2.5)).toBe("2.5x");
  });
});

describe("ProxiesPage", () => {
  it("restores filters, sort and pagination from the URL", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(listing({ proxies: [proxy()], total: 400 })));
    vi.stubGlobal("fetch", fetchMock);

    renderPage("?status=disabled&search=lhr&sort=updated_at&direction=desc&page=3&limit=25");

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const params = lastRequest(fetchMock);
    expect(params.get("search")).toBe("lhr");
    expect(params.get("enabled")).toBe("false");
    expect(params.get("sort")).toBe("updated_at");
    expect(params.get("direction")).toBe("desc");
    expect(params.get("limit")).toBe("25");
    expect(params.get("offset")).toBe("50");
    // The search box shows the committed filter, not an empty draft.
    expect(screen.getByLabelText<HTMLInputElement>("Search proxies").value).toBe("lhr");
  });

  it("falls back to defaults for a hand-edited status instead of dropping the rest", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(listing({ proxies: [proxy()], total: 1 })));
    vi.stubGlobal("fetch", fetchMock);

    renderPage("?status=nonsense&search=lhr");

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const params = lastRequest(fetchMock);
    expect(params.get("enabled")).toBeNull();
    expect(params.get("deleted")).toBeNull();
    expect(params.get("search")).toBe("lhr");
  });

  it("writes a sort toggle to the URL and re-requests", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(listing({ proxies: [proxy()], total: 1 })));
    vi.stubGlobal("fetch", fetchMock);

    const view = renderPage("?page=2");
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: "Updated" }));
    await waitFor(() => expect(lastRequest(fetchMock).get("sort")).toBe("updated_at"));

    const params = new URLSearchParams(view.search());
    expect(params.get("sort")).toBe("updated_at");
    // "Updated" reads newest-first on selection, and a filter change returns to page 1.
    expect(params.get("direction")).toBe("desc");
    expect(params.get("page")).toBeNull();

    // Clicking the active column flips direction rather than re-selecting it.
    // "asc" is the default, so it leaves the URL clean while still being sent.
    fireEvent.click(screen.getByRole("button", { name: "Updated" }));
    await waitFor(() => expect(lastRequest(fetchMock).get("direction")).toBe("asc"));
    expect(new URLSearchParams(view.search()).get("direction")).toBeNull();
    expect(new URLSearchParams(view.search()).get("sort")).toBe("updated_at");
  });

  it("commits the search box to the URL on submit", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(listing({ proxies: [proxy()], total: 1 })));
    vi.stubGlobal("fetch", fetchMock);

    const view = renderPage("");
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const input = screen.getByLabelText<HTMLInputElement>("Search proxies");
    fireEvent.change(input, { target: { value: "  reality  " } });
    // Typing alone must not navigate; only the submit commits.
    expect(view.search()).toBe("");

    fireEvent.submit(input.closest("form") as HTMLFormElement);
    await waitFor(() => expect(lastRequest(fetchMock).get("search")).toBe("reality"));
    expect(new URLSearchParams(view.search()).get("search")).toBe("reality");
  });

  it("clamps a page that outlives its rows instead of showing an empty table", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(listing({ proxies: [proxy()], total: 12 })));
    vi.stubGlobal("fetch", fetchMock);

    const view = renderPage("?page=9");

    // 12 rows at 10 per page is 2 pages, so page 9 collapses to page 2.
    await waitFor(() => expect(new URLSearchParams(view.search()).get("page")).toBe("2"));
    await waitFor(() => expect(lastRequest(fetchMock).get("offset")).toBe("10"));
  });

  it("renders a failed query as an error, never as an empty inventory", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("proxy listing unavailable", { status: 422 })));

    renderPage("");

    await waitFor(() => expect(screen.getByText("proxy listing unavailable")).toBeTruthy());
    expect(screen.queryByText("No proxies match this filter.")).toBeNull();
  });

  it("shows the empty state only when the server really returns no rows", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(listing({}))));

    renderPage("");

    await waitFor(() => expect(screen.getByText("No proxies match this filter.")).toBeTruthy());
  });
});
