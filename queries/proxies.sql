-- name: CreateProxy :exec
INSERT INTO proxies (
  id,
  node_id,
  name,
  protocol,
  listen,
  listen_port,
  transport,
  enabled,
  traffic_multiplier,
  settings_json,
  inbound_rules_json,
  outbound_rules_json,
  route_rules_json
) VALUES (
  sqlc.arg(id),
  sqlc.arg(node_id),
  sqlc.arg(name),
  sqlc.arg(protocol),
  sqlc.arg(listen),
  sqlc.arg(listen_port),
  sqlc.arg(transport),
  sqlc.arg(enabled),
  sqlc.arg(traffic_multiplier),
  sqlc.arg(settings_json),
  sqlc.arg(inbound_rules_json),
  sqlc.arg(outbound_rules_json),
  sqlc.arg(route_rules_json)
);

-- name: ListProxies :many
SELECT *
FROM proxy_details
ORDER BY node_name, listen_port, name;

-- name: ListProxiesByNodeName :many
SELECT *
FROM proxy_details
WHERE node_name = sqlc.arg(node_name)
ORDER BY node_name, listen_port, name;

-- name: GetProxyByNodeAndName :one
SELECT *
FROM proxy_details
WHERE node_name = sqlc.arg(node_name) AND name = sqlc.arg(name);

-- name: UpdateProxy :execrows
UPDATE proxies
SET
  listen = sqlc.arg(listen),
  listen_port = sqlc.arg(listen_port),
  transport = sqlc.arg(transport),
  enabled = sqlc.arg(enabled),
  traffic_multiplier = sqlc.arg(traffic_multiplier),
  settings_json = sqlc.arg(settings_json),
  inbound_rules_json = sqlc.arg(inbound_rules_json),
  outbound_rules_json = sqlc.arg(outbound_rules_json),
  route_rules_json = sqlc.arg(route_rules_json),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE node_id = sqlc.arg(node_id) AND name = sqlc.arg(name);

-- name: SetProxyEnabled :execrows
UPDATE proxies
SET
  enabled = sqlc.arg(enabled),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE node_id = sqlc.arg(node_id) AND name = sqlc.arg(name);
