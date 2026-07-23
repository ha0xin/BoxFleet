-- name: CreateMihomoProfileSubscriptionToken :exec
INSERT INTO mihomo_profile_subscription_tokens (id, profile_id, token)
VALUES (sqlc.arg(id), sqlc.arg(profile_id), sqlc.arg(token));

-- name: GetActiveMihomoProfileSubscriptionToken :one
SELECT
  t.id, t.profile_id, p.name AS profile_name,
  p.proxy_user_id, u.name AS proxy_user_name,
  t.token, t.created_at, t.last_used_at, t.revoked_at
FROM mihomo_profile_subscription_tokens t
JOIN mihomo_profiles p ON p.id = t.profile_id
JOIN proxy_users u ON u.id = p.proxy_user_id
WHERE t.profile_id = sqlc.arg(profile_id)
  AND u.deleted_at IS NULL
  AND t.revoked_at IS NULL;

-- name: GetActiveMihomoProfileSubscriptionTokenByValue :one
SELECT
  t.id, t.profile_id, p.name AS profile_name,
  p.proxy_user_id, u.name AS proxy_user_name,
  t.token, t.created_at, t.last_used_at, t.revoked_at
FROM mihomo_profile_subscription_tokens t
JOIN mihomo_profiles p ON p.id = t.profile_id
JOIN proxy_users u ON u.id = p.proxy_user_id
WHERE t.token = sqlc.arg(token)
  AND u.deleted_at IS NULL
  AND t.revoked_at IS NULL;

-- name: RevokeActiveMihomoProfileSubscriptionToken :execrows
UPDATE mihomo_profile_subscription_tokens
SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE profile_id = sqlc.arg(profile_id)
  AND revoked_at IS NULL;

-- name: MarkMihomoProfileSubscriptionTokenUsed :exec
UPDATE mihomo_profile_subscription_tokens
SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND revoked_at IS NULL;
