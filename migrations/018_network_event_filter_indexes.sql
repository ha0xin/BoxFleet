-- +goose Up
CREATE INDEX idx_log_events_visible_action_window
  ON log_events(action COLLATE NOCASE, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX idx_log_events_visible_node_window
  ON log_events(node_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX idx_log_events_visible_user_window
  ON log_events(proxy_user_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

CREATE INDEX idx_log_events_visible_node_user_window
  ON log_events(node_id, proxy_user_id, window_end, window_start)
  WHERE proxy_user_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_log_events_visible_node_user_window;
DROP INDEX IF EXISTS idx_log_events_visible_user_window;
DROP INDEX IF EXISTS idx_log_events_visible_node_window;
DROP INDEX IF EXISTS idx_log_events_visible_action_window;
