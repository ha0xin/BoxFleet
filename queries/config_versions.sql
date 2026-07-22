-- name: NextConfigVersion :one
SELECT COALESCE(MAX(version), 0) + 1 AS next_version
FROM config_versions
WHERE node_id = sqlc.arg(node_id);

-- name: CreateConfigVersion :exec
INSERT INTO config_versions (
  id,
  node_id,
  version,
  status,
  config_json,
  config_hash,
  published_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(version),
  sqlc.arg(status),
  sqlc.arg(config_json),
  sqlc.arg(config_hash),
  CASE
    WHEN sqlc.arg(status) = 'published' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    ELSE NULL
  END
);

-- name: GetConfigVersionByHash :one
SELECT
  id,
  node_id,
  version,
  status,
  config_json,
  config_hash,
  created_at,
  published_at
FROM config_versions
WHERE node_id = sqlc.arg(node_id)
  AND config_hash = sqlc.arg(config_hash);

-- name: GetConfigVersionByID :one
SELECT
  id,
  node_id,
  version,
  status,
  config_json,
  config_hash,
  created_at,
  published_at
FROM config_versions
WHERE id = sqlc.arg(id);

-- name: ListConfigVersionsByNode :many
SELECT
  cv.id,
  cv.node_id,
  cv.version,
  cv.status,
  cv.config_json,
  cv.config_hash,
  cv.created_at,
  cv.published_at
FROM config_versions cv
JOIN nodes n ON n.id = cv.node_id
WHERE n.name = sqlc.arg(node_name)
ORDER BY cv.version DESC;

-- name: SupersedePublishedConfigVersions :exec
UPDATE config_versions
SET status = 'superseded'
WHERE node_id = sqlc.arg(node_id)
  AND status = 'published'
  AND id != sqlc.arg(keep_id);

-- name: MarkConfigVersionPublished :exec
UPDATE config_versions
SET
  status = 'published',
  published_at = COALESCE(published_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
WHERE id = sqlc.arg(id);

-- name: UpsertNodeConfigTarget :exec
INSERT INTO node_config_status (
  node_id,
  target_config_version_id,
  current_config_version_id,
  last_apply_status,
  last_apply_error
) VALUES (
  sqlc.arg(node_id),
  sqlc.arg(target_config_version_id),
  NULL,
  'pending',
  ''
)
ON CONFLICT(node_id) DO UPDATE SET
  target_config_version_id = excluded.target_config_version_id,
  last_apply_status = 'pending',
  last_apply_error = '',
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: UpdateNodeConfigApplyStatus :exec
UPDATE node_config_status
SET
  current_config_version_id = CASE
    WHEN sqlc.arg(last_apply_status) = 'applied' THEN sqlc.arg(current_config_version_id)
    ELSE current_config_version_id
  END,
  last_apply_status = sqlc.arg(last_apply_status),
  last_apply_error = sqlc.arg(last_apply_error),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE node_id = sqlc.arg(node_id);

-- name: GetNodeConfigStatusByNodeName :one
SELECT
  n.id AS node_id,
  n.name AS node_name,
  s.target_config_version_id,
  target.version AS target_version,
  target.config_hash AS target_config_hash,
  s.current_config_version_id,
  current.version AS current_version,
  current.config_hash AS current_config_hash,
  s.last_apply_status,
  s.last_apply_error,
  s.updated_at
FROM nodes n
LEFT JOIN node_config_status s ON s.node_id = n.id
LEFT JOIN config_versions target ON target.id = s.target_config_version_id
LEFT JOIN config_versions current ON current.id = s.current_config_version_id
WHERE n.name = sqlc.arg(node_name);

-- name: ListNodeConfigStatuses :many
SELECT
  n.id AS node_id,
  n.name AS node_name,
  s.target_config_version_id,
  target.version AS target_version,
  target.config_hash AS target_config_hash,
  s.current_config_version_id,
  current.version AS current_version,
  current.config_hash AS current_config_hash,
  s.last_apply_status,
  s.last_apply_error,
  s.updated_at,
  h.reported_at AS latest_heartbeat,
  h.agent_version,
  COALESCE(h.sing_box_version, n.sing_box_version) AS sing_box_version,
  h.payload_json AS heartbeat_payload_json
FROM nodes n
LEFT JOIN node_config_status s ON s.node_id = n.id
LEFT JOIN config_versions target ON target.id = s.target_config_version_id
LEFT JOIN config_versions current ON current.id = s.current_config_version_id
LEFT JOIN node_latest_heartbeats latest ON latest.node_id = n.id
LEFT JOIN node_heartbeats h ON h.id = latest.heartbeat_id
ORDER BY n.name;

-- name: GetTargetConfigByNodeName :one
SELECT
  cv.id,
  cv.node_id,
  cv.version,
  cv.status,
  cv.config_json,
  cv.config_hash,
  cv.created_at,
  cv.published_at
FROM node_config_status s
JOIN nodes n ON n.id = s.node_id
JOIN config_versions cv ON cv.id = s.target_config_version_id
WHERE n.name = sqlc.arg(node_name);
