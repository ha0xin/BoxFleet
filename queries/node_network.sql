-- name: GetNodeNetworkSnapshot :one
SELECT * FROM node_network_snapshots WHERE node_id = ?;

-- name: UpsertNodeNetworkSnapshot :exec
INSERT INTO node_network_snapshots
(node_id, revision, content_hash, addresses_json, collected_at, received_at, attempted_at, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node_id) DO UPDATE SET
revision = excluded.revision, content_hash = excluded.content_hash,
addresses_json = CASE WHEN excluded.error = '' THEN excluded.addresses_json ELSE node_network_snapshots.addresses_json END,
collected_at = CASE WHEN excluded.error = '' THEN excluded.collected_at ELSE node_network_snapshots.collected_at END,
received_at = CASE WHEN excluded.error = '' THEN excluded.received_at ELSE node_network_snapshots.received_at END,
attempted_at = excluded.attempted_at, error = excluded.error;

-- name: UpsertNodeObservedIP :exec
INSERT INTO node_observed_ips (node_id, family, address, source, observed_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(node_id, address, source) DO UPDATE SET
address = excluded.address, source = excluded.source, observed_at = excluded.observed_at;

-- name: ListNodeObservedIPs :many
SELECT * FROM node_observed_ips WHERE node_id IN (sqlc.slice('node_ids')) ORDER BY node_id, family, address;

-- name: ListNodeNetworkSummaries :many
SELECT node_id, revision, collected_at, received_at, attempted_at, error,
CAST(json_array_length(addresses_json) AS INTEGER) AS address_count
FROM node_network_snapshots WHERE node_id IN (sqlc.slice('node_ids'));

-- name: PruneNodeObservedIPs :exec
DELETE FROM node_observed_ips WHERE node_observed_ips.node_id = sqlc.arg(node_id)
AND (node_observed_ips.observed_at < sqlc.arg(cutoff) OR node_observed_ips.rowid IN (
 SELECT recent.rowid FROM node_observed_ips AS recent WHERE recent.node_id = sqlc.arg(node_id)
 ORDER BY recent.observed_at DESC, recent.address LIMIT -1 OFFSET 128
));
