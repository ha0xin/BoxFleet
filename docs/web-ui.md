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

The UI uses React, TypeScript, Vite, Tailwind, shadcn-style local primitives,
and Radix UI. Prefer the project primitives in `web/src/components/ui/` over
native browser controls for app UI.

Current established libraries:

- Radix/shadcn primitives for dialogs, dropdown menus, popovers, selects,
  switches, tabs, labels, and related interactive controls.
- TanStack Query for admin API request state, caching, refresh, and
  invalidation.
- TanStack Table for filterable or paginated data tables.
- react-hook-form and zod for form state and validation.
- date-fns and react-day-picker for date/time formatting and date/range
  picking.
- lucide-react for icons.

Do not reintroduce native `<select>` elements for normal app dropdowns. Use the
Radix-backed `Select` component from `components/ui/select.tsx`.

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
- node/user/page-size selectors use the Radix-backed Select primitive
- date range selection uses react-day-picker inside a Radix Popover
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
