-- Connection telemetry from sing-box 1.14's daemon gRPC stream. Opt-in per node
-- and off by default: mixed-version rollout includes 1.13, where the
-- service.api config block does not parse.
--
-- Keep every comment in this file ASCII. sqlc's SQLite parser reports rune
-- offsets but slices the source by byte, so a single multi-byte character
-- silently corrupts every query after it in the file.

-- name: GetNodeConnectionTelemetry :one
SELECT
  node_id,
  enabled,
  listen_address,
  listen_port,
  secret,
  rotated_at,
  created_at,
  updated_at
FROM node_connection_telemetry
WHERE node_id = sqlc.arg(node_id);

-- name: UpsertNodeConnectionTelemetry :exec
INSERT INTO node_connection_telemetry (
  node_id,
  enabled,
  listen_address,
  listen_port,
  secret,
  rotated_at
) VALUES (
  sqlc.arg(node_id),
  sqlc.arg(enabled),
  sqlc.arg(listen_address),
  sqlc.arg(listen_port),
  sqlc.arg(secret),
  sqlc.narg(rotated_at)
) ON CONFLICT(node_id) DO UPDATE SET
  enabled = excluded.enabled,
  listen_address = excluded.listen_address,
  listen_port = excluded.listen_port,
  secret = excluded.secret,
  rotated_at = excluded.rotated_at,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: DeleteNodeConnectionTelemetry :exec
DELETE FROM node_connection_telemetry
WHERE node_id = sqlc.arg(node_id);

-- name: ListEnabledConnectionTelemetryNodes :many
SELECT
  t.node_id,
  n.name AS node_name,
  t.listen_address,
  t.listen_port
FROM node_connection_telemetry t
JOIN nodes n ON n.id = t.node_id
WHERE t.enabled = 1
  AND n.deleted_at IS NULL
ORDER BY n.name;

-- name: CreateConnectionReport :exec
INSERT INTO connection_reports (
  id,
  node_id,
  sequence,
  agent_boot_id,
  window_start,
  window_end,
  connections_observed,
  connections_attributed,
  connections_unattributed,
  connections_orphaned,
  stream_resets,
  dropped_buckets,
  bytes_observed,
  bytes_attributed,
  reported_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(sequence),
  sqlc.arg(agent_boot_id),
  sqlc.arg(window_start),
  sqlc.arg(window_end),
  sqlc.arg(connections_observed),
  sqlc.arg(connections_attributed),
  sqlc.arg(connections_unattributed),
  sqlc.arg(connections_orphaned),
  sqlc.arg(stream_resets),
  sqlc.arg(dropped_buckets),
  sqlc.arg(bytes_observed),
  sqlc.arg(bytes_attributed),
  sqlc.arg(reported_at)
);

-- name: GetConnectionReportBySequence :one
SELECT
  id,
  node_id,
  sequence,
  agent_boot_id,
  window_start,
  window_end,
  reported_at,
  created_at
FROM connection_reports
WHERE node_id = sqlc.arg(node_id)
  AND agent_boot_id = sqlc.arg(agent_boot_id)
  AND sequence = sqlc.arg(sequence);

-- Attribution coverage for a time range. Returned alongside any byte total so
-- the UI can state how much of the window is accounted for instead of implying
-- the estimate is exact.
-- name: SumConnectionCoverage :one
SELECT
  CAST(COALESCE(SUM(r.connections_observed), 0) AS INTEGER) AS connections_observed,
  CAST(COALESCE(SUM(r.connections_attributed), 0) AS INTEGER) AS connections_attributed,
  CAST(COALESCE(SUM(r.connections_unattributed), 0) AS INTEGER) AS connections_unattributed,
  CAST(COALESCE(SUM(r.connections_orphaned), 0) AS INTEGER) AS connections_orphaned,
  CAST(COALESCE(SUM(r.stream_resets), 0) AS INTEGER) AS stream_resets,
  CAST(COALESCE(SUM(r.dropped_buckets), 0) AS INTEGER) AS dropped_buckets,
  CAST(COALESCE(SUM(r.bytes_observed), 0) AS INTEGER) AS bytes_observed,
  CAST(COALESCE(SUM(r.bytes_attributed), 0) AS INTEGER) AS bytes_attributed,
  COUNT(*) AS reports
FROM connection_reports r
WHERE (sqlc.arg(node_id) = '' OR r.node_id = sqlc.arg(node_id))
  AND (sqlc.arg(start_time) = '' OR r.window_end >= sqlc.arg(start_time))
  AND (sqlc.arg(end_time) = '' OR r.window_start <= sqlc.arg(end_time));

-- Buckets merge across reports: a connection that stays open across two report
-- windows lands in the same (dimensions, bucket_start) row twice. Summing on
-- conflict is what keeps a long-lived session from being split into fragments,
-- and it is why report_id is not carried on the row: after a merge it would
-- name only one of several contributing reports.
-- name: UpsertConnectionEvent :exec
INSERT INTO connection_events (
  id,
  node_id,
  proxy_user_id,
  auth_name,
  source_ip,
  target_host,
  target_port,
  domain,
  network,
  ip_version,
  protocol,
  inbound,
  inbound_type,
  rule,
  outbound,
  outbound_type,
  chain,
  connections_opened,
  connections_closed,
  uplink_bytes,
  downlink_bytes,
  duration_ms_total,
  aggregate_key,
  bucket_start,
  window_start,
  window_end
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.narg(proxy_user_id),
  sqlc.arg(auth_name),
  sqlc.arg(source_ip),
  sqlc.arg(target_host),
  sqlc.arg(target_port),
  sqlc.arg(domain),
  sqlc.arg(network),
  sqlc.arg(ip_version),
  sqlc.arg(protocol),
  sqlc.arg(inbound),
  sqlc.arg(inbound_type),
  sqlc.arg(rule),
  sqlc.arg(outbound),
  sqlc.arg(outbound_type),
  sqlc.arg(chain),
  sqlc.arg(connections_opened),
  sqlc.arg(connections_closed),
  sqlc.arg(uplink_bytes),
  sqlc.arg(downlink_bytes),
  sqlc.arg(duration_ms_total),
  sqlc.arg(aggregate_key),
  sqlc.arg(bucket_start),
  sqlc.arg(window_start),
  sqlc.arg(window_end)
) ON CONFLICT(aggregate_key) DO UPDATE SET
  connections_opened = connection_events.connections_opened + excluded.connections_opened,
  connections_closed = connection_events.connections_closed + excluded.connections_closed,
  uplink_bytes = connection_events.uplink_bytes + excluded.uplink_bytes,
  downlink_bytes = connection_events.downlink_bytes + excluded.downlink_bytes,
  duration_ms_total = connection_events.duration_ms_total + excluded.duration_ms_total,
  proxy_user_id = COALESCE(connection_events.proxy_user_id, excluded.proxy_user_id),
  window_start = MIN(connection_events.window_start, excluded.window_start),
  window_end = MAX(connection_events.window_end, excluded.window_end),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: ListConnectionEventsPage :many
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.domain,
  e.network,
  e.ip_version,
  e.protocol,
  e.inbound,
  e.inbound_type,
  e.rule,
  e.outbound,
  e.outbound_type,
  e.chain,
  e.connections_opened,
  e.connections_closed,
  e.uplink_bytes,
  e.downlink_bytes,
  e.duration_ms_total,
  e.bucket_start,
  e.window_start,
  e.window_end,
  n.name AS node_name,
  COALESCE(u.name, '') AS user_name
FROM connection_events e
JOIN nodes n ON n.id = e.node_id
LEFT JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE (sqlc.arg(node_id) = '' OR e.node_id = sqlc.arg(node_id))
  AND (sqlc.arg(proxy_user_id) = '' OR e.proxy_user_id = sqlc.arg(proxy_user_id))
  AND (sqlc.arg(start_time) = '' OR e.bucket_start >= sqlc.arg(start_time))
  AND (sqlc.arg(end_time) = '' OR e.bucket_start <= sqlc.arg(end_time))
ORDER BY e.bucket_start DESC, e.id DESC
LIMIT sqlc.arg(limit)
OFFSET sqlc.arg(offset);

-- name: CountConnectionEvents :one
SELECT COUNT(*)
FROM connection_events e
WHERE (sqlc.arg(node_id) = '' OR e.node_id = sqlc.arg(node_id))
  AND (sqlc.arg(proxy_user_id) = '' OR e.proxy_user_id = sqlc.arg(proxy_user_id))
  AND (sqlc.arg(start_time) = '' OR e.bucket_start >= sqlc.arg(start_time))
  AND (sqlc.arg(end_time) = '' OR e.bucket_start <= sqlc.arg(end_time));

-- The one read that log_events structurally cannot answer: bytes per
-- destination host. Unattributed rows participate, so the totals are complete
-- even where `user` was absent on the wire.
-- name: SumConnectionBytesByHost :many
SELECT
  e.target_host,
  CAST(SUM(e.uplink_bytes) AS INTEGER) AS uplink_bytes,
  CAST(SUM(e.downlink_bytes) AS INTEGER) AS downlink_bytes,
  CAST(SUM(e.uplink_bytes + e.downlink_bytes) AS INTEGER) AS total_bytes,
  CAST(SUM(e.connections_opened) AS INTEGER) AS connections_opened
FROM connection_events e
WHERE e.bucket_start >= sqlc.arg(start_time)
  AND e.bucket_start <= sqlc.arg(end_time)
  AND (sqlc.arg(node_id) = '' OR e.node_id = sqlc.arg(node_id))
  AND (sqlc.arg(proxy_user_id) = '' OR e.proxy_user_id = sqlc.arg(proxy_user_id))
GROUP BY e.target_host
ORDER BY total_bytes DESC, e.target_host
LIMIT sqlc.arg(limit);

-- name: SumConnectionBytesByUser :many
SELECT
  COALESCE(u.name, '') AS user_name,
  CAST(SUM(e.uplink_bytes) AS INTEGER) AS uplink_bytes,
  CAST(SUM(e.downlink_bytes) AS INTEGER) AS downlink_bytes,
  CAST(SUM(e.uplink_bytes + e.downlink_bytes) AS INTEGER) AS total_bytes,
  CAST(SUM(e.connections_opened) AS INTEGER) AS connections_opened
FROM connection_events e
LEFT JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE e.bucket_start >= sqlc.arg(start_time)
  AND e.bucket_start <= sqlc.arg(end_time)
  AND (sqlc.arg(node_id) = '' OR e.node_id = sqlc.arg(node_id))
GROUP BY e.proxy_user_id
ORDER BY total_bytes DESC, user_name
LIMIT sqlc.arg(limit);

-- Retention is piggy-backed on ingest exactly like DeleteLogEventsBefore;
-- there is no scheduler in this server. bucket_start leads
-- idx_connection_events_bucket_host_bytes, so the delete is a bounded range
-- scan rather than a full table scan.
-- name: DeleteConnectionEventsBefore :exec
DELETE FROM connection_events
WHERE bucket_start < sqlc.arg(before_time);

-- name: DeleteConnectionReportsBefore :exec
DELETE FROM connection_reports
WHERE window_end < sqlc.arg(before_time);
