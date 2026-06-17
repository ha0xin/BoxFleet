-- +goose Up
ALTER TABLE proxy_users DROP COLUMN traffic_multiplier;

-- +goose Down
ALTER TABLE proxy_users ADD COLUMN traffic_multiplier REAL NOT NULL DEFAULT 1.0
  CHECK (traffic_multiplier >= 0);
