-- +goose Up
-- Give every existing node host a durable identity while retaining hosts_json
-- as the source of truth. Endpoint rows reference this opaque ID; the DB facade
-- enforces that the host still belongs to the proxy's node.
UPDATE nodes
SET hosts_json = COALESCE((
  SELECT json_group_array(
    json(json_set(
      host.value,
      '$.id',
      COALESCE(NULLIF(json_extract(host.value, '$.id'), ''), 'host_' || lower(hex(randomblob(16))))
    ))
  )
  FROM json_each(
    CASE
      WHEN json_valid(nodes.hosts_json) THEN nodes.hosts_json
      ELSE '[]'
    END
  ) AS host
), '[]');

CREATE TABLE proxy_publication_settings (
  proxy_id TEXT PRIMARY KEY REFERENCES proxies(id) ON DELETE CASCADE,
  direct_enabled INTEGER NOT NULL DEFAULT 1 CHECK (direct_enabled IN (0, 1)),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO proxy_publication_settings (proxy_id, direct_enabled)
SELECT id, 1 FROM proxies;

CREATE TABLE endpoints (
  id TEXT PRIMARY KEY,
  proxy_id TEXT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
  host_id TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (proxy_id, host_id)
);

CREATE INDEX idx_endpoints_proxy_id ON endpoints(proxy_id);
CREATE INDEX idx_endpoints_host_id ON endpoints(host_id);

CREATE TABLE paths (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  endpoint_id TEXT NOT NULL REFERENCES endpoints(id) ON DELETE RESTRICT,
  dialer_path_id TEXT REFERENCES paths(id) ON DELETE RESTRICT,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  visibility TEXT NOT NULL DEFAULT 'selectable'
    CHECK (visibility IN ('selectable', 'dependency')),
  managed INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1)),
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  CHECK (dialer_path_id IS NULL OR dialer_path_id <> id)
);

CREATE INDEX idx_paths_endpoint_id ON paths(endpoint_id);
CREATE INDEX idx_paths_dialer_path_id ON paths(dialer_path_id);

CREATE TABLE path_accesses (
  id TEXT PRIMARY KEY,
  path_id TEXT NOT NULL REFERENCES paths(id) ON DELETE CASCADE,
  proxy_user_id TEXT NOT NULL REFERENCES proxy_users(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  deleted_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (path_id, proxy_user_id)
);

CREATE INDEX idx_path_accesses_user_id ON path_accesses(proxy_user_id);

-- Preserve the legacy rule that every selected Proxy + Host pair is published
-- directly. display_name is the exact name emitted by the pre-Path renderer.
INSERT INTO endpoints (id, proxy_id, host_id)
SELECT
  'ep_' || lower(hex(randomblob(16))),
  p.id,
  json_extract(host.value, '$.id')
FROM proxies p
JOIN nodes n ON n.id = p.node_id
JOIN json_each(n.hosts_json) AS host
WHERE COALESCE(json_extract(host.value, '$.selected'), 0) = 1
  AND NULLIF(json_extract(host.value, '$.id'), '') IS NOT NULL;

INSERT INTO paths (id, name, display_name, endpoint_id, managed, sort_order)
SELECT
  'path_' || lower(hex(randomblob(16))),
  CASE
    WHEN COALESCE(json_extract(host.value, '$.tag'), '') <> ''
      THEN json_extract(host.value, '$.tag')
    ELSE 'direct'
  END,
  p.name || CASE
    WHEN COALESCE(json_extract(host.value, '$.tag'), '') <> ''
      THEN '-' || json_extract(host.value, '$.tag')
    WHEN CAST(host.key AS INTEGER) > 0
      THEN '-' || json_extract(host.value, '$.host')
    ELSE ''
  END,
  e.id,
  1,
  CAST(host.key AS INTEGER)
FROM endpoints e
JOIN proxies p ON p.id = e.proxy_id
JOIN nodes n ON n.id = p.node_id
JOIN json_each(n.hosts_json) AS host
  ON json_extract(host.value, '$.id') = e.host_id;

INSERT INTO path_accesses (id, path_id, proxy_user_id, enabled, deleted_at)
SELECT
  'pacc_' || lower(hex(randomblob(16))),
  path.id,
  credential.proxy_user_id,
  credential.enabled,
  credential.deleted_at
FROM paths path
JOIN endpoints endpoint ON endpoint.id = path.endpoint_id
JOIN proxy_accesses credential ON credential.proxy_id = endpoint.proxy_id;

-- +goose Down
DROP TABLE path_accesses;
DROP TABLE paths;
DROP TABLE endpoints;
DROP TABLE proxy_publication_settings;
