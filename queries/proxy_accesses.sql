-- name: CreateProxyAccess :exec
INSERT INTO proxy_accesses (
  id,
  proxy_id,
  proxy_user_id,
  auth_name,
  enabled,
  quota_bytes,
  traffic_multiplier,
  credential_json
) VALUES (
  sqlc.arg(id),
  sqlc.arg(proxy_id),
  sqlc.arg(proxy_user_id),
  sqlc.arg(auth_name),
  sqlc.arg(enabled),
  sqlc.arg(quota_bytes),
  sqlc.narg(traffic_multiplier),
  sqlc.arg(credential_json)
);

-- name: GetProxyAccess :one
SELECT *
FROM proxy_access_details
WHERE proxy_user_name = sqlc.arg(user_name)
  AND node_name = sqlc.arg(node_name)
  AND proxy_name = sqlc.arg(proxy_name);

-- name: GetProxyAccessByIDs :one
SELECT *
FROM proxy_access_details
WHERE proxy_user_id = sqlc.arg(proxy_user_id)
  AND proxy_id = sqlc.arg(proxy_id);

-- name: SetProxyAccessEnabled :execrows
UPDATE proxy_accesses
SET
  enabled = sqlc.arg(enabled),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE proxy_user_id = sqlc.arg(proxy_user_id)
  AND proxy_id = sqlc.arg(proxy_id);

-- name: ListProxyAccessesByNodeName :many
SELECT d.*
FROM proxy_access_details d
JOIN user_node_bindings b ON b.proxy_user_id = d.proxy_user_id AND b.node_id = d.node_id
WHERE d.node_name = sqlc.arg(node_name)
  AND d.enabled = 1
  AND d.proxy_user_status = 'active'
  AND d.node_status = 'active'
  AND d.proxy_enabled = 1
  AND b.enabled = 1
ORDER BY d.listen_port, d.proxy_name, d.proxy_user_name;

-- name: ListProxyAccessesByUserNode :many
SELECT d.*
FROM proxy_access_details d
JOIN user_node_bindings b ON b.proxy_user_id = d.proxy_user_id AND b.node_id = d.node_id
WHERE d.proxy_user_name = sqlc.arg(user_name)
  AND d.node_name = sqlc.arg(node_name)
  AND d.enabled = 1
  AND d.proxy_user_status = 'active'
  AND d.node_status = 'active'
  AND d.proxy_enabled = 1
  AND b.enabled = 1
ORDER BY d.listen_port, d.proxy_name;

-- name: ListProxyAccessesByUserName :many
SELECT *
FROM proxy_access_details
WHERE proxy_user_name = sqlc.arg(user_name)
ORDER BY node_name, listen_port, proxy_name;
