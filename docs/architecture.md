# Architecture

BoxFleet uses a central-control, node-pull model.

```text
BoxFleet Management Server
  - API for node agents
  - embedded Web UI for the admin operator
  - SQLite-backed state
  - operated by bf CLI
  - users
  - nodes
  - proxies
  - plans and quotas
  - config versions
  - generated user node information
  - traffic accounting
        |
        | HTTPS with node token or mTLS
        v
BoxFleet Agent
  - fetch config version
  - write candidate config
  - run sing-box check
  - atomically apply config
  - reload sing-box
  - report apply status, heartbeat, traffic, and logs
        |
        v
sing-box
```

## Server Responsibilities

- Store users, nodes, plans, traffic records, and audit logs.
- Use SQLite as the MVP database.
- Optionally run with Docker on the central server.
- Expose the central API used by node agents.
- Provide a server-side CLI for administration.
- Serve the embedded React admin UI and `/api/admin/*` endpoints.
- Generate `sing-box` configs per node.
- Sign or version generated configs.
- Disable expired or over-quota users.
- Generate per-user per-node connection information for clients.
- Receive node heartbeats and traffic reports.
- Receive node apply results and network log uploads.
- Parse known `sing-box` access log shapes into compact structured network
  events for CLI and admin UI queries.
- Keep system service logs separate from proxy network events if system log
  storage is re-enabled later.

The server-side CLI and API service can share packages, but they must not be
linked into the node agent binary.

## Engineering Stack

The management side uses mature infrastructure libraries instead of custom
implementations:

- Cobra for the `bf` command tree.
- Viper for config and environment binding.
- Goose for SQLite migrations.
- sqlc for generated, type-safe query methods.
- google/uuid for IDs.
- go-pretty/table for CLI table rendering.
- go-humanize for byte unit parsing and formatting.
- chi for HTTP routing.
- zerolog for structured logging.
- bcrypt for token hashes.
- React, TypeScript, Vite, Tailwind v4, Cloudflare Kumo, and
  @phosphor-icons/react for the embedded Web UI.
- TanStack Query for frontend request state and cache invalidation.
- TanStack Table for filterable/paginated admin data tables.
- react-hook-form and zod for frontend form state and validation.
- date-fns and react-day-picker for frontend time formatting and date/range
  picking.

The Web UI is built from `web/` and emitted into ignored generated files under
`internal/server/webui/assets/generated`, where it is embedded into
`boxfleet-server`.
The first UI surface covers overview, node add/edit, proxy add/edit as a
first-class resource, config preview/publish, users, shareable user node
information, traffic, network events, and system logs.

Network Events is the reference frontend workflow for server-side
pagination/filtering: the backend accepts `limit`, `offset`, `node`, `user`,
`start`, and `end`, while the browser UI converts local date/time range inputs
to RFC3339 UTC query parameters and keeps those filters in the URL. Structured
network events are retained for `network_event_retention_days`, defaulting to 90
days.

## Agent Responsibilities

- Authenticate to the server as one node.
- Poll for desired config version.
- Validate config with `sing-box check`.
- Apply config with rollback on failure.
- Reload or restart `sing-box`.
- Read `sing-box` V2Ray API counters and report traffic deltas.
- Treat the first V2Ray counter read after a fresh state as baseline, then
  report only positive deltas.
- Report node health, version, memory, interface counters, and raw recent log
  deltas. Protocol-specific log parsing belongs on the server side.
- Avoid public management surfaces by default. A future node maintenance port
  should bind to localhost or a private interface such as Tailscale, not the
  public internet.

## Node Constraints

Nodes should not run:

- Docker
- A database
- A web panel
- Prometheus or Grafana

Nodes should only need:

- `sing-box`
- `boxfleet-agent`
- `systemd`

Docker on nodes is optional. The preferred node deployment is still a small
native agent binary plus a native `sing-box` service.

The node agent should expose only local maintenance commands such as `run`,
`check`, `once`, and `version`. User, quota, generated node information, and
config management belong to the server-side `bf` CLI.

## Config Model

Config generation is per node and must support per-user per-node overrides.

```text
global user defaults
  -> node defaults
  -> proxy listener and protocol settings
  -> user-node assignment
  -> per-proxy user access
  -> generated sing-box config
```

This is required for relay nodes, custom routing, user-specific block rules, and
node-specific user availability.

## Database Choice

SQLite is the MVP database.

Use:

- WAL mode.
- `synchronous=NORMAL`.
- Busy timeout.
- Explicit schema migrations.
- Batched agent uploads.

PostgreSQL can be added later if BoxFleet gains multi-admin usage, heavy
dashboard queries, or a large number of high-frequency node reporters.
