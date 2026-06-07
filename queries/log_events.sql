-- name: CreateLogEvent :exec
INSERT INTO log_events (
  id,
  node_id,
  proxy_user_id,
  auth_name,
  source_ip,
  target_host,
  target_port,
  action,
  raw_message,
  count,
  aggregate_key,
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
  sqlc.arg(action),
  sqlc.arg(raw_message),
  sqlc.arg(count),
  sqlc.arg(aggregate_key),
  sqlc.arg(window_start),
  sqlc.arg(window_end)
) ON CONFLICT(aggregate_key) WHERE aggregate_key <> '' DO UPDATE SET
  count = log_events.count + excluded.count,
  window_start = CASE
    WHEN excluded.window_start < log_events.window_start THEN excluded.window_start
    ELSE log_events.window_start
  END,
  window_end = CASE
    WHEN excluded.window_end > log_events.window_end THEN excluded.window_end
    ELSE log_events.window_end
  END,
  raw_message = CASE
    WHEN log_events.raw_message = '' THEN excluded.raw_message
    ELSE log_events.raw_message
  END,
  created_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: GetProxyUserIDByNodeAuthName :one
SELECT c.proxy_user_id
FROM proxy_accesses c
JOIN proxies p ON p.id = c.proxy_id
JOIN nodes n ON n.id = p.node_id
WHERE n.name = sqlc.arg(node_name)
  AND c.auth_name = sqlc.arg(auth_name);

-- name: ListRecentLogEventsByNode :many
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.action,
  e.raw_message,
  e.count,
  e.aggregate_key,
  e.window_start,
  e.window_end,
  e.created_at
FROM log_events e
JOIN nodes n ON n.id = e.node_id
WHERE n.name = sqlc.arg(node_name)
  AND e.proxy_user_id IS NOT NULL
ORDER BY e.created_at DESC, e.window_end DESC, e.id DESC
LIMIT sqlc.arg(limit);

-- name: ListLogEventsPage :many
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.action,
  e.raw_message,
  e.count,
  e.aggregate_key,
  e.window_start,
  e.window_end,
  e.created_at,
  n.name AS node_name,
  u.name AS user_name
FROM log_events e
JOIN nodes n ON n.id = e.node_id
JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE e.proxy_user_id IS NOT NULL
  AND (sqlc.arg(node_name) = '' OR n.name = sqlc.arg(node_name))
  AND (sqlc.arg(user_name) = '' OR u.name = sqlc.arg(user_name))
  AND (sqlc.arg(start_time) = '' OR e.window_end >= sqlc.arg(start_time))
  AND (sqlc.arg(end_time) = '' OR e.window_start <= sqlc.arg(end_time))
ORDER BY e.created_at DESC, e.window_end DESC, e.id DESC
LIMIT sqlc.arg(limit)
OFFSET sqlc.arg(offset);

-- name: CountLogEvents :one
SELECT COUNT(*)
FROM log_events e
JOIN nodes n ON n.id = e.node_id
JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE e.proxy_user_id IS NOT NULL
  AND (sqlc.arg(node_name) = '' OR n.name = sqlc.arg(node_name))
  AND (sqlc.arg(user_name) = '' OR u.name = sqlc.arg(user_name))
  AND (sqlc.arg(start_time) = '' OR e.window_end >= sqlc.arg(start_time))
  AND (sqlc.arg(end_time) = '' OR e.window_start <= sqlc.arg(end_time));

-- name: DeleteLogEventsBefore :exec
DELETE FROM log_events
WHERE window_end < sqlc.arg(before_time);

-- name: ListRecentLogEventsByUser :many
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.action,
  e.raw_message,
  e.count,
  e.aggregate_key,
  e.window_start,
  e.window_end,
  e.created_at
FROM log_events e
JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE u.name = sqlc.arg(user_name)
ORDER BY e.created_at DESC, e.window_end DESC, e.id DESC
LIMIT sqlc.arg(limit);

-- name: ListRecentLogEvents :many
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.action,
  e.raw_message,
  e.count,
  e.aggregate_key,
  e.window_start,
  e.window_end,
  e.created_at
FROM log_events e
WHERE e.proxy_user_id IS NOT NULL
ORDER BY e.created_at DESC, e.window_end DESC, e.id DESC
LIMIT sqlc.arg(limit);
