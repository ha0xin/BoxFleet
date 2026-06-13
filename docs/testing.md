# Testing Strategy

BoxFleet should move from manual smoke tests to repeatable tests before the
agent and config renderer become complex.

## Current State

Current automated coverage is mostly compilation:

```bash
npm --prefix web run build
go test ./...
```

Remote smoke checks are manual for now. GitHub Actions builds downloadable
Linux amd64 artifacts for server, CLI, agent, and `sing-box`; use those
artifacts for node/server deployment tests.

## Test Layers

### Unit Tests

Start with deterministic helper packages:

- `internal/units`: byte parsing and formatting.
- `internal/id`: ID prefix and UUID format.
- `internal/secret`: generated key length and encoding format.

Random generators should be tested by shape and constraints, not exact values.

### SQLite Integration Tests

Use `t.TempDir()` and a temporary database file.

Cover:

- migration status after `Migrate`
- proxy user create/list/show/update
- node create/list/show/update
- user-node binding create/list/update
- proxy create/list/show/update
- proxy access issue/show
- listener conflict validation, including protocol-derived transport
- unique constraints
- foreign-key constraints
- not-found errors

These tests should exercise the `internal/server/db` facade, not the generated
`sqlc` package directly. The generated package is tested indirectly by the
facade behavior.

### CLI Integration Tests

Invoke `internal/cli/bf.NewRootCommand()` directly with temp database paths and
buffered stdout/stderr.

Cover:

- `bf --db <tmp> db init`
- `bf --db <tmp> user create/list/show`
- `bf --db <tmp> node create/list/show`
- `bf --db <tmp> bind user/list`
- `bf --db <tmp> proxy create/list/show`
- `bf --db <tmp> access issue/show`

The CLI currently uses package-level Viper state. If tests become flaky, refactor
the CLI into an app struct that owns config state per command instance.

### Config Renderer Golden Tests

Add and keep golden tests around `bf config render --node` and
`bf config render-client`.

The input should be a fixed SQLite fixture. The output should be deterministic
JSON.

Rules:

- Access generation happens before render, not during render.
- Golden tests use fixed access fixtures.
- Tests compare normalized JSON structures, not raw whitespace.
- Important generated fields should also get explicit assertions.

### Agent Tests

Agent unit tests should not require a real `sing-box` process.

Use fakes for:

- server API
- V2Ray API client
- filesystem writes
- command runner for `sing-box check`
- service reloader

Real remote `sing-box` checks belong in the deployment smoke flow, not unit or
integration tests.

## Web UI Checks

The embedded admin frontend should pass:

```bash
$(go env GOPATH)/bin/sqlc generate
npm --prefix web install
npm --prefix web run build
go test ./...
go vet ./...
npm --prefix web run test:e2e
```

`go test ./...` includes a smoke test that `/admin` serves the embedded index.
Manual UI smoke for the current MVP:

- Open `/admin`.
- Confirm the sidebar has Overview, Nodes, Proxies, Users, Traffic, Network
  Events, and System Logs.
- Open Proxies.
- Click Add Proxy, select a node, create a VLESS Reality proxy without a
  transport field.
- Confirm the created proxy appears in the list with read-only transport.
- Click the proxy and confirm Node, Name, and Protocol are fixed while
  listen/port/enabled/multiplier/settings/rules remain editable.
- Open Nodes, click a node, and confirm node edit plus render/publish controls
  are present. Proxy editing should not live under the Node modal.
- Open Network Events.
- Confirm Node, User, and Page Size are Kumo selects, not native browser
  selects.
- Pick a date range, set start/end local times, apply filters, and confirm the
  table updates with server-side pagination.
- Confirm Previous/Next updates `offset` without losing the applied filters.

The Network Events page intentionally exercises the current frontend stack:
TanStack Query, TanStack Table, react-hook-form, zod, react-day-picker, and
date-fns. When changing that page, keep those libraries in use instead of
returning to ad hoc local state, hand-written table loops, or native date/select
controls.

`npm --prefix web run build` may warn if the JS bundle exceeds Vite's default
chunk warning threshold. Treat that as a code-splitting/manualChunks follow-up,
not a reason to remove established libraries.

Browser-level automated tests live under `web/e2e/` and use Playwright. The
current lifecycle test starts a local `boxfleet-server` on `127.0.0.1:18081`,
starts the Vite dev server on `127.0.0.1:4173`, and drives the admin UI through
node/user/proxy creation, access grant/revoke, and soft-delete visibility.

The Playwright config uses `/usr/bin/google-chrome-stable` by default to avoid
downloading a browser. Override it if needed:

```bash
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/path/to/chrome npm --prefix web run test:e2e
```
