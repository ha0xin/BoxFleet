# Connection telemetry

BoxFleet consumes sing-box's daemon gRPC `SubscribeConnections` stream for
user, destination, cumulative bytes and session timing. It is **opt-in per node
and off by default**. The installed sing-box build must support the daemon API;
check release component targets in `.github/workflows/artifacts.yml` and exact
artifact versions in the release manifest.

The journal collector remains available during migration. Per-node source
selection prevents duplicate counts; rollout and collection verification belong
in deployment records. Historical rationale is in
[ADR 0001](adr/0001-network-event-telemetry-source.md) and
[ADR 0002](adr/0002-opt-in-connection-telemetry.md).

## Unified Logs

Network Events now displays journal and connection-stream records in **Logs**.
The former `?view=connections` URL redirects to Logs. The table, connection-start chart,
search, user/node filters and Service activity use the same server-side source selection.
Missing journal byte, protocol and duration fields display `—`; their API numeric
values are `null`. Existing aggregate history retains its Count and cannot be
reconstructed into individual connections.

Updated agents send one record per session, keyed by authenticated node ID,
sing-box connection ID and creation time. NEW / UPDATE / CLOSED update that row;
lifetime byte totals merge by maximum, including after agent restart. Reports
remain bounded and retryable. Coverage retains interval byte deltas; billing
continues to use V2Ray counters. Old agents' five-minute reports are accepted
until they are upgraded; those existing aggregates remain aggregates.

A node switches its primary Logs source on its first accepted stream report,
not merely when its version changes or the opt-in configuration is written.
The server stores source intervals: journal history preceding the switch remains
visible, and journal records in a streaming interval are excluded from all unified
reads. Disabling/removing the opt-in ends the interval and resumes journal
visibility. A temporary stream failure does not silently fall back and double
count journal rows. Raw source tables are preserved during migration.

Deploy the server migration first, then upgrade agents to get individual records.
This change does not opt nodes in automatically, delete old logs, or change
user traffic counters. The connection-only list remains available for diagnostics.
Interval byte series and host rankings are retired (HTTP 410): cumulative session
totals cannot measure bytes transferred inside a selected time window.

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

There is no admin mutation API or UI for enabling this feature. The db facade
provides `SetNodeConnectionTelemetry`, `RotateNodeConnectionTelemetrySecret`,
`DeleteNodeConnectionTelemetry` and `NodeConnectionTelemetryConfig`. Use the
facade where possible; direct SQL requires equivalent validation.

Absence of a `node_connection_telemetry` row *is* the disabled state, so the
fleet-wide default is off structurally rather than by convention.

```sql
-- Opt one node in. hex(randomblob(32)) is a 64-character secret; the schema
-- CHECK refuses anything under 32.
INSERT INTO node_connection_telemetry (node_id, enabled, listen_address, listen_port, secret)
SELECT id, 1, '127.0.0.1', 9091, lower(hex(randomblob(32)))
FROM nodes WHERE name = 'example-node' AND deleted_at IS NULL;
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

The db facade and renderer enforce loopback binding and secret length. Schema
CHECK constraints also reject empty addresses, invalid ports and short secrets.
The renderer **fails the whole render** rather than emitting an unsafe block:

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
Reality material. Disabling telemetry **keeps** the secret, so re-enabling can reuse it.
`RotateNodeConnectionTelemetrySecret` replaces it and
stamps `rotated_at` without touching the enabled flag or the endpoint; the node
picks the new value up on its next config apply, and the collector is briefly
unauthenticated in between — that gap is expected and shows up as a stream reset
in the coverage counters. `DeleteNodeConnectionTelemetry` removes the row and
returns telemetry to the structural default.

**Storage and access.** The secret is stored in plaintext because it must be
rendered into the node configuration. Node bearer tokens instead use a
nonreversible verifier: the server verifies them rather than redistributing them.
Protect database backups and rendered configuration as credential material.

The daemon endpoint is a full control plane: the same port and secret authorize
`StopService`, `ReloadService`, `CloseAllConnections`, `SelectOutbound` and
`TriggerDebugCrash`. Loopback binding limits network exposure but does not
protect it from every local process. BoxFleet's vendored client exposes only
`SubscribeConnections`; this restricts BoxFleet's calls, not the upstream server's
RPC surface.

**Telemetry inventory responses must omit the secret.** `NodeConnectionTelemetry`
carries it because the renderer needs it;
`GET /api/admin/connection-events/nodes` deliberately projects it away. Note that
`GET /api/admin/nodes/{node}/config/render` *does* return it for an opted-in
node — already true of Reality private keys on that same endpoint, but worth
being deliberate about.

## Unsupported installed builds

The renderer does not check reported sing-box versions or agent capabilities
before rendering the optional service. Enabling a node whose binary does not
support it causes candidate validation to fail:

1. The agent writes a candidate configuration.
2. The installed `sing-box check` rejects the unsupported configuration.
3. The candidate is discarded without replacing the live configuration.
4. The agent reports the apply failure; revert activation and publish, or install
   a compatible component before retrying.

A collector-capable agent advertises `telemetry.connections.v1`. This does not
prove that the installed sing-box can serve the stream. Validate the actual
build and stream before rollout.

## Reading the coverage figure

Report diagnostics expose coverage counters. Unified session rows expose lifetime
bytes without embedding a coverage block in each row. Treat those bytes as
best-effort observations and consult collection diagnostics when assessing quality.

| Field | Meaning |
| --- | --- |
| `connections_observed` | Connections the collector saw at all |
| `connections_attributed` | …that carried a `user` |
| `connections_unattributed` | …that did not. Single-user Shadowsocks never attributes |
| `connections_orphaned` | Closes seen without the matching open — a gap in the stream, recovered in full from the close event |
| `stream_resets` | Subscriptions lost and re-established. Non-zero means sing-box restarted or the stream broke |
| `dropped_buckets` | Identities, snapshots or legacy buckets the agent discarded on hitting its caps. Non-zero means the node is busier than the collector is sized for |
| `bytes_observed` / `bytes_attributed` | The same two populations by volume |
| `attribution_ratio` | `bytes_attributed / bytes_observed`, in `[0,1]` |
| `reports` | Report windows summed into this figure |

**`attribution_ratio` is by bytes, not by connection count** — a handful of
unattributed bulk transfers matters more than many unattributed idle connections.
**An empty observed window reports `1`** by convention; this is not evidence that
all real traffic was collected.

**Coverage is a property of a node's stream, not of a credential.**
`connection_reports` has no user column, so user filters narrow connection
rows, while report coverage remains node- or
fleet-wide. Attribution measures how much observed traffic had a user, not how
complete collection was.

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
`CounterEpoch` handling tracks counter regression and retries failed reports; it
cannot recover unread bytes after sing-box exits.

## Reading the data

Admin endpoints are behind `adminAuthMiddleware`. `/network-events` is the
unified Logs contract, including `source`, nullable rich fields, stable IDs and
`event_time`. List filters, connection-start charts and service/host counts all
use the half-open interval **[start, end)** on `event_time`: session creation time
for individual connections, the first observation (`window_start`) for journal
and legacy aggregate records. Updating a session does not move its start or
create another connection. `window_end` is the last observation, not the chart
bucket. Legacy Count still represents several connections with one observation
window; it cannot be reconstructed into precise individual start times.

The UI labels per-session bytes as **lifetime** totals, even when the selected
window covers only part of the session. Legacy aggregates have no per-session
lifetime total and display `—` in those columns. An open status means “last
observed open”, not proof that the connection is still alive.

| Route | Purpose |
| --- | --- |
| `GET /api/admin/connection-events` | Diagnostic source rows, newest bucket first; legacy rows contain interval deltas, session rows contain lifetime totals |
| `GET /api/admin/connection-events/series` | Retired; HTTP 410 with migration guidance |
| `GET /api/admin/connection-events/hosts` | Retired; HTTP 410 with migration guidance |
| `GET /api/admin/connection-events/nodes` | Nodes with telemetry enabled |

The diagnostic list accepts `node`, `user`, `host`, optional `start`/`end`
(observation-window overlap), `limit` and `offset`. It is not an interval traffic
analytics API. Use `/network-events/series`, `/network-events/services` and
`/network-events/hosts` for connection-start counts; use `/traffic/series` for
billing traffic. Destination traffic analytics would require a separately stored,
idempotent sequence of byte increments, with coverage and retention semantics.
It must not be derived by bucketing mutable lifetime snapshots.

## Rolling back

```sql
UPDATE node_connection_telemetry
SET enabled = 0
WHERE node_id = (SELECT id FROM nodes WHERE name = 'example-node' AND deleted_at IS NULL);
```

Then publish, as for opting in. The rendered config returns to the default
shape without the optional service — `services` is `omitempty` — which
`TestRenderNodeConfigOptOutRestoresDefaultBytes` pins. The node applies it
normally, and the agent's next poll sees a config with no api service and stops
the collector.

**Publishing is not optional here.** The collector follows the config on disk,
not the database row, so a node whose telemetry is disabled in the database but still
running the old config keeps collecting and keeps POSTing — and the server
answers 403 to every one of those reports. The window stays staged, the collector
keeps buffering up to its caps and may then discard new data. The node
reports a failing step on every poll until the new config lands.

Rolling back does not delete collected data. `connection_events` and
`connection_reports` age out on their own retention (below), or can be deleted
outright; unified Logs continues to read retained historical data.

To remove the opt-in entirely rather than disable it, delete the row. Removing
activation closes its source interval; it does not delete telemetry
history. Soft-deleting a node is distinct from physically deleting its row.

## Retention

`connection_event_retention_days`, default **14**, bounds 1..3650. Shorter than
`network_event_retention_days` (90) because `connection_events` carries a wider
row at a finer dimension tuple and accumulates faster for the same traffic.

Applied inline on ingest, in the same transaction as the write, exactly as
`RecordLogEvents` does — there is no scheduler on the server.
Legacy `connection_events` rows prune on `bucket_start`; session rows prune on
`window_end` so a long-lived active session is retained. `connection_reports` prune on
`window_end`.

The setting is not in `AdminSettings` and not on the settings PATCH handler yet,
so it is only changeable in the `settings` table directly. Use the same care as for direct activation edits.

## Node cost

Bounded by construction, because node memory is a hard constraint:

| Bound | Value | On hitting it |
| --- | --- | --- |
| Live connection identities | 4096 (~1.6 MB) | Identity tracking dropped, `dropped_buckets`++; a later close may recover metadata |
| Pending session snapshots | 2000 | Snapshot dropped, `dropped_buckets`++, its bytes excluded from `bytes_observed` so the denominator stays honest |
| Accounted close ids | 2048 | Oldest evicted (FIFO). Larger than sing-box's 1000-entry replay ring, to reduce duplicate close accounting within an agent run |

sing-box's own tracker spends roughly 1 KB per live connection, so a node at the
identity cap is already spending ~4 MB upstream before the collector's share.

The collector requests periodic traffic updates to reduce event overhead.
Session snapshots retain cumulative totals, and close events can supply final
totals when received. A missed close, lost stream event or process restart can
still leave gaps; the update interval does not guarantee complete bytes.

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

**Schema.** Activation, reports, session rows and source selection are in
[db-schema](db-schema.md#connection-telemetry).

**Rendering.** The conditional `services` block is in
[config generation](config-generation.md#connection-telemetry-service-optional).

**Wire contract.** `internal/model/connection_telemetry.go` defines the shared
payload and normalization. Updated agents send cumulative session snapshots;
`ConnectionBucketInterval` and bucket truncation apply to the legacy aggregate
format. Do not use that grain to reinterpret individual session starts or totals.

Ingest is `POST /api/node/connections`, idempotent on
`(agent_boot_id, sequence)` — resending an identical body after a network
failure is a no-op that returns 200. The handler checks the opt-in *before*
decoding and answers **403** when telemetry is disabled for the node, split out
from the 422 every other store rejection produces so a client can tell "the
operator turned this off" from "retry". The agent does not act on that
distinction yet: it treats 403 as an ordinary failed report and keeps the window
staged. Consuming it is the obvious next step.
