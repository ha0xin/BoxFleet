-- +goose Up
DROP VIEW network_event_records;
CREATE VIEW network_event_records AS
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, e.action, e.raw_message, e.count, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'journal' AS source, NULL AS domain, NULL AS network, NULL AS ip_version, NULL AS protocol, NULL AS inbound, NULL AS inbound_type, NULL AS rule, NULL AS outbound, NULL AS outbound_type, NULL AS chain, NULL AS uplink_bytes, NULL AS downlink_bytes, NULL AS duration_ms_total, NULL AS connections_closed, NULL AS connection_id, NULL AS started_at, e.window_start AS event_time FROM log_events e
WHERE e.proxy_user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)))
UNION ALL
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, 'connect', '', e.connections_opened, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'stream' AS source, e.domain, e.network, e.ip_version, e.protocol, e.inbound, e.inbound_type, e.rule, e.outbound, e.outbound_type, e.chain, e.uplink_bytes, e.downlink_bytes, e.duration_ms_total, e.connections_closed, e.connection_id, CASE WHEN e.connection_id <> '' THEN e.bucket_start ELSE NULL END AS started_at, CASE WHEN e.connection_id <> '' THEN e.bucket_start ELSE e.window_start END AS event_time FROM connection_events e
WHERE e.connection_id = '' OR EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)));
CREATE INDEX idx_log_events_event_time ON log_events(window_start, id) WHERE proxy_user_id IS NOT NULL;
CREATE INDEX idx_connection_events_event_time ON connection_events((CASE WHEN connection_id <> '' THEN bucket_start ELSE window_start END), id);

-- +goose Down
DROP VIEW network_event_records;
DROP INDEX idx_log_events_event_time;
DROP INDEX idx_connection_events_event_time;
CREATE VIEW network_event_records AS
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, e.action, e.raw_message, e.count, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'journal' AS source, NULL AS domain, NULL AS network, NULL AS ip_version, NULL AS protocol, NULL AS inbound, NULL AS inbound_type, NULL AS rule, NULL AS outbound, NULL AS outbound_type, NULL AS chain, NULL AS uplink_bytes, NULL AS downlink_bytes, NULL AS duration_ms_total, NULL AS connections_closed, NULL AS connection_id, NULL AS started_at FROM log_events e
WHERE e.proxy_user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)))
UNION ALL
SELECT e.id, e.node_id, e.proxy_user_id, e.auth_name, e.source_ip, e.target_host, e.target_port, 'connect', '', e.connections_opened, e.aggregate_key, e.window_start, e.window_end, e.created_at, 'stream' AS source, e.domain, e.network, e.ip_version, e.protocol, e.inbound, e.inbound_type, e.rule, e.outbound, e.outbound_type, e.chain, e.uplink_bytes, e.downlink_bytes, e.duration_ms_total, e.connections_closed, e.connection_id, CASE WHEN e.connection_id <> '' THEN e.bucket_start ELSE NULL END AS started_at FROM connection_events e
WHERE e.connection_id = '' OR EXISTS (SELECT 1 FROM network_event_source_intervals s WHERE s.node_id=e.node_id AND julianday(e.window_end)>=julianday(s.started_at) AND (s.ended_at IS NULL OR julianday(e.window_end)<julianday(s.ended_at)));
