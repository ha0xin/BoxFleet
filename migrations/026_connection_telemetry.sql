-- +goose Up
-- Rich connection telemetry from sing-box 1.14's daemon gRPC
-- SubscribeConnections stream. This is a SECOND producer alongside the
-- journalctl regex scraper that feeds log_events; the two coexist and the
-- fleet default is the scraper. Nothing here is reachable until a node has an
-- enabled node_connection_telemetry row.

-- Per-node opt-in for the 1.14 `service.api` block. A missing row means
-- disabled, so the fleet-wide default is off structurally rather than by
-- convention — the production fleet runs 1.13, where that config block does not
-- parse at all.
--
-- The secret is stored in the clear because the server must render it into the
-- node config, exactly like the Reality private key and Shadowsocks passwords;
-- node *tokens* are digests because the server only ever verifies those. The
-- length CHECK is load-bearing: sing-box's daemon authenticate() returns nil
-- for an empty secret, so an empty secret silently disables auth on an endpoint
-- that also exposes StopService/ReloadService/CloseAllConnections. The schema
-- refuses to store one.
CREATE TABLE node_connection_telemetry (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
  listen_address TEXT NOT NULL DEFAULT '127.0.0.1' CHECK (listen_address <> ''),
  listen_port INTEGER NOT NULL DEFAULT 9091 CHECK (listen_port > 0 AND listen_port <= 65535),
  secret TEXT NOT NULL CHECK (length(secret) >= 32),
  rotated_at TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- One row per agent report window. Mirrors traffic_reports: the
-- (node_id, agent_boot_id, sequence) uniqueness is the whole idempotency
-- mechanism, so a retried POST collides here and the batch is skipped instead
-- of double-counting bytes.
--
-- The coverage counters are not decoration. Bytes from this source are an
-- estimate, never a ledger: observable.Subscriber.Emit drops silently when a
-- listener's 64-slot buffer is full, the closed-connection ring evicts at 1000,
-- and connection ids plus in-flight totals reset when sing-box restarts. These
-- columns are how the admin UI states the size of that gap instead of implying
-- precision. Per-user billing stays on the v2ray counters.
CREATE TABLE connection_reports (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL,
  agent_boot_id TEXT NOT NULL,
  window_start TEXT NOT NULL,
  window_end TEXT NOT NULL,
  connections_observed INTEGER NOT NULL DEFAULT 0 CHECK (connections_observed >= 0),
  connections_attributed INTEGER NOT NULL DEFAULT 0 CHECK (connections_attributed >= 0),
  connections_unattributed INTEGER NOT NULL DEFAULT 0 CHECK (connections_unattributed >= 0),
  connections_orphaned INTEGER NOT NULL DEFAULT 0 CHECK (connections_orphaned >= 0),
  stream_resets INTEGER NOT NULL DEFAULT 0 CHECK (stream_resets >= 0),
  dropped_buckets INTEGER NOT NULL DEFAULT 0 CHECK (dropped_buckets >= 0),
  bytes_observed INTEGER NOT NULL DEFAULT 0 CHECK (bytes_observed >= 0),
  bytes_attributed INTEGER NOT NULL DEFAULT 0 CHECK (bytes_attributed >= 0),
  reported_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (node_id, agent_boot_id, sequence)
);

CREATE INDEX idx_connection_reports_node_window
  ON connection_reports(node_id, window_end);

-- Coverage is summed over a time range across the whole fleet for the
-- attribution figure shown next to any byte total, so the unfiltered range scan
-- needs its own leading-time index; the trailing columns keep it covering.
CREATE INDEX idx_connection_reports_window_coverage
  ON connection_reports(
    window_end,
    window_start,
    connections_observed,
    connections_attributed,
    bytes_observed,
    bytes_attributed
  );

-- Agent-side aggregated connection telemetry.
--
-- Agents ship pre-aggregated buckets, not raw per-connection rows. Order of
-- magnitude, for a ten-node fleet with twenty active users per node: a single
-- page load opens 30-80 connections and an active user opens ~20k/day, so raw
-- rows are ~4M/day fleet-wide (~1 GB/day at this row width) — not viable in one
-- SQLite file, and not viable to buffer on a node either. Bucketing by
-- (5 minutes, credential, source ip, host, port, ...) collapses repeat contact
-- with the same host: ~60 distinct hosts per user per active 5-minute bucket,
-- ~57k rows/node/day, ~570k/day fleet-wide. That is the same order as
-- log_events already carries, at a shorter default retention.
--
-- Byte columns deliberately live here and NOT on log_events. A mass UPDATE on
-- log_events fires five FTS3 triggers per row, and logEventAggregateKey buckets
-- per minute for connection COUNTS — the wrong aggregation discipline for
-- cumulative per-connection byte totals.
--
-- proxy_user_id is nullable and unattributed rows are KEPT. Single-user
-- Shadowsocks never populates `user` on the wire, and dropping those rows (as
-- RecordLogEvents does) would silently understate every top-hosts-by-bytes
-- total. The report's coverage counters say how much of the window is
-- attributed; the rows themselves stay complete.
CREATE TABLE connection_events (
  id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  proxy_user_id TEXT REFERENCES proxy_users(id) ON DELETE SET NULL,
  auth_name TEXT NOT NULL DEFAULT '',
  source_ip TEXT NOT NULL DEFAULT '',
  -- Normalised host: the sing-box `domain` field when non-empty, otherwise the
  -- host part of `destination`. BoxFleet renders no sniff action and
  -- buildConnectionProto has no Destination.Fqdn fallback, so in practice
  -- `domain` is empty and this is the destination host. Because the raw
  -- `domain` is kept alongside, provenance stays derivable: domain <> ''
  -- means this host was sniffed.
  --
  -- Lowercased on write and the CHECK enforces it. log_events.target_host does
  -- not do this, which is why lower() has to be sprinkled over every read of
  -- that table; the invariant belongs in one place.
  target_host TEXT NOT NULL DEFAULT '' CHECK (target_host = lower(target_host)),
  target_port INTEGER NOT NULL DEFAULT 0 CHECK (target_port >= 0 AND target_port <= 65535),
  domain TEXT NOT NULL DEFAULT '' CHECK (domain = lower(domain)),
  network TEXT NOT NULL DEFAULT '',
  ip_version INTEGER NOT NULL DEFAULT 0 CHECK (ip_version IN (0, 4, 6)),
  protocol TEXT NOT NULL DEFAULT '',
  inbound TEXT NOT NULL DEFAULT '',
  inbound_type TEXT NOT NULL DEFAULT '',
  rule TEXT NOT NULL DEFAULT '',
  outbound TEXT NOT NULL DEFAULT '',
  outbound_type TEXT NOT NULL DEFAULT '',
  -- chainList flattened with '>' separators. Stored as text because it is
  -- displayed and grouped, never joined against.
  chain TEXT NOT NULL DEFAULT '',
  -- Opened and closed are tracked separately because a long-lived connection
  -- contributes bytes to several consecutive buckets. Summing "connections"
  -- over a range must use connections_opened or it counts one session many
  -- times.
  connections_opened INTEGER NOT NULL DEFAULT 0 CHECK (connections_opened >= 0),
  connections_closed INTEGER NOT NULL DEFAULT 0 CHECK (connections_closed >= 0),
  -- Deltas of uplinkTotal/downlinkTotal (proto fields 16/17). Fields 14/15 are
  -- never populated server-side by sing-box; nothing here is built on them.
  uplink_bytes INTEGER NOT NULL DEFAULT 0 CHECK (uplink_bytes >= 0),
  downlink_bytes INTEGER NOT NULL DEFAULT 0 CHECK (downlink_bytes >= 0),
  -- Summed lifetime of the connections that closed in this bucket. Divided by
  -- connections_closed it gives a mean session duration, which no existing
  -- source can produce at all — the scraper never observes a close.
  duration_ms_total INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms_total >= 0),
  aggregate_key TEXT NOT NULL,
  -- Canonical time axis: the report window truncated to 5 minutes. Range reads
  -- filter on this single sargable column rather than repeating the
  -- window_start/window_end straddle that log_events needs.
  bucket_start TEXT NOT NULL,
  window_start TEXT NOT NULL,
  window_end TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- The upsert target. Unlike log_events this is a plain UNIQUE index with no
-- partial predicate: the server computes an aggregate key for every row it
-- accepts, so the empty-key escape hatch has no callers.
CREATE UNIQUE INDEX idx_connection_events_aggregate_key
  ON connection_events(aggregate_key);

-- Deliberately four secondary indexes and no more: this is the highest-write
-- table in the schema and log_events' twelve indexes are a warning, not a
-- model. The leading-time index doubles as the top-hosts-by-bytes covering
-- index and as the retention delete's scan, so no separate bucket_start index
-- exists.
CREATE INDEX idx_connection_events_bucket_host_bytes
  ON connection_events(
    bucket_start,
    target_host,
    uplink_bytes,
    downlink_bytes,
    connections_opened
  );

CREATE INDEX idx_connection_events_node_bucket
  ON connection_events(node_id, bucket_start);

CREATE INDEX idx_connection_events_user_bucket
  ON connection_events(proxy_user_id, bucket_start);

CREATE INDEX idx_connection_events_node_user_bucket
  ON connection_events(node_id, proxy_user_id, bucket_start);

-- +goose Down
DROP INDEX IF EXISTS idx_connection_events_node_user_bucket;
DROP INDEX IF EXISTS idx_connection_events_user_bucket;
DROP INDEX IF EXISTS idx_connection_events_node_bucket;
DROP INDEX IF EXISTS idx_connection_events_bucket_host_bytes;
DROP INDEX IF EXISTS idx_connection_events_aggregate_key;
DROP TABLE IF EXISTS connection_events;
DROP INDEX IF EXISTS idx_connection_reports_window_coverage;
DROP INDEX IF EXISTS idx_connection_reports_node_window;
DROP TABLE IF EXISTS connection_reports;
DROP TABLE IF EXISTS node_connection_telemetry;
