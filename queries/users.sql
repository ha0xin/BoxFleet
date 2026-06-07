-- name: CreateProxyUser :exec
INSERT INTO proxy_users (
  id,
  name,
  display_name,
  global_quota_bytes,
  traffic_multiplier,
  expire_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(display_name),
  sqlc.arg(global_quota_bytes),
  sqlc.arg(traffic_multiplier),
  sqlc.narg(expire_at)
);

-- name: ListProxyUsers :many
SELECT
  id,
  name,
  display_name,
  status,
  global_quota_bytes,
  traffic_multiplier,
  expire_at,
  created_at,
  updated_at
FROM proxy_users
ORDER BY name;

-- name: ListProxyUsersWithProxyCounts :many
SELECT
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.traffic_multiplier,
  u.expire_at,
  u.created_at,
  u.updated_at,
  COUNT(a.id) AS proxy_count
FROM proxy_users u
LEFT JOIN proxy_accesses a ON a.proxy_user_id = u.id
GROUP BY
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.traffic_multiplier,
  u.expire_at,
  u.created_at,
  u.updated_at
ORDER BY u.name;

-- name: GetProxyUserByName :one
SELECT
  id,
  name,
  display_name,
  status,
  global_quota_bytes,
  traffic_multiplier,
  expire_at,
  created_at,
  updated_at
FROM proxy_users
WHERE name = sqlc.arg(name);

-- name: SetProxyUserStatus :execrows
UPDATE proxy_users
SET
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);

-- name: SetProxyUserQuota :execrows
UPDATE proxy_users
SET
  global_quota_bytes = sqlc.arg(global_quota_bytes),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);

-- name: SetProxyUserMultiplier :execrows
UPDATE proxy_users
SET
  traffic_multiplier = sqlc.arg(traffic_multiplier),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);

-- name: SetProxyUserExpire :execrows
UPDATE proxy_users
SET
  expire_at = sqlc.narg(expire_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);
