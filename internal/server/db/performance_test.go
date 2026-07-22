package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
)

func TestTelemetryRollupMigrationBackfillsExistingHistory(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "boxfleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpToContext(ctx, db.sql, ".", 16); err != nil {
		t.Fatal(err)
	}

	statements := []string{
		`INSERT INTO proxy_users (id, name) VALUES ('user-1', 'alice')`,
		`INSERT INTO nodes (id, name, public_host) VALUES ('node-1', 'edge', '192.0.2.1')`,
		`INSERT INTO traffic_reports (id, node_id, sequence, agent_boot_id, reported_at) VALUES ('report-1', 'node-1', 1, 'boot-1', '2026-07-22T00:00:00Z')`,
		`INSERT INTO traffic_usage_deltas (id, report_id, node_id, proxy_user_id, auth_name, direction, raw_bytes_delta, effective_multiplier, billable_bytes_delta, counter_value, observed_at) VALUES ('delta-1', 'report-1', 'node-1', 'user-1', 'alice', 'uplink', 10, 1.2, 12, 10, '2026-07-22T00:00:00Z')`,
		`INSERT INTO traffic_usage_deltas (id, report_id, node_id, proxy_user_id, auth_name, direction, raw_bytes_delta, effective_multiplier, billable_bytes_delta, counter_value, observed_at) VALUES ('delta-2', 'report-1', 'node-1', 'user-1', 'alice', 'uplink', 15, 1.2, 18, 25, '2026-07-22T00:01:00Z')`,
		`INSERT INTO node_heartbeats (id, node_id, reported_at, created_at) VALUES ('heartbeat-old', 'node-1', '2026-07-22T00:00:00Z', '2026-07-22T00:00:00Z')`,
		`INSERT INTO node_heartbeats (id, node_id, reported_at, created_at) VALUES ('heartbeat-new', 'node-1', '2026-07-22T00:01:00Z', '2026-07-22T00:01:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var rawBytes, billableBytes int64
	if err := db.sql.QueryRowContext(ctx, `SELECT raw_bytes, billable_bytes FROM traffic_usage_totals WHERE proxy_user_id = 'user-1' AND direction = 'uplink'`).Scan(&rawBytes, &billableBytes); err != nil {
		t.Fatal(err)
	}
	if rawBytes != 25 || billableBytes != 30 {
		t.Fatalf("backfilled totals = (%d, %d), want (25, 30)", rawBytes, billableBytes)
	}
	var heartbeatID string
	if err := db.sql.QueryRowContext(ctx, `SELECT heartbeat_id FROM node_latest_heartbeats WHERE node_id = 'node-1'`).Scan(&heartbeatID); err != nil {
		t.Fatal(err)
	}
	if heartbeatID != "heartbeat-new" {
		t.Fatalf("latest heartbeat = %q, want heartbeat-new", heartbeatID)
	}
}

func TestManagementReadPathsUseBoundedIndexes(t *testing.T) {
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
			name: "traffic summaries use the incremental rollup",
			query: `
SELECT u.name, d.direction, d.raw_bytes, d.billable_bytes
FROM traffic_usage_totals d
JOIN proxy_users u ON u.id = d.proxy_user_id
WHERE u.deleted_at IS NULL
ORDER BY u.name, d.direction`,
			want:      "traffic_usage_totals",
			forbidden: "traffic_usage_deltas",
		},
		{
			name: "node status uses the latest-heartbeat pointer",
			query: `
SELECT n.name, h.reported_at
FROM nodes n
LEFT JOIN node_latest_heartbeats latest ON latest.node_id = n.id
LEFT JOIN node_heartbeats h ON h.id = latest.heartbeat_id
ORDER BY n.name`,
			want:      "node_latest_heartbeats",
			forbidden: "scan h",
		},
		{
			name: "network event count uses the visible time-window index",
			query: `
SELECT COUNT(*)
FROM log_events e
WHERE e.proxy_user_id IS NOT NULL
  AND e.window_end >= ?
  AND e.window_start <= ?`,
			args: []any{"2026-07-21T00:00:00Z", "2026-07-22T00:00:00Z"},
			want: "idx_log_events_visible_window",
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

func explainQueryPlan(t *testing.T, ctx context.Context, db *DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.sql.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, strings.ToLower(detail))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, "\n")
}
