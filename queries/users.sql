-- name: CreateProxyUser :exec
INSERT INTO proxy_users (
  id,
  name,
  display_name,
  global_quota_bytes,
  expire_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(display_name),
  sqlc.arg(global_quota_bytes),
  sqlc.narg(expire_at)
);

-- name: ListProxyUsers :many
SELECT
  id,
  name,
  display_name,
  status,
  global_quota_bytes,
  expire_at,
  deleted_at,
  created_at,
  updated_at
FROM proxy_users
WHERE deleted_at IS NULL
ORDER BY name;

-- name: ListProxyUsersWithProxyCounts :many
SELECT
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.expire_at,
  u.deleted_at,
  u.created_at,
  u.updated_at,
  COUNT(a.id) FILTER (WHERE a.deleted_at IS NULL) AS proxy_count
FROM proxy_users u
LEFT JOIN proxy_accesses a ON a.proxy_user_id = u.id
WHERE u.deleted_at IS NULL
GROUP BY
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.expire_at,
  u.deleted_at,
  u.created_at,
  u.updated_at
ORDER BY u.name;

-- name: ListDeletedProxyUsersWithProxyCounts :many
SELECT
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.expire_at,
  u.deleted_at,
  u.created_at,
  u.updated_at,
  COUNT(a.id) FILTER (WHERE a.deleted_at IS NULL) AS proxy_count
FROM proxy_users u
LEFT JOIN proxy_accesses a ON a.proxy_user_id = u.id
WHERE u.deleted_at IS NOT NULL
GROUP BY
  u.id,
  u.name,
  u.display_name,
  u.status,
  u.global_quota_bytes,
  u.expire_at,
  u.deleted_at,
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
  expire_at,
  deleted_at,
  created_at,
  updated_at
FROM proxy_users
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: GetProxyUserByNameIncludingDeleted :one
SELECT *
FROM proxy_users
WHERE name = sqlc.arg(name);

-- name: SetProxyUserStatus :execrows
UPDATE proxy_users
SET
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: SetProxyUserDisplayName :execrows
UPDATE proxy_users
SET
  display_name = sqlc.arg(display_name),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: SetProxyUserQuota :execrows
UPDATE proxy_users
SET
  global_quota_bytes = sqlc.arg(global_quota_bytes),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: SetProxyUserExpire :execrows
UPDATE proxy_users
SET
  expire_at = sqlc.narg(expire_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: SoftDeleteProxyUser :execrows
UPDATE proxy_users
SET
  status = 'disabled',
  deleted_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: RestoreProxyUser :execrows
UPDATE proxy_users
SET
  deleted_at = NULL,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NOT NULL;
