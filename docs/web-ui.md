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
viewport-bounded scroll area. Light/dark uses `data-mode` on the root element
(wired in `src/components/color-mode.ts`): it follows the system preference
until the operator selects a mode from the page header, then persists that
choice locally. Never use `dark:` variants.

Shared design-system components (`src/components/`):

- `admin-table.tsx` — `TableCard` (canonical table chrome), `TableColgroup` /
  `tableMinWidth` / `TableColumnWidth` (column sizing, see below), `SortHead`
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

## Dialog widths

`<Dialog size>` sets a **fixed** width at `sm:` and above, and it is the only
width control a dialog gets. The border-box totals are `sm` 288px, `base` 384px,
`lg` 512px, `xl` 768px; subtract the 48px of `p-6` for usable content. Below
640px the size variant drops out entirely and every dialog is `width: 100%`
clamped to `calc(100vw - 2rem)`, so a phone renders all four sizes identically.

Kumo 2.5.x behaved differently — the popup was `sm:w-auto` with the size prop
supplying only a `min-width` floor, so a dialog was actually sized by its widest
unwrappable content and the prop was close to decorative. Do not carry over
layout intuitions from that era, and never restore the old behaviour with a
`min-w-*` on the popup: that is the page-widening pattern documented below,
moved inside a dialog.

Pick the size from the widest row the form actually contains, and verify by
measuring rather than by eye — no descendant may have `scrollWidth >
clientWidth` unless it is a deliberate scroll container. A multi-column row is
what forces the choice: `grid-cols-2` of labelled fields needs `lg`, because two
`Input`s with labels like "Traffic multiplier" have a combined min-content width
of ~390px and silently overflow `base`. `EditNodeDialog`'s four-column hosts
repeater is `lg` for the same reason.

## Table column widths

Every list table declares its column widths. Kumo's `<Table>` is `w-full`, so
under the default `layout="auto"` the browser spreads all surplus width
proportionally across every column — a one-character "Config" cell ends up as
wide as a hostname. The fix is to say what each column is worth:

```tsx
const nodeColumns: TableColumnWidth[] = [{ min: 104 }, 144, /* … */ 52];

<TableCard>
  <Table layout="fixed" style={{ minWidth: tableMinWidth(nodeColumns) }}>
    <TableColgroup widths={nodeColumns} />
    …
```

- A **number** is an exact px width. Use it for content with a hard ceiling:
  status badges, version strings, counts, relative timestamps, the kebab. Derive
  it by measurement — render the table at `width: max-content` and read the
  column's natural width — not by guessing.
- **`{ min }`** marks a flexible column that takes the leftover width. Use it
  only for genuinely open-ended text (names, hosts, endpoints, log messages).
- Fixed table layout divides the leftover **equally** between flexible columns.
  That is the one distribution every browser implements identically, so it is
  what the helpers rely on. Percentage `<col>` widths do bias the split in
  Chrome but over-constrain the table, and `calc()` percentages are ignored
  entirely — do not reach for either. A column that must stay narrower than its
  peers gets a px width instead of being made flexible.
- `tableMinWidth` therefore reserves the *largest* flexible floor for every
  flexible column. Below that width `TableCard`'s scroll container takes over,
  which is the correct behaviour, not a bug.
- Never put `min-w-[NNNNpx]` on a `<Table>`. It is a page-wide constraint
  wearing a table's clothing — see below.

Fixed layout means overflow is visible instead of self-correcting, so **every
cell in a flexible column must truncate and carry a tooltip**: `block truncate`
plus `title` on the span, or `flex min-w-0` with `min-w-0 truncate` children when
the cell has an icon or an arrow. A cell with `whitespace-nowrap` and no
`overflow` will spill across the column border.

### Page roots need `min-w-0`

Admin page roots are grid items (`App.tsx` keys the route by pathname inside a
`grid`), and a grid item's default `min-width: auto` means it can never be
narrower than its min-content size. A table's minimum width therefore leaks
straight *past* `TableCard`'s `overflow-x-auto` and widens the page: the sidebar
`<main>` computes `overflow-x: auto` and the whole page slides sideways while the
table's own scroll container never scrolls at all — which also silently defeats
`sticky="left"`, because the sticky column is pinned to a scrollport that is not
the one moving. Every page root carries `min-w-0` for this reason:

```tsx
<div className="flex min-h-full min-w-0 flex-col bg-kumo-canvas">
```

Verify with geometry, never by eye: the sidebar `<main>` must have
`scrollWidth - clientWidth === 0` at every viewport, and `.bf-table-scroll` must
be the element that scrolls when a table is genuinely wider than its card.

### Draggable column resizing

Kumo does ship `Table.ResizeHandle` (`Table.ResizeHandle` in
`node_modules/@cloudflare/kumo/dist/src/components/table/table.d.ts`;
`Table.Head` is already `group relative` so the handle positions itself). It is
an affordance only — no drag logic, no state, no persistence — and Kumo's own
docs pair it with TanStack Table:
`refs/kumo/packages/kumo-docs-astro/src/pages/components/table.mdx`. **`npx kumo
docs Table` lists it with empty props and no example and never mentions
resizing**, the same CLI blind spot recorded above for charts.

It is deliberately not wired up. Resizing is an escape hatch on top of good
defaults, not a substitute for them, and it would cost: converting the
hand-rolled pages to TanStack column definitions, a persistence layer, and a
keyboard path (`getResizeHandler` binds only mouse and touch, so shipping it
as-is adds a focusable control per header that does nothing on Enter). If it is
ever added, `layout="fixed"` plus `TableColgroup` is already the substrate — feed
the widths from `column.getSize()` — and it should start on a page that is
already TanStack-driven. Pass `className="bg-kumo-elevated"` to the handle so it
matches the compact header; Kumo hardcodes `bg-kumo-base`.

## Charts

**The CLI catalog is incomplete, and how incomplete changes between releases.**
Through 2.5.x it covered 42 components and omitted charts entirely; `npx kumo
docs Chart` reported "Component not found". As of 2.8.0 it lists 48 and the
catalog has caught up on the chart *components* — `Chart`, `TimeseriesChart`,
`SankeyChart`, `BubbleMap`, and `ChoroplethMap` are all listed, and `npx kumo
docs TimeseriesChart` returns full props and examples. It has **not** caught up
on `ChartLegend` or `ChartPalette`, both of which this app uses and both of
which still report "Component not found".

So the rule stands, just for a smaller surface: never conclude from the CLI that
an export does not exist. `node_modules/@cloudflare/kumo/dist/src/components/chart/*.d.ts`
is the authority for the installed version. `refs/kumo/` is a checkout pinned to
an older release and goes stale the moment Kumo is upgraded — cross-read it for
behaviour only after confirming the version matches, never for API shape. Re-run
`npx kumo ls` after each upgrade and correct this paragraph.

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

### An accepted contrast deviation on Button

Kumo 2.6.0 gave the `primary` and `destructive` Button variants token-derived
gradient fills. Measured white-on-fill contrast in light mode fell below WCAG AA
as a result — primary 4.53:1 → 3.63:1, destructive 3.81:1 → 3.13:1. Dark mode
improved. This is upstream styling, it was reviewed, and it is **accepted as-is**:
do not override the variant tokens locally to "fix" it. Overriding would fork the
button treatment away from Kumo for every future release, to chase a value
upstream owns. Re-measure after each Kumo bump and revisit only if it regresses
further or upstream ships a correction.

Note that this is the button *fill*, not `text-kumo-danger`, which is a separate
token (`--text-color-kumo-danger`) and measures 6.64:1 on `bg-kumo-base` in dark
mode. The two are easy to conflate — a pre-upgrade review did exactly that and
raised a false alarm about the publish strip being unreadable.

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
