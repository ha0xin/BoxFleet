-- +goose Up
ALTER TABLE mihomo_profiles
ADD COLUMN proxy_user_id TEXT REFERENCES proxy_users(id) ON DELETE CASCADE;

CREATE INDEX idx_mihomo_profiles_proxy_user
ON mihomo_profiles(proxy_user_id);

CREATE TABLE mihomo_rewrite_templates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('yaml', 'javascript')),
  content TEXT NOT NULL,
  built_in INTEGER NOT NULL DEFAULT 0 CHECK (built_in IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO mihomo_rewrite_templates (
  id, name, description, kind, content, built_in
) VALUES (
  'mhrt_basic',
  'BoxFleet Basic',
  'A ready-to-use Mihomo baseline with DNS, PROXY/AUTO groups, and rules.',
  'yaml',
  'mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
unified-delay: true
tcp-concurrent: true
dns:
  enable: true
  enhanced-mode: fake-ip
  nameserver:
    - https://dns.alidns.com/dns-query
    - https://1.1.1.1/dns-query
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - AUTO
      - DIRECT
    include-all-proxies: true
    exclude-type: direct
  - name: AUTO
    type: url-test
    include-all-proxies: true
    exclude-type: direct
    url: https://www.gstatic.com/generate_204
    interval: 300
rules:
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
',
  1
);

CREATE TABLE mihomo_profile_subscription_tokens (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES mihomo_profiles(id) ON DELETE CASCADE,
  token TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  last_used_at TEXT,
  revoked_at TEXT
);

CREATE UNIQUE INDEX idx_mihomo_profile_subscription_tokens_active
ON mihomo_profile_subscription_tokens(profile_id)
WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS mihomo_profile_subscription_tokens;
DROP TABLE IF EXISTS mihomo_rewrite_templates;
DROP INDEX IF EXISTS idx_mihomo_profiles_proxy_user;
