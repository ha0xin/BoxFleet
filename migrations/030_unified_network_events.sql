-- +goose Up
ALTER TABLE connection_events ADD COLUMN connection_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_connection_events_window ON connection_events(window_end, window_start);
CREATE TABLE network_event_source_intervals (
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  PRIMARY KEY(node_id, started_at)
);
CREATE UNIQUE INDEX idx_network_source_active ON network_event_source_intervals(node_id) WHERE ended_at IS NULL;
INSERT INTO network_event_source_intervals(node_id, started_at, ended_at)
SELECT r.node_id, MIN(MIN(r.window_start), COALESCE((SELECT MIN(e.bucket_start) FROM connection_events e WHERE e.node_id=r.node_id), MIN(r.window_start))), CASE WHEN t.enabled=1 THEN NULL ELSE COALESCE(t.updated_at, MAX(r.window_end)) END
FROM connection_reports r LEFT JOIN node_connection_telemetry t ON t.node_id=r.node_id GROUP BY r.node_id;
-- +goose StatementBegin
CREATE TRIGGER network_source_disabled AFTER UPDATE OF enabled ON node_connection_telemetry
WHEN NEW.enabled=0 AND OLD.enabled=1 BEGIN
 UPDATE network_event_source_intervals SET ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE node_id=NEW.node_id AND ended_at IS NULL;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER network_source_deleted BEFORE DELETE ON node_connection_telemetry BEGIN
 UPDATE network_event_source_intervals SET ended_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE node_id=OLD.node_id AND ended_at IS NULL;
END;
-- +goose StatementEnd
CREATE VIEW network_event_records AS
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, e.action, e.raw_message, e.count, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'journal' AS source, NULL AS domain, NULL AS network, NULL AS ip_version, NULL AS protocol, NULL AS inbound, NULL AS inbound_type, NULL AS rule, NULL AS outbound, NULL AS outbound_type, NULL AS chain, NULL AS uplink_bytes, NULL AS downlink_bytes, NULL AS duration_ms_total, NULL AS connections_closed, NULL AS connection_id, NULL AS started_at FROM log_events e
WHERE e.proxy_user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)))
UNION ALL
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, 'connect', '', e.connections_opened, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'stream' AS source, e.domain, e.network, e.ip_version, e.protocol, e.inbound, e.inbound_type, e.rule, e.outbound, e.outbound_type, e.chain, e.uplink_bytes, e.downlink_bytes, e.duration_ms_total, e.connections_closed, e.connection_id, CASE WHEN e.connection_id <> '' THEN e.bucket_start ELSE NULL END AS started_at FROM connection_events e
WHERE e.connection_id = '' OR EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)));

-- +goose Down
DROP VIEW network_event_records;
DROP TRIGGER network_source_deleted;
DROP TRIGGER network_source_disabled;
DROP TABLE network_event_source_intervals;
DROP INDEX idx_connection_events_window;
ALTER TABLE connection_events DROP COLUMN connection_id;
