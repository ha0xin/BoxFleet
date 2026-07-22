-- name: CreateNodeOperation :exec
INSERT INTO node_operations (
  id,
  node_id,
  kind,
  payload_json,
  idempotency_key,
  required_capabilities_json,
  not_before,
  expires_at,
  requested_by,
  retry_of
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(kind),
  sqlc.arg(payload_json),
  sqlc.arg(idempotency_key),
  sqlc.arg(required_capabilities_json),
  sqlc.narg(not_before),
  sqlc.narg(expires_at),
  sqlc.arg(requested_by),
  sqlc.narg(retry_of)
);

-- name: GetNodeOperationByID :one
SELECT * FROM node_operations WHERE id = sqlc.arg(id);

-- name: GetNodeOperationByIdempotencyKey :one
SELECT *
FROM node_operations
WHERE node_id = sqlc.arg(node_id)
  AND idempotency_key = sqlc.arg(idempotency_key);

-- name: GetActiveNodeOperation :one
SELECT *
FROM node_operations
WHERE node_id = sqlc.arg(node_id)
  AND status IN ('queued', 'running')
ORDER BY requested_at
LIMIT 1;

-- name: ListActiveNodeOperations :many
SELECT *
FROM node_operations
WHERE status IN ('queued', 'running')
ORDER BY requested_at;

-- name: ListNodeOperations :many
SELECT *
FROM node_operations
WHERE node_id = sqlc.arg(node_id)
ORDER BY requested_at DESC
LIMIT sqlc.arg(result_limit)
OFFSET sqlc.arg(result_offset);

-- name: CountNodeOperations :one
SELECT COUNT(*)
FROM node_operations
WHERE node_id = sqlc.arg(node_id);

-- name: ExpireQueuedNodeOperations :exec
UPDATE node_operations
SET
  status = 'expired',
  phase = 'expired',
  finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  error = 'operation expired before it was claimed'
WHERE node_id = sqlc.arg(node_id)
  AND status = 'queued'
  AND expires_at IS NOT NULL
  AND expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: ClaimNodeOperation :one
UPDATE node_operations
SET
  status = 'running',
  phase = CASE WHEN status = 'queued' THEN 'claimed' ELSE phase END,
  attempt = attempt + 1,
  lease_token_hash = sqlc.arg(lease_token_hash),
  lease_expires_at = sqlc.arg(lease_expires_at),
  claimed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
  started_at = COALESCE(started_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND node_id = sqlc.arg(node_id)
  AND cancel_requested = 0
  AND (not_before IS NULL OR not_before <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
  AND (expires_at IS NULL OR expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
  AND (
    status = 'queued'
    OR (
      status = 'running'
      AND lease_expires_at IS NOT NULL
      AND lease_expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    )
  )
RETURNING *;

-- name: ResumeNodeOperationLease :one
UPDATE node_operations
SET
  lease_expires_at = sqlc.arg(lease_expires_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND node_id = sqlc.arg(node_id)
  AND status = 'running'
  AND lease_token_hash = sqlc.arg(lease_token_hash)
  AND lease_expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING *;

-- name: RenewNodeOperationLease :one
UPDATE node_operations
SET
  lease_expires_at = sqlc.arg(lease_expires_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND node_id = sqlc.arg(node_id)
  AND status = 'running'
  AND lease_token_hash = sqlc.arg(lease_token_hash)
  AND attempt = sqlc.arg(attempt)
  AND lease_expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING cancel_requested;

-- name: RequestNodeOperationCancel :one
UPDATE node_operations
SET
  cancel_requested = 1,
  status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE status END,
  phase = CASE WHEN status = 'queued' THEN 'cancelled' ELSE phase END,
  finished_at = CASE
    WHEN status = 'queued' THEN strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
    ELSE finished_at
  END,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND status IN ('queued', 'running')
RETURNING *;

-- name: AppendNodeOperationEvent :exec
INSERT INTO node_operation_events (
  id,
  operation_id,
  attempt,
  sequence,
  status,
  phase,
  message,
  details_json,
  result_json,
  error,
  reported_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(operation_id),
  sqlc.arg(attempt),
  sqlc.arg(sequence),
  sqlc.arg(status),
  sqlc.arg(phase),
  sqlc.arg(message),
  sqlc.arg(details_json),
  sqlc.arg(result_json),
  sqlc.arg(error),
  sqlc.arg(reported_at)
) ON CONFLICT(operation_id, attempt, sequence) DO NOTHING;

-- name: ApplyNodeOperationEvent :one
UPDATE node_operations
SET
  status = sqlc.arg(next_status),
  phase = sqlc.arg(phase),
  result_json = sqlc.arg(result_json),
  error = sqlc.arg(error),
  lease_expires_at = CASE
    WHEN sqlc.arg(next_status) = 'running' THEN sqlc.arg(lease_expires_at)
    ELSE NULL
  END,
  lease_token_hash = CASE
    WHEN sqlc.arg(next_status) = 'running' THEN lease_token_hash
    ELSE NULL
  END,
  finished_at = COALESCE(sqlc.narg(next_finished_at), finished_at),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
  AND node_id = sqlc.arg(node_id)
  AND status = 'running'
  AND lease_token_hash = sqlc.arg(lease_token_hash)
  AND attempt = sqlc.arg(attempt)
  AND lease_expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
RETURNING *;

-- name: ListNodeOperationEvents :many
SELECT *
FROM node_operation_events
WHERE operation_id = sqlc.arg(operation_id)
ORDER BY attempt, sequence;

-- name: GetNodeOperationEvent :one
SELECT *
FROM node_operation_events
WHERE operation_id = sqlc.arg(operation_id)
  AND attempt = sqlc.arg(attempt)
  AND sequence = sqlc.arg(sequence);

-- name: LastNodeOperationEventSequence :one
SELECT CAST(COALESCE(MAX(sequence), 0) AS INTEGER)
FROM node_operation_events
WHERE operation_id = sqlc.arg(operation_id)
  AND attempt = sqlc.arg(attempt);
