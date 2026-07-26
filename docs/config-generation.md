# Configuration Rendering

SQLite is the source of truth. `internal/server/render` produces deterministic,
complete sing-box JSON; identical relevant state must yield identical bytes and
hashes.

## Eligibility

Normal node configs include only:

- an `active` node;
- enabled VLESS-Reality or Shadowsocks 2022 proxies;
- active, unexpired users;
- enabled user-node bindings and ProxyCredentials.

A disabled node receives `RenderDisabledConfig`: a valid base config with no
inbounds plus `X-BoxFleet-Node-State: disabled`. New agents stop sing-box from
the header; legacy agents stop serving after applying the empty config.

## Base shape

Every config contains timestamped logging, `direct` and `block` outbounds, a
route whose final outbound is `direct`, and the local V2Ray API at
`127.0.0.1:18082`. User-defined outbound tags may not collide with built-ins.

## VLESS-Reality

Each proxy becomes one VLESS inbound. Listener and Reality settings come from
the proxy row. Each eligible access contributes:

```json
{
  "name": "<proxy>@<user>",
  "uuid": "...",
  "flow": "xtls-rprx-vision"
}
```

The server config includes the Reality private key, server name, handshake
target, and one normalized short ID. Client output uses the corresponding
public key. Short IDs are lowercase even-length hexadecimal strings of at most
eight characters.

## Shadowsocks 2022

Each Shadowsocks 2022 Proxy owns a generated server key and each
ProxyCredential owns a generated per-user key of the cipher's required length.
The sing-box inbound renders the server key in `password` and per-user keys in
`users`. Mihomo receives the standard `server-key:user-key` combined password.

## Hosts and client names

`nodes.hosts_json` is ordered. Every selected host produces a client profile.
The primary untagged host uses `<proxy>`; tagged hosts use
`<proxy>-<host-tag>`. Host tags are case-insensitively unique. Legacy untagged
supplemental hosts remain readable, but edits require tags. Duplicate final
profile names are rejected.

## Routing and statistics

Bindings without a route profile use `direct`. Profile rules are scoped to the
binding's rendered auth names and grouped where possible.

Every auth name is also listed in `experimental.v2ray_api.stats.users`. sing-box
then exposes counters named:

```text
user>>>AUTH_NAME>>>traffic>>>uplink
user>>>AUTH_NAME>>>traffic>>>downlink
```

The agent maps these names back to ProxyCredentials.

## Connection telemetry service (optional)

A node with an enabled `node_connection_telemetry` row also gets sing-box 1.14's
`services` block:

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

**No node has this by default, and it must stay that way.** The fleet runs
sing-box 1.13, whose parser rejects the key outright
(`services[0]: unknown inbound type: api`), so a node that has not opted in must
render byte-identically to the pre-1.14 output. The field is `omitempty` and two
renderer tests pin the invariant in both directions — the default shape, and the
bytes after an opt-out.

`RenderDisabledConfig` and the no-accesses config never carry the block: a node
serving nothing has no connections to report.

Rendering **fails** rather than degrades if the row's listen address is not a
loopback IP literal or its secret is under 32 characters. The endpoint is a
control plane, and upstream's `authenticate()` returns nil for an empty secret,
so an under-specified row must abort the render rather than emit a config that
silently exposes it. Because `GET /api/admin/config/changes` re-renders every
node, one malformed row breaks the fleet-wide changes and bulk-publish views
until it is fixed.

`dashboard` and the TLS container are deliberately not emitted. See
[connection telemetry](connection-telemetry.md) for the opt-in procedure and the
secret's lifecycle.

## Publish and validation

Publishing renders the node, stores an immutable config version and hash, and
sets it as the target. The node remains unchanged until its agent pulls and
applies that target.

Server validation rejects invalid protocol settings, listener conflicts,
duplicate names, bad outbound references, and ambiguous auth mappings. The
agent's final `sing-box check` remains authoritative for the installed binary.

Renderer tests use fixed SQLite fixtures and compare normalized JSON. Never add
random credentials during rendering; credentials are generated when a Path is
granted (or by the legacy Proxy grant compatibility API).
