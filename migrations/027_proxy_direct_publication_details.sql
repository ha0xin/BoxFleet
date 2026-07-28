-- +goose Up
DROP VIEW proxy_details;

CREATE VIEW proxy_details AS
SELECT
  p.id,
  p.node_id,
  n.name AS node_name,
  n.public_host AS node_public_host,
  p.name,
  p.protocol,
  p.listen,
  p.listen_port,
  p.transport,
  p.enabled,
  p.traffic_multiplier,
  COALESCE(publication.direct_enabled, 1) AS direct_publish,
  p.settings_json,
  p.inbound_rules_json,
  p.outbound_rules_json,
  p.route_rules_json,
  p.deleted_at,
  n.deleted_at AS node_deleted_at,
  p.created_at,
  p.updated_at
FROM proxies p
JOIN nodes n ON n.id = p.node_id
LEFT JOIN proxy_publication_settings publication ON publication.proxy_id = p.id;

-- +goose Down
DROP VIEW proxy_details;

CREATE VIEW proxy_details AS
SELECT
  p.id,
  p.node_id,
  n.name AS node_name,
  n.public_host AS node_public_host,
  p.name,
  p.protocol,
  p.listen,
  p.listen_port,
  p.transport,
  p.enabled,
  p.traffic_multiplier,
  p.settings_json,
  p.inbound_rules_json,
  p.outbound_rules_json,
  p.route_rules_json,
  p.deleted_at,
  n.deleted_at AS node_deleted_at,
  p.created_at,
  p.updated_at
FROM proxies p
JOIN nodes n ON n.id = p.node_id;
