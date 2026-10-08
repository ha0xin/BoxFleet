-- +goose Up
CREATE TEMP TABLE retired_paths AS
WITH RECURSIVE affected_paths(id) AS (
 SELECT t.id FROM paths t JOIN endpoints e ON e.id=t.endpoint_id
 JOIN proxies p ON p.id=e.proxy_id JOIN nodes n ON n.id=p.node_id
 WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL
 UNION
 SELECT t.id FROM paths t JOIN affected_paths a ON t.dialer_path_id=a.id
)
SELECT id FROM affected_paths;
CREATE TEMP TABLE retired_users AS SELECT DISTINCT proxy_user_id AS id FROM path_accesses WHERE deleted_at IS NULL AND path_id IN (SELECT id FROM retired_paths);
UPDATE endpoints SET enabled=0, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE enabled=1 AND proxy_id IN (SELECT p.id FROM proxies p JOIN nodes n ON n.id=p.node_id WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL);
UPDATE paths SET enabled=0, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE enabled=1 AND id IN (SELECT id FROM retired_paths);
UPDATE path_accesses SET enabled=0,deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE deleted_at IS NULL AND path_id IN (SELECT id FROM retired_paths);
UPDATE proxy_accesses SET enabled=0,deleted_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE deleted_at IS NULL AND proxy_id IN (SELECT p.id FROM proxies p JOIN nodes n ON n.id=p.node_id WHERE p.deleted_at IS NOT NULL OR n.deleted_at IS NOT NULL);
WITH RECURSIVE required_paths(user_id,id) AS (
 SELECT a.proxy_user_id,a.path_id FROM path_accesses a JOIN paths t ON t.id=a.path_id JOIN endpoints e ON e.id=t.endpoint_id JOIN proxies p ON p.id=e.proxy_id JOIN nodes n ON n.id=p.node_id
 WHERE a.enabled=1 AND a.deleted_at IS NULL AND t.enabled=1 AND e.enabled=1 AND p.deleted_at IS NULL AND n.deleted_at IS NULL
 UNION SELECT r.user_id,t.dialer_path_id FROM required_paths r JOIN paths t ON t.id=r.id WHERE t.dialer_path_id IS NOT NULL
)
UPDATE proxy_accesses SET enabled=0,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE enabled=1 AND deleted_at IS NULL AND proxy_user_id IN (SELECT id FROM retired_users)
AND NOT EXISTS(SELECT 1 FROM required_paths r JOIN paths t ON t.id=r.id JOIN endpoints e ON e.id=t.endpoint_id WHERE r.user_id=proxy_accesses.proxy_user_id AND e.proxy_id=proxy_accesses.proxy_id);
UPDATE user_node_bindings SET enabled=0,disabled_reason='node_deleted',updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE enabled=1 AND node_id IN (SELECT id FROM nodes WHERE deleted_at IS NOT NULL);
DROP TABLE retired_users;
DROP TABLE retired_paths;

-- +goose Down
-- Revoked grants cannot be reconstructed automatically. Restore the pre-migration backup.
CREATE TEMP TABLE irreversible_dependency_cleanup (value INTEGER CHECK(value=0));
INSERT INTO irreversible_dependency_cleanup VALUES(1);
