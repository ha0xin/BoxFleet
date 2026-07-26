// @vitest-environment jsdom

import { act, cleanup, render } from "@testing-library/react";
import { createElement } from "react";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import { z } from "zod";

import { useUrlFilters, type FilterRecord, type UrlFilters, type UseUrlFiltersOptions } from "./use-url-filters";

/** The Nodes page shape: free-text search, a status facet, and sortable columns. */
const nodeSchema = z.object({
  search: z.string(),
  status: z.enum(["all", "active", "pending", "disabled", "deleted"]),
  sort: z.enum(["name", "status", "last_seen_at"]),
  direction: z.enum(["asc", "desc"])
});

type NodeFilters = z.infer<typeof nodeSchema>;

const nodeDefaults: NodeFilters = { search: "", status: "all", sort: "name", direction: "asc" };

const nodeOptions: UseUrlFiltersOptions<NodeFilters> = { schema: nodeSchema, defaults: nodeDefaults };

/** A different shape entirely: a coerced number plus an optional facet. */
const logSchema = z.object({
  level: z.enum(["all", "info", "warn", "error"]),
  fetchLimit: z.coerce.number().int().min(50).max(1000),
  service: z.string().optional()
});

type LogFilters = z.infer<typeof logSchema>;

const logDefaults: LogFilters = { level: "all", fetchLimit: 100, service: undefined };

const logOptions: UseUrlFiltersOptions<LogFilters> = { schema: logSchema, defaults: logDefaults, perPage: 25 };

function setup<Filters extends FilterRecord>(search: string, options: UseUrlFiltersOptions<Filters>) {
  let latest: UrlFilters<Filters> | undefined;
  function Probe() {
    latest = useUrlFilters(options);
    return null;
  }
  const router = createMemoryRouter([{ path: "/nodes", element: createElement(Probe) }], {
    initialEntries: [`/nodes${search}`]
  });
  render(createElement(RouterProvider, { router }));
  return {
    state: () => {
      if (!latest) throw new Error("hook did not render");
      return latest;
    },
    search: () => router.state.location.search,
    action: () => router.state.historyAction,
    act: (run: (state: UrlFilters<Filters>) => void) => {
      act(() => {
        if (!latest) throw new Error("hook did not render");
        run(latest);
      });
    }
  };
}

afterEach(cleanup);

describe("useUrlFilters", () => {
  it("keeps a default view out of the query string", () => {
    const page = setup("", nodeOptions);

    expect(page.search()).toBe("");
    expect(page.state().filters).toEqual(nodeDefaults);
    expect(page.state().page).toBe(1);
    expect(page.state().perPage).toBe(10);
    expect(page.state().offset).toBe(0);
    expect(page.state().activeFilterCount).toBe(0);

    page.act((state) => state.setFilters({ status: "active" }));
    expect(page.search()).toBe("?status=active");
    expect(page.state().activeFilterCount).toBe(1);

    // Returning a filter to its default clears the param instead of pinning it.
    page.act((state) => state.setFilters({ status: "all" }));
    expect(page.search()).toBe("");
  });

  it("round-trips filters and pagination through the URL", () => {
    const page = setup("", nodeOptions);
    page.act((state) => state.setFilters({ search: "edge node", direction: "desc" }));
    page.act((state) => state.setPerPage(25));
    page.act((state) => state.setPage(3));

    const written = page.search();
    expect(written).toBe("?search=edge+node&direction=desc&page=3&limit=25");

    const reloaded = setup(written, nodeOptions);
    expect(reloaded.state().filters).toEqual({
      search: "edge node",
      status: "all",
      sort: "name",
      direction: "desc"
    });
    expect(reloaded.state().page).toBe(3);
    expect(reloaded.state().perPage).toBe(25);
    expect(reloaded.state().offset).toBe(50);
    expect(reloaded.state().activeFilterCount).toBe(2);

    // Serialising the reloaded state reproduces the same query string.
    reloaded.act((state) => state.setPage(state.page));
    expect(reloaded.search()).toBe(written);
  });

  it("resets to the first page when a filter changes", () => {
    const page = setup("?status=active&page=4&limit=25", nodeOptions);
    expect(page.state().page).toBe(4);

    page.act((state) => state.setFilters({ search: "lax" }));

    expect(page.state().page).toBe(1);
    expect(page.search()).not.toContain("page=");
    // The unrelated filter and the page size survive the reset.
    expect(page.state().filters.status).toBe("active");
    expect(page.state().perPage).toBe(25);
  });

  it("keeps filters when only the page changes", () => {
    const page = setup("?status=disabled&search=lhr&sort=status", nodeOptions);

    page.act((state) => state.setPage(2));

    expect(page.state().page).toBe(2);
    expect(page.state().filters).toEqual({
      search: "lhr",
      status: "disabled",
      sort: "status",
      direction: "asc"
    });
    expect(page.state().offset).toBe(10);
  });

  it("resets to the first page when the page size changes", () => {
    const page = setup("?page=5", nodeOptions);

    page.act((state) => state.setPerPage(50));

    expect(page.state().page).toBe(1);
    expect(page.state().perPage).toBe(50);
    expect(page.search()).toBe("?limit=50");

    // Back to the default size, so the param disappears again.
    page.act((state) => state.setPerPage(10));
    expect(page.search()).toBe("");
  });

  it("degrades a hand-edited URL to defaults field by field", () => {
    const page = setup("?status=nonsense&sort=%3B+drop+table&search=keep+me&page=abc&limit=-4", nodeOptions);

    expect(page.state().filters).toEqual({
      search: "keep me",
      status: "all",
      sort: "name",
      direction: "asc"
    });
    expect(page.state().page).toBe(1);
    expect(page.state().perPage).toBe(10);
  });

  it("clamps an oversized page size and ignores a fractional page", () => {
    const page = setup("?limit=5000&page=2.5", nodeOptions);

    expect(page.state().perPage).toBe(100);
    expect(page.state().page).toBe(1);
  });

  it("leaves unrelated query params alone", () => {
    const page = setup("?tab=tokens&highlight=n1", nodeOptions);

    page.act((state) => state.setFilters({ status: "pending" }));
    page.act((state) => state.setPage(2));

    const params = new URLSearchParams(page.search());
    expect(params.get("tab")).toBe("tokens");
    expect(params.get("highlight")).toBe("n1");
    expect(params.get("status")).toBe("pending");
    expect(params.get("page")).toBe("2");

    page.act((state) => state.resetFilters());
    const afterReset = new URLSearchParams(page.search());
    expect(afterReset.get("tab")).toBe("tokens");
    expect(afterReset.get("highlight")).toBe("n1");
    expect(afterReset.get("status")).toBeNull();
    expect(afterReset.get("page")).toBeNull();
  });

  it("replaces history for filter edits and pushes for navigation", () => {
    const page = setup("", nodeOptions);

    page.act((state) => state.setFilters({ search: "e" }));
    expect(page.action()).toBe("REPLACE");

    page.act((state) => state.setFilters({ search: "ed" }));
    expect(page.action()).toBe("REPLACE");

    page.act((state) => state.setPage(2));
    expect(page.action()).toBe("PUSH");

    page.act((state) => state.setPerPage(25));
    expect(page.action()).toBe("PUSH");

    page.act((state) => state.resetFilters());
    expect(page.action()).toBe("PUSH");

    // Callers can override either default, e.g. an applied filter panel.
    page.act((state) => state.setFilters({ status: "active" }, "push"));
    expect(page.action()).toBe("PUSH");
    page.act((state) => state.setPage(3, "replace"));
    expect(page.action()).toBe("REPLACE");
  });

  it("accepts an updater and merges it over the current filters", () => {
    const page = setup("?search=lhr", nodeOptions);

    page.act((state) =>
      state.setFilters((current) => ({ direction: current.direction === "asc" ? "desc" : "asc" }))
    );

    expect(page.state().filters.search).toBe("lhr");
    expect(page.state().filters.direction).toBe("desc");
  });

  it("supports a different filter shape with coerced and optional fields", () => {
    const page = setup("", logOptions);

    expect(page.state().filters).toEqual({ level: "all", fetchLimit: 100, service: undefined });
    expect(page.state().perPage).toBe(25);

    page.act((state) => state.setFilters({ fetchLimit: 500, service: "sing-box" }));
    expect(page.search()).toBe("?fetchLimit=500&service=sing-box");

    const reloaded = setup(page.search(), logOptions);
    expect(reloaded.state().filters).toEqual({ level: "all", fetchLimit: 500, service: "sing-box" });
    expect(reloaded.state().activeFilterCount).toBe(2);

    // An optional field set back to undefined drops out of the URL entirely.
    reloaded.act((state) => state.setFilters({ service: undefined }));
    expect(reloaded.search()).toBe("?fetchLimit=500");

    // Out-of-range numbers fall back rather than reaching the request builder.
    expect(setup("?fetchLimit=9999", logOptions).state().filters.fetchLimit).toBe(100);
  });
});
