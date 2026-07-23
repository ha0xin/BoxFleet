-- +goose Up
CREATE TABLE mihomo_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  draft_document_json TEXT NOT NULL DEFAULT '{"rewrites":[]}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE mihomo_profile_revisions (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  version INTEGER NOT NULL CHECK (version > 0),
  document_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (profile_id, version)
);

CREATE INDEX idx_mihomo_profile_revisions_profile_version
  ON mihomo_profile_revisions(profile_id, version DESC);

CREATE TABLE mihomo_profile_publications (
  profile_id TEXT PRIMARY KEY REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  revision_id TEXT NOT NULL REFERENCES mihomo_profile_revisions(id) ON DELETE RESTRICT,
  published_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE proxy_user_mihomo_profiles (
  proxy_user_id TEXT PRIMARY KEY REFERENCES proxy_users(id) ON DELETE CASCADE,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE RESTRICT,
  assigned_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_proxy_user_mihomo_profiles_profile
  ON proxy_user_mihomo_profiles(profile_id);

INSERT INTO mihomo_profiles (
  id,
  name,
  description,
  draft_document_json
) VALUES (
  'mhp_default',
  'Default',
  'Built-in BoxFleet Basic profile with optional administrator rewrites.',
  '{"rewrites":[]}'
);

INSERT INTO mihomo_profile_revisions (
  id,
  profile_id,
  version,
  document_json
) VALUES (
  'mhpr_default_1',
  'mhp_default',
  1,
  '{"rewrites":[]}'
);

INSERT INTO mihomo_profile_publications (profile_id, revision_id)
VALUES ('mhp_default', 'mhpr_default_1');

-- +goose Down
DROP TABLE IF EXISTS proxy_user_mihomo_profiles;
DROP TABLE IF EXISTS mihomo_profile_publications;
DROP TABLE IF EXISTS mihomo_profile_revisions;
DROP TABLE IF EXISTS mihomo_profiles;
