# internal/singboxapi

BoxFleet's client for sing-box's daemon gRPC connection stream — the 1.14
telemetry source that
[ADR 0001](../../docs/adr/0001-network-event-telemetry-source.md) identifies as
the eventual replacement for the journalctl regex scraper.

**This package is inert until a node opts in.** The production fleet runs
sing-box 1.13, whose config parser rejects the `service.api` block this stream
needs. Nothing here is reachable unless a node is explicitly configured for it.

- `daemonpb/` — the vendored proto, its generated stubs, and the committed
  upstream descriptor the conformance tests diff against. Trimmed to one RPC.
- `client.go` — the wrapper agent code uses.

Everything here is exercised without a real sing-box: `client_test.go` runs an
in-process gRPC server on `127.0.0.1:0` whose auth interceptor is a faithful
copy of `daemon/server.go`, so the credential tests assert against the check
sing-box actually performs.

## Pinned upstream revision

| | |
| --- | --- |
| Repository | `github.com/SagerNet/sing-box` |
| Tag | **`v1.14.0-beta.2`** |
| Commit | `03c3bf4c01e7b1fd165d0c46ff376828fa878aab` |
| Source file | `daemon/started_service.proto` |

The same tag appears in the proto header, in both generated files, and in the
table above; `TestProvenanceIsRecorded` fails if any of them drifts.

`refs/sing-box` is a local checkout of that revision for reference only. Per
AGENTS.md, nothing under `refs/` may be imported — which is precisely why the
proto is vendored rather than read from there.

## What is vendored, and what is not

Upstream's `StartedService` declares 34 RPCs. `daemonpb` declares one:

```
rpc SubscribeConnections(SubscribeConnectionsRequest) returns (stream ConnectionEvents)
```

### Why the rest are excluded

The daemon endpoint is not a telemetry API with some administrative extras
bolted on. It is a **full control plane on the same port and the same secret**:
`StopService` and `ReloadService` stop and reconfigure sing-box, `CloseConnection`
and `CloseAllConnections` sever live user traffic, `SelectOutbound` reroutes it,
and `TriggerDebugCrash` panics the process. BoxFleet applies configuration
through the agent's atomic-write-plus-`systemctl` path and must never acquire a
second, unaudited way to do any of that.

Excluding them from the descriptor is stronger than a code-review convention:
`protoc-gen-go-grpc` generates client methods from the descriptor, so
`StartedServiceClient` has exactly one method and there is no `StopService` to
call. The destructive RPCs are unreachable from BoxFleet code by construction,
not by discipline.

`Connection` fields 14 (`uplink`) and 15 (`downlink`) are omitted the same way,
via `reserved 14, 15`. Upstream's `buildConnectionProto` never assigns them
(`daemon/started_service.go:977-997`), so they would decode as a constant zero
and quietly invite someone to build on them. Reserving keeps the numbers claimed
against future upstream reuse while removing the Go accessors. Real byte figures
come from `uplinkTotal`/`downlinkTotal` and `ConnectionEvent.uplinkDelta`/
`downlinkDelta`.

### Why trimming is wire-faithful

gRPC dispatches on the fully-qualified method name, and proto3 binary encoding
identifies fields by number, not by name. A trimmed descriptor is therefore
indistinguishable from the full one on the wire as long as four things match
upstream exactly, and all four do:

1. the proto package — `daemon`
2. the service name — `StartedService`
3. the method name and its streaming mode — server-streaming
   `SubscribeConnections`
4. every field number, type and cardinality in the reachable messages

`daemonpb/upstream_conformance_test.go` asserts all four — mechanically, not by
hand. `daemonpb/testdata/upstream-v1.14.0-beta.2.descriptorset.binpb` is the
real compiled upstream descriptor, captured at the pinned tag, and the test
compares the vendored descriptor against it field by field. It also asserts that
the set of omitted fields is exactly `{Connection.14, Connection.15}`, so an
accidental omission — which would decode as a zero value and silently lose data
— fails as loudly as a wrong field number.

This is the substitute for `buf breaking`, which cannot be used here: every
deliberate omission would register as a breaking change and drown the signal.
`daemonpb/contract_test.go` covers what a conformance diff cannot express,
because they are the deviations: one RPC only, fields 14/15 reserved, the method
path constant, and the provenance record.

Keeping the proto package as `daemon` (requirement 1) means the vendored
messages register under the same full names as upstream's — `daemon.Connection`
and so on. That would panic at init if sing-box's own `daemon` package were ever
linked into a BoxFleet binary. It cannot be: AGENTS.md forbids importing from
`refs/`, and sing-box is a separate binary. The generated file registers under
`daemonpb/singbox_daemon_connections.proto`, so the file-path key does not
collide either.

## Regenerating

Codegen uses [buf](https://buf.build), which compiles protos in pure Go and
needs no `protoc` — nothing here depends on Homebrew or a system package
manager, and the toolchain installs identically on macOS and Linux.

```bash
# One-time. Plugin versions must match go.mod:
#   google.golang.org/protobuf v1.36.11
#   google.golang.org/grpc      v1.81.1
go install github.com/bufbuild/buf/cmd/buf@v1.58.0
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0

# Generate, from this directory.
PATH="$(go env GOPATH)/bin:$PATH" buf generate

go test ./internal/singboxapi/...
```

`buf.gen.yaml` drives both plugins with `paths=source_relative`, so the `.pb.go`
files land beside the `.proto` in `daemonpb/`. Generated files are committed, as
`internal/v2rayapi`'s are — the difference is that this package documents how
they were produced, so the next person does not have to reconstruct it.

`protoc-gen-go-grpc` is versioned separately from `grpc-go` itself; v1.6.0 emits
the `grpc.ServerStreamingClient[T]` generics that grpc-go v1.81.1 provides.

## Re-verifying against a new sing-box tag

Moving the pin is a deliberate act, gated by
[`docs/singbox-preflight.md`](../../docs/singbox-preflight.md). Do the preflight
first; this procedure covers only the proto contract, which the preflight does
not check.

All commands run from the repository root.

```bash
# 1. Move the reference checkout to the candidate and record what it resolves to.
git -C refs/sing-box fetch --tags
git -C refs/sing-box checkout v1.14.0        # the candidate tag
git -C refs/sing-box describe --tags
git -C refs/sing-box rev-parse HEAD

# 2. Capture the candidate's compiled descriptor as the new conformance fixture.
CANDIDATE=v1.14.0
PATH="$(go env GOPATH)/bin:$PATH" buf build refs/sing-box/daemon \
  --path refs/sing-box/daemon/started_service.proto \
  -o "internal/singboxapi/daemonpb/testdata/upstream-${CANDIDATE}.descriptorset.binpb"
git rm internal/singboxapi/daemonpb/testdata/upstream-v1.14.0-beta.2.descriptorset.binpb

# 3. Update upstreamTag in daemonpb/contract_test.go, plus the tag and commit in
#    the proto header and in the table at the top of this file. Then:
(cd internal/singboxapi && PATH="$(go env GOPATH)/bin:$PATH" buf generate)
go test ./internal/singboxapi/...
```

Step 3 is the whole check. `TestVendoredFieldsMatchUpstream` reports every field
whose number, name, type or cardinality moved, and every field upstream added
that is not vendored, as a readable diff. **Those diffs are findings, not chores:
work out what upstream changed before adjusting anything.** The same goes for
`TestVendoredEnumMatchesUpstream` and `TestVendoredMethodMatchesUpstream`.

Then re-read these three, which are behaviour rather than schema and so cannot
be diffed:

- `daemon/server.go` — `authenticate()`. If the empty-secret bypass at line 50
  is gone, update the rationale on `ErrEmptySecret`. Keep the refusal either way.
- `daemon/started_service.go` — `buildConnectionProto`. Check whether `Domain`
  gained a `Destination.Fqdn` fallback (that would simplify `Endpoint`) and
  whether `uplink`/`downlink` started being assigned (that would un-reserve
  fields 14 and 15).
- `daemon/started_service.go` — `SubscribeConnections`. Confirm `request.Interval`
  is still read as `time.Duration`, i.e. nanoseconds.

`TestProvenanceIsRecorded` fails if the pin is bumped in some of those places
and not the others, so a half-done move does not sit quietly in the tree.

## Using the client

```go
client, err := singboxapi.Dial(singboxapi.Options{
    Address:  "127.0.0.1:9090",  // loopback is enforced
    Secret:   nodeDaemonSecret,  // must not be empty
    Interval: 5 * time.Second,   // traffic-update tick; 0 means one second
})
if err != nil {
    return err
}
defer client.Close()

stream, err := client.Subscribe(ctx)
if err != nil {
    return err
}
for {
    batch, err := stream.Recv()
    if err != nil {
        return err // io.EOF on a clean close, codes.Canceled on ctx cancel
    }
    if batch.GetReset_() {
        // Full state replay. Discard anything accumulated before this.
    }
    for _, event := range batch.GetEvents() {
        // ...
    }
}
```

Two refusals are worth knowing about before they surprise you:

- **`ErrEmptySecret`.** sing-box's `authenticate()` returns nil immediately when
  its configured secret is empty (`daemon/server.go:50`), so an empty-secret node
  serves the whole control plane unauthenticated. A client that tolerated an
  empty secret would make that misconfiguration invisible.
  `TestEmptyDaemonSecretAcceptsAnything` pins the upstream behaviour so the
  reasoning stays checkable.
- **`ErrNonLoopbackAddress`.** The endpoint carries no transport security and is
  guarded by one shared secret. The agent and sing-box share a host, so loopback
  is the only address BoxFleet ever needs.

There is deliberately no client keepalive. gRPC servers police ping frequency
against `keepalive.EnforcementPolicy.MinTime`, which sing-box leaves at the
5-minute default, so any useful ping interval earns a GOAWAY after two strikes
and kills the stream. A dead daemon surfaces immediately anyway, as a Recv error
from the loopback TCP reset.

## What the stream is, and is not

It is **telemetry, not accounting**. Three structural loss modes make the byte
figures a best-effort estimate:

- `observable.Subscriber.Emit` drops silently when a listener's buffer is full.
  The source buffer holds 256 events and each listener gets only 64
  (`common/trafficcontrol/manager.go:55,65`). No error, no counter.
- The closed-connection ring evicts at 1000 entries (`manager.go:31,108-112`),
  so a burst of short connections between two subscriber reads is lost.
- Connection ids and in-flight totals reset when sing-box restarts.

Per-user billing stays on the V2Ray counters (`internal/v2raystats`), whose
`CounterEpoch` design in `internal/agent` already survives restarts and dropped
reports.

Two shapes of the data catch people out:

- **`Connection.Domain` is empty on every config BoxFleet renders.**
  `buildConnectionProto` copies `metadata.Metadata.Domain` with no fallback to
  `Destination.Fqdn`, and BoxFleet renders no sniff action. The host arrives in
  `Connection.Destination`. Use `singboxapi.Endpoint`, which reads the right one
  and still prefers `Domain` if a future renderer starts sniffing.
- **Only `CONNECTION_EVENT_NEW` carries a `Connection`.** `UPDATE` events are
  id plus deltas, and `CLOSED` attaches a `Connection` only while the entry is
  still in the closed ring. Identity has to be retained from the `NEW` event —
  including the connections replayed in the initial `reset` batch, which arrive
  as `NEW` with a non-zero `closedAt` when they are already closed.

`user` is populated for VLESS and multi-user Shadowsocks, which covers what
BoxFleet renders today. Single-user Shadowsocks never attributes.
