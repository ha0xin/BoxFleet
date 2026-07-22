-- +goose Up
ALTER TABLE proxy_users ADD COLUMN deleted_at TEXT;
ALTER TABLE nodes ADD COLUMN deleted_at TEXT;
ALTER TABLE proxies ADD COLUMN deleted_at TEXT;
ALTER TABLE proxy_accesses ADD COLUMN deleted_at TEXT;

DROP VIEW proxy_access_details;
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

CREATE VIEW proxy_access_details AS
SELECT
  a.id,
  a.proxy_id,
  a.proxy_user_id,
  u.name AS proxy_user_name,
  p.node_id,
  n.name AS node_name,
  n.public_host AS node_public_host,
  p.name AS proxy_name,
  p.protocol,
  p.listen,
  p.listen_port,
  p.transport,
  p.traffic_multiplier AS proxy_traffic_multiplier,
  p.enabled AS proxy_enabled,
  p.settings_json,
  a.auth_name,
  a.enabled,
  a.quota_bytes,
  a.traffic_multiplier,
  a.credential_json,
  u.status AS proxy_user_status,
  n.status AS node_status,
  a.deleted_at,
  p.deleted_at AS proxy_deleted_at,
  u.deleted_at AS proxy_user_deleted_at,
  n.deleted_at AS node_deleted_at,
  a.created_at,
  a.updated_at
FROM proxy_accesses a
JOIN proxy_users u ON u.id = a.proxy_user_id
JOIN proxies p ON p.id = a.proxy_id
JOIN nodes n ON n.id = p.node_id;

-- +goose Down
DROP VIEW proxy_access_details;
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
  p.created_at,
  p.updated_at
FROM proxies p
JOIN nodes n ON n.id = p.node_id;

CREATE VIEW proxy_access_details AS
SELECT
  a.id,
  a.proxy_id,
  a.proxy_user_id,
  u.name AS proxy_user_name,
  p.node_id,
  n.name AS node_name,
  n.public_host AS node_public_host,
  p.name AS proxy_name,
  p.protocol,
  p.listen,
  p.listen_port,
  p.transport,
  p.traffic_multiplier AS proxy_traffic_multiplier,
  p.enabled AS proxy_enabled,
  p.settings_json,
  a.auth_name,
  a.enabled,
  a.quota_bytes,
  a.traffic_multiplier,
  a.credential_json,
  u.status AS proxy_user_status,
  n.status AS node_status,
  a.created_at,
  a.updated_at
FROM proxy_accesses a
JOIN proxy_users u ON u.id = a.proxy_user_id
JOIN proxies p ON p.id = a.proxy_id
JOIN nodes n ON n.id = p.node_id;

ALTER TABLE proxy_accesses DROP COLUMN deleted_at;
ALTER TABLE proxies DROP COLUMN deleted_at;
ALTER TABLE nodes DROP COLUMN deleted_at;
ALTER TABLE proxy_users DROP COLUMN deleted_at;
