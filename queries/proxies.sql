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
WHERE node_id = (
    SELECT n.id
    FROM nodes n
    WHERE n.name = sqlc.arg(node_name)
       OR n.id = (
         SELECT node_id
         FROM node_name_aliases
         WHERE alias = sqlc.arg(node_name)
       )
  )
  AND id = (
    SELECT p.id
    FROM proxies p
    WHERE p.name = sqlc.arg(name)
       OR p.id = (
         SELECT proxy_id
         FROM proxy_name_aliases
         WHERE alias = sqlc.arg(name)
       )
  );

-- name: GetProxyIDByNameOrAlias :one
SELECT id
FROM proxies
WHERE name = sqlc.arg(name)
   OR id = (
     SELECT proxy_id
     FROM proxy_name_aliases
     WHERE alias = sqlc.arg(name)
   );

-- name: CreateProxyNameAlias :exec
INSERT INTO proxy_name_aliases (alias, proxy_id)
VALUES (sqlc.arg(alias), sqlc.arg(proxy_id));

-- name: DeleteProxyNameAlias :exec
DELETE FROM proxy_name_aliases
WHERE alias = sqlc.arg(alias) AND proxy_id = sqlc.arg(proxy_id);

-- name: RenameProxyByID :execrows
UPDATE proxies
SET
  name = sqlc.arg(name),
  updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id);

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
