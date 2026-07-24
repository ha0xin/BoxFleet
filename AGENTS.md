# CLAUDE.md

This file provides guidance to Claude Code/OpenAI Codex when working with code in this repository.

`CLAUDE.md` is a symlink to this file. Keep shared agent guidance here and do
not edit both paths separately.

## What BoxFleet is

A central server (`bfs`, the BoxFleet server) manages users / nodes / proxies / config versions in SQLite and exposes an admin Web UI and a node API. Edge nodes run only `sing-box` + `systemd` + a thin `boxfleet-agent` that pulls config, applies it, and reports heartbeats / traffic / logs. Operators use the admin Web UI. Node-side memory pressure is a hard constraint — do not push databases, panels, or Docker onto nodes.

Current target protocol is VLESS-Reality with `xtls-rprx-vision`; renderer rejects other protocols.

## Common commands

```bash
# Full pre-commit check (build the UI, run all tests, vet)
npm --prefix web run build
go test ./...
go vet ./...

# Run a single Go test
go test ./internal/server/db -run TestProxyAccessIssue -v

# Regenerate sqlc code after changing queries/*.sql or migrations
$(go env GOPATH)/bin/sqlc generate
# If sqlc is missing:
go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

# Local server (admin token is required by default — see "Admin auth")
go run ./cmd/bfs --db /tmp/bf.db --admin-token devtoken
# Or for local development without a token:
go run ./cmd/bfs --db /tmp/bf.db --allow-insecure-admin

# Web UI dev (vite); build output is embedded into the server binary via go:embed
npm --prefix web run dev
npm --prefix web run build

# CI/CD mirrors these checks on GitHub:
# - .github/workflows/ci.yml builds the embedded Web UI, runs Go tests, and runs go vet.
# - .github/workflows/artifacts.yml builds downloadable Linux amd64 artifacts for bfs, boxfleet-agent, and sing-box.
```

## Architecture

### Two binaries, two trust domains

- `cmd/bfs` — central API + admin UI. Owns SQLite, renders sing-box configs, stores published config versions, accepts node reports.
- `cmd/boxfleet-agent` — runs on each node. Pulls config from server, runs `sing-box check`, atomically writes config, restarts `sing-box` via systemctl, reports back. Talks to server with bearer tokens; never trusts node-supplied identity (server overrides `NodeName` in all decoded payloads).

### Server-side data flow

```
queries/*.sql ──sqlc generate──▶ internal/server/store/sqlc/  (typed raw layer)
                                            │
                                            ▼
                                 internal/server/db/  (facade: domain types, validation, ID generation)
                                            │
                                            ▼
                                 internal/server/api/  (chi handlers; admin auth middleware)
                                            │
                                            ▼
                                 internal/server/render/  (DB rows → sing-box JSON config)
```

- `internal/server/db` is the only package allowed to touch sqlc-generated code. Everything else (api and render) consumes the facade's domain types.
- `proxy_details` and `proxy_access_details` SQL views flatten the joins so sqlc generates a single row type per query — this is why no `mapProxy(any)` type-switch exists anymore. Keep it that way: when adding a new proxy/access query, select from the view, not from raw tables.
- API errors: `writeAdminError` returns 422 uniformly. If you need to distinguish NotFound, do it explicitly at the handler.

### Migrations rule

`migrations/010_init.sql` is the public baseline schema. Future migrations are **append-only** once committed: start at `011_*.sql`, never edit a migration that's already in `main`, and keep `schema/schema.sql` as the current full-state snapshot. The baseline intentionally starts at version 10 so existing dev DBs already migrated to v10 remain compatible.

### Agent ↔ server contract

Payload structs live in `internal/model/` and are imported by both `internal/agent` and `internal/server/db`. When changing the wire format, change them there once. `NodeName` fields on `*Report` types are decorative — the server overwrites them with the authenticated node name from the bearer token. Treat them as server-populated.

Agent state (`State` in `internal/agent/agent.go`) tracks v2ray counter values plus a per-counter `CounterEpoch` to detect sing-box restarts (counter goes backwards → epoch++, treat current value as the delta). Do not switch to `reset=true` on v2ray `GetStats` — losing a single response loses traffic.

Node lifecycle and disable semantics: a node is `pending` after bootstrap and becomes `active` on its first authenticated heartbeat (`RecordHeartbeat`). Disabling has two distinct paths that must stay distinct — **pause** (`PATCH /nodes/{node}` status) keeps the token valid; `GET /api/node/config` then returns `X-BoxFleet-Node-State: disabled` plus a valid no-inbound config (`render.RenderDisabledConfig`), and the agent stops `sing-box` while its daemon keeps polling. **Decommission** (`DELETE /nodes/{node}`) additionally revokes tokens (full cutoff). Token verification deliberately does **not** filter on node status — revocation is the kill switch, not the status — so do not re-add a `status != 'disabled'` clause to the node-token queries. The agent decides stop/restart from real `systemctl` `ActiveState`, never a persisted marker.

### Renderer and sing-box

`internal/server/render` produces the full sing-box config JSON. `refs/sing-box/` is a checkout of the upstream sing-box source used for reference only — do not import from it. Only VLESS-Reality is rendered today; adding a protocol means adding a new branch in `RenderNodeConfig` plus matching client-side `NodeInfo` generation.

Traffic counters use sing-box's v2ray API gRPC (`internal/v2raystats` is the client). Counter naming is `user>>><name>>>>traffic>>>{uplink,downlink}` — defined upstream in `refs/sing-box/experimental/v2rayapi/stats.go`. Per-connection metadata (source IP, host, etc.) is **not** exposed by v2ray API; current code scrapes journalctl log text from sing-box (`internal/server/db/log_events.go`) and is fragile by design — sing-box log format changes will break it.

### Web UI

`web/src/` is a Vite+React SPA built into `internal/server/webui/assets/generated/` and served via `go:embed` under `/admin` by the server. `types.ts` (the API contract mirroring the Go db facade) and `navigation.ts` are stable; the presentation layer is built directly on **native Cloudflare Kumo** components. The previous shadcn-shaped compatibility wrappers under `components/ui/` and the Geist `--ds-*` token overrides in `globals.css` have been removed. `internal/server/webui/assets/generated/` is generated output and ignored; run `npm --prefix web run build` before building or testing `bfs` so embedded assets exist locally.

Use the established frontend stack instead of hand-rolling UI behavior. See `docs/web-ui.md` for visual conventions and the Kumo CLI workflow.

- Cloudflare Kumo for all app UI: dialogs, dropdowns, popovers, selects, switches, tables, labels, buttons, inputs, banners, badges, surfaces, grids, and other interactive controls. Import native components from `@cloudflare/kumo` directly; use Base UI primitives re-exported by `@cloudflare/kumo/primitives/*` only when a styled Kumo component is not available.
- **Semantic tokens only** — `bg-kumo-base`, `text-kumo-default`, `bg-kumo-canvas`, `kumo-hairline`, etc. Never raw Tailwind colors and never `dark:` variants (Kumo handles light/dark via `light-dark()`).
- Kumo's CLI is the source of truth and the vendoring mechanism. When unfamiliar with a component or pattern, first run commands from the `web/` directory: `npx kumo ai` for the usage guide, `npx kumo docs <Component>` (or `npx kumo doc <Component>`) for component props/examples, `npx kumo ls` for the catalog, and `npx kumo add <Block>` to copy a block (e.g. `PageHeader`) into `blocksDir` from `kumo.json`. Do not guess Kumo APIs from memory.
- Tailwind v4 is wired through `@tailwindcss/vite`; `web/src/styles/globals.css` keeps `@source` then `@cloudflare/kumo/styles` then `tailwindcss` in that order.
- TanStack Query for API request state, caching, refresh, and invalidation.
- TanStack Table for non-trivial tables, especially paginated/filterable admin data.
- react-hook-form plus zod for form state and validation.
- date-fns plus react-day-picker for date/time formatting, duration math, and date/range picking.
- `@phosphor-icons/react` for app icons (Kumo's own components use Phosphor internally, so the app matches).

For visual checks, run `npm --prefix web run test:e2e` and inspect rendered geometry/computed styles directly. The repository Playwright configuration discovers Chrome on macOS, Linux, and Windows and falls back to bundled Chromium. `refs/kumo/` is a local checkout of Cloudflare Kumo for reference only: use `refs/kumo/packages/kumo-docs-astro/` for demo usage patterns and `refs/kumo/packages/kumo/src/` for component source; do not import from `refs/`.

Network Events is the reference implementation for this stack: the page uses server-side pagination/filtering, URL-synced filters, TanStack Query, TanStack Table, react-hook-form/zod filters, and a react-day-picker date range. Time inputs are interpreted in the browser's local timezone and sent to the server as RFC3339 UTC query parameters. Structured network events are retained for `network_event_retention_days` from `settings` (default 90); raw network log rows are not retained.

Admin pages use client-side routes under the admin mount. Keep sidebar tabs linkable and refresh-safe (`/admin/nodes`, `/admin/network-events`, etc.); the server's web UI handler intentionally falls back to `index.html` for nested admin paths.

## Admin auth

`bfs` requires `--admin-token` (or `BOXFLEET_ADMIN_TOKEN`) by default. `--allow-insecure-admin` disables auth and logs a WARN — only for local dev. The `/api/node/*` paths use per-node bearer tokens issued during Web UI enrollment or re-enrollment and are independent of admin auth.

## Library policy

Use the established libraries listed in README (`cobra`, `viper`, `chi`, `goose`, `sqlc`, `zerolog`, `humanize`, `@cloudflare/kumo`, `@phosphor-icons/react`, etc.). Do not hand-roll command parsing, UUID generation, migration execution, SQL scanning, token hashing, routing, logging, byte-unit parsing, app UI primitives, or protocol clients.

## Tests

Most coverage lives in `internal/agent`, `internal/server/{api,db,render}`, `internal/v2raystats`. Renderer tests are golden-style: render against a fixed SQLite fixture, compare normalized JSON. SQLite tests use `t.TempDir()`. Agent tests fake out the `Runner` interface (`ExecRunner` is replaced) — never spawn real `sing-box` / `systemctl` / `journalctl` in unit tests. Real-node checks belong in the deployment smoke flow, not the regular test suite.

## Docs to consult

- `docs/deployment.md` — artifact-based server and node deployment flow.
- `docs/testing.md` — test boundaries and release checks.
- `docs/architecture.md`, `docs/db-schema.md`, `docs/config-generation.md`, and `docs/web-ui.md` — implementation contracts.
- `deploy/sing-box/README.md` — pinned sing-box build requirements.
