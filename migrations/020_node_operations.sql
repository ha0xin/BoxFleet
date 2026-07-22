-- +goose Up
CREATE TABLE node_operations (
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

CREATE UNIQUE INDEX idx_node_operations_one_active_per_node
  ON node_operations(node_id)
  WHERE status IN ('queued', 'running');

CREATE INDEX idx_node_operations_node_requested
  ON node_operations(node_id, requested_at DESC);

CREATE INDEX idx_node_operations_claimable
  ON node_operations(node_id, status, not_before, expires_at, requested_at);

CREATE INDEX idx_node_operations_lease
  ON node_operations(status, lease_expires_at)
  WHERE status = 'running';

CREATE TABLE node_operation_events (
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

CREATE INDEX idx_node_operation_events_operation_sequence
  ON node_operation_events(operation_id, attempt, sequence);

CREATE TABLE node_update_campaigns (
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

CREATE TABLE node_update_campaign_members (
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

CREATE UNIQUE INDEX idx_node_update_campaigns_one_active
  ON node_update_campaigns((1))
  WHERE status IN ('queued', 'running', 'paused');

CREATE INDEX idx_node_update_campaign_members_batch
  ON node_update_campaign_members(campaign_id, batch_number, status, position);

-- +goose Down
DROP TABLE IF EXISTS node_update_campaign_members;
DROP TABLE IF EXISTS node_update_campaigns;
DROP TABLE IF EXISTS node_operation_events;
DROP TABLE IF EXISTS node_operations;
