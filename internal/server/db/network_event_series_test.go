package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// seedNetworkEventSeriesFixture builds two nodes and two users so every
// grouping dimension has more than one series to rank.
func seedNetworkEventSeriesFixture(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	seedTrafficFixture(t, ctx, store)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindUserToNode(ctx, "bob", "azus"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityCredential(ctx, IssueCredentialParams{
		UserName:  "bob",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "edge", "203.0.113.11", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:   "edge",
		Name:       "vless-39091",
		Protocol:   ProtocolVLESSReality,
		Listen:     "0.0.0.0",
		ListenPort: 39091,
		Transport:  TransportTCP,
		Enabled:    true,
		SettingsJSON: `{
			"server_name": "www.amazon.com",
			"reality_private_key": "private-key",
			"reality_public_key": "public-key",
			"short_id": "01234567",
			"handshake_server": "www.amazon.com",
			"handshake_port": 443
		}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindUserToNode(ctx, "alice", "edge"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityCredential(ctx, IssueCredentialParams{
		UserName:  "alice",
		NodeName:  "edge",
		ProxyName: "vless-39091",
	}); err != nil {
		t.Fatal(err)
	}
}

func recordNetworkEvent(t *testing.T, ctx context.Context, store *DB, nodeName, authName, host string, at string, count int64) {
	t.Helper()
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: nodeName,
		Events: []LogEventInput{{
			AuthName:    authName,
			SourceIP:    "115.27.221.55",
			TargetHost:  host,
			TargetPort:  443,
			Action:      "connect",
			Count:       count,
			WindowStart: at,
			WindowEnd:   at,
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func seedNetworkEventSeriesRows(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@alice", "www.youtube.com", "2026-07-25T00:10:00Z", 3)
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@alice", "i.ytimg.com", "2026-07-25T00:40:00Z", 2)
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@bob", "www.google.com", "2026-07-25T01:20:00Z", 5)
	recordNetworkEvent(t, ctx, store, "edge", "vless-39091@alice", "github.com", "2026-07-25T03:05:00Z", 7)
}

func networkEventSeriesWindow() (string, string) {
	return "2026-07-25T00:00:00Z", "2026-07-25T04:00:00Z"
}

func TestNetworkEventSeriesZeroFillsEveryHourBucket(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)

	start, end := networkEventSeriesWindow()
	result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketHour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Series) != 1 {
		t.Fatalf("series = %#v", result.Series)
	}
	series := result.Series[0]
	if series.Key != "total" || series.Label != "All events" {
		t.Fatalf("series identity = (%q, %q)", series.Key, series.Label)
	}
	if series.Total != 17 {
		t.Fatalf("total = %d, want 17", series.Total)
	}
	// Both ends inclusive: 00:00 through 04:00 is five hour buckets, three of
	// which have no rows at all and must still be present.
	want := []int64{5, 5, 0, 7, 0}
	if len(series.Points) != len(want) {
		t.Fatalf("points = %#v", series.Points)
	}
	for i, point := range series.Points {
		if point.Count != want[i] {
			t.Fatalf("bucket %s count = %d, want %d", point.BucketStart, point.Count, want[i])
		}
		expected := time.Date(2026, 7, 25, i, 0, 0, 0, time.UTC)
		if !point.BucketStart.Equal(expected) {
			t.Fatalf("bucket start = %s, want %s", point.BucketStart, expected)
		}
	}
	if len(result.Actions) != 1 || result.Actions[0].Action != "connect" || result.Actions[0].Count != 17 {
		t.Fatalf("actions = %#v", result.Actions)
	}
	if result.Truncated {
		t.Fatal("total series should never be truncated")
	}
}

func TestNetworkEventSeriesDayBucketsHonourTheOffset(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)

	start, end := networkEventSeriesWindow()
	result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketDay,
		OffsetMinutes:  -480,
	})
	if err != nil {
		t.Fatal(err)
	}
	series := result.Series[0]
	// At UTC-8 the whole window is still July 24 locally, so every event lands
	// in one bucket whose key is the UTC instant of local midnight.
	if len(series.Points) != 1 {
		t.Fatalf("points = %#v", series.Points)
	}
	if series.Points[0].Count != 17 {
		t.Fatalf("count = %d, want 17", series.Points[0].Count)
	}
	wantStart := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	if !series.Points[0].BucketStart.Equal(wantStart) {
		t.Fatalf("bucket start = %s, want %s", series.Points[0].BucketStart, wantStart)
	}
}

func TestNetworkEventSeriesGroupsRankAndZeroFill(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)
	start, end := networkEventSeriesWindow()

	tests := []struct {
		name      string
		group     NetworkEventSeriesGroup
		wantKeys  []string
		wantTotal []int64
	}{
		{
			name:      "user",
			group:     NetworkEventSeriesGroupUser,
			wantKeys:  []string{"alice", "bob"},
			wantTotal: []int64{12, 5},
		},
		{
			name:      "node",
			group:     NetworkEventSeriesGroupNode,
			wantKeys:  []string{"azus", "edge"},
			wantTotal: []int64{10, 7},
		},
		{
			name:      "action",
			group:     NetworkEventSeriesGroupAction,
			wantKeys:  []string{"connect"},
			wantTotal: []int64{17},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
				LogEventFilter: LogEventFilter{Start: start, End: end},
				Bucket:         BucketHour,
				Group:          tt.group,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Series) != len(tt.wantKeys) {
				t.Fatalf("series = %#v", result.Series)
			}
			for i, series := range result.Series {
				if series.Key != tt.wantKeys[i] || series.Label != tt.wantKeys[i] {
					t.Fatalf("series %d identity = (%q, %q)", i, series.Key, series.Label)
				}
				if series.Total != tt.wantTotal[i] {
					t.Fatalf("series %q total = %d, want %d", series.Key, series.Total, tt.wantTotal[i])
				}
				if len(series.Points) != 5 {
					t.Fatalf("series %q points = %#v", series.Key, series.Points)
				}
				var summed int64
				for _, point := range series.Points {
					summed += point.Count
				}
				if summed != tt.wantTotal[i] {
					t.Fatalf("series %q buckets sum to %d, want %d", series.Key, summed, tt.wantTotal[i])
				}
			}
			if result.Truncated {
				t.Fatal("unexpected truncation")
			}
		})
	}
}

func TestNetworkEventSeriesReportsTruncatedGroups(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)

	start, end := networkEventSeriesWindow()
	result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketHour,
		Group:          NetworkEventSeriesGroupUser,
		Limit:          1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatal("two users over a limit of one should report truncation")
	}
	if len(result.Series) != 1 || result.Series[0].Key != "alice" {
		t.Fatalf("series = %#v", result.Series)
	}
	// The action histogram is never truncated: it covers the full scope so the
	// legend beside the chart still adds up.
	if len(result.Actions) != 1 || result.Actions[0].Count != 17 {
		t.Fatalf("actions = %#v", result.Actions)
	}
}

func TestNetworkEventSeriesAppliesTheSameFiltersAsTheTable(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)
	start, end := networkEventSeriesWindow()

	filters := []LogEventFilter{
		{Start: start, End: end, NodeName: "azus"},
		{Start: start, End: end, UserName: "alice"},
		{Start: start, End: end, Action: "connect"},
		{Start: start, End: end, Search: "github.com"},
	}
	for _, filter := range filters {
		page, err := store.ListLogEventsPage(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{LogEventFilter: filter, Bucket: BucketHour})
		if err != nil {
			t.Fatal(err)
		}
		var rowCounts int64
		for _, event := range page.Events {
			rowCounts += event.Count
		}
		if result.Series[0].Total != rowCounts {
			t.Fatalf("filter %#v: series total = %d, table rows sum to %d", filter, result.Series[0].Total, rowCounts)
		}
	}
}

func TestNetworkEventSeriesKeepsSoftDeletedUsers(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedNetworkEventSeriesRows(t, ctx, store)
	if _, err := store.SoftDeleteProxyUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}

	start, end := networkEventSeriesWindow()
	result, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketHour,
		Group:          NetworkEventSeriesGroupUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The paged table joins proxy_users without a deleted_at filter, so the
	// chart above it must not quietly drop the same rows.
	if len(result.Series) != 2 {
		t.Fatalf("series = %#v", result.Series)
	}
	if result.Series[1].Key != "bob" || result.Series[1].Total != 5 {
		t.Fatalf("deleted user series = %#v", result.Series[1])
	}
}

func TestNetworkEventSeriesRequiresAWindow(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)

	if _, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{Bucket: BucketHour}); err == nil {
		t.Fatal("an open window cannot be zero-filled and must be rejected")
	}
	start, end := networkEventSeriesWindow()
	if _, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketHour,
		OffsetMinutes:  5000,
	}); err == nil {
		t.Fatal("an out-of-range offset must be rejected")
	}
	if _, err := store.NetworkEventSeries(ctx, NetworkEventSeriesFilter{
		LogEventFilter: LogEventFilter{Start: start, End: end},
		Bucket:         BucketHour,
		Group:          NetworkEventSeriesGroup("proxy"),
	}); err == nil {
		t.Fatal("log_events has no proxy_id, so grouping by proxy must be rejected")
	}
}

func TestNetworkEventAggregationsUseBoundedIndexes(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		query string
		args  []any
		want  string
	}{
		{
			name: "hour bucket aggregation uses the visible time-window index",
			query: `
SELECT ` + bucketExpr("e.window_start", BucketHour, 0) + ` AS bucket_key, SUM(e.count) AS connections
FROM log_events e
WHERE e.proxy_user_id IS NOT NULL
  AND e.window_end >= ?
  AND e.window_start <= ?
GROUP BY bucket_key`,
			args: []any{"2026-07-25T00:00:00Z", "2026-07-25T04:00:00Z"},
			want: "idx_log_events_visible_window",
		},
		{
			name: "day bucket aggregation uses the visible time-window index",
			query: `
SELECT ` + bucketExpr("e.window_start", BucketDay, -480) + ` AS bucket_key, SUM(e.count) AS connections
FROM log_events e
WHERE e.proxy_user_id IS NOT NULL
  AND e.window_end >= ?
  AND e.window_start <= ?
GROUP BY bucket_key`,
			args: []any{"2026-07-25T00:00:00Z", "2026-07-25T04:00:00Z"},
			want: "idx_log_events_visible_window",
		},
		{
			name: "host aggregation uses the covering host index",
			query: `
SELECT lower(e.target_host) AS host, SUM(e.count) AS connections, MAX(e.window_end) AS last_seen
FROM log_events e
WHERE e.proxy_user_id IS NOT NULL
  AND e.window_end >= ?
  AND e.window_start <= ?
GROUP BY host
ORDER BY connections DESC, host ASC
LIMIT ?`,
			args: []any{"2026-07-25T00:00:00Z", "2026-07-25T04:00:00Z", 100},
			want: "idx_log_events_visible_window_host",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := explainQueryPlan(t, ctx, store, tt.query, tt.args...)
			if !strings.Contains(plan, strings.ToLower(tt.want)) {
				t.Fatalf("query plan does not contain %q:\n%s", tt.want, plan)
			}
			if strings.Contains(plan, "scan log_events") {
				t.Fatalf("query plan falls back to a full table scan:\n%s", plan)
			}
		})
	}
}
