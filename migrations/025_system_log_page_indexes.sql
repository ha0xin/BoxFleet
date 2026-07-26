-- +goose Up
-- The paged system-logs endpoint orders by (observed_at DESC, id DESC) across
-- every node. idx_system_logs_node_observed only helps the node-scoped path, so
-- the unfiltered page scanned the whole table into a temp b-tree — 90 days of
-- journal lines from the entire fleet.
CREATE INDEX idx_system_logs_observed_id
  ON system_logs(observed_at, id);

-- The Service combobox lists every distinct service regardless of the active
-- filter, so it must not depend on the rows in the current page.
CREATE INDEX idx_system_logs_service
  ON system_logs(service);

-- +goose Down
DROP INDEX IF EXISTS idx_system_logs_service;
DROP INDEX IF EXISTS idx_system_logs_observed_id;
