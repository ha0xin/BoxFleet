-- name: UpsertUserNodeBinding :exec
INSERT INTO user_node_bindings (
  id,
  proxy_user_id,
  node_id,
  enabled,
  disabled_reason
) VALUES (
  sqlc.arg(id),
  sqlc.arg(proxy_user_id),
  sqlc.arg(node_id),
  1,
  ''
)
ON CONFLICT(proxy_user_id, node_id) DO UPDATE SET
  enabled = 1,
  disabled_reason = '',
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: ListUserNodeBindings :many
SELECT
  b.id,
  b.proxy_user_id,
  u.name AS proxy_user_name,
  b.node_id,
  n.name AS node_name,
  b.enabled,
  b.node_quota_bytes,
  b.traffic_multiplier,
  b.disabled_reason,
  b.created_at,
  b.updated_at
FROM user_node_bindings b
JOIN proxy_users u ON u.id = b.proxy_user_id
JOIN nodes n ON n.id = b.node_id
ORDER BY u.name, n.name;

-- name: ListUserNodeBindingsByUserName :many
SELECT
  b.id,
  b.proxy_user_id,
  u.name AS proxy_user_name,
  b.node_id,
  n.name AS node_name,
  b.enabled,
  b.node_quota_bytes,
  b.traffic_multiplier,
  b.disabled_reason,
  b.created_at,
  b.updated_at
FROM user_node_bindings b
JOIN proxy_users u ON u.id = b.proxy_user_id
JOIN nodes n ON n.id = b.node_id
WHERE u.name = sqlc.arg(user_name)
ORDER BY u.name, n.name;

-- name: GetUserNodeBinding :one
SELECT
  b.id,
  b.proxy_user_id,
  u.name AS proxy_user_name,
  b.node_id,
  n.name AS node_name,
  b.enabled,
  b.node_quota_bytes,
  b.traffic_multiplier,
  b.disabled_reason,
  b.created_at,
  b.updated_at
FROM user_node_bindings b
JOIN proxy_users u ON u.id = b.proxy_user_id
JOIN nodes n ON n.id = b.node_id
WHERE u.name = sqlc.arg(user_name) AND n.name = sqlc.arg(node_name);

-- name: SetUserNodeBindingEnabled :execrows
UPDATE user_node_bindings
SET
  enabled = sqlc.arg(enabled),
  disabled_reason = sqlc.arg(disabled_reason),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE proxy_user_id = sqlc.arg(proxy_user_id) AND node_id = sqlc.arg(node_id);

-- name: SetUserNodeQuota :execrows
UPDATE user_node_bindings
SET
  node_quota_bytes = sqlc.arg(node_quota_bytes),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE proxy_user_id = sqlc.arg(proxy_user_id) AND node_id = sqlc.arg(node_id);

-- name: SetUserNodeMultiplier :execrows
UPDATE user_node_bindings
SET
  traffic_multiplier = sqlc.narg(traffic_multiplier),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE proxy_user_id = sqlc.arg(proxy_user_id) AND node_id = sqlc.arg(node_id);
