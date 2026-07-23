PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS proxy_users (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
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

CREATE TABLE IF NOT EXISTS subscription_tokens (
  id TEXT PRIMARY KEY,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_subscription_tokens_proxy_user_id
  ON subscription_tokens(proxy_user_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subscription_tokens_active_user
  ON subscription_tokens(proxy_user_id)
  WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS mihomo_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  draft_document_json TEXT NOT NULL DEFAULT '{"rewrites":[]}',
  proxy_user_id TEXT REFERENCES proxy_users(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_mihomo_profiles_proxy_user
  ON mihomo_profiles(proxy_user_id);

CREATE TABLE IF NOT EXISTS mihomo_profile_revisions (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  version INTEGER NOT NULL CHECK (version > 0),
  document_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (profile_id, version)
);

CREATE INDEX IF NOT EXISTS idx_mihomo_profile_revisions_profile_version
  ON mihomo_profile_revisions(profile_id, version DESC);

CREATE TABLE IF NOT EXISTS mihomo_profile_publications (
  profile_id TEXT PRIMARY KEY REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  revision_id TEXT NOT NULL REFERENCES mihomo_profile_revisions(id) ON DELETE RESTRICT,
  published_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS proxy_user_mihomo_profiles (
  proxy_user_id TEXT PRIMARY KEY REFERENCES proxy_users(id) ON DELETE CASCADE,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE RESTRICT,
  assigned_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_proxy_user_mihomo_profiles_profile
  ON proxy_user_mihomo_profiles(profile_id);

CREATE TABLE IF NOT EXISTS mihomo_rewrite_templates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('yaml', 'javascript')),
  content TEXT NOT NULL,
  built_in INTEGER NOT NULL DEFAULT 0 CHECK (built_in IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS mihomo_profile_subscription_tokens (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mihomo_profile_subscription_tokens_active
  ON mihomo_profile_subscription_tokens(profile_id)
  WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
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

CREATE TABLE IF NOT EXISTS node_name_aliases (
  alias TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_node_name_aliases_node_id
  ON node_name_aliases(node_id);

CREATE TABLE IF NOT EXISTS node_tokens (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  token_digest TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_node_tokens_node_id ON node_tokens(node_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_node_tokens_token_digest
  ON node_tokens(token_digest)
  WHERE token_digest IS NOT NULL;

CREATE TABLE IF NOT EXISTS proxies (
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
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, name)
);

CREATE INDEX IF NOT EXISTS idx_proxies_node_id ON proxies(node_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_proxies_name ON proxies(name);
CREATE INDEX IF NOT EXISTS idx_proxies_node_listener
  ON proxies(node_id, listen, listen_port, transport, protocol);

CREATE TABLE IF NOT EXISTS proxy_name_aliases (
  alias TEXT PRIMARY KEY,
  proxy_id TEXT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_proxy_name_aliases_proxy_id
  ON proxy_name_aliases(proxy_id);

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

CREATE TABLE IF NOT EXISTS proxy_accesses (
  id TEXT PRIMARY KEY,
  proxy_id TEXT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  auth_name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  quota_bytes INTEGER NOT NULL DEFAULT 0 CHECK (quota_bytes >= 0),
  traffic_multiplier REAL CHECK (traffic_multiplier IS NULL OR traffic_multiplier >= 0),
  credential_json TEXT NOT NULL DEFAULT '{}',
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (proxy_id, proxy_user_id),
  UNIQUE (proxy_id, auth_name)
);

CREATE INDEX IF NOT EXISTS idx_proxy_accesses_user_id
  ON proxy_accesses(proxy_user_id);

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

CREATE TABLE IF NOT EXISTS node_outbounds (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  type TEXT NOT NULL,
  settings_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, tag)
);

CREATE INDEX IF NOT EXISTS idx_node_outbounds_node_id ON node_outbounds(node_id);

CREATE TABLE IF NOT EXISTS route_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  node_id TEXT REFERENCES nodes(id) ON DELETE CASCADE,
  rules_json TEXT NOT NULL DEFAULT '[]',
  final_outbound_tag TEXT NOT NULL DEFAULT 'direct',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, name)
);

CREATE INDEX IF NOT EXISTS idx_route_profiles_node_id ON route_profiles(node_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_route_profiles_scope_name
  ON route_profiles(COALESCE(node_id, 'global'), name);

CREATE TABLE IF NOT EXISTS user_node_bindings (
  id TEXT PRIMARY KEY,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  node_quota_bytes INTEGER NOT NULL DEFAULT 0 CHECK (node_quota_bytes >= 0),
  traffic_multiplier REAL CHECK (traffic_multiplier IS NULL OR traffic_multiplier >= 0),
  route_profile_id TEXT REFERENCES route_profiles(id) ON DELETE SET NULL,
  disabled_reason TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (proxy_user_id, node_id)
);

CREATE INDEX IF NOT EXISTS idx_user_node_bindings_user_id
  ON user_node_bindings(proxy_user_id);

CREATE INDEX IF NOT EXISTS idx_user_node_bindings_node_id
  ON user_node_bindings(node_id);

CREATE TABLE IF NOT EXISTS config_versions (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'published', 'superseded', 'failed')),
  config_json TEXT NOT NULL,
  config_hash TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  published_at TEXT,
  UNIQUE (node_id, version),
  UNIQUE (node_id, config_hash)
);

CREATE INDEX IF NOT EXISTS idx_config_versions_node_id
  ON config_versions(node_id);

CREATE TABLE IF NOT EXISTS node_config_status (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  target_config_version_id TEXT REFERENCES config_versions(id) ON DELETE SET NULL,
  current_config_version_id TEXT REFERENCES config_versions(id) ON DELETE SET NULL,
  last_apply_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (last_apply_status IN ('pending', 'applied', 'failed', 'rolled_back')),
  last_apply_error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS traffic_reports (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL,
  agent_boot_id TEXT NOT NULL,
  reported_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, agent_boot_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_traffic_reports_node_id_created_at
  ON traffic_reports(node_id, created_at);

CREATE TABLE IF NOT EXISTS traffic_usage_deltas (
  id TEXT PRIMARY KEY,
  report_id TEXT NOT NULL REFERENCES traffic_reports(id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  proxy_id TEXT REFERENCES proxies(id) ON DELETE SET NULL,
  auth_name TEXT NOT NULL,
  direction TEXT NOT NULL CHECK (direction IN ('uplink', 'downlink')),
  raw_bytes_delta INTEGER NOT NULL CHECK (raw_bytes_delta >= 0),
  effective_multiplier REAL NOT NULL CHECK (effective_multiplier >= 0),
  billable_bytes_delta INTEGER NOT NULL CHECK (billable_bytes_delta >= 0),
  counter_value INTEGER NOT NULL CHECK (counter_value >= 0),
  counter_epoch INTEGER NOT NULL DEFAULT 0 CHECK (counter_epoch >= 0),
  observed_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_user_observed
  ON traffic_usage_deltas(proxy_user_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_user_node_observed
  ON traffic_usage_deltas(proxy_user_id, node_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_node_observed
  ON traffic_usage_deltas(node_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_traffic_usage_auth_node_observed
  ON traffic_usage_deltas(auth_name, node_id, observed_at);

CREATE TABLE IF NOT EXISTS traffic_usage_totals (
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  direction TEXT NOT NULL CHECK (direction IN ('uplink', 'downlink')),
  raw_bytes INTEGER NOT NULL DEFAULT 0 CHECK (raw_bytes >= 0),
  billable_bytes INTEGER NOT NULL DEFAULT 0 CHECK (billable_bytes >= 0),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (proxy_user_id, direction)
);

CREATE TABLE IF NOT EXISTS node_heartbeats (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  agent_version TEXT NOT NULL DEFAULT '',
  sing_box_version TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  memory_bytes INTEGER NOT NULL DEFAULT 0 CHECK (memory_bytes >= 0),
  rx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (rx_bytes >= 0),
  tx_bytes INTEGER NOT NULL DEFAULT 0 CHECK (tx_bytes >= 0),
  payload_json TEXT NOT NULL DEFAULT '{}',
  reported_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_node_heartbeats_node_created
  ON node_heartbeats(node_id, created_at);

CREATE TABLE IF NOT EXISTS node_latest_heartbeats (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  heartbeat_id TEXT NOT NULL UNIQUE REFERENCES node_heartbeats(id) ON DELETE CASCADE,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS raw_log_entries (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  journal_cursor TEXT,
  message_hash TEXT NOT NULL,
  raw_message TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  ingested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, journal_cursor),
  UNIQUE (node_id, message_hash)
);

CREATE INDEX IF NOT EXISTS idx_raw_log_entries_node_observed
  ON raw_log_entries(node_id, observed_at);

CREATE TABLE IF NOT EXISTS system_logs (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  service TEXT NOT NULL,
  journal_cursor TEXT,
  message_hash TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT '',
  raw_message TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  ingested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, service, journal_cursor),
  UNIQUE (node_id, service, message_hash)
);

CREATE INDEX IF NOT EXISTS idx_system_logs_node_observed
  ON system_logs(node_id, observed_at);

CREATE TABLE IF NOT EXISTS log_events (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  proxy_user_id TEXT REFERENCES proxy_users(id) ON DELETE SET NULL,
  auth_name TEXT NOT NULL DEFAULT '',
  source_ip TEXT NOT NULL DEFAULT '',
  target_host TEXT NOT NULL DEFAULT '',
  target_port INTEGER NOT NULL DEFAULT 0 CHECK (target_port >= 0 AND target_port <= 65535),
  action TEXT NOT NULL DEFAULT '',
  raw_message TEXT NOT NULL DEFAULT '',
  count INTEGER NOT NULL DEFAULT 1 CHECK (count > 0),
  aggregate_key TEXT NOT NULL DEFAULT '',
  window_start TEXT NOT NULL,
  window_end TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_log_events_node_window
  ON log_events(node_id, window_start);

CREATE INDEX IF NOT EXISTS idx_log_events_user_window
  ON log_events(proxy_user_id, window_start);

CREATE INDEX IF NOT EXISTS idx_log_events_created
  ON log_events(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_log_events_node_created
  ON log_events(node_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_log_events_user_created
  ON log_events(proxy_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_log_events_window_end
  ON log_events(window_end);

CREATE INDEX IF NOT EXISTS idx_log_events_visible_window
  ON log_events(window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_log_events_visible_action_window
  ON log_events(action COLLATE NOCASE, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_log_events_visible_node_window
  ON log_events(node_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_log_events_visible_user_window
  ON log_events(proxy_user_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_log_events_visible_node_user_window
  ON log_events(node_id, proxy_user_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_log_events_created_window
  ON log_events(created_at DESC, window_end DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_log_events_node_created_window
  ON log_events(node_id, created_at DESC, window_end DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_log_events_user_created_window
  ON log_events(proxy_user_id, created_at DESC, window_end DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_log_events_aggregate_key
  ON log_events(aggregate_key)
  WHERE aggregate_key <> '';

CREATE TABLE IF NOT EXISTS log_event_search_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL UNIQUE REFERENCES log_events(id) ON DELETE CASCADE
);

CREATE VIRTUAL TABLE IF NOT EXISTS log_events_search USING fts3(
  node_name,
  user_name,
  auth_name,
  source_ip,
  target_host,
  target_port,
  action,
  raw_message
);

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

CREATE TRIGGER IF NOT EXISTS log_events_search_after_delete
BEFORE DELETE ON log_events
BEGIN
  DELETE FROM log_events_search
  WHERE docid = (
    SELECT id FROM log_event_search_documents WHERE event_id = OLD.id
  );
END;

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

CREATE TABLE IF NOT EXISTS audit_events (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL DEFAULT 'admin',
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL DEFAULT '',
  before_json TEXT NOT NULL DEFAULT '',
  after_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_audit_events_resource_created
  ON audit_events(resource_type, resource_id, created_at);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS node_operations (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN (
    'update.bundle',
    'update.agent',
    'update.sing_box',
    'config.reconcile',
    'diagnostics.collect',
    'logs.collect'
  )),
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'expired')),
  phase TEXT NOT NULL DEFAULT 'queued',
  payload_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  idempotency_key TEXT NOT NULL,
  required_capabilities_json TEXT NOT NULL DEFAULT '[]',
  attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  lease_token_hash TEXT,
  lease_expires_at TEXT,
  cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0, 1)),
  not_before TEXT,
  expires_at TEXT,
  requested_by TEXT NOT NULL DEFAULT 'admin',
  requested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  claimed_at TEXT,
  started_at TEXT,
  finished_at TEXT,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  error TEXT NOT NULL DEFAULT '',
  retry_of TEXT REFERENCES node_operations(id) ON DELETE SET NULL,
  UNIQUE (node_id, idempotency_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_node_operations_one_active_per_node
  ON node_operations(node_id)
  WHERE status IN ('queued', 'running');

CREATE INDEX IF NOT EXISTS idx_node_operations_node_requested
  ON node_operations(node_id, requested_at DESC);

CREATE INDEX IF NOT EXISTS idx_node_operations_claimable
  ON node_operations(node_id, status, not_before, expires_at, requested_at);

CREATE INDEX IF NOT EXISTS idx_node_operations_lease
  ON node_operations(status, lease_expires_at)
  WHERE status = 'running';

CREATE TABLE IF NOT EXISTS node_operation_events (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES node_operations(id) ON DELETE CASCADE,
  attempt INTEGER NOT NULL CHECK (attempt > 0),
  sequence INTEGER NOT NULL CHECK (sequence > 0),
  status TEXT NOT NULL
    CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled')),
  phase TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  details_json TEXT NOT NULL DEFAULT '{}',
  result_json TEXT NOT NULL DEFAULT '{}',
  error TEXT NOT NULL DEFAULT '',
  reported_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (operation_id, attempt, sequence)
);

CREATE INDEX IF NOT EXISTS idx_node_operation_events_operation_sequence
  ON node_operation_events(operation_id, attempt, sequence);

CREATE TABLE IF NOT EXISTS node_update_campaigns (
  id TEXT PRIMARY KEY,
  release TEXT NOT NULL,
  components_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued', 'running', 'paused', 'succeeded', 'cancelled')),
  idempotency_key TEXT NOT NULL UNIQUE,
  spec_hash TEXT NOT NULL,
  batch_size INTEGER NOT NULL DEFAULT 2 CHECK (batch_size BETWEEN 1 AND 20),
  current_batch INTEGER NOT NULL DEFAULT -1,
  requested_by TEXT NOT NULL DEFAULT 'admin',
  requested_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  started_at TEXT,
  finished_at TEXT,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS node_update_campaign_members (
  campaign_id TEXT NOT NULL REFERENCES node_update_campaigns(id) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  batch_number INTEGER NOT NULL CHECK (batch_number >= 0),
  kind TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  operation_id TEXT REFERENCES node_operations(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
  error TEXT NOT NULL DEFAULT '',
  started_at TEXT,
  finished_at TEXT,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (campaign_id, node_id),
  UNIQUE (campaign_id, position),
  UNIQUE (operation_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_node_update_campaigns_one_active
  ON node_update_campaigns((1))
  WHERE status IN ('queued', 'running', 'paused');

CREATE INDEX IF NOT EXISTS idx_node_update_campaign_members_batch
  ON node_update_campaign_members(campaign_id, batch_number, status, position);
