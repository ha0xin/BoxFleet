# Web UI

The React/Vite admin UI is served under `/admin` (or the configured hidden
prefix) and embedded in `bfs`.

```bash
npm ci --prefix web
npm --prefix web run dev       # mock API
npm --prefix web run dev:api   # proxy to server on :18081
npm --prefix web run build
```

Build output goes to ignored
`internal/server/webui/assets/generated`. Build it before compiling or testing
the server.

## Stack and boundaries

- Cloudflare Kumo for UI components; Base UI primitives only when Kumo lacks a
  styled component.
- Tailwind v4 with Kumo semantic tokens only.
- TanStack Query for API state and invalidation.
- TanStack Table for non-trivial tables.
- react-hook-form and zod for forms.
- date-fns and react-day-picker for local-time filters.
- Phosphor icons for application icons.
- Monaco/monaco-yaml for the lazy-loaded Mihomo editor.
- echarts for page charts, via Kumo's own chart components. `recharts` has been
  removed and must not come back.

Admin requests live behind `AdminApiProvider`. Endpoints and query keys belong
in `src/admin`; mutations use `useAdminMutation` so cache invalidation completes
before dialogs close. API failures preserve HTTP status, and a 401 routes the
operator to Settings.

## Visual rules

Use native Kumo components and semantic tokens such as `bg-kumo-base`,
`text-kumo-default`, and `kumo-line`. Never use raw Tailwind colours, hardcoded
component colours, `dark:` variants, native `<select>`, or a parallel component
wrapper library.

Use Kumo's component documentation instead of guessing APIs:

```bash
cd web
npx kumo ai
npx kumo docs <Component>
npx kumo ls
```

Navigation is defined in `src/navigation.ts`; routes are registered in
`src/App.tsx`. Every page uses the single `AppPageHeader` (breadcrumb bar +
title block + actions slot) and the shared 1400px content width; the 58px
breadcrumb bar aligns with `Sidebar.Header`. Mobile navigation must remain
reachable, tables own their horizontal scrolling, and dialogs use a
viewport-bounded scroll area. Light/dark follows the system preference via
`data-mode` on the root element (wired in `src/main.tsx`); never use `dark:`
variants.

Shared design-system components (`src/components/`):

- `admin-table.tsx` — `TableCard` (canonical table chrome), `SortHead`
  (aria-sort, optional `sticky="left"`), `TableEmpty` / `TableError` /
  `TableLoading`, and `AdminPagination`. Query errors render `TableError`, not
  the empty state. Empty tables show only `0 items` (no page-size control).
- `status-badge.tsx` — `StatusBadge` (Kumo dot Badge) is the canonical status
  cell: success/neutral/warning/error/info with capitalized human labels, never
  raw enum values or colored spans.
- `row-actions-menu.tsx` — `RowActionsMenu` is the canonical per-row kebab;
  destructive items use `variant="danger"` after a separator and confirm via
  `SoftDeleteDialog`.

Use Kumo `Table.Head`/`Table.Cell` `sticky="left"` for the identity column when
row actions require horizontal scrolling — never hand-rolled sticky classes.
`useAdminMutation` shows an error toast by default; dialogs that render an
inline error `Banner` pass `toastError: false`.

## Charts

**Kumo ships a chart system that `npx kumo ls` does not list.** The CLI catalog
covers 42 components and omits charts entirely; `npx kumo docs Chart` reports
"Component not found". `@cloudflare/kumo` nevertheless re-exports `Chart`,
`ChartPalette`, `TimeseriesChart`, `ChartLegend`, and `SankeyChart` from its root
barrel. Do not conclude from the CLI that Kumo has no charts — check
`node_modules/@cloudflare/kumo/dist/src/components/chart/*.d.ts`, which is the
authority for the installed version, and cross-read
`refs/kumo/packages/kumo/src/components/chart/` for behaviour. Re-run the CLI
after a Kumo upgrade in case the catalog has caught up; until it does, these
APIs can change without the CLI ever surfacing it.

`echarts` is Kumo's own declared **optional** peer dependency, so using it is
policy-compliant. Kumo's chart components take the core instance as a prop
rather than importing it, so the app owns tree shaking:

- `src/components/chart/echarts.ts` is the single registration point. Every
  registered module is load-bearing — `Brush` and `Toolbox` back
  `onTimeRangeChange`, `Aria` backs `ariaDescription`, `AxisPointer` backs
  `tooltipFollowCursor`. A missing registration fails silently at runtime.
- `src/components/chart/time-bar-chart.tsx` wraps `TimeseriesChart` with
  `type="bar"`. Kumo hardcodes `stack: "total"` on bar series, so there is no
  `stacked` prop; bars always stack.
- `src/components/chart/ranked-bar-list.tsx` is DOM, not canvas, on purpose. Bar
  length encodes magnitude so one hue is correct and there is no colour-vision
  exposure, every row is direct-labelled, and drill-down needs a real focusable
  click target.
- `src/components/sparkline.tsx` is hand-rolled inline SVG. Kumo has no
  sparkline and the only Kumo-shaped alternative is one canvas per table row.
  It uses `preserveAspectRatio="none"` so it is fluid with zero measurement — no
  ResizeObserver, no effects — `currentColor` so a row re-tones with a semantic
  class, and no `<defs>` so there is no DOM-id namespace to collide in a
  paginated table. It renders nothing for fewer than two points or any
  non-finite value, so a one-bucket tile needs its own empty affordance.

Canvas colours come from `src/components/chart/chart-palette.ts`, pinned to
`ChartPalette.categorical(0)` `#4290F0` and `categorical(5)` `#D37536`. Those
two pass every lightness, contrast, and colour-vision check in both modes. Slot 1
(yellow) misses the lightness band and lands at 1.75:1 in light mode; slot 3
(purple) misses the normal-vision separation floor against slot 0; slots 2 (pink)
and 4 (teal) miss the lightness band in dark mode. **Do not use slots 1-4 for a
two-series chart.** Never source chart marks from the UI semantic tokens: those
are tuned for a 16px badge, chart semantics are deliberately softer, and a canvas
cannot read CSS variables anyway. Per-action breakdowns stay as `StatusBadge`
plus a count — a dot, a label, and a number — rather than a stacked-by-action
bar chart.

**The server owns bucketing, zero-fill, and ordering.** Series points arrive
contiguous and oldest-first; render them verbatim. Never bucket, fill, or sort
client-side — the bug this replaced aggregated one page of table rows and changed
whenever the operator paged.

## Page contracts

- Nodes owns enrollment, pause/resume, decommission/re-enroll, config and managed
  component updates.
- Proxies owns listener/protocol inventory; transport is server-derived and
  read-only.
- Users owns identity, quota, access grants, connection details and legacy user
  subscriptions.
- Mihomo Profiles owns complete configuration pipelines, live templates,
  preview, and configuration-scoped subscriptions.
- Network Events is the reference server-paginated, URL-synchronised table and
  the reference server-bucketed chart. Its activity chart reads
  `/api/admin/network-events/series`, and its audit panel ranks services from
  `/api/admin/network-events/services` with per-host drill-down from
  `/api/admin/network-events/hosts`. The audit panel counts **connections**,
  never bytes: `log_events` has no byte columns and a destination host can never
  be attributed bytes. Grouping by action yields one series against a real
  server — only `connect` rows exist — so the page charts `group=total` and keeps
  the `StatusBadge` action row as its legend, fed by the server's unbucketed
  `actions` histogram rather than by the visible page.
- Traffic charts bucketed uplink/downlink volume from
  `/api/admin/traffic/series`, with a per-user table whose row sparklines come
  from one batched `group=user` response — never one request per user.
- Overview trend lines are live: a fixed 7-day, day-bucketed window from
  `/api/admin/traffic/series` (`group=total` for the billable-traffic tile,
  `group=node` for the nodes widget) and `/api/admin/network-events/series` for
  the network-events tile. The placeholder arrays and their disclaimer are gone.
  The "Active nodes" tile intentionally has no trend line — there is no
  historical node-status table, so no real backing series exists. Do not
  reintroduce a fabricated one.

The global publish bar compares current rendered configs with published targets.
Writes need not predict whether they are dirty; root invalidation lets the
server-derived diff decide.

## Time and mock behavior

Network Event and Traffic date/time inputs use the browser timezone and send
RFC3339 UTC. Filters, offset, limit, and chart view preferences stay in the URL.
Relative time windows must be anchored when applied, not when the page mounted.

Hour buckets are UTC and need no offset. Day buckets send
`offset_minutes: -new Date().getTimezoneOffset()` — **the sign is inverted
relative to the JS convention**, because the server adds the value to UTC to get
local time. Offer hourly granularity only for ranges of 7 days or less; the
server independently rejects hour buckets beyond 8 days and day buckets beyond
400.

`web/mocks/admin.ts` is a typed development API. Keep every implemented route in
sync with the real method and response shape. It buckets and zero-fills like the
server does, so a page cannot pass in dev by re-bucketing on the client. Two
places where it deliberately differs from production: its service catalog is a
small stub unrelated to the embedded v2fly dataset, and its network-event
fixture carries four `action` values where production only ever has `connect`.

## Verification

```bash
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
npm --prefix web run test:e2e
```

Playwright discovers Chrome on macOS, Linux, and Windows, then falls back to its
bundled Chromium. Prefer geometry and computed-style assertions over screenshot
judgment. `refs/kumo/` is reference-only and must never be imported.

Vitest runs without `globals`, so Testing Library never registers its automatic
cleanup. Any component test that renders more than once must call
`afterEach(cleanup)` itself. ECharts needs a real canvas and `ResizeObserver`,
neither of which jsdom provides, so chart success paths belong in the Playwright
pass; unit-test the pure projection helpers instead.
