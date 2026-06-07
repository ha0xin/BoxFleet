-- name: CreateNodeToken :exec
INSERT INTO node_tokens (
  id,
  node_id,
  token_hash
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(token_hash)
);

-- name: ListActiveNodeTokensByNodeName :many
SELECT
  t.id,
  t.node_id,
  t.token_hash,
  t.created_at,
  t.last_used_at,
  t.revoked_at
FROM node_tokens t
JOIN nodes n ON n.id = t.node_id
WHERE n.name = sqlc.arg(node_name)
  AND n.status != 'disabled'
  AND t.revoked_at IS NULL
ORDER BY t.created_at DESC;

-- name: MarkNodeTokenUsed :exec
UPDATE node_tokens
SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: RevokeNodeTokensByNodeID :exec
UPDATE node_tokens
SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE node_id = sqlc.arg(node_id)
  AND revoked_at IS NULL;
