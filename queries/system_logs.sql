-- name: CreateSystemLog :execrows
INSERT OR IGNORE INTO system_logs (
  id,
  node_id,
  service,
  journal_cursor,
  message_hash,
  level,
  raw_message,
  observed_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(service),
  sqlc.narg(journal_cursor),
  sqlc.arg(message_hash),
  sqlc.arg(level),
  sqlc.arg(raw_message),
  sqlc.arg(observed_at)
);

-- name: ListRecentSystemLogs :many
SELECT
  l.id,
  l.node_id,
  n.name AS node_name,
  l.service,
  l.journal_cursor,
  l.message_hash,
  l.level,
  l.raw_message,
  l.observed_at,
  l.ingested_at
FROM system_logs l
JOIN nodes n ON n.id = l.node_id
ORDER BY l.observed_at DESC
LIMIT sqlc.arg(limit);

-- name: ListRecentSystemLogsByNode :many
SELECT
  l.id,
  l.node_id,
  n.name AS node_name,
  l.service,
  l.journal_cursor,
  l.message_hash,
  l.level,
  l.raw_message,
  l.observed_at,
  l.ingested_at
FROM system_logs l
JOIN nodes n ON n.id = l.node_id
WHERE n.name = sqlc.arg(node_name)
ORDER BY l.observed_at DESC
LIMIT sqlc.arg(limit);
