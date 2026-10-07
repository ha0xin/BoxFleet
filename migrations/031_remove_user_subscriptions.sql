-- +goose Up
-- Retire all user-scoped links; profile-scoped subscriptions are independent.
DROP TABLE subscription_tokens;

-- +goose Down
-- Removed links cannot be recovered by a downgrade; restore a backup if needed.
CREATE TABLE subscription_tokens (
  id TEXT PRIMARY KEY,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE INDEX idx_subscription_tokens_proxy_user_id
  ON subscription_tokens(proxy_user_id);

CREATE UNIQUE INDEX idx_subscription_tokens_active_user
  ON subscription_tokens(proxy_user_id)
  WHERE revoked_at IS NULL;

