# Web UI

The embedded admin UI is served by `boxfleet-server` at `/admin`.

Build it with:

```bash
npm --prefix web install
npm --prefix web run build
```

The build output goes to `internal/server/webui/assets/generated` and is
embedded into the Go server binary. Generated files in that directory are
ignored by Git; build the Web UI before building or testing `boxfleet-server`.

## Frontend Stack

The UI uses React, TypeScript, Vite, Tailwind v4, and Cloudflare Kumo. Build app
UI directly on **native Kumo components** imported from `@cloudflare/kumo`, not on
native browser controls. The earlier shadcn-shaped compatibility wrappers under
`web/src/components/ui/` and the Geist `--ds-*` token overrides in `globals.css`
have been removed in favour of native Kumo + semantic tokens.

See `docs/design-system.md` for the visual conventions (semantic tokens, brand
accent, status tones, app-shell layout, typography).

Current established libraries:

- Cloudflare Kumo for dialogs, dropdown menus, popovers, selects, switches,
  tables, labels, buttons, inputs, banners, badges, surfaces, grids, and related
  interactive controls. Import components from `@cloudflare/kumo` directly.
- Kumo's Base UI primitive re-exports under `@cloudflare/kumo/primitives/*`
  when a styled Kumo component is not available.
- Tailwind v4 through `@tailwindcss/vite`. `web/src/styles/globals.css`
  keeps `@source`, then `@cloudflare/kumo/styles`, then `tailwindcss`, in that
  order.
- TanStack Query for admin API request state, caching, refresh, and
  invalidation.
- TanStack Table for filterable or paginated data tables.
- react-hook-form and zod for form state and validation.
- date-fns and react-day-picker for date/time formatting and date/range
  picking.
- `@phosphor-icons/react` for app icons, matching Kumo's own internals.

Do not reintroduce native `<select>` elements for normal app dropdowns. Use the
native Kumo `Select` (with `Select.Option` children) from `@cloudflare/kumo`.

## Kumo CLI

Kumo ships a CLI that is both the API reference and the block-vendoring tool.
Use it instead of guessing component shapes:

```bash
cd web
npx kumo ai                  # AI usage guide: tokens, rules, patterns
npx kumo doc <Component>     # full prop/variant reference for one component
npx kumo ls                  # list all components with categories
npx kumo blocks              # list installable page-level blocks
npx kumo add <Block>         # copy a block (e.g. PageHeader) into the project
```

`kumo add` reads the block source from the installed package (offline), rewrites
its relative imports to `@cloudflare/kumo`, and writes it into the `blocksDir`
configured in `web/kumo.json` (the Kumo default, `src/components/kumo`). Blocks are
**vendored source you then adapt**, not imported dependencies — adapt the copy to
fit BoxFleet.

When a Kumo API or layout behavior is unfamiliar, run `npx kumo ai` and
`npx kumo docs <Component>` from `web/` before editing. Do not infer compound
component names, prop names, variants, or accessibility requirements from memory.
For examples beyond CLI docs, consult the local Kumo demo site source in
`refs/kumo/packages/kumo-docs-astro/`; for implementation details, consult
`refs/kumo/packages/kumo/src/`. The `refs/` tree is reference-only.

The two authoritative conventions from `kumo ai`: **semantic tokens only**
(`bg-kumo-base`, `text-kumo-default`, …; never raw Tailwind colors) and **no
`dark:` variants** (light/dark is handled automatically via `light-dark()`).

## Current Shell Checkpoint

The active native-Kumo shell was checkpointed after the rewrite:

- `web/src/App.tsx` uses Kumo `Sidebar.Provider`, `Sidebar`, grouped nav items,
  and a bottom `Sidebar.Trigger`.
- `web/src/components/app-page-header.tsx` is the active page header. Its top
  breadcrumb/action bar is `h-[58px]` to align with Kumo `Sidebar.Header`.
- `web/src/pages/overview.tsx` and `web/src/pages/settings.tsx` are implemented
  on native Kumo. Other pages are placeholders until rewritten.
- `web/src/components/kumo/page-header/page-header.tsx` is a vendored Kumo block
  reference, not the active header.

Future page-header work should use the existing `actions` slot for review/publish
controls and should preserve the 58px top-bar alignment. Pending/unpublished
configuration state may tint or otherwise alter the header, but it must keep
semantic Kumo tokens and avoid raw color utilities.

## Visual Verification

Use Playwright for layout-sensitive UI checks. Launch system Chrome directly:

```ts
chromium.launch({ executablePath: "/usr/bin/google-chrome-stable" })
```

This avoids relying on a user's default Chrome profile. If comparing with a
running Cloudflare UI or a local dev server, measure actual DOM geometry and
computed styles rather than eyeballing. The 58px header/sidebar alignment was
verified this way by comparing the two elements' bounding boxes.

## Navigation

The first MVP sidebar has these first-class pages:

```text
Overview
Nodes
Proxies
Users
Traffic
Network Events
System Logs
```

`Proxies` is intentionally separate from `Nodes`. A node is machine state and
agent lifecycle. A proxy is a user-facing listener/protocol/routing object that
can have its own multiplier and rules.

Each page has a client-side route under the admin mount:

```text
/admin/                 Overview
/admin/nodes            Nodes
/admin/proxies          Proxies
/admin/users            Users
/admin/traffic          Traffic
/admin/network-events   Network Events
/admin/system-logs      System Logs
```

If `BOXFLEET_ADMIN_PATH_TOKEN` is configured, the same routes live under
`/<token>/admin/...`. The server returns the embedded `index.html` for nested
admin paths so browser refresh and copied links keep working.

## Nodes Page

The Nodes page is for node inventory and node-level operations:

- list nodes
- add a node
- edit public host, API base URL, and status
- render the current generated sing-box config
- publish the generated config version for the agent to pull

Proxy editing does not live in the Node modal.

## Proxies Page

The Proxies page is for proxy inventory and proxy-level operations:

- list all proxies across nodes
- add a proxy by selecting a node
- edit listen address, port, enabled state, traffic multiplier, settings JSON,
  and rules JSON
- inspect read-only transport

On create, the operator selects:

- node
- proxy name
- protocol
- listen address
- listen port

After create, node, proxy name, and protocol are fixed in the first version.
Renaming or changing protocol should be handled later as an explicit migration
operation because it affects generated auth names, stats mapping, and client
information.

## Transport

Transport is not a user-editable field. The server derives it from protocol:

```text
vless_reality     -> tcp
hysteria2         -> udp
shadowsocks_2022  -> tcp_udp
```

The UI shows transport only as read-only diagnostic data. It helps explain
listener conflict errors, but users should not have to choose it.

## Access And User Node Information

The Users page lists proxy users and can render shareable node information for a
selected user and node. The first UI does not yet manage proxy access grants;
use CLI for that:

```bash
bf access issue <user> --node <node> --proxy <proxy>
```

## Network Events

The Network Events page is the reference implementation for the current frontend
stack:

- requests are managed with TanStack Query
- the main table uses TanStack Table
- filters are managed with react-hook-form and zod
- node/user/page-size selectors use the Kumo-backed Select primitive
- date range selection uses react-day-picker inside a Kumo Popover
- time formatting and duration math use date-fns

Filtering is server-side. The admin API accepts `limit`, `offset`, `node`,
`user`, `start`, and `end` query parameters at `/api/admin/network-events`.
Date range and time inputs are interpreted in the browser's local timezone and
sent to the server as RFC3339 UTC timestamps.

The page mirrors filters and pagination into the browser URL so a refresh or
shared link preserves the selected time range, user, node, page size, and page.
It also exposes the structured event retention setting through
`/api/admin/settings`; `network_event_retention_days` defaults to 90.

The embedded bundle currently includes these shared frontend libraries. If the
bundle grows too much, split route-level or vendor chunks in Vite instead of
removing the libraries and hand-rolling behavior again.

## Dev Server And Mock Data

`npm --prefix web run dev` runs the Vite dev server with hot reload. By default
it serves **mock admin data** so the UI is fully populated without a running
`boxfleet-server`:

```bash
npm --prefix web run dev      # mock API (default), open http://127.0.0.1:5173/admin/
npm --prefix web run dev:api  # proxy /api to a real server on :18081
```

The mock layer is a dev-only Vite middleware plugin in `web/mocks/admin.ts`,
wired through `vite.config.ts`. It intercepts `/api/admin/*` and returns fixture
data for overview, nodes, proxies, users, traffic, network events (with
server-side `limit`/`offset` paging), system logs, settings, config changes, and
node bootstrap. Write requests (POST/PUT/PATCH/DELETE) get a generic
`{ ok: true }` echo so optimistic UI flows resolve. Fixture timestamps are
relative to "now" so "last seen" / "N minutes ago" displays look realistic.

This plugin is never bundled into the production build — `vite build` only ships
the SPA. To populate or tweak the demo data, edit the arrays at the top of
`web/mocks/admin.ts` (`nodes`, `users`, `proxies`, `networkEvents`, …); they are
typed against `src/types.ts`, so a shape mismatch fails the `tsc` step of
`npm run build`.

Set `BOXFLEET_DEV_API=1` (or use `dev:api`) to disable the mock and proxy `/api`
to a real server instead — useful when validating against actual backend
responses.

## Local Smoke

Run a local server with a development database:

```bash
go run ./cmd/boxfleet-server --db /tmp/boxfleet-ui-demo.db --addr 127.0.0.1:18082 --allow-insecure-admin
```

Open:

```text
http://127.0.0.1:18082/admin
```

If an older asset remains cached, hard refresh the browser.
