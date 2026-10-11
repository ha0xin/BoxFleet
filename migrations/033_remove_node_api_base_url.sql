-- +goose Up
ALTER TABLE nodes DROP COLUMN api_base_url;

-- +goose Down
-- Restoring a column cannot recover its values or its original position used
-- by older SELECT * queries. Restore the pre-upgrade database for a downgrade.
CREATE TEMP TABLE node_api_url_rollback_guard (value INTEGER CHECK (value = 0));
INSERT INTO node_api_url_rollback_guard VALUES (1);
