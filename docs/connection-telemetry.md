# Connection telemetry (sing-box 1.14)

sing-box 1.14 exposes a daemon gRPC stream, `SubscribeConnections`, that reports
every connection with its user, destination, byte totals and duration on one
message. BoxFleet can consume it. **Nothing consumes it today.**

This path is **opt-in per node and off by default**, and it must stay that way
until the pin moves:

- `SING_BOX_REVISION` in `.github/workflows/artifacts.yml` is `v1.13.14`. The
  production fleet runs 1.13.
- 1.13's config parser rejects the `services` block this stream needs —
  `services[0]: unknown inbound type: api`. Rendering it unconditionally would
  break every node.
- The journalctl regex scraper (`log_events`) is unchanged, still the fleet
  default, and still the only source that covers every node. The two coexist
  permanently; neither replaces the other in this release.

The decision to build it now, ahead of the switch, is
[ADR 0002](adr/0002-opt-in-connection-telemetry.md). The unchanged trigger for
actually switching the fleet is in
[ADR 0001](adr/0001-network-event-telemetry-source.md): the `v1.14.0` **stable**
tag published *and* a candidate build passing
[the preflight](singbox-preflight.md). Both.

## What it adds

`log_events` cannot answer bytes-per-destination — it has no byte columns, and
`traffic_usage_deltas` has no host column. `connection_events` can, for the
nodes that stream:

| | journal scraper | connection stream |
| --- | --- | --- |
| Node coverage | every node | opted-in nodes only |
| Bytes per destination host | no | yes (estimate) |
| Session duration | no | yes |
| Connection close observed | no | yes |
| Rule / outbound / chain | no | yes |
| Per-user billing | no | **no** — stays on the V2Ray counters |

Byte figures from this source are an **estimate, not a ledger**. See
[Reading the coverage figure](#reading-the-coverage-figure).

## Opting a node in

> **There is no admin API or UI for this yet.** The `internal/server/db` facade
> is complete (`SetNodeConnectionTelemetry`, `RotateNodeConnectionTelemetrySecret`,
> `DeleteNodeConnectionTelemetry`, `NodeConnectionTelemetryConfig`) but no HTTP
> route is mounted, so the opt-in is reachable only from Go or from SQL against
> the server database. That is deliberate for a path nothing should be enabling
> yet, and it is the first thing to build when the trigger fires.

Absence of a `node_connection_telemetry` row *is* the disabled state, so the
fleet-wide default is off structurally rather than by convention.

```sql
-- Opt one node in. hex(randomblob(32)) is a 64-character secret; the schema
-- CHECK refuses anything under 32.
INSERT INTO node_connection_telemetry (node_id, enabled, listen_address, listen_port, secret)
SELECT id, 1, '127.0.0.1', 9091, lower(hex(randomblob(32)))
FROM nodes WHERE name = 'azus';
```

Then publish. The change surfaces through the normal pipeline —
`GET /api/admin/config/changes` re-renders every node and hash-compares against
its published target, so the node appears as changed and
`POST /api/admin/config/publish` (or the admin UI's apply) publishes it. The node
picks it up on its next poll.

The rendered config gains exactly one block:

```json
"services": [
  {
    "type": "api",
    "tag": "boxfleet-telemetry",
    "listen": "127.0.0.1",
    "listen_port": 9091,
    "secret": "<64 hex characters>"
  }
]
```

No `dashboard` and no TLS container: the first would serve a web UI off the node,
the second would put a certificate on a loopback listener that does not need one,
and node memory is a hard constraint.

The agent needs no configuration. It discovers the endpoint by reading the
`services[]` entry out of the sing-box config it applied — that file is the
authoritative statement of what sing-box is running, so enable and disable are
pure renderer decisions with no second distribution channel to keep in sync.

Two invariants are enforced at three layers (schema CHECK, db facade, renderer),
and the renderer **fails the whole render** rather than emitting a block that
violates either:

- **Loopback only.** `listen` must be a loopback IP literal. `localhost` is
  rejected — sing-box parses `listen` as a `netip.Addr`, so a hostname would
  store a value the renderer could only emit as an invalid config.
- **Secret at least 32 characters.** sing-box's `authenticate()` returns nil for
  an empty secret, so a missing secret does not fail closed upstream; it disables
  authentication entirely.

A hand-edited row that violates either one makes `RenderNodeConfig` error for
that node, and `GET /api/admin/config/changes` renders every node — so one bad
row breaks the fleet-wide config-changes and bulk-publish views until it is
fixed. Prefer the facade over raw SQL.

## The secret

**Lifecycle.** Minted on first `SetNodeConnectionTelemetry`, 32 random bytes
rendered as 64 hex characters, from the same `internal/secret` package that mints
Reality material. Disabling a node **keeps** the secret, so re-enabling does not
force a config change. `RotateNodeConnectionTelemetrySecret` replaces it and
stamps `rotated_at` without touching the enabled flag or the endpoint; the node
picks the new value up on its next config apply, and the collector is briefly
unauthenticated in between — that gap is expected and shows up as a stream reset
in the coverage counters. `DeleteNodeConnectionTelemetry` removes the row and
returns the node to the structural default.

**It is stored in the clear, and it has to be.** The renderer emits it into the
node config, so the server is the *client* presenting this credential to sing-box,
not the verifier of it. That is the exact inverse of a node token, which is
hashed precisely because the server only ever calls `token.Verify`. A digest here
is not a weaker option; it is a non-option.

**Blast radius, stated plainly.** If the server database leaks, the attacker
already holds every Reality private key and Shadowsocks server password
(`proxies.settings_json`), every per-user credential
(`proxy_accesses.credential_json`), and every rendered config containing all of
them (`config_versions.config_json`). The telemetry secret adds gRPC access to a
**loopback-bound** endpoint, which is unreachable without already having code
execution on that node — and anyone with node-local code execution can
`systemctl stop sing-box` without any secret at all. The marginal exposure is
approximately zero relative to what leaks in the same dump.

That is not an argument for casualness, because the endpoint is a **control
plane**, not read-only telemetry: the same port and the same secret carry
`StopService`, `ReloadService`, `CloseAllConnections`, `SelectOutbound` and
`TriggerDebugCrash`. Two things contain it. The bind is loopback-only, enforced
above. And BoxFleet's client cannot call any of those RPCs — `internal/singboxapi`
vendors one RPC, so `StartedServiceClient` has exactly one method and there is no
`StopService` symbol to call. That is a compile-time property, not a review
convention.

**Never put the secret in an admin API response.** `NodeConnectionTelemetry`
carries it because the renderer needs it;
`GET /api/admin/connection-events/nodes` deliberately projects it away. Note that
`GET /api/admin/nodes/{node}/config/render` *does* return it for an opted-in
node — already true of Reality private keys on that same endpoint, but worth
being deliberate about.

## What happens on a 1.13 node

Nothing silently. The renderer does **not** check the node's sing-box version or
its advertised capability, so opting in a 1.13 node produces a config that node
cannot parse. The failure is loud and safe:

1. The agent fetches the new config and writes it to `<config>.candidate`.
2. `sing-box check -c <config>.candidate` fails with
   `services[0]: unknown inbound type: api`.
3. The candidate is deleted. **The live config is never touched** — this is a
   pre-install check, not a rollback, so sing-box keeps running exactly what it
   was running.
4. The agent reports `apply-result` as `failed` with the check output, and
   retries on every poll until the opt-in is reverted or the node is upgraded.

The node keeps serving traffic throughout, and keeps producing journal-scraped
`log_events`. The cost is a stuck config target and a node that shows as failed
in the admin UI.

Agents that ship the collector advertise `telemetry.connections.v1` on the
heartbeat. That says only *this agent knows how to collect the stream* — never
*this node's sing-box can serve it*. The `sing_box_version` already carried on
every heartbeat is the better gate, and a version check in the renderer is the
obvious hardening when an admin endpoint is built.

## Reading the coverage figure

Every response that carries bytes also carries a `coverage` block, so a client
physically cannot render the estimate without the figure that qualifies it.

| Field | Meaning |
| --- | --- |
| `connections_observed` | Connections the collector saw at all |
| `connections_attributed` | …that carried a `user` |
| `connections_unattributed` | …that did not. Single-user Shadowsocks never attributes |
| `connections_orphaned` | Closes seen without the matching open — a gap in the stream, recovered in full from the close event |
| `stream_resets` | Subscriptions lost and re-established. Non-zero means sing-box restarted or the stream broke |
| `dropped_buckets` | Aggregates the agent discarded on hitting its own map cap. Non-zero means the node is busier than the collector is sized for |
| `bytes_observed` / `bytes_attributed` | The same two populations by volume |
| `attribution_ratio` | `bytes_attributed / bytes_observed`, in `[0,1]` |
| `reports` | Report windows summed into this figure |

**`attribution_ratio` is by bytes, not by connection count** — a handful of
unattributed bulk transfers matters more than many unattributed idle connections.
**An empty window reports `1`**: nothing was observed, so nothing was lost, and
rendering 0% coverage for an idle node would be misleading.

**Coverage is a property of a node's stream, not of a credential.**
`connection_reports` has no user column, so filtering by `user` narrows the byte
rankings but leaves the coverage figure node- or fleet-wide. Read it as "how
complete is this node's telemetry", not "how much of this user's traffic we saw".

Three structural loss modes upstream are why this is an estimate at all — a full
subscriber buffer, an evicting closed-connection ring, and identity reset on
restart. They are enumerated with line references in
[`internal/singboxapi/README.md`](../internal/singboxapi/README.md#what-the-stream-is-and-is-not).
**The largest of them is invisible to `coverage`**: the buffer drop is silent,
with no error and no counter, so a stellar attribution ratio is not proof that
nothing was missed.

Rules of thumb: `attribution_ratio` below ~0.9 on a VLESS-only node means
something is wrong, not that users are anonymous — VLESS always populates `user`.
On a node with single-user Shadowsocks, a permanently reduced ratio is expected
and correct. `dropped_buckets` growing steadily is a sizing problem, not a bug.
`stream_resets` tracking sing-box restarts one-for-one is normal.

**Per-user billing never reads this.** It stays on the V2Ray counters, whose
`CounterEpoch` design already survives restarts and dropped reports.

## Reading the data

Admin endpoints, all behind `adminAuthMiddleware`. They sit beside the
`/network-events` family and are never merged with it: which producer a row came
from is a fact the UI has to be able to state, and only one of the two covers the
whole fleet.

| Route | Purpose |
| --- | --- |
| `GET /api/admin/connection-events` | Paged aggregate rows, newest bucket first |
| `GET /api/admin/connection-events/series` | Bucketed volume, zero-filled, plus totals and coverage |
| `GET /api/admin/connection-events/hosts` | Bytes or connections per destination host |
| `GET /api/admin/connection-events/nodes` | Which nodes actually stream. An empty list is today's normal fleet-wide answer |

Shared filters: `node`, `user`, `host`, `start`, `end`. There is deliberately no
`action` (the stream carries no classified action) and no `search`
(`connection_events` has no full-text index). `start` and `end` are **required**
on `/series` — an unbounded window cannot be zero-filled or span-clamped — and
optional elsewhere. `/series` also takes `bucket` (`hour|day`) and
`offset_minutes`, with the same span ceilings as every other series.
`/hosts` takes `sort=bytes|connections` (default `bytes`, unknown values are 422)
and `limit` (default 20, max 100), and returns `distinct_hosts` plus `truncated`
so a partial ranking cannot read as a complete one.

`connections_opened` and `connections_closed` are separate on purpose: a
long-lived session contributes bytes to several consecutive buckets, so summing
"connections" must use `opened`.

The Network Events page reads these through a **Connection stream** panel. It
queries `/connection-events/nodes` first and renders nothing but a short
explanation when no node is opted in — which is every node today — so the panel
never shows as an empty table that reads like breakage. When a node is opted in
the panel makes clear it covers only those nodes, not the fleet, because the
journal-based table above it covers everything.

Byte figures in that panel always carry the attribution ratio from the coverage
counters. Do not present them without it: they under-count by an amount that
varies with load and cannot be bounded.

## Rolling back

```sql
UPDATE node_connection_telemetry
SET enabled = 0
WHERE node_id = (SELECT id FROM nodes WHERE name = 'azus');
```

Then publish, as for opting in. The rendered config returns to **byte-identical**
pre-1.14 output — `services` is `omitempty` and nothing else changes — which
`TestRenderNodeConfigOptOutRestoresDefaultBytes` pins. The node applies it
normally, and the agent's next poll sees a config with no api service and stops
the collector.

**Publishing is not optional here.** The collector follows the config on disk,
not the database row, so a node that is disabled in the database but still
running the old config keeps collecting and keeps POSTing — and the server
answers 403 to every one of those reports. The window stays staged, the collector
keeps aggregating up to its caps, and nothing is lost or leaked, but the node
reports a failing step on every poll until the new config lands.

Rolling back does not delete collected data. `connection_events` and
`connection_reports` age out on their own retention (below), or can be deleted
outright; nothing else reads them.

To remove the opt-in entirely rather than disable it, delete the row. It
cascades from `nodes`, so decommissioning a node cleans up after itself.

## Retention

`connection_event_retention_days`, default **14**, bounds 1..3650. Shorter than
`network_event_retention_days` (90) because `connection_events` carries a wider
row at a finer dimension tuple and accumulates faster for the same traffic.

Applied inline on ingest, in the same transaction as the write, exactly as
`RecordLogEvents` does — there is no scheduler on the server.
`connection_events` prunes on `bucket_start`, `connection_reports` on
`window_end`.

The setting is not in `AdminSettings` and not on the settings PATCH handler yet,
so it is only changeable in the `settings` table directly. Same gap as the admin
opt-in endpoint, same reason.

## Node cost

Bounded by construction, because node memory is a hard constraint:

| Bound | Value | On hitting it |
| --- | --- | --- |
| Live connection identities | 4096 (~1.6 MB) | Connection refused, `dropped_buckets`++. Recovered in full from its close event |
| Pending aggregation buckets | 2000 | Bucket dropped, `dropped_buckets`++, its bytes excluded from `bytes_observed` so the denominator stays honest |
| Accounted close ids | 2048 | Oldest evicted (FIFO). Larger than sing-box's 1000-entry replay ring, so a replay within one agent run can never double count |

sing-box's own tracker spends roughly 1 KB per live connection, so a node at the
identity cap is already spending ~4 MB upstream before the collector's share.

The collector paces traffic updates at 5 seconds rather than sing-box's 1-second
default: data is bucketed at 5 minutes, so a 1-second tick on a busy node is
thousands of proto messages per second for no resolution gain. It loses no bytes
on its own, because a connection's final totals arrive on its close event.

The collector never touches sing-box supervision or the config-apply loop. Its
`Run` returns no error and is not wired into the agent's error channel; every
failure is logged once per outage and swallowed. Stream errors back off 1s→30s.

## For developers

**The gRPC client and the vendored proto.**
[`internal/singboxapi/README.md`](../internal/singboxapi/README.md) is the single
home for the pinned upstream revision, what is vendored and what is deliberately
not, why trimming to one RPC is wire-faithful, the exact regeneration commands,
and the procedure for re-verifying the contract against a newer sing-box tag.
Read it before touching anything under `internal/singboxapi/`. Do not restate its
contents elsewhere.

**Testing.** Every test in this feature runs in-process against a fake gRPC
daemon; none spawns a real `sing-box`. See
[testing](testing.md#connection-telemetry) for the boundaries and the pinned
invariants.

**Schema.** Three tables, their indexes and the ingest idempotency rule are in
[db-schema](db-schema.md#connection-telemetry).

**Rendering.** The conditional `services` block is in
[config generation](config-generation.md#connection-telemetry-service-optional).

**Wire contract.** `internal/model/connection_telemetry.go` holds the payload
shared by agent and server, plus the normalisation both sides must agree on
(`Normalize`, `DimensionKey`, `TruncateConnectionBucket`). The bucket grain is
`ConnectionBucketInterval` (5 minutes) and both sides read that constant; never
re-derive it.

Ingest is `POST /api/node/connections`, idempotent on
`(agent_boot_id, sequence)` — resending an identical body after a network
failure is a no-op that returns 200. The handler checks the opt-in *before*
decoding and answers **403** when telemetry is disabled for the node, split out
from the 422 every other store rejection produces so a client can tell "the
operator turned this off" from "retry". The agent does not act on that
distinction yet: it treats 403 as an ordinary failed report and keeps the window
staged. Consuming it is the obvious next step.
