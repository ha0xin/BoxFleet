-- +goose Up
CREATE TABLE traffic_usage_totals (
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  direction TEXT NOT NULL CHECK (direction IN ('uplink', 'downlink')),
  raw_bytes INTEGER NOT NULL DEFAULT 0 CHECK (raw_bytes >= 0),
  billable_bytes INTEGER NOT NULL DEFAULT 0 CHECK (billable_bytes >= 0),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  PRIMARY KEY (proxy_user_id, direction)
);

INSERT INTO traffic_usage_totals (
  proxy_user_id,
  direction,
  raw_bytes,
  billable_bytes
)
SELECT
  proxy_user_id,
  direction,
  CAST(SUM(raw_bytes_delta) AS INTEGER),
  CAST(SUM(billable_bytes_delta) AS INTEGER)
FROM traffic_usage_deltas
GROUP BY proxy_user_id, direction;

CREATE TABLE node_latest_heartbeats (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  heartbeat_id TEXT NOT NULL UNIQUE REFERENCES node_heartbeats(id) ON DELETE CASCADE,
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO node_latest_heartbeats (node_id, heartbeat_id)
SELECT
  n.id,
  (
    SELECT h.id
    FROM node_heartbeats h
    WHERE h.node_id = n.id
    ORDER BY h.created_at DESC, h.id DESC
    LIMIT 1
  )
FROM nodes n
WHERE EXISTS (
  SELECT 1
  FROM node_heartbeats h
  WHERE h.node_id = n.id
);

CREATE INDEX idx_log_events_visible_window
  ON log_events(window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_log_events_visible_window;
DROP TABLE IF EXISTS node_latest_heartbeats;
DROP TABLE IF EXISTS traffic_usage_totals;
