-- name: CreateMihomoProfile :exec
INSERT INTO mihomo_profiles (
  id,
  name,
  description,
  draft_document_json,
  proxy_user_id
) VALUES (
  sqlc.arg(id),
  sqlc.arg(name),
  sqlc.arg(description),
  sqlc.arg(draft_document_json),
  sqlc.arg(proxy_user_id)
);

-- name: GetMihomoProfile :one
SELECT
  p.id,
  p.name,
  p.description,
  COALESCE(p.proxy_user_id, '') AS proxy_user_id,
  COALESCE(u.name, '') AS proxy_user_name,
  p.draft_document_json AS document_json,
  p.created_at,
  p.updated_at
FROM mihomo_profiles p
LEFT JOIN proxy_users u ON u.id = p.proxy_user_id
WHERE p.id = sqlc.arg(id);

-- name: ListMihomoProfiles :many
SELECT
  p.id,
  p.name,
  p.description,
  COALESCE(p.proxy_user_id, '') AS proxy_user_id,
  COALESCE(u.name, '') AS proxy_user_name,
  p.draft_document_json AS document_json,
  p.created_at,
  p.updated_at
FROM mihomo_profiles p
LEFT JOIN proxy_users u ON u.id = p.proxy_user_id
WHERE p.proxy_user_id IS NOT NULL
ORDER BY p.updated_at DESC, p.name, p.id;

-- name: UpdateMihomoProfileDocument :execrows
UPDATE mihomo_profiles
SET
  draft_document_json = sqlc.arg(document_json),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: AssignMihomoProfileToUser :exec
INSERT INTO proxy_user_mihomo_profiles (proxy_user_id, profile_id)
VALUES (sqlc.arg(proxy_user_id), sqlc.arg(profile_id))
ON CONFLICT(proxy_user_id) DO UPDATE SET
  profile_id = excluded.profile_id,
  assigned_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: GetMihomoProfileIDForUser :one
SELECT COALESCE(binding.profile_id, 'mhp_default') AS profile_id
FROM proxy_users u
LEFT JOIN proxy_user_mihomo_profiles binding ON binding.proxy_user_id = u.id
WHERE u.name = sqlc.arg(proxy_user_name)
  AND u.deleted_at IS NULL;

-- name: CreateMihomoRewriteTemplate :exec
INSERT INTO mihomo_rewrite_templates (
  id, name, description, kind, content, built_in
) VALUES (
  sqlc.arg(id), sqlc.arg(name), sqlc.arg(description),
  sqlc.arg(kind), sqlc.arg(content), 0
);

-- name: GetMihomoRewriteTemplate :one
SELECT id, name, description, kind, content, built_in, created_at, updated_at
FROM mihomo_rewrite_templates
WHERE id = sqlc.arg(id);

-- name: ListMihomoRewriteTemplates :many
SELECT id, name, description, kind, content, built_in, created_at, updated_at
FROM mihomo_rewrite_templates
ORDER BY built_in DESC, name, id;

-- name: UpdateMihomoRewriteTemplate :execrows
UPDATE mihomo_rewrite_templates
SET name = sqlc.arg(name),
    description = sqlc.arg(description),
    kind = sqlc.arg(kind),
    content = sqlc.arg(content),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND built_in = 0;
