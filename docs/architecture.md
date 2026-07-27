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

Node report bodies are bounded so a node token cannot exhaust server memory:
64 KiB for fixed-shape apply results and heartbeats, 1 MiB for journal and
traffic batches, 256 KiB for operation progress, 2 MiB for connection telemetry
(the ingest caps a batch at 2000 pre-aggregated buckets). Over-limit requests
get 413.
The HTTP server sets read, write, and idle timeouts and shuts down gracefully.

Read paths that decide whether a node keeps serving fail closed. A `GetNode` or
config-status lookup failure on `GET /api/node/config` returns 500 rather than
falling through to the enabled path, which would restart sing-box on a paused
node or bypass the publish workflow with a live render.

Writes whose halves must not diverge are transactional: node decommission
commits the soft delete with its token revocation, and path revocation and user
soft delete commit with the credential teardown they trigger. Restoring a
revoked credential always mints a fresh secret — nothing in the schema separates
a leak-driven revocation from a routine unused-credential disable.

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

The agent keeps the configuration sing-box was last observed running. After a
restart it verifies the service actually came up; if the new config fails to
apply or the service does not come up, the agent restores the last-good config
and reports the failure. The daemon's own hiccup cannot trigger a rollback of a
healthy config.

The agent requires `https` for the server and bootstrap URLs. Plaintext `http`
is accepted only behind the explicit `allow_insecure_transport` development
opt-out.

## Telemetry

Two pipelines run on every node, and they share only
`(node, user, approximate time)`:

- **Traffic.** The agent reads cumulative V2Ray API counters. A fresh state
  establishes a baseline; counter regression starts a new epoch. This avoids
  losing traffic when a report fails or sing-box restarts. This is the only
  source per-user billing reads.
- **Network events.** The agent ships journal text from sing-box; the server
  parses it with regexes into per-minute aggregated `log_events`.

Neither can be joined to the other at connection granularity, so on these two
sources bytes are never attributable to a destination host. The choice to keep
both — and the conditions under which the journal parser is replaced — is
[ADR 0001](adr/0001-network-event-telemetry-source.md).

A third pipeline exists and is **opt-in per node, off by default, and enabled
nowhere**: sing-box 1.14's daemon gRPC connection stream, aggregated on the node
into `connection_events`. It answers bytes-per-destination and session duration,
which the other two structurally cannot, for the nodes that run it. It cannot be
turned on fleet-wide — the fleet runs 1.13, whose parser rejects the config block
it needs — and it never feeds billing. Rationale is
[ADR 0002](adr/0002-opt-in-connection-telemetry.md); operation and contracts are
in [connection telemetry](connection-telemetry.md).

Bucketing and zero-fill are server concerns. `internal/server/db/series_common.go`
is the single source of bucket truth (granularity derivation, span ceilings,
offset validation, the SQL grouping expression, and the Go bucket walk), and
`buildLogEventPredicates` in `log_events.go` is the single source of log-event
filter truth — the paged table, the bucketed series, and the service breakdown
all filter through it, so a chart cannot silently disagree with the table beneath
it.

`internal/servicecatalog` classifies a destination host to a service at read
time. It is a pure library: no sqlc, no `internal/server/db` import, no runtime
network access. Its dataset is `data/services.tsv` (~313 KB, 156 services,
~18k rules), generated by `go run ./internal/servicecatalog/gen` from
`v2fly/domain-list-community` (MIT) and committed via `go:embed`; regeneration is
manual and documented in `internal/servicecatalog/gen/README.md`. Lookup is a
right-to-left label walk over a map — longest suffix wins — with
`golang.org/x/net/publicsuffix` as the eTLD+1 fallback and IP-literal detection
ahead of both. Operator overrides from `domain_service_overrides` derive a new
catalog per request; the embedded default is immutable and shared.

Admin telemetry endpoints, all under the admin prefix and behind
`adminAuthMiddleware`:

| Route | Purpose |
| --- | --- |
| `GET /api/admin/traffic/series` | Bucketed uplink/downlink bytes, `group=total\|user\|node` |
| `GET /api/admin/network-events/series` | Bucketed connection counts plus the unbucketed action histogram, `group=total\|action\|node\|user` |
| `GET /api/admin/network-events/services` | Connections and distinct hosts per service or category |
| `GET /api/admin/network-events/hosts` | Per-host drill-down with its classification and match source |
| `GET /api/admin/connection-events{,/series,/hosts,/nodes}` | The opt-in 1.14 stream — see [connection telemetry](connection-telemetry.md#reading-the-data) |
| `GET/PUT /api/admin/service-overrides`, `DELETE .../{suffix}` | Operator classification overrides |

`start` and `end` are RFC3339 and **required** on both `/series` endpoints — an
unbounded window cannot be zero-filled or span-clamped. They are optional on
`/services` and `/hosts`, which are bounded by a 200k distinct-host scan cap
instead. Shared parameters are `bucket` (`hour|day`, derived from the span when
absent), `offset_minutes`, `group`, and `limit`. The network-event endpoints take
the same `node`, `user`, `action`, and `search` filters as the paged
`/api/admin/network-events` table, with byte-identical semantics.

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
- The supported renderer protocols are VLESS-Reality with `xtls-rprx-vision` and
  Shadowsocks 2022; adding a protocol requires server rendering, client output,
  validation, and tests together.
- Multi-admin authentication, quota enforcement, and rate limiting are not
  implemented.
