# ADR 0001 — Network event telemetry source

- Status: **accepted and current for the fleet default**; amended 2026-07-26 (see
  "Amendment"); extended by
  [ADR 0002](0002-opt-in-connection-telemetry.md) (2026-07-27)
- Date: 2026-07-26
- Applies to: `internal/server/db/log_events.go`, `internal/v2raystats`,
  `internal/agent`, `internal/server/render`

> **Read this with [ADR 0002](0002-opt-in-connection-telemetry.md).** A third
> telemetry source — the 1.14 daemon gRPC connection stream described under
> "Trigger for revisiting" below — now exists in the tree as an **opt-in per
> node, off by default** path. Everything this ADR decides about the *default*
> path is still in force, and the trigger for actually switching the fleet is
> unchanged. No node is opted in.

## Decision

Keep both existing telemetry sources unchanged:

- **Network events** (source IP, destination host/port, per-minute connection
  counts) continue to come from `journalctl` text scraped off `sing-box` and
  parsed by `parseSingBoxLogEvent` in `internal/server/db/log_events.go`.
- **Traffic accounting** (per-user uplink/downlink bytes) continues to come from
  the V2Ray API gRPC stats counters read by `internal/v2raystats`.

Do not enable `experimental.clash_api` in rendered node configs, and do not
build a second collector against it.

## Context

The audit and charting work in this release reads `log_events.target_host`,
which exists only because of that regex parser. Before building on it, the Clash
API alternative was evaluated against the sing-box revision actually shipped
(`SING_BOX_REVISION=v1.13.13`) and against the `refs/sing-box` checkout
(`v1.14.0-alpha.24`).

### The build-tag premise is false

`with_clash_api` is already in `SING_BOX_TAGS` in
`.github/workflows/artifacts.yml`. Every shipped sing-box binary has the Clash
API compiled in. There has never been a build-tag gate, so nobody needs to
re-litigate one.

What is absent is *configuration*: `internal/server/render/render.go` emits an
`experimental` block containing only `v2ray_api`, so the Clash API is compiled
in but inert. Enabling it is a renderer change, not a rebuild.

## Rejected alternative: Clash API `/connections`

### It cannot attribute a connection to a user

`TrackerMetadata.MarshalJSON`
(`experimental/clashapi/trafficontrol/tracker.go:31`) emits a fixed nine-field
metadata object:

```text
network, type, sourceIP, destinationIP, sourcePort, destinationPort,
host, dnsMode, processPath
```

There is no `user` field. The value exists in memory —
`adapter.InboundContext.User` is populated by both VLESS and Shadowsocks
multi-user inbounds — and is simply never marshalled. The shape is identical in
v1.13.13, v1.14.0-alpha.24, and v1.14.0-beta.2; it is frozen for Clash-dashboard
compatibility.

Every workaround fails:

- **Correlate by source IP.** BoxFleet's model explicitly allows one credential
  from many addresses, and CGNAT makes the reverse true as well.
- **Correlate by connection id.** There is no join key. Clash mints
  `uuid.NewV4()` per tracker (`tracker.go:122`); the sing-box log line carries a
  `rand.Uint32()` id. Independent generators, no shared value.
- **SSM API.** `service/ssmapi` does real per-user bytes, but requires
  `adapter.ManagedSSMServer`, implemented only by
  `protocol/shadowsocks/inbound_multi.go`. VLESS-Reality — BoxFleet's primary
  protocol — does not implement it. Partial coverage is split-brain, not a
  solution.

Migrating would trade fragile-but-attributed data for robust-but-anonymous data.
Every current network-events filter (`user`, per-user drill-down, the
service-per-user audit) would stop working.

### Its byte totals are structurally lossy

`Manager.Snapshot()` (`trafficontrol/manager.go:141`) ranges only the live
`connections` map. Closed connections are retained internally — capped at
`closedConnectionsLimit = 1000` — but `ClosedConnections()` is consumed only by
the 1.14 gRPC service (`daemon/started_service.go`); **no HTTP endpoint exposes
it**. `connectionRouter` offers only `GET /`, `DELETE /`, and `DELETE /{id}`.

Totalling bytes by polling `/connections` therefore misses every connection that
opens and closes between two polls. That is unusable for quota or billing, which
is exactly what the V2Ray counters are used for. The V2Ray `CounterEpoch` design
in `internal/agent` already survives sing-box restarts and dropped reports; it
is strictly better for this purpose.

### Enabling it costs real node memory and CPU

Node memory pressure is a hard constraint (see AGENTS.md). Turning on
`clash_api` would add, per node:

- A **stop-the-world `runtime.ReadMemStats()` on every `/connections` call**
  (`manager.go:150-151`). The WebSocket path re-snapshots on a ticker whose
  default interval is 1000 ms, so that is a STW pause per second per node.
  This cost is specific to the Clash *HTTP* path; see the Amendment — it does
  not apply to the 1.14 push-based gRPC stream.
- Up to 1000 retained `metadataCopy := *metadata` values (`manager.go:76`), each
  a full `adapter.InboundContext` including a `DNSResponse *dns.Msg` pointer —
  DNS allocations pinned long after the connection dies.
- A **control plane**, not read-only telemetry: `DELETE /connections`, plus the
  Clash server's `/configs` reload and `/proxies` select.

A parallel collector behind a setting was also rejected: it costs a renderer
change, a new client, a settings key, dual-write plumbing, and a rollout, to
acquire a strictly less useful dataset.

## Trigger for revisiting

sing-box 1.14's `daemon.StartedService` gRPC API. `daemon/started_service.proto`
declares `rpc SubscribeConnections(SubscribeConnectionsRequest) returns (stream
ConnectionEvents)`, and its `Connection` message carries **`string user = 10`**
(`daemon/started_service.proto:195`), populated from
`metadata.Metadata.User` in `buildConnectionProto`
(`daemon/started_service.go:1016`). The stream emits NEW / UPDATE / CLOSED
events with per-connection deltas *and* totals, and it replays closed
connections on subscribe (`buildInitialConnectionState`), so it avoids the
`Snapshot()` live-only blind spot. It has smaller loss modes of its own — see
the Amendment; the resulting per-host byte view is a best-effort estimate, not
a ledger.

**Trigger, restated after the amendment below:** the `v1.14.0` **stable** tag is
published *and* a build of it at BoxFleet's `SING_BOX_TAGS` passes the four
off-fleet checks listed in the Amendment. Both conditions — "the beta looks
quiet" is not one of them. **This trigger is unchanged by ADR 0002 and remains
unmet.**

This section originally predicted that the migration would be a producer swap
into the existing `model.LogEventInput` shape, deleting `parseSingBoxLogEvent`.
That prediction was wrong, and [ADR 0002](0002-opt-in-connection-telemetry.md)
records why: the stream carries bytes, duration, rule, outbound and chain, none
of which `log_events` has columns for, and it covers a different set of nodes.
The implemented design is a separate table and a separate wire type alongside the
scraper, which is deleted at no point.

## Consequences

- **The entire domain/service audit feature depends on a regex parser** reading
  an unstructured log format that upstream may change in any release. This is
  accepted and documented, not overlooked.
- **That dependency is why golden fixtures exist.** `log_events_parse_test.go`
  and `testdata/singbox_logs/*.{input,golden}.txt` replay real log shapes
  through the parser and diff the normalized result, so an upstream wording
  change surfaces as a readable diff instead of a silent drop to zero events. A
  golden diff after a `SING_BOX_REVISION` bump is a real signal — investigate it
  before regenerating.
- Two parser quirks are pinned as goldens rather than fixed, so the future
  gRPC producer need not reproduce them: `host:+443` is accepted as port 443
  (`strconv.Atoi` tolerates a leading `+`), and a bracketed-offset-only
  timestamp prefix yields no connection window, leaving ingest to substitute the
  server clock.
- **Only `action="connect"` rows survive ingestion.** The parser also emits
  `invalid_connection` and `outbound_connect`, but neither carries an
  `auth_name`, so `RecordLogEvents` drops both. The admin action filter's other
  values are aspirational, and grouping a series by action returns one bucket.
- **Bytes cannot be attributed to a destination host on these two sources.**
  `traffic_usage_deltas` has no host column and `log_events` has no byte columns.
  The service audit view is a connections-per-service view and must never be
  labelled traffic or bytes. This remains true of every node in the fleet. It is
  a property of the two sources in use, not a permanent law: the 1.14 stream
  answers it, for opted-in nodes only, in a separate table
  (`connection_events`) that the service audit does not read.

## Amendment (2026-07-26)

Re-investigated against `v1.14.0-beta.2` fetched from upstream, after the
original was written against a `refs/sing-box` checkout 28 prereleases stale.
**The decision is unchanged. Three statements above were wrong.**

### Corrections

1. **Precondition "the service is unreachable from a headless node" is resolved
   in 1.14.** It was true for v1.13.x and v1.14.0-alpha.24, where the only
   `RegisterStartedServiceServer` call site is
   `experimental/libbox/command_server.go:156`, a gomobile binding `cmd/sing-box`
   never imports. 1.14 adds `service/api` — a normal config-declarable service
   (`option.APIServiceOptions`) that attaches to the running instance via
   `daemon.NewAttachedService` and serves the same gRPC over a listen address.
   It is registered unconditionally in `include/registry.go:146` (no build tag,
   no `_stub.go`). Adopting it is a **renderer change only**: no systemd change,
   no supervision change, no rebuild.
2. **`experimental.clash_api` is *not* required.** The tracker moved from
   `experimental/clashapi/trafficontrol` to `common/trafficcontrol`, and
   `box.go:245` creates the manager under `needClashAPI || needAPIService`. The
   api service alone enables connection tracking.
3. **The per-poll stop-the-world `runtime.ReadMemStats` does not apply** to the
   push-based gRPC path; it is absent from `common/trafficcontrol/manager.go`.
   The *retention* cost stands: a 1000-entry closed-connection ring plus roughly
   1 KB per live connection.

### New risk the original missed

`service.api` is a **full control plane on the same endpoint as the telemetry
stream** — `StopService`, `ReloadService`, `CloseAllConnections`,
`SelectOutbound`, `TriggerDebugCrash` — and `authenticate()` begins with
`if secret == "" { return nil }`, so **an empty secret disables auth entirely**.
Any adoption is loopback-bind plus a mandatory strong per-node secret, generated
and stored server-side like node tokens.

### What the switch would and would not buy

Verified: `Connection` carries `user` (10), `destination` (7), `domain` (8),
`createdAt`/`closedAt` (12/13) and `uplinkTotal`/`downlinkTotal` (16/17) on one
message, all populated from a single `TrackerMetadata` in `buildConnectionProto`.
Bytes-per-(user, host) becomes expressible, and connection *duration* — of which
there is zero data today, since the scraper never observes a close — becomes
available.

Three structural loss modes keep it an estimate rather than a ledger:
`observable.Subscriber.Emit` drops silently on a full 256-slot buffer with no
error and no counter; the closed-connection ring evicts at 1000; and connection
UUIDs and in-flight totals reset when sing-box restarts. **Per-user billing must
stay on the V2Ray counters.**

Two gotchas for whoever implements it:

- **`domain` would be empty on BoxFleet's rendered config.** `buildConnectionProto`
  has no fallback to `Destination.Fqdn` and BoxFleet renders no sniff action, so
  the host arrives in `destination` (field 7), not `domain` (field 8).
- **Fields 14/15 (`uplink`/`downlink`) are never populated server-side.** Build
  nothing on them.

### Release status

Latest stable is **v1.13.14** (2026-06-25). Latest tag is **v1.14.0-beta.2**
(2026-07-25); the beta line is days old on top of large features landed in
alpha.48-50. Config-compat risk for BoxFleet is genuinely low — 1.14's breaking
surface is DNS, ACME and rule-sets, and BoxFleet emits no `dns` block — but the
payoff is unreachable by pinning alone: it also needs a renderer `services`
block, per-node secrets, a node-side aggregator, an ingest path and schema work.
Carrying beta risk across remote edge nodes buys nothing until that lands.

### Off-fleet checks required before the pin moves past 1.13

Build the candidate at BoxFleet's `SING_BOX_TAGS` on a throwaway host and assert:

1. `with_v2ray_api` and `with_clash_api` still appear in `sing-box version` —
   `go build -tags` silently ignores unknown tags, so a rename is a silent
   feature loss (CI already greps for this; replicate it against the candidate).
2. `sing-box check -c` passes against the renderer's golden configs.
3. Real traffic replayed through
   `go test ./internal/server/db -run TestParseSingBoxLogEventGoldenFixtures`.
   This is the highest-probability breakage and the one no gate catches — the
   agent's health check asserts systemd `ActiveState` only, so a sing-box that
   starts cleanly but changed its log wording sends the audit view silently to
   zero. A golden diff here is a real regression: investigate, do not regenerate.
4. `user>>>NAME>>>traffic>>>{uplink,downlink}` still increments.

### Outcome

Everything in this amendment was implemented as an opt-in path in
[ADR 0002](0002-opt-in-connection-telemetry.md), against this exact revision.
Both gotchas above are enforced in code — `Connection.Domain` behind
`singboxapi.Endpoint`, and fields 14/15 behind a `reserved` declaration that
removes their Go accessors entirely.

The amendment also proposed that the eventual migration would reuse
`model.LogEventInput` as a producer-agnostic seam. It does not; see the
correction under "Trigger for revisiting".
