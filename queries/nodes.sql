-- name: CreateNode :exec
INSERT INTO nodes (
  id,
  name,
  public_host,
  hosts_json,
  api_base_url,
  status
) VALUES (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(public_host),
  sqlc.arg(hosts_json),
  sqlc.arg(api_base_url),
  'active'
);

-- name: ListNodes :many
SELECT
  id,
  name,
  public_host,
  hosts_json,
  api_base_url,
  status,
  sing_box_version,
  last_seen_at,
  deleted_at,
  created_at,
  updated_at
FROM nodes
WHERE deleted_at IS NULL
ORDER BY name;

-- name: GetNodeByName :one
SELECT
  id,
  name,
  public_host,
  hosts_json,
  api_base_url,
  status,
  sing_box_version,
  last_seen_at,
  deleted_at,
  created_at,
  updated_at
FROM nodes
WHERE deleted_at IS NULL
  AND (name = sqlc.arg(name)
   OR id = (
     SELECT node_id
     FROM node_name_aliases
     WHERE alias = sqlc.arg(name)
   ));

-- name: GetNodeByNameIncludingDeleted :one
SELECT *
FROM nodes
WHERE name = sqlc.arg(name)
   OR id = (
     SELECT node_id
     FROM node_name_aliases
     WHERE alias = sqlc.arg(name)
   );

-- name: GetNodeIDByNameOrAlias :one
SELECT id
FROM nodes
WHERE name = sqlc.arg(name)
   OR id = (
     SELECT node_id
     FROM node_name_aliases
     WHERE alias = sqlc.arg(name)
   );

-- name: CreateNodeNameAlias :exec
INSERT INTO node_name_aliases (alias, node_id)
VALUES (sqlc.arg(alias), sqlc.arg(node_id));

-- name: DeleteNodeNameAlias :exec
DELETE FROM node_name_aliases
WHERE alias = sqlc.arg(alias) AND node_id = sqlc.arg(node_id);

-- name: RenameNodeByID :execrows
UPDATE nodes
SET
  name = sqlc.arg(name),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: SetNodeStatus :execrows
UPDATE nodes
SET
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: PromotePendingNodeToActive :execrows
UPDATE nodes
SET
  status = 'active',
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL
  AND status = 'pending';

-- name: UpdateNode :execrows
UPDATE nodes
SET
  public_host = sqlc.arg(public_host),
  hosts_json = sqlc.arg(hosts_json),
  api_base_url = sqlc.arg(api_base_url),
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: SoftDeleteNode :execrows
UPDATE nodes
SET
  status = 'disabled',
  deleted_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NULL;

-- name: RestoreNode :execrows
UPDATE nodes
SET
  deleted_at = NULL,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND deleted_at IS NOT NULL;
