# Database Invariants

The authoritative schema is [schema/schema.sql](../schema/schema.sql).
`migrations/010_init.sql` is the public baseline; later migrations are
append-only. SQL queries live in `queries/`, and generated code lives in
`internal/server/store/sqlc/`.

After changing schema or queries:

```bash
$(go env GOPATH)/bin/sqlc generate
go test ./internal/server/db ./internal/server/api
```

Only `internal/server/db` may import sqlc-generated types.

## Storage rules

- SQLite runs in WAL mode with `synchronous=NORMAL`, a busy timeout, foreign
  keys enabled, and a small connection pool.
- The Go binary uses its bundled SQLite amalgamation; no host SQLite library is
  required.
- User, node, proxy, and access deletion is soft. Default queries exclude
  `deleted_at`; admin deleted views may restore rows.
- Canonical node/proxy renames retain aliases so old references resolve without
  changing stable IDs or credentials.
- `proxy_details` and `proxy_access_details` flatten joins. New access/proxy
  queries should select from those views rather than recreate mapping logic.

## Core relationships

```text
nodes ──< proxies ──< proxy_accesses >── proxy_users
  │                                      │
  └────────────< user_node_bindings >────┘

nodes ──< config_versions
nodes ──1 node_config_status
nodes ──< node_operations ──< node_operation_events
```

`nodes.hosts_json` is the ordered host source of truth;
`nodes.public_host` mirrors its first entry for views and search. A paused and a
decommissioned node both have status `disabled`; active token presence
distinguishes them.

Proxy names are globally unique. The only rendered protocol is
`vless_reality`; transport is derived as TCP. Access credentials and stable
`auth_name` values belong to `proxy_accesses`.

## Billing and traffic

The effective multiplier is:

```text
proxy_accesses.traffic_multiplier
?? user_node_bindings.traffic_multiplier
?? proxies.traffic_multiplier
?? 1.0
```

Traffic ingestion stores raw and billable deltas. Historical billable bytes are
never rewritten after a multiplier changes. Counter regression increments
`counter_epoch`. Summary reads use maintained rollups rather than scanning all
deltas.

## Logs

Node log uploads are parsed into aggregated `log_events`. Raw rows are not
retained in normal operation; `raw_message` on an event is a compact diagnostic
sample. FTS tables and triggers maintain operator search. Retention is controlled
by the `network_event_retention_days` setting, default 90.

`system_logs` and `raw_log_entries` remain in the schema for compatibility but
the current ingestion path does not retain them.

## Mihomo data

`mihomo_profiles` stores a complete processor pipeline bound to one proxy user.
`mihomo_rewrite_templates` stores reusable live processors, and
`mihomo_profile_subscription_tokens` stores revocable configuration links.
Legacy revision/publication and per-user profile tables remain only for migration
compatibility; the current application has no Mihomo draft/publish lifecycle.

## Operations

Only one queued/running operation may exist per node. Claims use hashed lease
tokens. Progress is append-only and idempotent per operation, attempt, and
sequence. Update campaigns release a canary before bounded batches and retain
retry lineage through `retry_of`.
