-- name: ListRetiredResourcePaths :many
WITH RECURSIVE affected_paths(id) AS (
 SELECT t.id FROM paths t JOIN endpoints e ON e.id=t.endpoint_id
 JOIN proxies p ON p.id=e.proxy_id JOIN nodes n ON n.id=p.node_id
 WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL
 UNION
 SELECT t.id FROM paths t JOIN affected_paths a ON t.dialer_path_id=a.id
)
SELECT id FROM affected_paths;

-- name: ListPathAccessUsersIncludingDeleted :many
SELECT DISTINCT proxy_user_id FROM path_accesses WHERE path_id=sqlc.arg(path_id) AND deleted_at IS NULL;

-- name: ArchivePathAccesses :exec
UPDATE path_accesses SET enabled=0, deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'), updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE path_id=sqlc.arg(path_id) AND deleted_at IS NULL;

-- name: DisableRetiredEndpoints :exec
UPDATE endpoints SET enabled=0, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE enabled=1 AND proxy_id IN (
 SELECT p.id FROM proxies p JOIN nodes n ON n.id=p.node_id WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL
);

-- name: ArchiveRetiredProxyCredentials :exec
UPDATE proxy_accesses SET enabled=0, deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'), updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE deleted_at IS NULL AND proxy_id IN (
 SELECT p.id FROM proxies p JOIN nodes n ON n.id=p.node_id WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL
);

-- name: ListDeletionProxies :many
SELECT p.id,p.node_id,p.name,p.deleted_at,n.deleted_at AS node_deleted_at FROM proxies p JOIN nodes n ON n.id=p.node_id;

-- name: ListDeletionAccesses :many
SELECT a.id,a.path_id,a.proxy_user_id,a.enabled,a.deleted_at,u.name AS user_name FROM path_accesses a JOIN proxy_users u ON u.id=a.proxy_user_id;

-- name: ListDeletionCredentials :many
SELECT a.id,a.proxy_id,a.proxy_user_id,a.enabled,a.deleted_at,u.name AS user_name FROM proxy_accesses a JOIN proxy_users u ON u.id=a.proxy_user_id;

-- name: RestoreProxyEndpoints :exec
UPDATE endpoints SET enabled=1, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE proxy_id=sqlc.arg(proxy_id) AND enabled=0;

-- name: DisableRetiredNodeBindings :exec
UPDATE user_node_bindings SET enabled=0, disabled_reason='node_deleted', updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
WHERE enabled=1 AND node_id IN (SELECT id FROM nodes WHERE deleted_at IS NOT NULL);
