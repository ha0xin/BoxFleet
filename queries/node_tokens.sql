-- name: CreateNodeToken :exec
INSERT INTO node_tokens (
  id,
  node_id,
  token_hash,
  token_digest
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(token_hash),
  sqlc.arg(token_digest)
);

-- name: ListNodeNamesWithActiveTokens :many
SELECT DISTINCT n.name
FROM nodes n
JOIN node_tokens t ON t.node_id = n.id
WHERE t.revoked_at IS NULL;

-- name: GetActiveNodeTokenByDigest :one
SELECT
  t.id,
  t.node_id,
  t.token_hash,
  t.token_digest,
  t.created_at,
  t.last_used_at,
  t.revoked_at
FROM node_tokens t
JOIN nodes n ON n.id = t.node_id
WHERE n.name = sqlc.arg(node_name)
  AND t.revoked_at IS NULL
  AND t.token_digest = sqlc.arg(token_digest);

-- name: ListActiveNodeTokensByNodeName :many
SELECT
  t.id,
  t.node_id,
  t.token_hash,
  t.token_digest,
  t.created_at,
  t.last_used_at,
  t.revoked_at
FROM node_tokens t
JOIN nodes n ON n.id = t.node_id
WHERE n.name = sqlc.arg(node_name)
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
