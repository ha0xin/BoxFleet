// @vitest-environment jsdom

import { Sidebar } from "@cloudflare/kumo";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AdminApiProvider } from "@/admin/api";
import { PublishStatusProvider } from "@/publish/publish-status";
import type { AdminUserRow, AdminUsersResponse, TrafficVolume } from "../types";
import { UsersPage, billableBytes, formatExpiry, rawBytes, usersRequestPath, type UserFilterValues } from "./users";

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

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const defaults: UserFilterValues = { search: "", status: "all", sort: "name", direction: "asc" };

function volume(uplink: number, downlink: number, multiplier = 1): TrafficVolume {
  return {
    uplink_raw_bytes: uplink,
    uplink_billable_bytes: uplink * multiplier,
    downlink_raw_bytes: downlink,
    downlink_billable_bytes: downlink * multiplier
  };
}

function userRow(overrides: Partial<AdminUserRow> = {}): AdminUserRow {
  return {
    id: "usr_1",
    name: "alice",
    display_name: "Alice",
    status: "active",
    global_quota_bytes: 0,
    expire_at: "",
    proxy_count: 2,
    deleted_at: "",
    effective_status: "active",
    traffic: volume(0, 0),
    ...overrides
  };
}

function page(users: AdminUserRow[], total = users.length, limit = 10, offset = 0): AdminUsersResponse {
  return { users, total, limit, offset };
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
}

/**
 * Routes fetch: the users endpoint gets `respond`, everything else gets a quiet
 * answer. The page header mounts the global publish bar, which polls
 * /config/changes; letting that fall through to the users fixture would make the
 * bar report changes that this test never made.
 */
function stubUsersFetch(respond: (url: string) => Response | Promise<Response>) {
  const urls: string[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/api/admin/users")) {
      urls.push(url);
      return respond(url);
    }
    return jsonResponse({ changed: [] });
  }));
  return urls;
}

/** Renders the page at `/users${search}` and hands back the router for URL assertions. */
function renderUsers(search = "") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([{ path: "/users", element: <UsersPage /> }], {
    initialEntries: [`/users${search}`]
  });
  render(
    <QueryClientProvider client={client}>
      <AdminApiProvider token="devtoken">
        <PublishStatusProvider>
          {/* The page header renders Kumo's mobile sidebar trigger. */}
          <Sidebar.Provider>
            <RouterProvider router={router} />
          </Sidebar.Provider>
        </PublishStatusProvider>
      </AdminApiProvider>
    </QueryClientProvider>
  );
  return router;
}

describe("usersRequestPath", () => {
  it("always requests a page, so the server never falls back to the bare array", () => {
    expect(usersRequestPath(defaults, 10, 0)).toBe("/api/admin/users?limit=10&offset=0&sort=name&direction=asc");
  });

  it("sends offset for later pages and omits an empty search", () => {
    expect(usersRequestPath(defaults, 25, 50)).toBe("/api/admin/users?limit=25&offset=50&sort=name&direction=asc");
  });

  it("trims the search term a hand-edited URL may carry", () => {
    const path = usersRequestPath({ ...defaults, search: "  carol wu  " }, 10, 0);
    expect(path).toContain("search=carol+wu");
  });

  it("passes a derived status through as the status facet", () => {
    const path = usersRequestPath({ ...defaults, status: "quota_exceeded" }, 10, 0);
    expect(path).toContain("status=quota_exceeded");
    expect(path).not.toContain("deleted=");
  });

  it("maps the deleted facet onto the deleted axis instead of the status one", () => {
    // status=deleted alongside the default live-inventory scope would ask for
    // deleted users that are not deleted, and always return nothing.
    const path = usersRequestPath({ ...defaults, status: "deleted" }, 10, 0);
    expect(path).toContain("deleted=true");
    expect(path).not.toContain("status=");
  });

  it("carries the sort column and direction", () => {
    const path = usersRequestPath({ ...defaults, sort: "traffic", direction: "desc" }, 10, 0);
    expect(path).toContain("sort=traffic");
    expect(path).toContain("direction=desc");
  });
});

describe("traffic totals", () => {
  it("separates billable bytes from the raw wire volume", () => {
    const traffic = volume(100, 900, 2);
    expect(billableBytes(traffic)).toBe(2000);
    expect(rawBytes(traffic)).toBe(1000);
  });
});

describe("formatExpiry", () => {
  it("reads an empty expiry as never and a past one as elapsed", () => {
    expect(formatExpiry("")).toBe("never");
    expect(formatExpiry(new Date(Date.now() - 3 * 86_400_000).toISOString())).toBe("3d ago");
    expect(formatExpiry(new Date(Date.now() + 2 * 3_600_000).toISOString())).toBe("in 2h");
  });

  it("returns an unparseable value unchanged rather than NaN", () => {
    expect(formatExpiry("not a date")).toBe("not a date");
  });
});

describe("UsersPage", () => {
  it("renders the page the server returned, with its traffic and derived status", async () => {
    const urls = stubUsersFetch(() => jsonResponse(page([
      userRow({ traffic: volume(1024, 3072), effective_status: "quota_exceeded", global_quota_bytes: 8192 })
    ], 42)));
    renderUsers();

    await waitFor(() => expect(screen.getByText("alice")).toBeTruthy());
    expect(screen.getByText("Over quota")).toBeTruthy();
    // 4 KB billable, and the raw line stays separate from it.
    expect(screen.getByText("4.0 KB")).toBeTruthy();
    expect(screen.getByText("raw 4.0 KB")).toBeTruthy();
    // The count is the server's total, not the length of this page.
    expect(screen.getByText("42 users")).toBeTruthy();
    expect(urls[0]).toContain("/api/admin/users?limit=10");
  });

  it("renders a failed query as an error, never as an empty inventory", async () => {
    stubUsersFetch(() => new Response("users unavailable", { status: 500 }));
    renderUsers();

    await waitFor(() => expect(screen.getByText("users unavailable")).toBeTruthy());
    expect(screen.queryByText("No users match this filter.")).toBeNull();
  });

  it("honours a deep-linked page, filter and sort on first render", async () => {
    const urls = stubUsersFetch(() => jsonResponse(page([userRow()], 40, 10, 20)));
    renderUsers("?page=3&status=disabled&sort=traffic&direction=desc");

    await waitFor(() => expect(urls.length).toBeGreaterThan(0));
    expect(urls[0]).toContain("offset=20");
    expect(urls[0]).toContain("status=disabled");
    expect(urls[0]).toContain("sort=traffic&direction=desc");
  });

  it("keeps a deep-linked page while the first response is still in flight", async () => {
    // total is 0 until the response lands; clamping on that would rewrite
    // ?page=3 to page 1 before the request that justifies it even returns.
    let release!: (value: Response) => void;
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    stubUsersFetch(() => pending);
    const router = renderUsers("?page=3");

    await waitFor(() => expect(screen.getByLabelText("Search users")).toBeTruthy());
    expect(router.state.location.search).toBe("?page=3");

    release(jsonResponse(page([userRow()], 40, 10, 20)));
    await waitFor(() => expect(screen.getByText("alice")).toBeTruthy());
    expect(router.state.location.search).toBe("?page=3");
  });

  it("clamps a page that outlived its rows once the total is known", async () => {
    stubUsersFetch(() => jsonResponse(page([], 5, 10, 80)));
    const router = renderUsers("?page=9");

    await waitFor(() => expect(router.state.location.search).toBe(""));
  });

  it("commits a search to the URL and resets to the first page", async () => {
    stubUsersFetch(() => jsonResponse(page([userRow()], 40, 10, 20)));
    const router = renderUsers("?page=3");
    await waitFor(() => expect(screen.getByText("alice")).toBeTruthy());

    const input = screen.getByLabelText("Search users");
    fireEvent.change(input, { target: { value: "  carol  " } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);

    await waitFor(() => expect(router.state.location.search).toBe("?search=carol"));
  });

  it("toggles the direction when the active sort column is clicked again", async () => {
    stubUsersFetch(() => jsonResponse(page([userRow()])));
    const router = renderUsers();
    await waitFor(() => expect(screen.getByText("alice")).toBeTruthy());

    // A new column adopts its own natural direction; traffic reads high-first.
    fireEvent.click(screen.getByRole("button", { name: /Traffic/ }));
    await waitFor(() => expect(router.state.location.search).toBe("?sort=traffic&direction=desc"));

    // Clicking the active column flips it back to ascending, which is the
    // default and therefore drops out of the URL again.
    fireEvent.click(screen.getByRole("button", { name: /Traffic/ }));
    await waitFor(() => expect(router.state.location.search).toBe("?sort=traffic"));
  });
});
