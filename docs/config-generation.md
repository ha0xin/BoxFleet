# Config Generation

BoxFleet treats the SQLite database as source of truth and renders complete
`sing-box` configs per node.

Generated configs should be deterministic: the same database state should render
the same JSON and hash.

## Inputs

For one node, the renderer reads:

- `nodes`
- `proxies`
- `proxy_users`
- `user_node_bindings`
- `proxy_accesses`
- `node_outbounds`
- `route_profiles`

Only active rows are rendered:

- `nodes.status = active`
- `proxies.enabled = 1`
- `proxy_users.status = active`
- `user_node_bindings.enabled = 1`
- `proxy_accesses.enabled = 1`

### Disabled nodes

A disabled node does not get its normal render. `GET /api/node/config` returns
`render.RenderDisabledConfig()` — the base config below with **no inbounds** —
together with the `X-BoxFleet-Node-State: disabled` header. New agents act on the
header and stop `sing-box`; older agents that ignore it still stop serving because
applying a no-inbound config takes down all listeners. A node with no proxy
accesses renders the same empty shape.

## Base Config

Every generated config should include:

```json
{
  "log": {
    "level": "info",
    "timestamp": true
  },
  "inbounds": [],
  "outbounds": [
    {"type": "direct", "tag": "direct"},
    {"type": "block", "tag": "block"}
  ],
  "route": {
    "rules": [],
    "final": "direct"
  }
}
```

Node-defined outbounds are appended after the built-ins. If a node defines a tag
that collides with a built-in tag, config generation should fail.

## Proxy Users

For each `proxies` row, collect access rows where:

```text
proxy_accesses.proxy_id = proxies.id
user_node_bindings.node_id = proxies.node_id
user_node_bindings.proxy_user_id = proxy_accesses.proxy_user_id
proxy_users.status = active
user_node_bindings.enabled = 1
```

The rendered `users[]` entries use `proxy_accesses.auth_name`.

### VLESS Reality

Protocol/listener settings come from `proxies.settings_json`.

Rendered shape:

```json
{
  "type": "vless",
  "tag": "vless-443",
  "listen": "::",
  "listen_port": 443,
  "users": [
    {
      "name": "vless-443@alice",
      "uuid": "...",
      "flow": "xtls-rprx-vision"
    }
  ],
  "tls": {
    "enabled": true,
    "server_name": "example.com",
    "reality": {
      "enabled": true,
      "handshake": {
        "server": "example.com",
        "server_port": 443
      },
      "private_key": "...",
      "short_id": "01234567"
    }
  }
}
```

The public Reality key is not rendered into the server config, but it is needed
for `bf user node-info`.

BoxFleet stores one Reality `short_id` per proxy. The value is a hexadecimal
string with 0 to 8 digits. `sing-box` server-side options can technically accept
a list, but the MVP intentionally keeps one value so server and client node
information stay symmetric.

Use an even-length value such as `01234567`; this matches the current
`sing-box` hex decoder behavior used during `sing-box check`.

### Shadowsocks 2022

This is a planned renderer shape. The current tested render/apply path is VLESS
Reality.

Rendered shape:

```json
{
  "type": "shadowsocks",
  "tag": "ss-8388",
  "listen": "::",
  "listen_port": 8388,
  "method": "2022-blake3-aes-128-gcm",
  "password": "...",
  "users": [
    {
      "name": "ss-8388@alice",
      "password": "..."
    }
  ]
}
```

For 2022 methods, the server password and user passwords must match the key
length required by the selected method.

### Hysteria2

This is a planned renderer shape. The current tested render/apply path is VLESS
Reality.

Rendered shape:

```json
{
  "type": "hysteria2",
  "tag": "hy2-8443",
  "listen": "::",
  "listen_port": 8443,
  "up_mbps": 100,
  "down_mbps": 100,
  "users": [
    {
      "name": "hy2-8443@alice",
      "password": "..."
    }
  ],
  "tls": {}
}
```

TLS is required. The first implementation can support certificate paths before
adding ACME or other certificate providers.

## Routing

Route rules are generated from `user_node_bindings.route_profile_id`.

Current MVP behavior:

- If a binding has no route profile, route its auth users to `direct`.
- If a binding has a route profile, render its rules with `auth_user` set to the
  user's rendered auth names on that node.
- `route.final` is currently `direct`.

The renderer should group users by equivalent route profile so generated rules
stay compact.

## V2Ray API

The config must include V2Ray API stats for all rendered auth names:

```json
{
  "experimental": {
    "v2ray_api": {
      "listen": "127.0.0.1:18082",
      "stats": {
        "enabled": true,
        "users": [
          "vless-443@alice",
          "ss-8388@alice"
        ]
      }
    }
  }
}
```

The agent maps returned stat names back through `proxy_accesses.auth_name`.

Expected stat name shape:

```text
user>>>AUTH_NAME>>>traffic>>>uplink
user>>>AUTH_NAME>>>traffic>>>downlink
```

## Validation

Before publishing a config version, the server should validate:

- Proxy names are unique per node.
- Built-in outbound tags are not overwritten.
- Proxy listener conflicts are valid for the network layer.
- Every route profile references an existing outbound tag.
- Every enabled proxy has valid settings for its protocol.
- Every rendered V2Ray API user maps to exactly one access row.

The node agent still runs `sing-box check` before applying the config. Server
validation is an early guard; node validation is authoritative.
