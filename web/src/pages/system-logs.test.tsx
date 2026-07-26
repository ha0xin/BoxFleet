// @vitest-environment jsdom

import { Sidebar } from "@cloudflare/kumo";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { PublishStatusProvider } from "@/publish/publish-status";
import type { SystemLog, SystemLogsResponse } from "../types";
import { SystemLogsPage, choiceList } from "./system-logs";

const entry: SystemLog = {
  node: "edge-1",
  service: "boxfleet-agent",
  level: "err",
  message: "config apply failed: address already in use",
  observed_at: "2026-07-26T09:00:00Z",
  ingested_at: "2026-07-26T09:00:02Z"
};

function logsResponse(overrides: Partial<SystemLogsResponse> = {}): SystemLogsResponse {
  return {
    logs: [entry],
    services: ["boxfleet-agent", "sing-box"],
    total: 1,
    limit: 25,
    offset: 0,
    note: "",
    ...overrides
  };
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
}

/** Records every request URL so assertions can prove the query reached the server. */
function stubFetch(logs: () => Response | Promise<Response>) {
  const urls: string[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    urls.push(url);
    if (url.startsWith("/api/admin/system-logs")) return logs();
    if (url.startsWith("/api/admin/nodes")) return jsonResponse([{ name: "edge-1" }, { name: "edge-2" }]);
    throw new Error(`unexpected request: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return {
    urls,
    logRequests: () => urls.filter((url) => url.startsWith("/api/admin/system-logs"))
  };
}

function renderPage(search = "") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter(
    [
      {
        path: "/system-logs",
        element: (
          <QueryClientProvider client={queryClient}>
            <AdminApiProvider token="devtoken">
              <PublishStatusProvider>
                {/* The page header renders Kumo's mobile sidebar trigger. */}
                <Sidebar.Provider>
                  <SystemLogsPage />
                </Sidebar.Provider>
              </PublishStatusProvider>
            </AdminApiProvider>
          </QueryClientProvider>
        )
      }
    ],
    { initialEntries: [`/system-logs${search}`] }
  );
  render(<RouterProvider router={router} />);
  return { search: () => router.state.location.search };
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

describe("choiceList", () => {
  it("prefixes the server's options with the all sentinel", () => {
    expect(choiceList(["sing-box", "boxfleet-agent"], "all")).toEqual(["all", "boxfleet-agent", "sing-box"]);
  });

  it("keeps an active value that the server no longer lists", () => {
    expect(choiceList(["sing-box"], "retired-agent")).toEqual(["all", "retired-agent", "sing-box"]);
  });

  it("survives an options list that has not loaded yet", () => {
    expect(choiceList(undefined, "all")).toEqual(["all"]);
    expect(choiceList(undefined, "edge-9")).toEqual(["all", "edge-9"]);
  });
});

describe("SystemLogsPage", () => {
  it("asks the server for the page, filters and sort named in the URL", async () => {
    const fetches = stubFetch(() => jsonResponse(logsResponse({ total: 60, offset: 25 })));
    renderPage("?search=boom&level=error&node=edge-1&service=sing-box&sort=node&direction=asc&page=2");

    await waitFor(() => expect(fetches.logRequests().length).toBeGreaterThan(0));
    const url = new URL(fetches.logRequests()[0], "http://admin.test");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      limit: "25",
      offset: "25",
      search: "boom",
      level: "error",
      node: "edge-1",
      service: "sing-box",
      sort: "node",
      direction: "asc"
    });
    // Paging is the server's: one page of rows, the full count in the footer.
    await waitFor(() => expect(screen.getByText("Showing 26-50 of 60")).toBeTruthy());
  });

  it("omits the all sentinel, which the server has no value for", async () => {
    const fetches = stubFetch(() => jsonResponse(logsResponse()));
    renderPage();

    await waitFor(() => expect(fetches.logRequests().length).toBeGreaterThan(0));
    const params = new URL(fetches.logRequests()[0], "http://admin.test").searchParams;
    expect(params.get("level")).toBeNull();
    expect(params.get("node")).toBeNull();
    expect(params.get("service")).toBeNull();
    expect(params.get("search")).toBeNull();
    expect(params.get("sort")).toBe("observed_at");
    expect(params.get("direction")).toBe("desc");
  });

  it("renders a failed query as an error, never as an empty archive", async () => {
    stubFetch(() => new Response("system logs unavailable", { status: 500 }));
    renderPage();

    await waitFor(() => expect(screen.getByText("system logs unavailable")).toBeTruthy());
    expect(screen.queryByText("No logs yet")).toBeNull();
    expect(screen.queryByText("No logs match this filter")).toBeNull();
  });

  it("distinguishes an archive with no logs from a filter that matched none", async () => {
    stubFetch(() => jsonResponse(logsResponse({ logs: [], services: [], total: 0 })));
    renderPage();

    await waitFor(() => expect(screen.getByText("No logs yet")).toBeTruthy());
    expect(screen.queryByText("No logs match this filter")).toBeNull();

    cleanup();
    renderPage("?level=error");
    await waitFor(() => expect(screen.getByText("No logs match this filter")).toBeTruthy());
    expect(screen.queryByText("No logs yet")).toBeNull();
  });

  it("moves a column sort into the URL and refetches from the server", async () => {
    const fetches = stubFetch(() => jsonResponse(logsResponse()));
    const page = renderPage();

    await waitFor(() => expect(screen.getByText("edge-1")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Service" }));

    await waitFor(() => expect(page.search()).toBe("?sort=service&direction=asc"));
    await waitFor(() =>
      expect(fetches.logRequests().some((url) => url.includes("sort=service") && url.includes("direction=asc"))).toBe(true)
    );
  });

  it("keeps the row detail dialog reachable from the message cell", async () => {
    stubFetch(() => jsonResponse(logsResponse()));
    renderPage();

    await waitFor(() => expect(screen.getByText("edge-1")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "View full log message from edge-1" }));

    await waitFor(() => expect(screen.getByRole("heading", { name: "Log entry" })).toBeTruthy());
    expect(screen.getAllByText(entry.message).length).toBeGreaterThan(0);
  });
});
