-- name: CreateNodeHeartbeat :exec
INSERT INTO node_heartbeats (
  id,
  node_id,
  agent_version,
  sing_box_version,
  status,
  memory_bytes,
  rx_bytes,
  tx_bytes,
  payload_json,
  reported_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(agent_version),
  sqlc.arg(sing_box_version),
  sqlc.arg(status),
  sqlc.arg(memory_bytes),
  sqlc.arg(rx_bytes),
  sqlc.arg(tx_bytes),
  sqlc.arg(payload_json),
  sqlc.arg(reported_at)
);

-- name: UpsertNodeLatestHeartbeat :exec
INSERT INTO node_latest_heartbeats (node_id, heartbeat_id)
VALUES (sqlc.arg(node_id), sqlc.arg(heartbeat_id))
ON CONFLICT(node_id) DO UPDATE SET
  heartbeat_id = excluded.heartbeat_id,
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now');

-- name: TouchNodeSeen :exec
UPDATE nodes
SET
  last_seen_at = sqlc.arg(last_seen_at),
  sing_box_version = sqlc.arg(sing_box_version),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(node_id);

-- name: LatestNodeHeartbeatByNodeName :one
SELECT
  h.id,
  h.node_id,
  h.agent_version,
  h.sing_box_version,
  h.status,
  h.memory_bytes,
  h.rx_bytes,
  h.tx_bytes,
  h.payload_json,
  h.reported_at,
  h.created_at
FROM node_latest_heartbeats latest
JOIN node_heartbeats h ON h.id = latest.heartbeat_id
JOIN nodes n ON n.id = latest.node_id
WHERE n.name = sqlc.arg(node_name)
LIMIT 1;
