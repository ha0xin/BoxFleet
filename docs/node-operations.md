# Node Operations And Updates

BoxFleet delivers privileged node work as durable, typed operations. The
database is the source of truth; HTTP long polling only reduces delivery
latency. Nodes continue to make outbound HTTPS connections only.

## Invariants

- An operation has one authenticated target node and one allow-listed kind.
- A node has at most one non-terminal operation at a time.
- Claiming is transactional and returns an opaque lease token. Only the lease
  holder can renew the lease or append progress events.
- Agent progress events carry a monotonically increasing sequence and are
  idempotent per operation attempt.
- Agent state is saved before every irreversible phase. Restarting either the
  server or agent resumes the same operation instead of creating another one.
- Cancellation is cooperative and is accepted only at safe phase boundaries.
- The operation payload can refer only to release assets selected by the
  server. It never contains an arbitrary command, service, local path, or URL.
- Node lifecycle remains authoritative. Updating a disabled node must not start
  sing-box; decommissioning a node revokes its ability to claim or report work.

## Capability Negotiation

Heartbeats report an extensible list rather than a global protocol number:

```json
{
  "capabilities": [
    "operations.v1",
    "update.agent.v1",
    "update.sing_box.v1",
    "download.streaming.v1",
    "install.versioned.v1",
    "restart_resume.agent.v1",
    "rollback.sing_box.v1"
  ]
}
```

The server creates work only when the latest heartbeat advertises every
required capability. Older agents remain visible and show that one manual
upgrade is required. There is deliberately no `update_protocol` field: each
independent capability is negotiated by name.

## Transport

The agent calls `POST /api/node/operations/claim`. With `wait_seconds: 45`, the
server first checks SQLite, subscribes to an in-memory node notification, then
checks SQLite again before waiting. This ordering avoids a missed wake-up. A
task creation wakes the node, which claims the durable row transactionally.

The request returns an operation or `204 No Content`. It is safe to reconnect
after any timeout or proxy error. A normal periodic claim without waiting is
the fallback.

Forty-five seconds stays below Cloudflare's 120-second proxy read timeout and
the Cloudflare Tunnel 90-second default origin keepalive timeout. The node uses
a longer HTTP client timeout and reconnects with full-jitter exponential
backoff. Node endpoints use `Cache-Control: no-store` and authenticated POST
requests, so the control path is not cacheable. SQLite retains the operation if
a tunnel, process, or TCP connection disappears.

## Status Model

`status` is deliberately small:

```text
queued -> running -> succeeded | failed | cancelled | expired
```

`phase` carries operation-specific detail, for example:

```text
preflight
downloading_agent
verifying_agent
switching_agent
restarting_agent
monitoring_agent
downloading_sing_box
verifying_sing_box
switching_sing_box
restarting_sing_box
rolling_back_sing_box
verifying_health
```

Every accepted transition appends a `node_operation_events` row. UI progress
is derived from the operation plus its event history, not from an open HTTP
connection.

## Update Installation

Downloads use `github.com/cavaliergopher/grab/v3` to stream directly to a
same-filesystem temporary file, support cancellation/resume, and verify SHA256.
The final byte count must exactly match the release asset metadata.

Versioned binaries live below `/opt/boxfleet/releases`. Stable executable
paths are symbolic links switched atomically with
`github.com/google/renameio/v2`. This retains the previous immutable target for
rollback and never exposes a partial executable at the systemd `ExecStart`
path.

Version ordering uses `golang.org/x/mod/semver`. Context-aware retry and jitter
use the already-transitive `github.com/sethvargo/go-retry` package as a direct
dependency.

Generic GitHub self-update packages are intentionally not used: they select a
release and replace the current executable themselves, while BoxFleet needs a
server-selected asset, versioned installation, systemd restart guard, durable
operation resume, and a combined agent/sing-box transaction.

## Rollback

sing-box rollback is owned by the still-running agent. It restores the previous
link and service state when candidate validation, restart, or health checks
fail.

Agent rollback is owned by a separate systemd update guard launched from the
last confirmed version using `ExecStartPre`. Three failed candidate starts (or
a passed guard deadline on a subsequent start) restore the previous link before
systemd launches the service again. The new agent promotes itself to the stable
guard only after its initial config/heartbeat cycle succeeds.

## Bulk Rollout

An update campaign owns child operations. It releases one online canary first,
then at most two nodes per batch. A failed or rolled-back child pauses the
campaign before later children become claimable. Pending, deleted, revoked, or
incapable nodes are excluded; offline eligible nodes remain queued. An operator
can retry only the failed members of the current batch; every retry points back
to the failed operation through `retry_of`. Cancelling a campaign requests safe
cancellation for released children and never releases later batches.

## API Surface

```text
Agent:
POST /api/node/operations/claim
POST /api/node/operations/{operation}/lease
POST /api/node/operations/{operation}/events

Admin:
GET  /api/admin/release
POST /api/admin/nodes/{node}/updates
GET  /api/admin/nodes/{node}/operations/current
GET  /api/admin/nodes/{node}/operations/{operation}
POST /api/admin/nodes/{node}/operations/{operation}/cancel
POST /api/admin/node-updates/bulk
GET  /api/admin/node-update-campaigns/{campaign}
POST /api/admin/node-update-campaigns/{campaign}/resume
POST /api/admin/node-update-campaigns/{campaign}/cancel
```

Generic admin operations accept only allow-listed non-update kinds. Managed
update endpoints do not accept a URL: they resolve assets from the server's
fixed `boxfleet-update.json` release catalog.

## Release Catalog

Formal tag builds publish `boxfleet-update.json` beside the binaries. It fixes
the server release plus each platform's independent agent/sing-box version,
basename, byte length, and SHA256. The server rejects development releases and
catalogs that do not match its compiled server release and pinned component
targets.

Catalog signing/TUF is intentionally deferred for this single-operator
deployment. SHA256 protects transfer and local installation integrity; it does
not establish publisher authenticity. The task boundary still remains closed
because admin requests cannot supply arbitrary download URLs.

## Agent State And Recovery

`/opt/boxfleet/state/operation-state.json` is a 0600 checkpoint containing the
assignment lease, exact event outbox, and committed installation phases. An
event is saved before it is sent, so an ambiguous HTTP response is retried with
the same attempt/sequence rather than duplicated.

Agent updates return control to systemd after switching the versioned symlink.
The restarted process resumes the same server operation and local checkpoint.
sing-box updates remain in the old agent process until the new service state is
verified or the previous symlink has been restored.
