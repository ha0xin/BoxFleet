package db

import (
	"context"
	"strings"
	"testing"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

// These tests own the connection-telemetry schema contract: the invariants the
// DDL is supposed to enforce, and the query plans the indexes exist for. They
// deliberately drive raw SQL rather than the facade, so they keep passing while
// the ingest and read paths are still being built on top.

// The empty-secret hazard is not a style point. sing-box's daemon
// authenticate() returns nil when the secret is empty, which disables auth on
// an endpoint that also exposes StopService, ReloadService and
// CloseAllConnections. The schema has to refuse to store one.
func TestNodeConnectionTelemetryRejectsWeakSecrets(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	const strong = "0123456789abcdef0123456789abcdef"
	rejects := map[string]string{
		"empty secret": "",
		"short secret": "0123456789abcdef",
	}
	for name, secret := range rejects {
		_, err := db.sql.ExecContext(ctx,
			`INSERT INTO node_connection_telemetry (node_id, enabled, secret) VALUES ('node-1', 1, ?)`, secret)
		if err == nil {
			t.Errorf("%s: insert succeeded, want a CHECK violation", name)
			if _, cleanupErr := db.sql.ExecContext(ctx, `DELETE FROM node_connection_telemetry`); cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
		}
	}
	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO node_connection_telemetry (node_id, enabled, secret) VALUES ('node-1', 1, ?)`, strong); err != nil {
		t.Fatal(err)
	}
}

// Absence of a row is the disabled state, so a fresh node must never render the
// 1.14 service.api block. The production fleet runs 1.13, where that block does
// not parse at all.
func TestConnectionTelemetryDefaultsOffPerNode(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	rows, err := db.q.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("enabled nodes = %d before any opt-in, want 0", len(rows))
	}

	if _, err := db.sql.ExecContext(ctx,
		`INSERT INTO node_connection_telemetry (node_id, enabled, secret)
		 VALUES ('node-1', 0, '0123456789abcdef0123456789abcdef')`); err != nil {
		t.Fatal(err)
	}
	rows, err = db.q.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("enabled nodes = %d for an explicitly disabled row, want 0", len(rows))
	}

	if _, err := db.sql.ExecContext(ctx,
		`UPDATE node_connection_telemetry SET enabled = 1 WHERE node_id = 'node-1'`); err != nil {
		t.Fatal(err)
	}
	rows, err = db.q.ListEnabledConnectionTelemetryNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].NodeName != "edge" {
		t.Fatalf("enabled nodes = %+v, want just edge", rows)
	}
}

// log_events.target_host is stored as reported, which is why lower() has to be
// repeated in every read of it. This table pushes the invariant into the DDL.
func TestConnectionEventsRejectUppercaseHosts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	const bucket = "2026-07-26T10:00:00.000Z"
	if _, err := db.sql.ExecContext(ctx, connectionEventInsert,
		"ce-upper", "Example.com", "", "key-upper", bucket, bucket, bucket); err == nil {
		t.Error("uppercase target_host was accepted")
	}
	if _, err := db.sql.ExecContext(ctx, connectionEventInsert,
		"ce-upper-domain", "example.com", "CDN.example.com", "key-upper-domain", bucket, bucket, bucket); err == nil {
		t.Error("uppercase domain was accepted")
	}
	if _, err := db.sql.ExecContext(ctx, connectionEventInsert,
		"ce-lower", "example.com", "", "key-lower", bucket, bucket, bucket); err != nil {
		t.Fatal(err)
	}
}

// The upsert is what keeps a session that spans two report windows in one row.
// Measures sum; the observed window widens in both directions.
func TestUpsertConnectionEventMergesMeasures(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	insert := func(id string, opened, uplink, downlink int64, windowStart, windowEnd string) {
		t.Helper()
		if _, err := db.sql.ExecContext(ctx, `
INSERT INTO connection_events (
  id, node_id, proxy_user_id, target_host, target_port,
  connections_opened, connections_closed, uplink_bytes, downlink_bytes, duration_ms_total,
  aggregate_key, bucket_start, window_start, window_end
) VALUES (?, 'node-1', 'user-1', 'example.com', 443, ?, 1, ?, ?, 1000, 'shared-key',
  '2026-07-26T10:00:00.000Z', ?, ?)
ON CONFLICT(aggregate_key) DO UPDATE SET
  connections_opened = connection_events.connections_opened + excluded.connections_opened,
  connections_closed = connection_events.connections_closed + excluded.connections_closed,
  uplink_bytes = connection_events.uplink_bytes + excluded.uplink_bytes,
  downlink_bytes = connection_events.downlink_bytes + excluded.downlink_bytes,
  duration_ms_total = connection_events.duration_ms_total + excluded.duration_ms_total,
  proxy_user_id = COALESCE(connection_events.proxy_user_id, excluded.proxy_user_id),
  window_start = MIN(connection_events.window_start, excluded.window_start),
  window_end = MAX(connection_events.window_end, excluded.window_end)`,
			id, opened, uplink, downlink, windowStart, windowEnd); err != nil {
			t.Fatal(err)
		}
	}
	insert("ce-1", 2, 100, 900, "2026-07-26T10:01:00.000Z", "2026-07-26T10:02:00.000Z")
	insert("ce-2", 3, 20, 80, "2026-07-26T10:00:30.000Z", "2026-07-26T10:04:30.000Z")

	var (
		rows                                  int64
		opened, closed, uplink, downlink, dur int64
		windowStart, windowEnd                string
	)
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_events`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want the two reports merged into 1", rows)
	}
	if err := db.sql.QueryRowContext(ctx, `
SELECT connections_opened, connections_closed, uplink_bytes, downlink_bytes, duration_ms_total,
       window_start, window_end
FROM connection_events WHERE aggregate_key = 'shared-key'`).
		Scan(&opened, &closed, &uplink, &downlink, &dur, &windowStart, &windowEnd); err != nil {
		t.Fatal(err)
	}
	if opened != 5 || closed != 2 || uplink != 120 || downlink != 980 || dur != 2000 {
		t.Fatalf("merged measures = (%d, %d, %d, %d, %d)", opened, closed, uplink, downlink, dur)
	}
	if windowStart != "2026-07-26T10:00:30.000Z" || windowEnd != "2026-07-26T10:04:30.000Z" {
		t.Fatalf("merged window = (%q, %q)", windowStart, windowEnd)
	}
}

// A retried POST must not double-count bytes, so the report row collides before
// any bucket is applied. Same mechanism as traffic_reports.
func TestConnectionReportSequenceIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	insert := func(id string) error {
		_, err := db.sql.ExecContext(ctx, `
INSERT INTO connection_reports (id, node_id, sequence, agent_boot_id, window_start, window_end, reported_at)
VALUES (?, 'node-1', 7, 'boot-1', '2026-07-26T10:00:00.000Z', '2026-07-26T10:05:00.000Z', '2026-07-26T10:05:01.000Z')`, id)
		return err
	}
	if err := insert("cr-1"); err != nil {
		t.Fatal(err)
	}
	if err := insert("cr-2"); err == nil {
		t.Fatal("replayed (node, boot, sequence) was accepted")
	} else if !isSQLiteUniqueConstraint(err) {
		t.Fatalf("replay failed with %v, want a unique-constraint violation", err)
	}
	// A new boot id restarts the sequence space, exactly as for traffic.
	if _, err := db.sql.ExecContext(ctx, `
INSERT INTO connection_reports (id, node_id, sequence, agent_boot_id, window_start, window_end, reported_at)
VALUES ('cr-3', 'node-1', 7, 'boot-2', '2026-07-26T10:00:00.000Z', '2026-07-26T10:05:00.000Z', '2026-07-26T10:05:01.000Z')`); err != nil {
		t.Fatal(err)
	}
}

// Bytes per destination host is the one read log_events structurally cannot
// answer. Unattributed rows must participate: single-user Shadowsocks never
// populates `user`, and dropping those rows the way RecordLogEvents does would
// silently understate every total.
func TestSumConnectionBytesIncludesUnattributedRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedConnectionTelemetryFixture(t, ctx, db)

	const bucket = "2026-07-26T10:00:00.000Z"
	rows := []struct {
		id     string
		user   any
		host   string
		uplink int64
		down   int64
	}{
		{"ce-a", "user-1", "example.com", 100, 900},
		{"ce-b", nil, "example.com", 10, 90},
		{"ce-c", "user-1", "cdn.example.net", 1, 4},
	}
	for _, row := range rows {
		if _, err := db.sql.ExecContext(ctx, `
INSERT INTO connection_events (
  id, node_id, proxy_user_id, target_host, target_port,
  connections_opened, uplink_bytes, downlink_bytes,
  aggregate_key, bucket_start, window_start, window_end
) VALUES (?, 'node-1', ?, ?, 443, 1, ?, ?, ?, ?, ?, ?)`,
			row.id, row.user, row.host, row.uplink, row.down, "key-"+row.id, bucket, bucket, bucket); err != nil {
			t.Fatal(err)
		}
	}

	hosts, err := db.q.SumConnectionBytesByHost(ctx, store.SumConnectionBytesByHostParams{
		StartTime:   bucket,
		EndTime:     bucket,
		NodeID:      "",
		ProxyUserID: "",
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(hosts))
	}
	if hosts[0].TargetHost != "example.com" || hosts[0].TotalBytes != 1100 {
		t.Fatalf("top host = (%q, %d), want (example.com, 1100) including the unattributed row",
			hosts[0].TargetHost, hosts[0].TotalBytes)
	}

	users, err := db.q.SumConnectionBytesByUser(ctx, store.SumConnectionBytesByUserParams{
		StartTime: bucket,
		EndTime:   bucket,
		NodeID:    "",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("user buckets = %d, want alice plus one unattributed bucket", len(users))
	}
	byUser := map[string]int64{}
	for _, row := range users {
		byUser[row.UserName] = row.TotalBytes
	}
	if byUser["alice"] != 1005 {
		t.Fatalf("alice total = %d, want 1005", byUser["alice"])
	}
	if byUser[""] != 100 {
		t.Fatalf("unattributed total = %d, want 100", byUser[""])
	}
}

func TestConnectionTelemetryReadPathsUseBoundedIndexes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		query     string
		args      []any
		want      string
		forbidden string
	}{
		{
			name: "unfiltered time range rides the leading-time index",
			query: `
SELECT COUNT(*)
FROM connection_events e
WHERE e.bucket_start >= ? AND e.bucket_start <= ?`,
			args:      []any{"2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			want:      "idx_connection_events_bucket_host_bytes",
			forbidden: "scan e",
		},
		{
			name: "top hosts by bytes stays covering",
			query: `
SELECT e.target_host, SUM(e.uplink_bytes + e.downlink_bytes) AS total_bytes
FROM connection_events e
WHERE e.bucket_start >= ? AND e.bucket_start <= ?
GROUP BY e.target_host
ORDER BY total_bytes DESC
LIMIT 20`,
			args: []any{"2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			// Covering matters here specifically: without the trailing byte
			// columns this degrades into one table lookup per row in the window.
			want: "covering index idx_connection_events_bucket_host_bytes",
		},
		{
			name: "node plus time range",
			query: `
SELECT COUNT(*)
FROM connection_events e
WHERE e.node_id = ? AND e.bucket_start >= ? AND e.bucket_start <= ?`,
			args: []any{"node-1", "2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			want: "idx_connection_events_node_bucket",
		},
		{
			name: "user plus time range",
			query: `
SELECT COUNT(*)
FROM connection_events e
WHERE e.proxy_user_id = ? AND e.bucket_start >= ? AND e.bucket_start <= ?`,
			args: []any{"user-1", "2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			want: "idx_connection_events_user_bucket",
		},
		{
			name: "node and user plus time range",
			query: `
SELECT COUNT(*)
FROM connection_events e
WHERE e.node_id = ? AND e.proxy_user_id = ? AND e.bucket_start >= ? AND e.bucket_start <= ?`,
			args: []any{"node-1", "user-1", "2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			want: "idx_connection_events_node_user_bucket",
		},
		{
			name: "retention delete is a bounded range scan",
			query: `
DELETE FROM connection_events
WHERE bucket_start < ?`,
			args:      []any{"2026-07-21T00:00:00.000Z"},
			want:      "idx_connection_events_bucket_host_bytes",
			forbidden: "scan connection_events",
		},
		{
			name: "coverage sum over a range is covering",
			query: `
SELECT SUM(r.connections_observed), SUM(r.connections_attributed),
       SUM(r.bytes_observed), SUM(r.bytes_attributed)
FROM connection_reports r
WHERE r.window_end >= ? AND r.window_start <= ?`,
			args:      []any{"2026-07-21T00:00:00.000Z", "2026-07-22T00:00:00.000Z"},
			want:      "covering index idx_connection_reports_window_coverage",
			forbidden: "scan r",
		},
		{
			name: "the aggregate key upsert target is unique",
			query: `
SELECT id FROM connection_events WHERE aggregate_key = ?`,
			args: []any{"some-key"},
			want: "idx_connection_events_aggregate_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := explainQueryPlan(t, ctx, db, tt.query, tt.args...)
			if !strings.Contains(plan, strings.ToLower(tt.want)) {
				t.Fatalf("query plan does not contain %q:\n%s", tt.want, plan)
			}
			if tt.forbidden != "" && strings.Contains(plan, strings.ToLower(tt.forbidden)) {
				t.Fatalf("query plan contains forbidden %q:\n%s", tt.forbidden, plan)
			}
		})
	}
}

const connectionEventInsert = `
INSERT INTO connection_events (
  id, node_id, proxy_user_id, target_host, target_port, domain,
  aggregate_key, bucket_start, window_start, window_end
) VALUES (?, 'node-1', 'user-1', ?, 443, ?, ?, ?, ?, ?)`

func seedConnectionTelemetryFixture(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO proxy_users (id, name) VALUES ('user-1', 'alice')`,
		`INSERT INTO nodes (id, name, public_host) VALUES ('node-1', 'edge', '192.0.2.1')`,
	} {
		if _, err := db.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
}
