# Configuration Rendering

SQLite is the source of truth. `internal/server/render` produces deterministic,
complete sing-box JSON; identical relevant state must yield identical bytes and
hashes.

## Eligibility

Normal node configs include only:

- an `active` node;
- enabled VLESS-Reality proxies;
- active, unexpired users;
- enabled user-node bindings and proxy accesses.

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

The renderer currently rejects every other protocol. Protocol expansion must
add validation, server rendering, client/Mihomo output, and golden tests in the
same change.

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

The agent maps these names back to proxy accesses.

## Publish and validation

Publishing renders the node, stores an immutable config version and hash, and
sets it as the target. The node remains unchanged until its agent pulls and
applies that target.

Server validation rejects invalid protocol settings, listener conflicts,
duplicate names, bad outbound references, and ambiguous auth mappings. The
agent's final `sing-box check` remains authoritative for the installed binary.

Renderer tests use fixed SQLite fixtures and compare normalized JSON. Never add
random credentials during rendering; credentials are generated when access is
created.
