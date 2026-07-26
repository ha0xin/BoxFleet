-- name: CreatePath :exec
INSERT INTO paths (
  id, name, display_name, endpoint_id, dialer_path_id,
  enabled, visibility, managed, sort_order
) VALUES (
  sqlc.arg(id), sqlc.arg(name), sqlc.arg(display_name), sqlc.arg(endpoint_id),
  sqlc.narg(dialer_path_id), sqlc.arg(enabled), sqlc.arg(visibility), sqlc.arg(managed),
  sqlc.arg(sort_order)
);

-- name: GetPathByID :one
SELECT * FROM paths WHERE id = sqlc.arg(id);

-- name: ListPaths :many
SELECT * FROM paths ORDER BY sort_order, name, id;

-- name: ListPathsByEndpointID :many
SELECT * FROM paths
WHERE endpoint_id = sqlc.arg(endpoint_id)
ORDER BY sort_order, name, id;

-- name: CountPathsByEndpointID :one
SELECT COUNT(*) FROM paths WHERE endpoint_id = sqlc.arg(endpoint_id);

-- name: UpdatePath :execrows
UPDATE paths
SET name = sqlc.arg(name),
    display_name = sqlc.arg(display_name),
    endpoint_id = sqlc.arg(endpoint_id),
    dialer_path_id = sqlc.narg(dialer_path_id),
    enabled = sqlc.arg(enabled),
    visibility = sqlc.arg(visibility),
    managed = sqlc.arg(managed),
    sort_order = sqlc.arg(sort_order),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: SetPathEnabled :execrows
UPDATE paths
SET enabled = sqlc.arg(enabled),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: DeletePath :execrows
DELETE FROM paths WHERE id = sqlc.arg(id);

-- name: CreatePathAccess :exec
INSERT INTO path_accesses (id, path_id, proxy_user_id, enabled)
VALUES (sqlc.arg(id), sqlc.arg(path_id), sqlc.arg(proxy_user_id), sqlc.arg(enabled));

-- name: GetPathAccessByIDs :one
SELECT * FROM path_accesses
WHERE path_id = sqlc.arg(path_id) AND proxy_user_id = sqlc.arg(proxy_user_id);

-- name: RestorePathAccess :execrows
UPDATE path_accesses
SET enabled = 1,
    deleted_at = NULL,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE path_id = sqlc.arg(path_id) AND proxy_user_id = sqlc.arg(proxy_user_id);

-- name: SoftDeletePathAccess :execrows
UPDATE path_accesses
SET enabled = 0,
    deleted_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE path_id = sqlc.arg(path_id)
  AND proxy_user_id = sqlc.arg(proxy_user_id)
  AND deleted_at IS NULL;

-- name: ListActivePathAccessesByUserID :many
SELECT * FROM path_accesses
WHERE proxy_user_id = sqlc.arg(proxy_user_id)
  AND enabled = 1
  AND deleted_at IS NULL
ORDER BY created_at, id;

-- name: ListUserNamesWithActivePathAccess :many
SELECT DISTINCT u.name
FROM path_accesses a
JOIN proxy_users u ON u.id = a.proxy_user_id
WHERE a.enabled = 1
  AND a.deleted_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY u.name;

-- name: ListUserNamesByPathID :many
SELECT u.name
FROM path_accesses a
JOIN proxy_users u ON u.id = a.proxy_user_id
WHERE a.path_id = sqlc.arg(path_id)
  AND a.enabled = 1
  AND a.deleted_at IS NULL
  AND u.deleted_at IS NULL
ORDER BY u.name;
