package db

import (
	"context"
	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnifiedLogsSessionUpdatesSourceAndSearch(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	seedConnectionIngestFixture(t, ctx, d)
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second).Format(time.RFC3339Nano)
	after := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second).Format(time.RFC3339Nano)
	if _, err := d.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	legacy := func(at string) {
		t.Helper()
		if err := d.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: []LogEventInput{{AuthName: "vless-39090@alice", SourceIP: "198.51.100.9", TargetHost: "example.com", TargetPort: 443, Action: "connect", Count: 1, WindowStart: at, WindowEnd: at}}}); err != nil {
			t.Fatal(err)
		}
	}
	legacy(start)
	legacy(after)
	raw := ConnectionBucket{ConnectionID: "session-1", StartedAt: after, AuthName: "vless-39090@alice", SourceIP: "198.51.100.9", TargetHost: "example.com", TargetPort: 443, Network: "tcp", ConnectionsOpened: 1, UplinkBytes: 100, WindowStart: after, WindowEnd: after}
	r := connectionTestReport(1, after, raw)
	if err := d.RecordConnectionReport(ctx, r); err != nil {
		t.Fatal(err)
	}
	raw.UplinkBytes = 150
	raw.DownlinkBytes = 900
	raw.ConnectionsClosed = 1
	raw.DurationMsTotal = 1000
	r = connectionTestReport(2, after, raw)
	if err := d.RecordConnectionReport(ctx, r); err != nil {
		t.Fatal(err)
	}
	// Restart/replay sends absolute lifetime totals, not another additive contribution.
	r.AgentBootID = "restarted"
	r.Sequence = 1
	if err := d.RecordConnectionReport(ctx, r); err != nil {
		t.Fatal(err)
	}
	page, err := d.ListLogEventsPage(ctx, LogEventFilter{Search: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("total=%d events=%+v", page.Total, page.Events)
	}
	var stream, old *LogEventDetail
	for i := range page.Events {
		e := &page.Events[i]
		if e.Source == "stream" {
			stream = e
		} else {
			old = e
		}
	}
	if stream == nil || stream.Count != 1 || stream.UplinkBytes == nil || *stream.UplinkBytes != 150 || stream.DownlinkBytes == nil || *stream.DownlinkBytes != 900 || stream.ConnectionID != "session-1" {
		t.Fatalf("stream=%+v", stream)
	}
	if old == nil || old.UplinkBytes != nil || old.Network != "" {
		t.Fatalf("legacy=%+v", old)
	}
	boundary, err := d.ListLogEventsPage(ctx, LogEventFilter{Start: after, End: after})
	if err != nil || boundary.Total != 1 {
		t.Fatalf("mixed timestamp boundary: %+v err=%v", boundary, err)
	}
	nodeSearch, err := d.ListLogEventsPage(ctx, LogEventFilter{Search: "azus"})
	if err != nil || nodeSearch.Total != 2 {
		t.Fatalf("node search=%+v err=%v", nodeSearch, err)
	}
	hosts, _, err := d.NetworkEventHostCounts(ctx, LogEventFilter{}, 10)
	if err != nil || len(hosts) != 1 || hosts[0].Connections != 2 {
		t.Fatalf("hosts=%+v err=%v", hosts, err)
	}
	if _, err := d.SetNodeConnectionTelemetry(ctx, SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	legacy(time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano))
	page, err = d.ListLogEventsPage(ctx, LogEventFilter{})
	if err != nil || page.Total != 3 {
		t.Fatalf("fallback=%+v err=%v", page, err)
	}
}

func TestUnifiedLogsUsesTimeIndexes(t *testing.T) {
	d := openTestDB(t)
	plan := explainQueryPlan(t, context.Background(), d, `SELECT COUNT(*) FROM network_event_records e WHERE e.window_end >= ? AND e.window_start <= ?`, "2026-10-01T00:00:00Z", "2026-10-07T00:00:00Z")
	t.Log(plan)
	if !strings.Contains(plan, "idx_log_events_visible_window") || !strings.Contains(plan, "idx_connection_events_window") {
		t.Fatalf("unbounded unified query: %s", plan)
	}
}

func TestUnifiedLogsMigrationPreservesHistoryAndTraffic(t *testing.T) {
	ctx := context.Background()
	d, err := OpenSQLite(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpToContext(ctx, d.sql, ".", 29); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO nodes(id,name,public_host) VALUES('node','edge','192.0.2.1')`,
		`INSERT INTO proxy_users(id,name) VALUES('user','alice')`,
		`INSERT INTO traffic_usage_totals(proxy_user_id,direction,raw_bytes,billable_bytes) VALUES('user','uplink',100,150)`,
		`INSERT INTO node_connection_telemetry(node_id,enabled,secret) VALUES('node',1,'01234567890123456789012345678901')`,
		`INSERT INTO connection_reports(id,node_id,sequence,agent_boot_id,window_start,window_end,reported_at) VALUES('report','node',1,'boot','2026-10-01T01:00:00.000Z','2026-10-01T01:05:00.000Z','2026-10-01T01:05:00.000Z')`,
		`INSERT INTO connection_events(id,node_id,proxy_user_id,target_host,connections_opened,aggregate_key,bucket_start,window_start,window_end) VALUES('stream','node','user','example.com',2,'stream-key','2026-10-01T01:00:00.000Z','2026-10-01T01:00:00.000Z','2026-10-01T01:05:00.000Z')`,
		`INSERT INTO log_events(id,node_id,proxy_user_id,target_host,action,count,window_start,window_end) VALUES('before','node','user','old.example.com','connect',1,'2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z'),('duplicate','node','user','example.com','connect',2,'2026-10-01T01:00:00.000Z','2026-10-01T01:05:00.000Z')`,
	} {
		if _, err := d.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := d.ListLogEventsPage(ctx, LogEventFilter{})
	if err != nil || page.Total != 2 {
		t.Fatalf("migration reads=%+v err=%v", page, err)
	}
	var raw, billable, oldRows int64
	if err := d.sql.QueryRowContext(ctx, `SELECT raw_bytes,billable_bytes FROM traffic_usage_totals WHERE proxy_user_id='user'`).Scan(&raw, &billable); err != nil {
		t.Fatal(err)
	}
	d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM log_events`).Scan(&oldRows)
	if raw != 100 || billable != 150 || oldRows != 2 {
		t.Fatalf("migration altered ledger/history: %d %d %d", raw, billable, oldRows)
	}
}
