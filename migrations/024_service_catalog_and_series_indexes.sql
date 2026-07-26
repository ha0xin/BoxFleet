-- +goose Up
-- Operator-supplied domain -> service mappings. Consulted ahead of the embedded
-- catalog at read time, so a correction here retroactively reclassifies history
-- without touching log_events (and without re-firing the FTS triggers).
-- suffix is stored already lowercased and dot-normalized by the DB facade.
CREATE TABLE domain_service_overrides (
  suffix TEXT PRIMARY KEY,
  service TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  category TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- Time-bucketed traffic series aggregate over an observed_at range with no
-- user or node filter. Every existing traffic_usage_deltas index leads with an
-- entity column, so an unfiltered range scan had no usable index at all; the
-- trailing columns make this covering for the aggregation.
CREATE INDEX idx_traffic_usage_deltas_observed
  ON traffic_usage_deltas(observed_at, direction, billable_bytes_delta, raw_bytes_delta);

-- Host/service audit aggregation reads the same visible time window as the
-- network-event table but groups by host and sums count, so the two trailing
-- columns keep the group-by off the table for the bounded range scan.
CREATE INDEX idx_log_events_visible_window_host
  ON log_events(window_end, window_start, target_host, count)
  WHERE proxy_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_log_events_visible_window_host;
DROP INDEX IF EXISTS idx_traffic_usage_deltas_observed;
DROP TABLE IF EXISTS domain_service_overrides;
