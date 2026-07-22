-- name: CreateNodeUpdateCampaign :exec
INSERT INTO node_update_campaigns (
  id, release, components_json, idempotency_key, spec_hash, batch_size, requested_by
) VALUES (
  sqlc.arg(id), sqlc.arg(release), sqlc.arg(components_json),
  sqlc.arg(idempotency_key), sqlc.arg(spec_hash), sqlc.arg(batch_size), sqlc.arg(requested_by)
);

-- name: GetNodeUpdateCampaign :one
SELECT * FROM node_update_campaigns WHERE id = sqlc.arg(id);

-- name: GetNodeUpdateCampaignByIdempotencyKey :one
SELECT * FROM node_update_campaigns WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: GetActiveNodeUpdateCampaign :one
SELECT * FROM node_update_campaigns
WHERE status IN ('queued', 'running', 'paused')
ORDER BY requested_at DESC
LIMIT 1;

-- name: ListNodeUpdateCampaigns :many
SELECT * FROM node_update_campaigns
ORDER BY requested_at DESC
LIMIT sqlc.arg(result_limit) OFFSET sqlc.arg(result_offset);

-- name: CreateNodeUpdateCampaignMember :exec
INSERT INTO node_update_campaign_members (
  campaign_id, node_id, position, batch_number, kind, payload_json
) VALUES (
  sqlc.arg(campaign_id), sqlc.arg(node_id), sqlc.arg(position),
  sqlc.arg(batch_number), sqlc.arg(kind), sqlc.arg(payload_json)
);

-- name: ListNodeUpdateCampaignMembers :many
SELECT
  m.campaign_id,
  m.node_id,
  n.name AS node_name,
  m.position,
  m.batch_number,
  m.kind,
  m.payload_json,
  m.operation_id,
  m.status,
  m.error,
  m.started_at,
  m.finished_at,
  m.updated_at
FROM node_update_campaign_members m
JOIN nodes n ON n.id = m.node_id
WHERE m.campaign_id = sqlc.arg(campaign_id)
ORDER BY m.position;

-- name: AttachNodeUpdateCampaignOperation :exec
UPDATE node_update_campaign_members
SET
  operation_id = sqlc.arg(operation_id),
  status = sqlc.arg(status),
  started_at = COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE campaign_id = sqlc.arg(campaign_id)
  AND node_id = sqlc.arg(node_id)
  AND operation_id IS NULL;

-- name: ReplaceNodeUpdateCampaignOperationForRetry :exec
UPDATE node_update_campaign_members
SET
  operation_id = sqlc.arg(operation_id),
  status = sqlc.arg(status),
  error = '',
  started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  finished_at = NULL,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE campaign_id = sqlc.arg(campaign_id)
  AND node_id = sqlc.arg(node_id)
  AND status IN ('failed', 'cancelled');

-- name: UpdateNodeUpdateCampaignMemberState :exec
UPDATE node_update_campaign_members
SET
  status = sqlc.arg(status),
  error = sqlc.arg(error),
  finished_at = COALESCE(sqlc.narg(next_finished_at), finished_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE campaign_id = sqlc.arg(campaign_id)
  AND node_id = sqlc.arg(node_id);

-- name: UpdateNodeUpdateCampaignState :exec
UPDATE node_update_campaigns
SET
  status = sqlc.arg(status),
  current_batch = sqlc.arg(current_batch),
  error = sqlc.arg(error),
  started_at = COALESCE(sqlc.narg(next_started_at), started_at),
  finished_at = COALESCE(sqlc.narg(next_finished_at), finished_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

-- name: GetNodeUpdateCampaignIDByOperation :one
SELECT campaign_id
FROM node_update_campaign_members
WHERE operation_id = sqlc.arg(operation_id);
