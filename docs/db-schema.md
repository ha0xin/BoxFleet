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
- User, node, ProxyCredential, and PathAccess deletion is soft. Default queries exclude
  `deleted_at`; admin deleted views may restore rows.
- Canonical node/proxy renames retain aliases so old references resolve without
  changing stable IDs or credentials.
- `proxy_details` and `proxy_access_details` flatten joins. New access/proxy
  queries should select from those views rather than recreate mapping logic.

## Core relationships

```text
nodes ──< proxies ──< proxy_accesses (ProxyCredential) >── proxy_users
  │         │                                               │
  │         └──< endpoints >── node hosts                   │
  │                    │                                    │
  │                    └──< paths ──< path_accesses >────────┘
  └────────────────< user_node_bindings >───────────────────┘

nodes ──< config_versions
nodes ──1 node_config_status
nodes ──< node_operations ──< node_operation_events
```

`nodes.hosts_json` is the ordered host source of truth;
`nodes.public_host` mirrors its first entry for views and search. A paused and a
decommissioned node both have status `disabled`; active token presence
distinguishes them.

Proxy names are globally unique. Server inbounds support `vless_reality` and
`shadowsocks_2022`. Technical credentials and stable `auth_name` values remain
stored in the compatibility-named `proxy_accesses` table, but the domain model
calls them `ProxyCredential`. They do not authorize publication.

An `Endpoint` is one Proxy plus one durable Host ID. A `Path` points to an
Endpoint and may point recursively to a dialer Path; depth is capped at three
and cycles are rejected. `PathAccess` is the product-level user authorization.
Dependency Paths are emitted only when required by a granted selectable Path.
`proxy_publication_settings.direct_enabled` is the explicit compatibility
switch that materializes managed direct Paths for each selected Host; disabling
it preserves those rows but removes them from resolution. Deselecting a Host
also disables its managed Path. Managed Paths cannot be edited or deleted via
the general Path API; synchronization owns them.

Granting a Path ensures credentials for every Proxy in its dialer chain.
Revoking a Path recalculates the user's remaining chains and disables each
ProxyCredential that is no longer referenced. A shared dialer credential stays
enabled until its final referencing Path is revoked.

Deleting a custom Path removes its Endpoint when no other Path references it
and reconciles credentials for users whose grant was removed by the cascade.

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

Bucketed traffic series aggregate `traffic_usage_deltas` directly — there is no
time-bucketed rollup table. `traffic_usage_totals` is keyed
`(proxy_user_id, direction)` with no timestamp, node, or proxy column, so it
cannot be decomposed into buckets. `idx_traffic_usage_deltas_observed`
(migration 024) leads with `observed_at` and carries `direction`,
`billable_bytes_delta`, and `raw_bytes_delta`, making the unfiltered range
aggregation covering; every other delta index leads with an entity column.
Series reads bound the scan at the API layer instead of at the schema layer.

`traffic_usage_deltas` and `traffic_reports` are never pruned. There is no
scheduler on the server; network-event retention is piggy-backed inline on
`RecordLogEvents`. Traffic retention needs the same trick or a real scheduler.

## Time buckets

Every bucketed read derives its keys in one place, `series_common.go`, and both
the SQL expression and the Go walk are pinned against each other by test.

- Bucket on `window_start` / `window_end` (events) or `observed_at` (traffic).
  Never on `created_at`: the log-event upsert bumps `created_at` to `now` on
  every merge, so it is last-touched, not first-seen, and bucketing on it
  silently shifts events into later buckets.
- Hour buckets are UTC (`substr(col,1,13) || ':00:00Z'`).
- Day buckets honour an `offset_minutes` parameter and resolve to the UTC
  instant of local midnight. Valid range is `-720`..`840` — real UTC offsets run
  from -12:00 to +14:00.
- Both slice `substr(col,1,19)` to fixed width before any `datetime()` call.
  Stored values are RFC3339Nano with up to nine fractional digits and SQLite's
  date parser is reliably specified to three.
- Span ceilings are enforced by the API: 8 days for hour buckets, 400 days for
  day buckets, 422 beyond.
- Zero-fill is server-side. Clients receive contiguous, oldest-first points and
  must never bucket, fill, or sort.

`observed_at`, `window_start`, and `window_end` all come from the agent's clock
via sing-box timestamps and are unclamped; only `created_at` is server
authoritative. A skewed node lands in the wrong bucket and nothing corrects it.

Retention also skews the oldest buckets: pruning is on `window_end` while reads
filter on both `window_end >= start` and `window_start <= end`, so a long-lived
connection whose `window_start` predates the cutoff survives. Buckets near the
retention boundary are partially populated and not reproducible afterwards.

Soft-deleted users are handled **asymmetrically, deliberately**: traffic series
exclude them, matching the existing traffic summaries; network-event series
include them, matching the paged event table rendered above the chart. Each
aggregation matches the pipeline it extends. Do not unify these.

## Logs

Node log uploads are parsed into aggregated `log_events`. Raw rows are not
retained in normal operation; `raw_message` on an event is a compact diagnostic
sample. FTS tables and triggers maintain operator search. Retention is controlled
by the `network_event_retention_days` setting, default 90.

`RecordSystemLogs` writes `system_logs` rows keyed by a
`(service, cursor, message)` hash. `raw_log_entries` remains in the schema for
compatibility but the current ingestion path does not retain it.

Only `action = 'connect'` rows reach `log_events`. The parser also produces
`invalid_connection` and `outbound_connect`, but neither carries an `auth_name`,
so ingestion drops both. Grouping a series by action returns one bucket.

`target_host` is stored with its original casing — only `aggregate_key`
lowercases it — so `Example.com` and `example.com` are distinct rows. Any host
aggregation must `GROUP BY lower(target_host)`.

`log_events` has no `proxy_id` and no byte columns, so proxy grouping is
traffic-only and **bytes are never attributable to a destination host on this
table**. The service audit is a connections-per-service view and must never be
labelled bytes. `connection_events` answers that question for opted-in nodes
only; it is a separate table and the service audit does not read it.

## Connection telemetry

Three tables added by `migrations/026_connection_telemetry.sql`, all inert until
a node opts in. Operation and rationale are in
[connection telemetry](connection-telemetry.md).

- `node_connection_telemetry` — the per-node opt-in, keyed by `node_id` with
  `ON DELETE CASCADE`. **A missing row is the disabled state**, which is why the
  fleet default is off structurally. `CHECK (length(secret) >= 32)` is
  load-bearing, not cosmetic: sing-box's daemon `authenticate()` returns nil for
  an empty secret, so an empty secret disables authentication on an endpoint that
  also exposes `StopService` and `CloseAllConnections`. Deliberately a sidecar
  table rather than columns on `nodes`, which is the hottest table in the schema.
- `connection_reports` — one row per agent report window.
  `UNIQUE (node_id, agent_boot_id, sequence)` mirrors `traffic_reports` and is
  the entire idempotency mechanism. It matters more here than for traffic:
  `UpsertConnectionEvent` *adds* into an existing row, so a partially applied
  replay would inflate byte totals with no way to detect it afterwards. A
  collision skips the whole batch before any bucket is applied. The eight
  coverage counters live here.
- `connection_events` — the aggregate rows, unique on `aggregate_key`
  = `sha256(node_id || NUL || bucket.DimensionKey())`. The key is computed
  server-side; a node-supplied key could collide onto another node's row.

Schema choices that differ from `log_events`, each deliberate:

- **`bucket_start` is a single sargable time axis.** Reads filter one column
  instead of the `window_end >= ? AND window_start <= ?` straddle; the observed
  extremes are kept in `window_start`/`window_end` and folded with `MIN()`/`MAX()`
  on merge.
- **Timestamps are fixed-width** (`model.ConnectionInstantLayout`,
  millisecond precision). Those `MIN()`/`MAX()` folds compare TEXT, and
  RFC3339Nano trims trailing zeros — `"…:11.482Z" < "…:11Z"`, so the later
  instant would win a `MIN()`.
- **Lowercasing is a CHECK constraint**, on both `target_host` and `domain`.
  `log_events` skipped this, which is why `lower()` is scattered across every
  read of it.
- **Four secondary indexes and no more**, against `log_events`' twelve, on a
  table that writes more. `idx_connection_events_bucket_host_bytes` serves the
  unfiltered range read, the top-hosts ranking and the retention delete as a
  covering index; the other three lead with `node_id`, `proxy_user_id`, or both.
- **Unattributed rows are kept** with `proxy_user_id` NULL, where
  `RecordLogEvents` would drop them. Single-user Shadowsocks never populates
  `user`, and dropping those rows would understate every host total.

Retention is `connection_event_retention_days` (default 14, bounds 1..3650),
applied inline on ingest in the same transaction — there is no scheduler.
`connection_events` prunes on `bucket_start`, `connection_reports` on
`window_end`. The default is shorter than the network-event 90 because these rows
are wider and at a finer dimension tuple.

> `queries/*.sql` is sliced by byte offset from rune offsets by sqlc's SQLite
> parser. **A single non-ASCII character in a comment silently corrupts every
> query after it in the file** — an em dash is enough. Keep those files ASCII.

## Domain service classification

`domain_service_overrides` (migration 024) stores operator-supplied
`suffix -> (service, label, category)` mappings. `suffix` is the primary key and
is normalized on write: lowercased, trimmed, leading and trailing dots stripped;
empty values and suffixes containing `/ ? # @` or whitespace are rejected.

Classification happens at **read time**, not at ingest. Overrides are consulted
first, then the embedded catalog in `internal/servicecatalog`, then eTLD+1 via
`publicsuffix`; IP literals short-circuit to `direct-ip`.

There is deliberately **no `service` column on `log_events`**:

- A stored column freezes each row's classification at ingest, so a catalog or
  override change would only affect future rows.
- Correcting history would mean a mass `UPDATE` over the highest-volume table.
  `log_events` search is maintained by five FTS triggers, and every updated row
  fires `log_events_search_after_update`, which deletes and re-inserts that
  row's FTS3 document — the backfill cost is the FTS rebuild, not the update.
- Read-time classification is a pure function over a bounded, already-grouped
  host list, so a catalog bump retroactively improves historical views with no
  backfill and no schema change.

`idx_log_events_visible_window_host` covers the host aggregation:
`(window_end, window_start, target_host, count) WHERE proxy_user_id IS NOT NULL`.
It is a bounded range scan rather than a genuinely covering one — the query
references `proxy_user_id` explicitly and the partial predicate does not satisfy
the planner's covering check. Adding `proxy_user_id` to the index would satisfy
it at the cost of a TEXT id per row on the highest-volume table; that trade was
measured and declined.

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
