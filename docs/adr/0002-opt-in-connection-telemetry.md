# ADR 0002 — Opt-in connection telemetry from sing-box 1.14

- Status: accepted
- Date: 2026-07-27
- Extends: [ADR 0001](0001-network-event-telemetry-source.md)
- Applies to: `internal/singboxapi`, `internal/model/connection_telemetry.go`,
  `internal/agent/connections.go`, `internal/server/db/connection_events.go`,
  `internal/server/render`, `migrations/026_connection_telemetry.sql`

## Decision

Build BoxFleet's side of sing-box 1.14's `SubscribeConnections` stream now,
**before** the pin moves, as a third telemetry source that is **opt-in per node
and off by default**.

- The journalctl regex scraper and `log_events` are unchanged and remain the
  fleet default. `parseSingBoxLogEvent`, its regexes, its golden fixtures and its
  correlation map all stay. Nothing was deleted.
- Per-user traffic accounting stays on the V2Ray counters. This source never
  feeds billing.
- A node streams only when it has an enabled `node_connection_telemetry` row.
  Absence of the row *is* the disabled state, so the default is off structurally
  rather than by convention.

**This remains opt-in activation.** Moving `SING_BOX_REVISION` to
`v1.14.0-beta.2` makes the service available but does not enable it on any node;
an enabled `node_connection_telemetry` row is still required.

The original stable-only trigger was overridden by an explicit operator choice
on 2026-07-28. ADR 0001 records the exception. The preflight remains mandatory,
including the opted-in config and authenticated stream checks added for 1.14.

## Context

ADR 0001 evaluated the Clash API and rejected it, then recorded 1.14's daemon
gRPC stream as the thing worth revisiting for. Its amendment verified that stream
against `v1.14.0-beta.2` in detail: reachable from a headless node via
`service/api`, no build tag, no `experimental.clash_api` needed, `user`
populated for VLESS and multi-user Shadowsocks, and two implementation traps
(`domain` empty on BoxFleet's config, fields 14/15 never populated).

What it did not do is estimate the work. The amendment's own closing argument was
that pinning 1.14 "buys nothing" until a renderer block, per-node secrets, a
node-side aggregator, an ingest path and schema work all exist. That is a large
amount of code to write under the time pressure of a stable release, against an
API whose shape can only be checked by reading upstream.

Writing it now, against the pinned `v1.14.0-beta.2` contract, converts that
release-day work into a review. The constraint is that it must be impossible for
this code to affect the running fleet, because the 1.14 `services` block does not
parse on 1.13 — verified, not assumed: the rendered opted-in config was run
through a real v1.13.14 option parser and fails with
`services[0]: unknown inbound type: api`, while the default-shape config parses
clean.

## Consequences

### The two sources are separate all the way down

ADR 0001 predicted the migration would be "a producer swap into the existing
`model.LogEventInput` shape". That is not what was built, and the prediction was
wrong for a reason worth recording: the stream carries bytes, duration, rule,
outbound and chain, none of which `log_events` has columns for, and it covers a
different set of nodes.

So `connection_events` is its own table with its own wire type
(`model.ConnectionReport`), its own ingest route, its own retention key and its
own admin endpoints, mounted beside `/network-events` rather than merged into it.
A union view would have to fake one half of every row. Which producer a row came
from is a fact the UI must be able to state.

Adding byte columns to `log_events` was rejected independently: a backfill would
be a mass `UPDATE` over the highest-volume table, and every updated row fires
five FTS triggers that delete and re-insert the row's FTS3 document.

### Agents ship pre-aggregated buckets, not raw connections

Raw per-connection rows were estimated at roughly 4M rows/day fleet-wide for ten
nodes at twenty active users each — around 1 GB/day at this row width. Not viable
in one SQLite file and not bufferable on a node. Aggregating on the node by
`(bucket, dimensions)` collapses repeat contact with the same host to roughly
570k rows/day fleet-wide, the same order `log_events` already carries.

The grain is five minutes (`model.ConnectionBucketInterval`), not one: a minute
would be ~4x the rows for resolution nobody needs on a bytes-per-host view, and
the per-minute connection-count chart stays on `log_events`. Agent and server
read the same constant.

### Unattributed rows are kept

`RecordLogEvents` drops rows with no `auth_name`. This path does not: it stores
them with a NULL `proxy_user_id`. Single-user Shadowsocks never populates `user`,
and dropping those rows would silently understate every bytes-per-host total.
Completeness lives in the rows; honesty about attribution lives in the coverage
counters.

### Coverage is a column set, not a disclaimer

Eight counters per report window — observed / attributed / unattributed /
orphaned connections, stream resets, dropped buckets, bytes observed /
attributed — summable over a window through a covering index, and attached to
every API response that carries bytes. This is how "bytes are an estimate" gets
stated with a number.

It cannot be complete: `observable.Subscriber.Emit` drops silently with no error
and no counter, so the largest loss mode is invisible to the counters that
measure loss. See [connection telemetry](../connection-telemetry.md) for how to
read the figure.

### The client can only subscribe

`internal/singboxapi` vendors one RPC out of upstream's 34. The endpoint is a
full control plane — `StopService`, `ReloadService`, `CloseAllConnections`,
`SelectOutbound`, `TriggerDebugCrash` — on the same port and the same secret as
the telemetry stream. Because `protoc-gen-go-grpc` generates client methods from
the descriptor, `StartedServiceClient` has exactly one method and there is no
`StopService` symbol to call. Unreachable by construction, not by discipline.

Trimming is wire-faithful, and the four conditions that make it so are asserted
mechanically against a committed copy of the real upstream descriptor. The
rationale, the regeneration recipe and the re-verification procedure live in
`internal/singboxapi/README.md`.

### The secret is stored in plaintext, deliberately

The renderer emits it into the node config, so the server is the client
presenting this credential, not the verifier of it — the inverse of a node token.
A digest is not an option. The blast radius is bounded by the same database dump
already containing every Reality private key, and by a loopback-only bind
enforced at three layers. Full reasoning in
[connection telemetry](../connection-telemetry.md#the-secret).

The renderer **fails the whole render** for a node whose telemetry row is
malformed rather than degrading, because upstream's `authenticate()` returns nil
for an empty secret — a weak secret would fail open, silently, on a control
plane.

## Known gaps

Recorded rather than hidden, because each is a deliberate stopping point for a
feature nothing is meant to enable yet:

- **No admin API or UI for the opt-in.** The db facade is complete; no HTTP route
  is mounted. Opting a node in means SQL or Go today.
- **The renderer does not gate on sing-box version.** Opting in a 1.13 node
  renders a config that node's `sing-box check` rejects before install. The
  failure is loud and safe — the live config is never touched — but a version
  check against the `sing_box_version` already on every heartbeat would prevent
  it entirely. `telemetry.connections.v1` on the heartbeat says only that the
  *agent* can collect; it says nothing about the sing-box binary.
- **The agent does not act on 403.** The ingest route distinguishes "telemetry is
  disabled" (403) from "bad payload" (422) precisely so a client can stop
  collecting; the agent currently treats both as a failed report.
- **`connection_event_retention_days` is not in `AdminSettings`.** Changeable
  only in the `settings` table.
- **The preflight exercises the stream.** Its fifth check requires the real
  BoxFleet client to receive an authenticated event after the initial reset.

## What this ADR does not change

- ADR 0001's rejection of the Clash API `/connections` endpoint. Every reason
  still holds; nothing here reopens it.
- The preflight's authority over moving `SING_BOX_REVISION`.
- The journal scraper, its golden fixtures, or the rule that a golden diff after
  a pin bump is a regression to investigate rather than regenerate.
- Per-user billing, which stays on the V2Ray counters.
