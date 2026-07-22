-- name: CreateTrafficReport :exec
INSERT INTO traffic_reports (
  id,
  node_id,
  sequence,
  agent_boot_id,
  reported_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(sequence),
  sqlc.arg(agent_boot_id),
  sqlc.arg(reported_at)
);

-- name: GetTrafficReportBySequence :one
SELECT
  id,
  node_id,
  sequence,
  agent_boot_id,
  reported_at,
  created_at
FROM traffic_reports
WHERE node_id = sqlc.arg(node_id)
  AND agent_boot_id = sqlc.arg(agent_boot_id)
  AND sequence = sqlc.arg(sequence);

-- name: CreateTrafficUsageDelta :exec
INSERT INTO traffic_usage_deltas (
  id,
  report_id,
  node_id,
  proxy_user_id,
  proxy_id,
  auth_name,
  direction,
  raw_bytes_delta,
  effective_multiplier,
  billable_bytes_delta,
  counter_value,
  counter_epoch,
  observed_at
) VALUES (
  sqlc.arg(id),
  sqlc.arg(report_id),
  sqlc.arg(node_id),
  sqlc.arg(proxy_user_id),
  sqlc.narg(proxy_id),
  sqlc.arg(auth_name),
  sqlc.arg(direction),
  sqlc.arg(raw_bytes_delta),
  sqlc.arg(effective_multiplier),
  sqlc.arg(billable_bytes_delta),
  sqlc.arg(counter_value),
  sqlc.arg(counter_epoch),
  sqlc.arg(observed_at)
);

-- name: GetTrafficCredentialByNodeAuthName :one
SELECT
  c.proxy_user_id,
  c.proxy_id,
  COALESCE(c.traffic_multiplier, b.traffic_multiplier, p.traffic_multiplier, 1.0) AS effective_multiplier
FROM proxy_accesses c
JOIN proxies p ON p.id = c.proxy_id
JOIN nodes n ON n.id = p.node_id
JOIN user_node_bindings b ON b.proxy_user_id = c.proxy_user_id AND b.node_id = n.id
WHERE n.name = sqlc.arg(node_name)
  AND c.auth_name = sqlc.arg(auth_name);

-- name: SumTrafficByUser :many
SELECT
  u.name AS user_name,
  d.direction,
  CAST(COALESCE(SUM(d.raw_bytes_delta), 0) AS INTEGER) AS raw_bytes,
  CAST(COALESCE(SUM(d.billable_bytes_delta), 0) AS INTEGER) AS billable_bytes
FROM traffic_usage_deltas d
JOIN proxy_users u ON u.id = d.proxy_user_id
WHERE u.name = sqlc.arg(user_name)
  AND u.deleted_at IS NULL
GROUP BY u.name, d.direction
ORDER BY d.direction;

-- name: SumTrafficByAllUsers :many
SELECT
  u.name AS user_name,
  d.direction,
  CAST(COALESCE(SUM(d.raw_bytes_delta), 0) AS INTEGER) AS raw_bytes,
  CAST(COALESCE(SUM(d.billable_bytes_delta), 0) AS INTEGER) AS billable_bytes
FROM traffic_usage_deltas d
JOIN proxy_users u ON u.id = d.proxy_user_id
WHERE u.deleted_at IS NULL
GROUP BY u.name, d.direction
ORDER BY u.name, d.direction;
