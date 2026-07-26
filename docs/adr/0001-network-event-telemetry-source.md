# ADR 0001 — Network event telemetry source

- Status: accepted
- Date: 2026-07-26
- Applies to: `internal/server/db/log_events.go`, `internal/v2raystats`,
  `internal/agent`, `internal/server/render`

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
connections on subscribe (`buildInitialConnectionState`), so it does not have
the `Snapshot()` loss problem.

Two preconditions must both hold before this is actionable, and only one of them
is "1.14 goes stable":

1. **1.14.0 stable ships** and `SING_BOX_REVISION` is bumped past it.
2. **The service is reachable from a headless node.** In v1.14.0-alpha.24 the
   `daemon` gRPC service is served only by `experimental/libbox`'s command
   server (unix socket or `127.0.0.1:<port>`), which is built for the mobile and
   desktop GUI clients. Nothing in `cmd/sing-box` serves it, so `sing-box run`
   on a BoxFleet node exposes no such endpoint today. Re-check this before
   planning the migration.

Note also that connection tracking itself lives in the Clash API server
(`experimental/clashapi/server.go:253` is the only `NewTCPTracker` call site),
so the gRPC path still requires `with_clash_api` **and** a configured
`experimental.clash_api` block — the memory costs above do not disappear, they
only become better justified once the data is attributable.

When both preconditions hold, the migration is a producer swap into the existing
`model.LogEventInput` shape: delete `parseSingBoxLogEvent` and its three regexes
and the `connectionSources` correlation map, and leave every aggregation, index,
retention rule and UI intact. It also fixes the ingest drop described below,
because `user` arrives on the event instead of being recovered from a preceding
log line.

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
- **Bytes can never be attributed to a destination host.** `traffic_usage_deltas`
  has no host column and `log_events` has no byte columns. The service audit view
  is a connections-per-service view and must never be labelled traffic or bytes.
