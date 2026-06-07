-- name: GetSetting :one
SELECT
  key,
  value_json,
  updated_at
FROM settings
WHERE key = sqlc.arg(key);

-- name: UpsertSetting :exec
INSERT INTO settings (
  key,
  value_json
) VALUES (
  sqlc.arg(key),
  sqlc.arg(value_json)
) ON CONFLICT(key) DO UPDATE SET
  value_json = excluded.value_json,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');
