-- +goose Up
CREATE TABLE IF NOT EXISTS node_network_snapshots (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK (revision > 0),
  content_hash TEXT NOT NULL,
  addresses_json TEXT NOT NULL DEFAULT '[]',
  collected_at TEXT NOT NULL DEFAULT '',
  received_at TEXT NOT NULL DEFAULT '',
  attempted_at TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS node_observed_ips (
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  family INTEGER NOT NULL CHECK (family IN (4, 6)),
  address TEXT NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('direct', 'cloudflare')),
  observed_at TEXT NOT NULL,
  PRIMARY KEY (node_id, address, source)
);

-- +goose Down
DROP TABLE node_observed_ips;
DROP TABLE node_network_snapshots;
