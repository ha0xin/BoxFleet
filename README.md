# BoxFleet

BoxFleet is a lightweight multi-node proxy management system designed around
small edge servers.

The intended architecture is:

- A central management server handles users, nodes, proxies, plans, quotas,
  audit logs, traffic records, and generated configurations.
- A server-side `bf` CLI manages the central server.
- Each node runs only `sing-box`, `systemd`, and a small `boxfleet-agent`.
- Nodes pull signed configuration from the central server, validate it, apply it,
  reload `sing-box`, and report heartbeats plus traffic counters.

## Goals

- Manage many low-memory proxy nodes from one server.
- Keep node-side memory usage low enough for small VPS instances.
- Support managed proxy users, quota accounting, and expiration.
- Generate per-user per-node `sing-box` connection information.
- Serve per-user Mihomo `proxy-provider` YAML through revocable subscription links.
- Avoid running Docker, databases, or web panels on edge nodes.

## Non-goals

- Reimplementing a proxy core.
- Depending on abandoned panel backends.
- Exposing node control APIs directly to the public internet.

## Repository Layout

```text
cmd/
  bf/                Server-side management CLI
  boxfleet-server/   Central API service for node agents
  boxfleet-agent/    Lightweight node agent
internal/
  server/            Server application code
  agent/             Agent application code
  config/            Shared configuration loading
  model/             Shared domain models
web/                 Management UI
docs/                Design notes and operations docs
deploy/
  systemd/           Service unit examples
  sing-box/          Generated config templates and examples
configs/             Local development config examples
migrations/          SQLite schema migrations
refs/                Local upstream/reference repositories
```

## Development Tooling

Use established project libraries instead of hand-rolling solved
infrastructure:

```text
CLI:              github.com/spf13/cobra
config/env:       github.com/spf13/viper
IDs:              github.com/google/uuid
migrations:       github.com/pressly/goose/v3
SQL generation:   sqlc
terminal color:   github.com/fatih/color
CLI tables:       github.com/jedib0t/go-pretty/v6/table
byte units:       github.com/dustin/go-humanize
HTTP router:      github.com/go-chi/chi/v5
logging:          github.com/rs/zerolog
token hashes:     golang.org/x/crypto/bcrypt
Web UI:           React + TypeScript + Vite + Tailwind v4 + Cloudflare Kumo + @phosphor-icons/react
frontend data:    TanStack Query + TanStack Table
frontend forms:   react-hook-form + zod
frontend dates:   date-fns + react-day-picker
```

Refresh generated SQL with the GOPATH binary, not an assumed global `sqlc`:

```bash
$(go env GOPATH)/bin/sqlc generate
```

If it is missing:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
```

Do not hand-roll command parsing, UUID generation, migration execution, SQL
scanning, token hashing, routing, logging, version comparison, byte-unit
parsing, CLI table rendering, protocol clients, frontend request caching,
non-trivial frontend tables, form validation, date/range picking, or app
dropdown primitives.

## Development Checks

Build the Web UI and run the current Go test suite:

```bash
npm --prefix web run build
go test ./...
```

GitHub Actions builds downloadable Linux amd64 artifacts when the Build
Artifacts workflow is manually dispatched or a `v*` tag is pushed. Pushing a
`v*` tag publishes a GitHub Release, which is the default deployment source.
The release bundle contains versioned Linux amd64 assets:

- `bf-<boxfleet-version>-linux-amd64`
- `boxfleet-server-<boxfleet-version>-linux-amd64`
- `boxfleet-agent-<boxfleet-version>-linux-amd64`
- `sing-box-v1.13.13-linux-amd64` built with BoxFleet's required tags

The management server embeds `/install.sh` for node bootstrap. Nodes fetch that
script from the server; the script downloads the versioned agent and sing-box
assets from the matching GitHub Release.

See:

- [docs/deployment.md](docs/deployment.md)
- [docs/web-ui.md](docs/web-ui.md)
- [docs/testing.md](docs/testing.md)
- [deploy/sing-box/README.md](deploy/sing-box/README.md)

## Current Milestone

1. SQLite-backed central server.
2. Management through `bf` and the embedded admin Web UI.
3. Single-admin operation.
4. Proxy generation for VLESS over TCP with Reality TLS and
   `xtls-rprx-vision`.
5. Per-user per-node config overrides.
6. Agent pull/check/apply/reload workflow.
7. V2Ray API traffic reporting.
8. Proxy user create/disable/expire.
9. Per-user node information generation.
10. Structured network activity, parsed from node log uploads and stored as
    compact aggregated network events with configurable retention.
