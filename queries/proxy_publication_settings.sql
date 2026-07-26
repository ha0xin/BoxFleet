-- name: SetProxyDirectPublication :exec
INSERT INTO proxy_publication_settings (proxy_id, direct_enabled)
VALUES (sqlc.arg(proxy_id), sqlc.arg(direct_enabled))
ON CONFLICT (proxy_id) DO UPDATE SET
  direct_enabled = excluded.direct_enabled,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: GetProxyDirectPublication :one
SELECT direct_enabled
FROM proxy_publication_settings
WHERE proxy_id = sqlc.arg(proxy_id);
