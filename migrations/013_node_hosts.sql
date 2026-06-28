-- +goose Up
-- Support multiple public hosts per node (domain, several IPv4, IPv6). The
-- canonical `public_host` stays as the primary host (kept in sync with the first
-- entry) so the proxy_details / proxy_access_details views, search, and sorting
-- are unaffected; the full ordered list with per-host "generate a client
-- profile" selection lives in hosts_json as [{"host":..,"selected":..}, ..].
ALTER TABLE nodes ADD COLUMN hosts_json TEXT NOT NULL DEFAULT '[]';

UPDATE nodes
SET hosts_json = json_array(json_object('host', public_host, 'selected', json('true')))
WHERE hosts_json = '[]' OR hosts_json = '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN hosts_json;
