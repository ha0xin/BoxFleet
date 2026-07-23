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
  p.draft_document_json,
  COALESCE(pub.revision_id, '') AS published_revision_id,
  COALESCE(r.version, 0) AS published_version,
  COALESCE(r.document_json, '{"rewrites":[]}') AS published_document_json,
  p.created_at,
  p.updated_at
FROM mihomo_profiles p
LEFT JOIN proxy_users u ON u.id = p.proxy_user_id
LEFT JOIN mihomo_profile_publications pub ON pub.profile_id = p.id
LEFT JOIN mihomo_profile_revisions r ON r.id = pub.revision_id
WHERE p.id = sqlc.arg(id);

-- name: ListMihomoProfiles :many
SELECT
  p.id,
  p.name,
  p.description,
  COALESCE(p.proxy_user_id, '') AS proxy_user_id,
  COALESCE(u.name, '') AS proxy_user_name,
  p.draft_document_json,
  COALESCE(pub.revision_id, '') AS published_revision_id,
  COALESCE(r.version, 0) AS published_version,
  COALESCE(r.document_json, '{"rewrites":[]}') AS published_document_json,
  p.created_at,
  p.updated_at
FROM mihomo_profiles p
LEFT JOIN proxy_users u ON u.id = p.proxy_user_id
LEFT JOIN mihomo_profile_publications pub ON pub.profile_id = p.id
LEFT JOIN mihomo_profile_revisions r ON r.id = pub.revision_id
WHERE p.proxy_user_id IS NOT NULL
ORDER BY p.updated_at DESC, p.name, p.id;

-- name: UpdateMihomoProfileDraft :execrows
UPDATE mihomo_profiles
SET
  draft_document_json = sqlc.arg(draft_document_json),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: NextMihomoProfileVersion :one
SELECT COALESCE(MAX(version), 0) + 1
FROM mihomo_profile_revisions
WHERE profile_id = sqlc.arg(profile_id);

-- name: CreateMihomoProfileRevision :exec
INSERT INTO mihomo_profile_revisions (
  id,
  profile_id,
  version,
  document_json
) VALUES (
  sqlc.arg(id),
  sqlc.arg(profile_id),
  sqlc.arg(version),
  sqlc.arg(document_json)
);

-- name: GetMihomoProfileRevision :one
SELECT id, profile_id, version, document_json, created_at
FROM mihomo_profile_revisions
WHERE id = sqlc.arg(id)
  AND profile_id = sqlc.arg(profile_id);

-- name: ListMihomoProfileRevisions :many
SELECT id, profile_id, version, document_json, created_at
FROM mihomo_profile_revisions
WHERE profile_id = sqlc.arg(profile_id)
ORDER BY version DESC;

-- name: PublishMihomoProfileRevision :exec
INSERT INTO mihomo_profile_publications (profile_id, revision_id)
VALUES (sqlc.arg(profile_id), sqlc.arg(revision_id))
ON CONFLICT(profile_id) DO UPDATE SET
  revision_id = excluded.revision_id,
  published_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

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
