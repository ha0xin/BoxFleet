# Database Schema Draft

BoxFleet MVP uses SQLite with WAL mode, `synchronous=NORMAL`, a busy timeout,
and one open connection per process.

The driver `github.com/mattn/go-sqlite3` compiles its own bundled SQLite
amalgamation via CGO (no `libsqlite3` tag), so the binary carries a recent SQLite
and needs none installed on the host (`ldd` shows no `libsqlite3`). Migration
`012_remove_proxy_user_traffic_multiplier.sql` uses `ALTER TABLE ... DROP COLUMN`
(SQLite ≥ 3.35), which the bundled SQLite supports regardless of host SQLite.

The schema separates raw traffic from billable traffic so global quota,
per-node quota, and non-user traffic multiplier overrides can be handled
consistently.

The executable draft lives in `migrations/`. `010_init.sql` is the public
baseline; later migrations are append-only (e.g.
`012_remove_proxy_user_traffic_multiplier.sql`). Migration
`014_subscription_tokens.sql` adds revocable Mihomo proxy-provider links.
`021_mihomo_profiles.sql` introduced Mihomo profile documents, historical
revision tables, and per-user profile bindings. The revision/publication tables
remain in the schema for migration compatibility but are no longer used by the
application.
`022_mihomo_configurations.sql` binds configurations directly to their proxy
source user, adds reusable rewrite templates, and adds configuration-scoped
subscription tokens.
Migration `015_names_and_aliases.sql` adds canonical rename aliases and makes
proxy names globally unique.
Migration `016_soft_delete.sql` adds reversible soft deletion for users, nodes,
proxies, and proxy accesses. Default inventory and operational queries exclude
rows whose `deleted_at` is set; admin `deleted=true` views expose them for
restore.
Migration `020_node_operations.sql` adds durable node operations, immutable
progress events, and canary/batched update campaigns.
Regenerate sqlc after editing queries or the schema snapshot.

## Core Tables

### proxy_users

Managed proxy identities. These are not BoxFleet login accounts.

```text
id
name                      unique, stable CLI name
display_name
status                    active | disabled | expired | quota_exceeded
global_quota_bytes        0 means unlimited
expire_at                 nullable
deleted_at                nullable; non-null hides the user by default
created_at
updated_at
```

### subscription_tokens

One active Mihomo subscription bearer per proxy user. Revoked rows are retained
for lifecycle history; proxy changes update both the complete profile and legacy
provider response without rotating the token.

```text
id
proxy_user_id
token                     unique high-entropy bearer token
created_at
last_used_at
revoked_at
```

### mihomo_profiles and Mihomo rewrite data

`mihomo_profiles` owns the saved configuration document and references the
proxy user whose inline `proxies` form the base document. A user may own
multiple profiles. Its JSON document is an ordered list of enabled/disabled
Clash Party YAML or JavaScript processors. Template-derived processors retain a
`template_id`; their stored content is informational and is replaced with the
template library's latest saved content during preview and subscription
rendering.

Saving validates the complete rendered document and immediately makes it
available to configuration-scoped subscription links. There is no Mihomo
draft/publish or rollback operation. The `mihomo_profile_revisions` and
`mihomo_profile_publications` tables are dormant legacy schema retained so
existing databases remain migration-compatible.

`proxy_user_mihomo_profiles` assigns one saved profile to a proxy user.
Users without a row resolve to the seeded `mhp_default` profile.
This table is retained for legacy per-user subscription compatibility; new
complete configurations use `mihomo_profiles.proxy_user_id` and their own token.

`mihomo_rewrite_templates` is the global template library. The seeded
`mhrt_basic` row is immutable through the admin facade. Custom templates may be
edited; changes apply to every linked configuration on its next preview or
subscription request.

`mihomo_profile_subscription_tokens` stores one active revocable bearer per
configuration. Revoked rows remain available for audit history.

### nodes

```text
id
name                      unique
public_host               primary host; mirrors hosts_json[0]
hosts_json                JSON [{"host":..,"tag":..,"selected":..}, ..]
api_base_url
status                    pending | active | disabled | degraded
sing_box_version
last_seen_at
deleted_at                nullable; delete also disables and revokes tokens
created_at
updated_at
```

A node may publish several reachable addresses (a domain, multiple IPv4, IPv6).
`hosts_json` is the ordered source of truth; `public_host` is kept in sync with
the first entry so the `proxy_details` / `proxy_access_details` views, search,
and sorting stay on a single column. Each host marked `selected` produces its own
client connection profile (`render.NodeInfoForUser`); the first host is always
present and at least one host is always selected (`db.normalizeNodeHosts`). Rows
written before migration 013 fall back to `[{public_host, selected:true}]`.
Additional hosts require a case-insensitively unique tag on update; legacy
multi-host rows without tags are accepted until they are edited.

`node_name_aliases` maps every retained historical name to the stable node ID.
A rename changes `nodes.name` transactionally and keeps the old name as an
alias, so existing URLs, CLI references, and agent credentials can resolve to
the current canonical name.

Status semantics (see "Node Lifecycle" in `docs/architecture.md`):

- `pending` — enrolled via bootstrap, awaiting the first authenticated heartbeat,
  which promotes it to `active` (`RecordHeartbeat`). Note `CreateNode` inserts
  `active`; the bootstrap handler then sets `pending`.
- `active` — agent has checked in; rendered and publishable.
- `disabled` — paused (token intact, reversible) or decommissioned (tokens
  revoked). The `has_active_token` field on the admin node response (from
  `ListNodeNamesWithActiveTokens`) distinguishes the two for the UI.
- `degraded` — reserved.

Node token verification (`GetActiveNodeTokenByDigest` /
`ListActiveNodeTokensByNodeName`) no longer filters on node status, so a disabled
node still authenticates; revoking the token (`revoked_at`) is the real cutoff.

### node_tokens

```text
id
node_id
token_hash
created_at
last_used_at
revoked_at
```

### node_operations and node_operation_events

`node_operations` is the durable command queue. A partial unique index allows
only one `queued`/`running` operation per node. It stores the typed kind,
server-selected payload, required capabilities, idempotency key, attempt,
hashed lease token, lease/expiry timestamps, cancellation flag, phase, result,
error, and optional `retry_of` relation.

`node_operation_events` is append-only progress per `(operation, attempt,
sequence)`. Exact replay of an accepted event is idempotent, including a
terminal event whose HTTP response was lost.

### node_update_campaigns and node_update_campaign_members

One active campaign releases exactly one canary, followed by bounded batches.
Members retain order, batch, fixed payload, current child operation, and error.
Failed campaigns remain `paused` until explicitly retried or cancelled; a new
child preserves the failed operation through `retry_of`.

## Proxies And Access

### proxies

A concrete proxy entry on one node.

```text
id
node_id
name                      globally unique
protocol                  vless_reality | shadowsocks_2022 | hysteria2
listen
listen_port
transport                 tcp | udp | tcp_udp
enabled
traffic_multiplier
settings_json             protocol/listener settings
inbound_rules_json        proxy-specific inbound rules
outbound_rules_json       proxy-specific outbound definitions
route_rules_json          proxy-specific route rules
deleted_at                nullable; delete also sets enabled=false
created_at
updated_at
```

The DB layer validates listener conflicts before insert/update. Two proxies can
share the same node/listen/port/transport only when the protocol supports
multi-user listeners and the listener settings match.

`transport` is derived from `protocol` by default and should not normally be an
operator-facing input:

```text
vless_reality     -> tcp
hysteria2         -> udp
shadowsocks_2022  -> tcp_udp
```

The UI may show transport as read-only diagnostic data because it explains
listener conflict checks.

`proxy_name_aliases` maps retained historical names to the stable proxy ID.
Renaming a proxy does not rewrite `proxy_accesses.auth_name` or credentials.

Examples of `settings_json`:

```text
vless_reality:
  server_name
  reality_private_key
  reality_public_key
  short_id
  handshake_server
  handshake_port

shadowsocks_2022:
  method
  server_password
  network

hysteria2:
  up_mbps
  down_mbps
  obfs
  tls
  masquerade
```

### proxy_accesses

Per-user access on one proxy.

```text
id
proxy_id
proxy_user_id
auth_name                 sing-box users[].name
enabled
quota_bytes
traffic_multiplier        optional override
credential_json           profile-specific credential
deleted_at                nullable; issuing the same access restores it
created_at
updated_at
```

Examples of `credential_json`:

```text
vless_reality:
  uuid
  flow

shadowsocks_2022:
  password

hysteria2:
  password
```

## Per-user Per-node Binding

### user_node_bindings

Controls whether a proxy user can use a node and how that node is billed.

```text
id
proxy_user_id
node_id
enabled
node_quota_bytes          0 means unlimited
traffic_multiplier        nullable; per-node billing override
route_profile_id          nullable
created_at
updated_at
```

Billing multiplier resolution:

```text
effective_multiplier =
  proxy_accesses.traffic_multiplier
  ?? user_node_bindings.traffic_multiplier
  ?? proxies.traffic_multiplier
  ?? 1.0
```

Quota checks:

- Global user quota is checked against total billable bytes across all nodes.
- Node quota is checked against billable bytes for that user on that node.
- Either quota can disable the affected access path.

Recommended behavior:

- Global quota exceeded: disable the proxy user globally.
- Node quota exceeded: disable only the `user_node_bindings` row for that node.

## Routing

### node_outbounds

```text
id
node_id
tag
type                      direct | block | socks | http | shadowsocks | ...
settings_json
created_at
updated_at
```

### route_profiles

```text
id
name
node_id                   nullable; null means reusable global profile
rules_json
final_outbound_tag
created_at
updated_at
```

Routes are applied by assigning `route_profile_id` on `user_node_bindings`.

## Config Versions

### config_versions

```text
id
node_id
version
status                    draft | published | superseded | failed
config_json
config_hash
created_at
published_at
```

### node_config_status

```text
node_id
target_config_version_id
current_config_version_id
last_apply_status         pending | applied | failed | rolled_back
last_apply_error
updated_at
```

## Traffic Accounting

### traffic_reports

One upload batch from a node agent.

```text
id
node_id
sequence                  monotonically increasing per agent boot
agent_boot_id
reported_at
created_at
```

### traffic_usage_deltas

Raw counter deltas from V2Ray API, plus billable bytes after multiplier.

```text
id
report_id
node_id
proxy_user_id
proxy_id
auth_name
direction                 uplink | downlink
raw_bytes_delta
effective_multiplier
billable_bytes_delta
counter_value
counter_epoch
observed_at
created_at
```

Important rules:

- Store raw and billable bytes.
- Do not rewrite old rows if a multiplier changes later.
- Apply the effective multiplier at report ingestion time.
- Round billable bytes up when the multiplier creates a fractional byte.
- If a counter decreases, start a new `counter_epoch`.

Recommended indexes:

```text
traffic_usage_deltas(proxy_user_id, observed_at)
traffic_usage_deltas(proxy_user_id, node_id, observed_at)
traffic_usage_deltas(node_id, observed_at)
traffic_usage_deltas(auth_name, node_id, observed_at)
```

## Logs And Audit

### raw_log_entries

```text
id
node_id
journal_cursor nullable
message_hash
raw_message
observed_at
ingested_at
```

Raw log rows preserve what the node uploaded. The server deduplicates with
`(node_id, journal_cursor)` when a cursor is present and with `(node_id,
message_hash)` as a fallback.

### system_logs

```text
id
node_id
service
journal_cursor nullable
message_hash
level
raw_message
observed_at
ingested_at
```

System logs are service/runtime logs uploaded by the agent. The table exists so
the contract can be re-enabled later, but the current server does not retain
system log rows in normal operation. System logs remain separate from proxy
network events.

### log_events

```text
id
node_id
proxy_user_id nullable
auth_name
source_ip
target_host
target_port
action
raw_message
count
aggregate_key
window_start
window_end
created_at
```

Structured events are derived from node log uploads where possible. Raw log rows
are not retained in normal operation; `raw_message` is only a short sample on the
structured event. `aggregate_key` groups repeated activity for the same
node/user/auth/source/target/action/minute and increments `count` while
expanding `window_start`/`window_end`.

`bf logs user` queries events that were mapped to a known proxy user through
`auth_name`. The admin Network Events API supports server-side `limit`,
`offset`, `search`, `action`, `node`, `user`, `start`, and `end` filtering.
`log_event_search_documents` gives each event a stable integer search document
ID, while the FTS3 `log_events_search` table indexes operator-visible names,
addresses, actions, and compact raw-message samples. Database triggers keep the
search index synchronized across event ingestion, aggregation, retention
cleanup, and node/user renames. Structured events are retained for the
configured `network_event_retention_days` setting, defaulting to 90 days, and
cleanup compares the event `window_end` against that horizon.

### audit_events

```text
id
actor
action
resource_type
resource_id
before_json
after_json
created_at
```

## Settings

### settings

```text
key
value_json
updated_at
```

Current settings:

- `network_event_retention_days`: integer JSON value, default `90`, controls how
  long structured network events remain queryable.
