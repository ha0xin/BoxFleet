-- name: CreateNode :exec
INSERT INTO nodes (
  id,
  name,
  public_host,
  api_base_url,
  status
) VALUES (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(public_host),
  sqlc.arg(api_base_url),
  'active'
);

-- name: ListNodes :many
SELECT
  id,
  name,
  public_host,
  api_base_url,
  status,
  sing_box_version,
  last_seen_at,
  created_at,
  updated_at
FROM nodes
ORDER BY name;

-- name: GetNodeByName :one
SELECT
  id,
  name,
  public_host,
  api_base_url,
  status,
  sing_box_version,
  last_seen_at,
  created_at,
  updated_at
FROM nodes
WHERE name = sqlc.arg(name);

-- name: SetNodeStatus :execrows
UPDATE nodes
SET
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);

-- name: PromotePendingNodeToActive :execrows
UPDATE nodes
SET
  status = 'active',
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name)
  AND status = 'pending';

-- name: UpdateNode :execrows
UPDATE nodes
SET
  public_host = sqlc.arg(public_host),
  api_base_url = sqlc.arg(api_base_url),
  status = sqlc.arg(status),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE name = sqlc.arg(name);
