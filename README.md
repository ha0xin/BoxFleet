# BoxFleet

BoxFleet is a lightweight control plane for multiple `sing-box` nodes. The
central server owns SQLite state, renders configuration, serves the admin UI,
and receives telemetry. Edge nodes run only `boxfleet-agent`, `sing-box`, and
systemd.

The supported proxy path is VLESS-Reality over TCP with
`xtls-rprx-vision`. Nodes pull versioned configuration and report heartbeats,
traffic counters, apply results, and network logs.

## Components

- `boxfleet-server`: central API, embedded Web UI, renderer, and SQLite owner.
- `bf`: local operator CLI that opens the same SQLite database directly.
- `boxfleet-agent`: node-side pull/apply/report daemon.
- `web/`: React/Vite admin UI built into `boxfleet-server`.

Important directories:

```text
cmd/          binary entrypoints
internal/     application packages
migrations/   append-only SQLite migrations
queries/      sqlc query sources
schema/       current full schema snapshot
web/          admin UI and browser tests
deploy/       service and sing-box examples
docs/         architecture and operational contracts
refs/         reference-only upstream checkouts
```

## Development

```bash
npm ci --prefix web
npm --prefix web run lint
npm --prefix web test
npm --prefix web run build
go test ./...
go vet ./...
npm --prefix web run test:e2e
```

After changing `queries/*.sql`, migrations, or `schema/schema.sql`:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 # once
$(go env GOPATH)/bin/sqlc generate
```

Run a local server with authentication disabled only for development:

```bash
go run ./cmd/boxfleet-server --db /tmp/boxfleet.db --allow-insecure-admin
```

Run `bf --help` or `boxfleet-agent --help` for the current command surface.

## Releases

Pushing a `v*` tag publishes Linux amd64 artifacts. Server, agent, and sing-box
versions are independent, so a server-only release does not advertise a no-op
node update. See [deployment](docs/deployment.md) for releases and node
bootstrap, and [azus runbook](docs/azus-runbook.md) for the production host.

## Documentation

- [Architecture](docs/architecture.md)
- [Database invariants](docs/db-schema.md)
- [Configuration rendering](docs/config-generation.md)
- [Node operations](docs/node-operations.md)
- [Mihomo subscriptions](docs/mihomo-subscriptions.md)
- [Web UI](docs/web-ui.md)
- [Testing](docs/testing.md)
- [Performance targets](docs/performance.md)
