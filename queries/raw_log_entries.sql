-- name: CreateRawLogEntry :execrows
INSERT OR IGNORE INTO raw_log_entries (
  id,
  node_id,
  journal_cursor,
  message_hash,
  raw_message,
  observed_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.narg(journal_cursor),
  sqlc.arg(message_hash),
  sqlc.arg(raw_message),
  sqlc.arg(observed_at)
);

-- name: ListRecentRawLogEntriesByNode :many
SELECT
  r.id,
  r.node_id,
  r.journal_cursor,
  r.message_hash,
  r.raw_message,
  r.observed_at,
  r.ingested_at
FROM raw_log_entries r
JOIN nodes n ON n.id = r.node_id
WHERE n.name = sqlc.arg(node_name)
ORDER BY r.observed_at DESC
LIMIT sqlc.arg(limit);
