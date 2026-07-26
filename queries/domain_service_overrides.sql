-- name: ListDomainServiceOverrides :many
SELECT
  suffix,
  service,
  label,
  category,
  created_at,
  updated_at
FROM domain_service_overrides
ORDER BY suffix;

-- name: GetDomainServiceOverride :one
SELECT
  suffix,
  service,
  label,
  category,
  created_at,
  updated_at
FROM domain_service_overrides
WHERE suffix = sqlc.arg(suffix);

-- name: UpsertDomainServiceOverride :exec
INSERT INTO domain_service_overrides (
  suffix,
  service,
  label,
  category
) VALUES (
  sqlc.arg(suffix),
  sqlc.arg(service),
  sqlc.arg(label),
  sqlc.arg(category)
) ON CONFLICT(suffix) DO UPDATE SET
  service = excluded.service,
  label = excluded.label,
  category = excluded.category,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: DeleteDomainServiceOverride :exec
DELETE FROM domain_service_overrides
WHERE suffix = sqlc.arg(suffix);
