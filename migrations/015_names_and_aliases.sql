-- +goose Up
CREATE TABLE node_name_aliases (
  alias TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_node_name_aliases_node_id
  ON node_name_aliases(node_id);

CREATE TABLE proxy_name_aliases (
  alias TEXT PRIMARY KEY,
  proxy_id TEXT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_proxy_name_aliases_proxy_id
  ON proxy_name_aliases(proxy_id);

-- Proxy names are also Mihomo profile names, so they must be globally unique.
CREATE UNIQUE INDEX idx_proxies_name
  ON proxies(name);

-- +goose Down
DROP INDEX IF EXISTS idx_proxies_name;
DROP TABLE IF EXISTS proxy_name_aliases;
DROP TABLE IF EXISTS node_name_aliases;
