-- name: CreateSubscriptionToken :exec
INSERT INTO subscription_tokens (
  id,
  proxy_user_id,
  token
) VALUES (
  sqlc.arg(id),
  sqlc.arg(proxy_user_id),
  sqlc.arg(token)
);

-- name: GetActiveSubscriptionTokenByUserName :one
SELECT
  t.id,
  t.proxy_user_id,
  u.name AS proxy_user_name,
  t.token,
  t.created_at,
  t.last_used_at,
  t.revoked_at
FROM subscription_tokens t
JOIN proxy_users u ON u.id = t.proxy_user_id
WHERE u.name = sqlc.arg(proxy_user_name)
  AND t.revoked_at IS NULL;

-- name: GetActiveSubscriptionTokenByValue :one
SELECT
  t.id,
  t.proxy_user_id,
  u.name AS proxy_user_name,
  t.token,
  t.created_at,
  t.last_used_at,
  t.revoked_at
FROM subscription_tokens t
JOIN proxy_users u ON u.id = t.proxy_user_id
WHERE t.token = sqlc.arg(token)
  AND t.revoked_at IS NULL;

-- name: RevokeActiveSubscriptionTokenByUserID :execrows
UPDATE subscription_tokens
SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE proxy_user_id = sqlc.arg(proxy_user_id)
  AND revoked_at IS NULL;

-- name: MarkSubscriptionTokenUsed :exec
UPDATE subscription_tokens
SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL;
