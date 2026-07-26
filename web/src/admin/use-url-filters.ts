import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import type { z } from "zod";

/**
 * URL-synced filter + pagination state for admin list pages.
 *
 * `network-events.tsx` established the shape of this: filters live in the query
 * string, the parsed object feeds both the filter form (`values`) and the
 * TanStack Query key, and every write rebuilds the params in one place. This
 * hook is that logic lifted out so Nodes / Proxies / Users / System logs get
 * linkable, refresh-safe state without four copies of `filtersFromSearchParams`.
 *
 * Two deliberate differences from the page it generalises:
 *
 *  - Pagination is written as `page` (1-based) and `limit`, not `offset`. A
 *    shared link reading `?page=3` is what an operator expects; `offset` is a
 *    transport detail, so it stays derived (`offset`) for the request builder.
 *  - Unowned params are carried through automatically. The page enumerated
 *    `bucket` / `breakdown` / `service` by hand and dropped anything it forgot;
 *    here only the filter keys and the two pagination keys are ever touched.
 */

/** Filter values must survive a query-string round trip, so primitives only. */
export type FilterValue = string | number | boolean | undefined;

export type FilterRecord = Record<string, FilterValue>;

/**
 * `"replace"` overwrites the current history entry, `"push"` adds one.
 *
 * Defaults are chosen so the Back button stays useful: refining a filter is an
 * edit of the view the operator is already looking at (replace — a live search
 * box would otherwise push one entry per keystroke and trap the user on the
 * page), while paging is navigation between distinct result sets (push — Back
 * returns to the previous page of rows). `resetFilters` pushes so a mis-clicked
 * "Clear" is one Back away from the filters it discarded.
 */
export type HistoryMode = "push" | "replace";

export type UseUrlFiltersOptions<Filters extends FilterRecord> = {
  /**
   * Validates the raw query string. Every value arrives as a string, so numeric
   * and boolean fields need `z.coerce.*`; enums and strings work as written.
   * Fields whose default is `undefined` must be `.optional()`.
   */
  schema: z.ZodType<Filters>;
  /** Canonical state. Values equal to a default are omitted from the URL. */
  defaults: Filters;
  /** Rows per page when `limit` is absent from the URL. */
  perPage?: number;
  /** Upper bound for a hand-edited `limit`, mirroring the server's own cap. */
  maxPerPage?: number;
};

export type UrlFilters<Filters extends FilterRecord> = {
  /** Validated filters with defaults applied; safe to spread into a query key. */
  filters: Filters;
  /** 1-based, always >= 1. */
  page: number;
  perPage: number;
  /** `(page - 1) * perPage`, for `limit`/`offset` style endpoints. */
  offset: number;
  /** How many filters differ from their default — drives the "Filter (2)" badge. */
  activeFilterCount: number;
  /** Merges a patch over the current filters and returns to page 1. */
  setFilters: (
    patch: Partial<Filters> | ((current: Filters) => Partial<Filters>),
    history?: HistoryMode
  ) => void;
  /** Moves pages without disturbing the filters. */
  setPage: (page: number, history?: HistoryMode) => void;
  /** Changing the page size returns to page 1; a stale offset would skip rows. */
  setPerPage: (perPage: number, history?: HistoryMode) => void;
  /** Drops every owned param, leaving unrelated ones in place. */
  resetFilters: (history?: HistoryMode) => void;
};

const PAGE_PARAM = "page";
const LIMIT_PARAM = "limit";
const DEFAULT_PER_PAGE = 10;
const DEFAULT_MAX_PER_PAGE = 100;

/** A positive integer, or `fallback` for anything a hand-edited URL can hold. */
function positiveInt(raw: string | null, fallback: number, max: number): number {
  if (raw === null || raw.trim() === "") return fallback;
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 1) return fallback;
  return Math.min(value, max);
}

/**
 * Parses the owned params, degrading per field rather than per URL.
 *
 * The candidate record starts from the serialised defaults so the schema always
 * sees a complete object, then URL values override it. If validation fails, the
 * fields zod named are reverted to their defaults and it is retried once — one
 * bad `?status=nonsense` must not discard a perfectly good `?search=`.
 */
function parseFilters<Filters extends FilterRecord>(
  params: URLSearchParams,
  schema: z.ZodType<Filters>,
  defaults: Filters
): Filters {
  const candidate: Record<string, string> = {};
  for (const [key, value] of Object.entries(defaults)) {
    if (value !== undefined) candidate[key] = String(value);
    const raw = params.get(key);
    if (raw !== null) candidate[key] = raw;
  }

  const first = schema.safeParse(candidate);
  if (first.success) return first.data;

  for (const issue of first.error.issues) {
    const key = issue.path[0];
    if (typeof key !== "string") continue;
    const fallback = defaults[key];
    if (fallback === undefined) delete candidate[key];
    else candidate[key] = String(fallback);
  }
  const second = schema.safeParse(candidate);
  return second.success ? second.data : defaults;
}

/**
 * Writes the owned params onto a copy of `base`.
 *
 * Only the filter keys and the two pagination keys are cleared, so anything
 * else already in the query string survives. Values matching a default are
 * omitted, which is what makes a clean view produce a clean link and what makes
 * parse -> serialise -> parse stable.
 */
function serializeState<Filters extends FilterRecord>(
  base: URLSearchParams,
  filters: Filters,
  page: number,
  perPage: number,
  defaults: Filters,
  defaultPerPage: number
): URLSearchParams {
  const next = new URLSearchParams(base);
  for (const [key, fallback] of Object.entries(defaults)) {
    next.delete(key);
    const value = filters[key];
    // `undefined` has no query-string spelling; it is the absent state itself.
    if (value === undefined || value === fallback) continue;
    next.set(key, String(value));
  }
  next.delete(PAGE_PARAM);
  next.delete(LIMIT_PARAM);
  if (page > 1) next.set(PAGE_PARAM, String(page));
  if (perPage !== defaultPerPage) next.set(LIMIT_PARAM, String(perPage));
  return next;
}

/**
 * Keeps a typed filter object and pagination in the query string.
 *
 * `schema` and `defaults` are read on every render, so declare them at module
 * scope (as `network-events.tsx` does with `filterSchema` / `defaultFilters`)
 * to keep the returned `filters` object referentially stable between renders.
 *
 * Nothing here clamps `page` against a row count — the hook never sees a total.
 * A page that outlives its rows should be corrected by the caller, which does
 * know the total, with `setPage(lastPage, "replace")`.
 */
export function useUrlFilters<Filters extends FilterRecord>({
  schema,
  defaults,
  perPage: defaultPerPage = DEFAULT_PER_PAGE,
  maxPerPage = DEFAULT_MAX_PER_PAGE
}: UseUrlFiltersOptions<Filters>): UrlFilters<Filters> {
  const [searchParams, setSearchParams] = useSearchParams();

  const filters = useMemo(
    () => parseFilters(searchParams, schema, defaults),
    [searchParams, schema, defaults]
  );
  const perPage = positiveInt(searchParams.get(LIMIT_PARAM), defaultPerPage, maxPerPage);
  const page = positiveInt(searchParams.get(PAGE_PARAM), 1, Number.MAX_SAFE_INTEGER);

  /**
   * Every write re-derives the current state from the params React Router hands
   * back, never from this render's closure: two updates in one tick (a filter
   * change plus a page reset, say) then compose instead of overwriting.
   */
  const write = useCallback(
    (
      update: (current: { filters: Filters; page: number; perPage: number }) => {
        filters: Filters;
        page: number;
        perPage: number;
      },
      history: HistoryMode
    ) => {
      setSearchParams(
        (previous) => {
          const next = update({
            filters: parseFilters(previous, schema, defaults),
            page: positiveInt(previous.get(PAGE_PARAM), 1, Number.MAX_SAFE_INTEGER),
            perPage: positiveInt(previous.get(LIMIT_PARAM), defaultPerPage, maxPerPage)
          });
          return serializeState(previous, next.filters, next.page, next.perPage, defaults, defaultPerPage);
        },
        { replace: history === "replace" }
      );
    },
    [defaultPerPage, defaults, maxPerPage, schema, setSearchParams]
  );

  const setFilters = useCallback<UrlFilters<Filters>["setFilters"]>(
    (patch, history = "replace") => {
      write((current) => {
        const resolved = typeof patch === "function" ? patch(current.filters) : patch;
        // Page 1 is the only safe landing spot: the row that was on page 3
        // under the old filters is not on page 3 under the new ones.
        return { filters: { ...current.filters, ...resolved }, page: 1, perPage: current.perPage };
      }, history);
    },
    [write]
  );

  const setPage = useCallback<UrlFilters<Filters>["setPage"]>(
    (value, history = "push") => {
      write((current) => ({
        filters: current.filters,
        page: Math.max(1, Math.floor(value)),
        perPage: current.perPage
      }), history);
    },
    [write]
  );

  const setPerPage = useCallback<UrlFilters<Filters>["setPerPage"]>(
    (value, history = "push") => {
      write((current) => ({
        filters: current.filters,
        page: 1,
        perPage: Math.min(Math.max(1, Math.floor(value)), maxPerPage)
      }), history);
    },
    [maxPerPage, write]
  );

  const resetFilters = useCallback<UrlFilters<Filters>["resetFilters"]>(
    (history = "push") => {
      write(() => ({ filters: defaults, page: 1, perPage: defaultPerPage }), history);
    },
    [defaultPerPage, defaults, write]
  );

  const activeFilterCount = useMemo(
    () => Object.keys(defaults).filter((key) => filters[key] !== defaults[key]).length,
    [defaults, filters]
  );

  return {
    filters,
    page,
    perPage,
    offset: (page - 1) * perPage,
    activeFilterCount,
    setFilters,
    setPage,
    setPerPage,
    resetFilters
  };
}
