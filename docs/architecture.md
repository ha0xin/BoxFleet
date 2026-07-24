# Architecture

BoxFleet uses a central-control, node-pull model.

```text
admin UI ──HTTP──▶ bfs (BoxFleet server) ──▶ SQLite
                         ▲
                         │ outbound HTTPS
                         │
                  boxfleet-agent ──▶ sing-box
```

## Trust boundaries

`bfs` owns users, nodes, proxies, access grants, configuration
versions, subscriptions, operations, and telemetry. The admin API requires one
operator token. The server is the sole SQLite owner and administrative write
path.

Each agent authenticates with a node-scoped bearer token. The server derives
node identity from that token and ignores identity fields supplied in request
bodies. Nodes make outbound connections only; no public node-management API is
required.

## Data flow

```text
queries/*.sql ──sqlc──▶ internal/server/store/sqlc
                              │
                              ▼
                       internal/server/db
                         │           │
                         ▼           ▼
                 internal/server/api  internal/server/render
```

Only `internal/server/db` may use sqlc-generated types. API, renderer, and tests
consume its domain types. Shared agent/server wire payloads live in
`internal/model`.

The Web UI is compiled into `internal/server/webui/assets/generated` and
embedded in the server. Admin requests use TanStack Query; writes invalidate the
admin query root so inventory and publish status converge from server state.

## Configuration lifecycle

The renderer produces deterministic, complete sing-box JSON from active
database rows. Publishing stores an immutable config version and makes it the
node target. The agent pulls that target, runs `sing-box check`, atomically
installs it, restarts sing-box, and reports the result. It never edits live JSON
with string replacement.

The agent reads cumulative V2Ray API counters. A fresh state establishes a
baseline; counter regression starts a new epoch. This avoids losing traffic
when a report fails or sing-box restarts.

## Node lifecycle

- `pending`: enrolled but not yet authenticated; excluded from render/publish.
- `active`: promoted by the first authenticated heartbeat.
- `disabled`: administratively paused or decommissioned.
- `degraded`: active but unhealthy.

Pause and decommission are deliberately different:

- Pause keeps the token valid. The config endpoint returns a disabled header and
  a valid no-inbound config; the daemon keeps polling while sing-box stays off.
- Decommission also revokes tokens, cutting off the daemon. The retained row may
  later be re-enrolled.

Token verification checks revocation, not node status.

## Durable node operations

Agents long-poll for typed, allow-listed operations stored in SQLite. Claims use
leases; progress events are sequenced and idempotent; local checkpoints survive
agent restarts. Update payloads may reference only assets from the formal
release catalog. See [node operations](node-operations.md).

Server, agent, and sing-box versions are independent. A server release advertises
the pinned component targets from its build, so unchanged node components do not
appear outdated.

## Constraints

- Nodes run no database, Docker daemon, panel, or monitoring stack.
- SQLite uses WAL, a busy timeout, explicit migrations, and bounded queries.
- The supported renderer protocol is VLESS-Reality with
  `xtls-rprx-vision`; adding a protocol requires server rendering, client output,
  validation, and tests together.
- Multi-admin authentication, quota enforcement, and rate limiting are not
  implemented.
