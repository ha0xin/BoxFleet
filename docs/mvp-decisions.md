# MVP Decisions

## Product Direction

BoxFleet is a multi-node `sing-box` management system.

The first version is operated through the `bf` CLI and an embedded admin Web UI
served by `boxfleet-server`.

## Runtime Split

### Central Server

- May use Docker.
- Uses SQLite as the MVP database.
- Owns users, nodes, plans, quotas, config versions, logs, traffic accounting,
  and generated user node information.

PostgreSQL is deferred. SQLite is enough for the first version because BoxFleet
is single-admin and node agents can batch reports. Use WAL mode and a busy
timeout to make concurrent reads and agent writes predictable.

### Node

- Must stay lightweight.
- Runs `sing-box` and `boxfleet-agent`.
- Docker is optional, not required.
- Should work on small VPS instances where possible.

## CLI

The preferred management CLI command is `bf`.

`bf` is short and ergonomic, but it has some collision risk with smaller tools,
especially Brainfuck interpreters. If global package distribution becomes a
priority, keep `boxfleet` as a fallback binary name or install `bf` as an alias.

The server-side management binary owns the CLI experience. It manages proxy
users, nodes, generated node information, config publishing, quotas, and
diagnostics.

The node-side binary must remain separate and minimal. Do not build the server,
database, migration, generated-profile, or admin CLI dependencies into the node
agent.

Recommended binary split:

```text
bf                 Server-side admin CLI
boxfleet-server    Central API service for nodes
boxfleet-agent     Lightweight node agent
```

The node agent can expose a few local maintenance commands, but it is not the
main management CLI.

Recommended node commands:

```text
boxfleet-agent run
boxfleet-agent check
boxfleet-agent once
boxfleet-agent version
```

## Admin Model

The MVP is single-admin.

There is no multi-user BoxFleet login system in the first version. The
administrator operates BoxFleet through the CLI.

Proxy users are not BoxFleet accounts. They are managed proxy identities used
for `sing-box` config generation, per-node connection information, quotas, and
traffic records.

RBAC is reserved for a later version. Node agents still use node-scoped tokens.

## First Supported Proxy Protocols

Reality is not modeled as a standalone protocol. It is part of the TLS settings
for a VLESS proxy.

The current tested production path is:

- VLESS over TCP with Reality TLS and `xtls-rprx-vision` flow.

The data model and create commands reserve room for:

- Shadowsocks 2022.
- Hysteria2.

Transport is not an operator choice in the first version. It is derived from
protocol and stored for validation/debugging:

```text
vless_reality     -> tcp
hysteria2         -> udp
shadowsocks_2022  -> tcp_udp
```

Planned Shadowsocks 2022 methods:

- `2022-blake3-aes-128-gcm`
- `2022-blake3-aes-256-gcm`
- `2022-blake3-chacha20-poly1305`

Planned Hysteria2 users use `name` plus `password`. Hysteria2 proxy settings also
include bandwidth fields such as `up_mbps` and `down_mbps`, optional Salamander
obfuscation, and required TLS settings.

Other protocols are deferred.

## Per-user Per-node Config

BoxFleet must support node-specific user behavior.

Examples:

- User A exits directly on node 1.
- User A uses relay outbound on node 2.
- User B is only allowed on selected nodes.
- User C has custom route rules on a specific node.

This means the config model should not treat a user as one global static
`sing-box` user entry. It needs an override layer:

```text
global user defaults
  -> node defaults
  -> user-node assignment
  -> generated sing-box config for that node
```

Users can also have billing controls at multiple non-user-default levels:

- Global quota on the proxy user.
- Per-node quota and optional per-node traffic multiplier override on the
  user-node binding.
- Optional per-access or proxy traffic multiplier overrides for listener- or
  credential-specific billing.

Raw bytes and billable bytes must be stored separately. Changing a multiplier
later must not rewrite historical usage rows.

## Statistics

The first statistics backend is `sing-box` V2Ray API.

The agent reads counters from the local V2Ray API listener and uploads deltas to
the central server.

The first counter read after a fresh agent state establishes the baseline.
Only positive deltas after that baseline are persisted, which prevents existing
`sing-box` cumulative counters from being counted as new usage after an agent
restart or reinstall.

The node should also report system interface counters as a reconciliation source.

## Logs

The MVP can store administrator-visible logs without a privacy model, because
there is only one administrator.

The current MVP has the agent upload `sing-box` journald entries with
cursor-based deltas, but the server does not retain raw log rows in normal
operation. It parses known sing-box access log shapes into compact structured
`log_events` fields and aggregates repeated activity by time window, user,
source IP, target host, target port, and action. This keeps protocol-specific
parser logic out of the lightweight node agent while avoiding unbounded raw log
growth.

The eventual production log path should:

- Add a non-journald file reader if needed.
- Aggregate them by time window, user, source IP, target host, target port, and
  action.
- Upload compact batches to the server.
- Keep raw log retention off unless a bounded retention policy and storage
  budget are explicitly added.

## User Node Information

`bf` generates connection information for each proxy user and node. The admin
Web UI also exposes that information and can issue one revocable Mihomo
`proxy-provider` URL per user. The provider response contains only a top-level
`proxies:` list; it is not a complete Clash profile with groups or rules.

Provider content is rendered from the current active accesses on every request,
so adding, removing, or editing a proxy changes the content without changing
the URL. Rotating or revoking the subscription token invalidates the URL.

Recommended command shape:

```text
bf user node-info <user>
bf user node-info <user> --node <node>
bf user node-info <user> --format json
```

## sing-box Management

`bf` should support installing and updating `sing-box`, but this should be
optional and explicit.

Recommended command shape:

```text
bf node install-sing-box
bf node update-sing-box
bf node pin-sing-box <version>
bf node check-sing-box
```

The default node workflow should not silently upgrade `sing-box`. Config format
changes can break nodes, so upgrades must be planned, versioned, and reversible.
