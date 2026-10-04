-- +goose Up
DROP TRIGGER IF EXISTS log_events_search_after_insert;
DROP TRIGGER IF EXISTS log_events_search_after_update;
DROP TRIGGER IF EXISTS log_events_search_after_delete;
DROP TRIGGER IF EXISTS log_events_search_after_node_rename;
DROP TRIGGER IF EXISTS log_events_search_after_user_rename;

DROP VIEW proxy_access_details;

DROP VIEW proxy_details;

CREATE TABLE nodes_replacement (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  public_host TEXT NOT NULL,
  hosts_json TEXT NOT NULL DEFAULT '[]',
  api_base_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'active', 'disabled', 'degraded')),
  sing_box_version TEXT NOT NULL DEFAULT '',
  last_seen_at TEXT,
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO nodes_replacement SELECT id,name,public_host,hosts_json,api_base_url,status,COALESCE(sing_box_version,''),last_seen_at,deleted_at,created_at,updated_at FROM nodes;

DROP TABLE nodes;

ALTER TABLE nodes_replacement RENAME TO nodes;

CREATE TABLE proxy_users_replacement (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'disabled', 'expired', 'quota_exceeded')),
  global_quota_bytes INTEGER NOT NULL DEFAULT 0
    CHECK (global_quota_bytes >= 0),
  expire_at TEXT,
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO proxy_users_replacement SELECT id,name,display_name,status,global_quota_bytes,expire_at,deleted_at,COALESCE(created_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')),COALESCE(updated_at,created_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')) FROM proxy_users;

DROP TABLE proxy_users;

ALTER TABLE proxy_users_replacement RENAME TO proxy_users;

CREATE TABLE proxies_replacement (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL
    CHECK (protocol IN ('vless_reality', 'shadowsocks_2022', 'hysteria2')),
  listen TEXT NOT NULL DEFAULT '::',
  listen_port INTEGER NOT NULL CHECK (listen_port > 0 AND listen_port <= 65535),
  transport TEXT NOT NULL DEFAULT 'tcp'
    CHECK (transport IN ('tcp', 'udp', 'tcp_udp')),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  traffic_multiplier REAL NOT NULL DEFAULT 1.0 CHECK (traffic_multiplier >= 0),
  settings_json TEXT NOT NULL DEFAULT '{}',
  inbound_rules_json TEXT NOT NULL DEFAULT '[]',
  outbound_rules_json TEXT NOT NULL DEFAULT '[]',
  route_rules_json TEXT NOT NULL DEFAULT '[]',
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO proxies_replacement (id,node_id,name,protocol,listen,listen_port,transport,enabled,traffic_multiplier,settings_json,inbound_rules_json,outbound_rules_json,route_rules_json,deleted_at,created_at,updated_at) SELECT id,node_id,name,protocol,listen,listen_port,transport,enabled,traffic_multiplier,settings_json,inbound_rules_json,outbound_rules_json,route_rules_json,deleted_at,COALESCE(created_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')),COALESCE(updated_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')) FROM proxies;

DROP TABLE proxies;

ALTER TABLE proxies_replacement RENAME TO proxies;

CREATE UNIQUE INDEX idx_proxy_users_active_name ON proxy_users(name) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX idx_nodes_active_name ON nodes(name) WHERE deleted_at IS NULL;

CREATE INDEX idx_proxies_node_id ON proxies(node_id);

CREATE UNIQUE INDEX idx_proxies_name ON proxies(name) WHERE deleted_at IS NULL;

CREATE INDEX idx_proxies_node_listener
  ON proxies(node_id, listen, listen_port, transport, protocol);

CREATE VIEW IF NOT EXISTS proxy_details AS
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

CREATE VIEW IF NOT EXISTS proxy_access_details AS
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

DELETE FROM node_name_aliases WHERE node_id IN (SELECT id FROM nodes WHERE deleted_at IS NOT NULL);
UPDATE proxies SET enabled = 0, deleted_at = COALESCE(deleted_at, (SELECT deleted_at FROM nodes WHERE id = proxies.node_id)) WHERE node_id IN (SELECT id FROM nodes WHERE deleted_at IS NOT NULL);
DELETE FROM proxy_name_aliases WHERE proxy_id IN (SELECT id FROM proxies WHERE deleted_at IS NOT NULL);

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS log_events_search_after_insert
AFTER INSERT ON log_events
BEGIN
  INSERT INTO log_event_search_documents (event_id) VALUES (NEW.id);
  INSERT INTO log_events_search (
    docid, node_name, user_name, auth_name, source_ip, target_host,
    target_port, action, raw_message
  )
  SELECT
    d.id, n.name, u.name, NEW.auth_name, NEW.source_ip,
    NEW.target_host, CAST(NEW.target_port AS TEXT), NEW.action, NEW.raw_message
  FROM log_event_search_documents d
  JOIN nodes n ON n.id = NEW.node_id
  JOIN proxy_users u ON u.id = NEW.proxy_user_id
  WHERE d.event_id = NEW.id
    AND NEW.proxy_user_id IS NOT NULL;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS log_events_search_after_update
AFTER UPDATE ON log_events
BEGIN
  DELETE FROM log_events_search
  WHERE docid = (
    SELECT id FROM log_event_search_documents WHERE event_id = OLD.id
  );
  INSERT INTO log_events_search (
    docid, node_name, user_name, auth_name, source_ip, target_host,
    target_port, action, raw_message
  )
  SELECT
    d.id, n.name, u.name, NEW.auth_name, NEW.source_ip,
    NEW.target_host, CAST(NEW.target_port AS TEXT), NEW.action, NEW.raw_message
  FROM log_event_search_documents d
  JOIN nodes n ON n.id = NEW.node_id
  JOIN proxy_users u ON u.id = NEW.proxy_user_id
  WHERE d.event_id = NEW.id
    AND NEW.proxy_user_id IS NOT NULL;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS log_events_search_after_delete
BEFORE DELETE ON log_events
BEGIN
  DELETE FROM log_events_search
  WHERE docid = (
    SELECT id FROM log_event_search_documents WHERE event_id = OLD.id
  );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS log_events_search_after_node_rename
AFTER UPDATE OF name ON nodes
WHEN OLD.name <> NEW.name
BEGIN
  UPDATE log_events_search
  SET node_name = NEW.name
  WHERE docid IN (
    SELECT d.id
    FROM log_event_search_documents d
    JOIN log_events e ON e.id = d.event_id
    WHERE e.node_id = NEW.id AND e.proxy_user_id IS NOT NULL
  );
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER IF NOT EXISTS log_events_search_after_user_rename
AFTER UPDATE OF name ON proxy_users
WHEN OLD.name <> NEW.name
BEGIN
  UPDATE log_events_search
  SET user_name = NEW.name
  WHERE docid IN (
    SELECT d.id
    FROM log_event_search_documents d
    JOIN log_events e ON e.id = d.event_id
    WHERE e.proxy_user_id = NEW.id
  );
END;
-- +goose StatementEnd
CREATE TEMP TABLE identity_fk_guard (violations INTEGER CHECK (violations = 0));

INSERT INTO identity_fk_guard SELECT COUNT(*) FROM pragma_foreign_key_check;

DROP TABLE identity_fk_guard;

-- +goose Down
-- Duplicate historical names cannot be made globally unique again.
CREATE TEMP TABLE identity_rollback_guard (value INTEGER CHECK (value = 0));
INSERT INTO identity_rollback_guard VALUES (1);
