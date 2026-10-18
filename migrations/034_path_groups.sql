-- +goose Up
CREATE TABLE path_groups (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  revision INTEGER NOT NULL DEFAULT 1,
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX idx_path_groups_active_name ON path_groups(name) WHERE deleted_at IS NULL;
CREATE TABLE path_group_members (
  group_id TEXT NOT NULL REFERENCES path_groups(id) ON DELETE CASCADE,
  path_id TEXT NOT NULL REFERENCES paths(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (group_id, path_id)
);
CREATE INDEX idx_path_group_members_path ON path_group_members(path_id);
CREATE TABLE user_path_group_grants (
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  group_id TEXT NOT NULL REFERENCES path_groups(id) ON DELETE CASCADE,
  PRIMARY KEY (proxy_user_id, group_id)
);
CREATE INDEX idx_user_path_group_grants_group ON user_path_group_grants(group_id);
CREATE TABLE direct_path_grants (
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  path_id TEXT NOT NULL REFERENCES paths(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  PRIMARY KEY (proxy_user_id, path_id)
);
CREATE INDEX idx_direct_path_grants_path ON direct_path_grants(path_id);

INSERT INTO direct_path_grants (proxy_user_id,path_id,enabled)
SELECT a.proxy_user_id,a.path_id,a.enabled FROM path_accesses a
JOIN proxy_users u ON u.id=a.proxy_user_id
WHERE a.deleted_at IS NULL AND u.deleted_at IS NULL;

CREATE TABLE path_group_changes (
  id TEXT PRIMARY KEY,
  group_id TEXT NOT NULL REFERENCES path_groups(id) ON DELETE RESTRICT,
  change_json TEXT NOT NULL,
  impact_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_path_group_changes_group ON path_group_changes(group_id,created_at);

-- +goose Down
-- Older builds cannot preserve group authorization sources. Require a backup
-- restore for a downgrade while any user is still granted a group.
CREATE TEMP TABLE path_group_rollback_guard (value INTEGER CHECK(value=0));
INSERT INTO path_group_rollback_guard SELECT COUNT(*) FROM user_path_group_grants;
DROP TABLE path_group_rollback_guard;
DROP TABLE path_group_changes;
DROP TABLE direct_path_grants;
DROP TABLE user_path_group_grants;
DROP TABLE path_group_members;
DROP TABLE path_groups;
