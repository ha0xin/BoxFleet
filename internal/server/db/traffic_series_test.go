package db

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

type trafficSeriesDelta struct {
	nodeID     string
	userID     string
	direction  string
	rawBytes   int64
	billable   int64
	observedAt string
}

func seedTrafficSeriesFixture(t *testing.T, ctx context.Context, store *DB, deltas []trafficSeriesDelta) {
	t.Helper()
	setup := []string{
		`INSERT INTO proxy_users (id, name) VALUES ('user-1', 'alice')`,
		`INSERT INTO proxy_users (id, name) VALUES ('user-2', 'bob')`,
		`INSERT INTO proxy_users (id, name, deleted_at) VALUES ('user-3', 'carol', '2026-07-01T00:00:00Z')`,
		`INSERT INTO nodes (id, name, public_host) VALUES ('node-1', 'edge-a', '192.0.2.1')`,
		`INSERT INTO nodes (id, name, public_host) VALUES ('node-2', 'edge-b', '192.0.2.2')`,
		`INSERT INTO traffic_reports (id, node_id, sequence, agent_boot_id, reported_at) VALUES ('report-1', 'node-1', 1, 'boot-1', '2026-07-26T00:00:00Z')`,
		`INSERT INTO traffic_reports (id, node_id, sequence, agent_boot_id, reported_at) VALUES ('report-2', 'node-2', 1, 'boot-1', '2026-07-26T00:00:00Z')`,
	}
	for _, statement := range setup {
		if _, err := store.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for i, delta := range deltas {
		reportID := "report-1"
		if delta.nodeID == "node-2" {
			reportID = "report-2"
		}
		if _, err := store.sql.ExecContext(ctx, `
INSERT INTO traffic_usage_deltas (
  id, report_id, node_id, proxy_user_id, auth_name, direction,
  raw_bytes_delta, effective_multiplier, billable_bytes_delta, counter_value, observed_at
) VALUES (?, ?, ?, ?, 'auth', ?, ?, 1, ?, 0, ?)`,
			"delta-"+strconv.Itoa(i),
			reportID,
			delta.nodeID,
			delta.userID,
			delta.direction,
			delta.rawBytes,
			delta.billable,
			delta.observedAt,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestTrafficSeriesPivotsDirectionsAndZeroFillsEmptyBuckets(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 100, billable: 120, observedAt: "2026-07-26T00:10:00Z"},
		{nodeID: "node-1", userID: "user-1", direction: "downlink", rawBytes: 900, billable: 1080, observedAt: "2026-07-26T00:40:00Z"},
		{nodeID: "node-1", userID: "user-2", direction: "uplink", rawBytes: 50, billable: 50, observedAt: "2026-07-26T00:50:00Z"},
		// Third hour only, so the middle bucket must come back zero-filled.
		{nodeID: "node-1", userID: "user-1", direction: "downlink", rawBytes: 7, billable: 7, observedAt: "2026-07-26T02:05:00Z"},
	})

	result, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T02:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroupTotal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Series) != 1 {
		t.Fatalf("series count = %d, want 1", len(result.Series))
	}
	series := result.Series[0]
	if series.Key != "total" {
		t.Fatalf("series key = %q, want total", series.Key)
	}
	if len(series.Points) != 3 {
		t.Fatalf("points = %d, want 3", len(series.Points))
	}
	first := series.Points[0]
	if first.BucketStart != mustParseTime(t, "2026-07-26T00:00:00Z") {
		t.Fatalf("first bucket = %s", first.BucketStart)
	}
	if first.UplinkRawBytes != 150 || first.UplinkBillableBytes != 170 {
		t.Fatalf("first uplink = (%d, %d), want (150, 170)", first.UplinkRawBytes, first.UplinkBillableBytes)
	}
	if first.DownlinkRawBytes != 900 || first.DownlinkBillableBytes != 1080 {
		t.Fatalf("first downlink = (%d, %d), want (900, 1080)", first.DownlinkRawBytes, first.DownlinkBillableBytes)
	}
	empty := series.Points[1]
	if empty.BucketStart != mustParseTime(t, "2026-07-26T01:00:00Z") {
		t.Fatalf("second bucket = %s", empty.BucketStart)
	}
	if empty.UplinkRawBytes != 0 || empty.DownlinkRawBytes != 0 {
		t.Fatalf("second bucket is not zero-filled: %+v", empty)
	}
	if series.Totals.UplinkRawBytes != 150 || series.Totals.DownlinkRawBytes != 907 {
		t.Fatalf("totals = %+v", series.Totals)
	}
	if series.Totals.DownlinkBillableBytes != 1087 {
		t.Fatalf("totals downlink billable = %d, want 1087", series.Totals.DownlinkBillableBytes)
	}
	if result.Truncated {
		t.Fatal("an ungrouped series is never truncated")
	}
}

// The traffic pipeline filters soft-deleted users, unlike the network-event
// pipeline. Losing this predicate would make the chart disagree with the
// traffic summaries rendered beside it.
func TestTrafficSeriesExcludesSoftDeletedUsers(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 10, billable: 10, observedAt: "2026-07-26T00:10:00Z"},
		{nodeID: "node-1", userID: "user-3", direction: "uplink", rawBytes: 999, billable: 999, observedAt: "2026-07-26T00:20:00Z"},
	})

	result, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Series[0].Totals.UplinkRawBytes; got != 10 {
		t.Fatalf("uplink total = %d, want 10 (deleted user excluded)", got)
	}

	grouped, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroupUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, series := range grouped.Series {
		if series.Key == "carol" {
			t.Fatal("grouped series includes a soft-deleted user")
		}
	}
}

func TestTrafficSeriesDayBucketsHonourTheOffset(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		// 03:00Z is still 2026-07-25 in UTC-8.
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 10, billable: 10, observedAt: "2026-07-26T03:00:00Z"},
		// 17:00Z is 2026-07-26 in UTC-8.
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 20, billable: 20, observedAt: "2026-07-26T17:00:00Z"},
	})

	result, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:         mustParseTime(t, "2026-07-25T08:00:00Z"),
		End:           mustParseTime(t, "2026-07-27T07:59:00Z"),
		Bucket:        BucketDay,
		OffsetMinutes: -480,
	})
	if err != nil {
		t.Fatal(err)
	}
	points := result.Series[0].Points
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	if points[0].BucketStart != mustParseTime(t, "2026-07-25T08:00:00Z") {
		t.Fatalf("first day bucket = %s, want the UTC instant of local midnight", points[0].BucketStart)
	}
	if points[0].UplinkRawBytes != 10 {
		t.Fatalf("first day uplink = %d, want 10", points[0].UplinkRawBytes)
	}
	if points[1].BucketStart != mustParseTime(t, "2026-07-26T08:00:00Z") {
		t.Fatalf("second day bucket = %s", points[1].BucketStart)
	}
	if points[1].UplinkRawBytes != 20 {
		t.Fatalf("second day uplink = %d, want 20", points[1].UplinkRawBytes)
	}

	utcDays, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-25T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T23:59:00Z"),
		Bucket: BucketDay,
	})
	if err != nil {
		t.Fatal(err)
	}
	utcPoints := utcDays.Series[0].Points
	if utcPoints[1].BucketStart != mustParseTime(t, "2026-07-26T00:00:00Z") {
		t.Fatalf("utc day bucket = %s", utcPoints[1].BucketStart)
	}
	if utcPoints[1].UplinkRawBytes != 30 {
		t.Fatalf("utc day uplink = %d, want both deltas on one UTC day", utcPoints[1].UplinkRawBytes)
	}
}

func TestTrafficSeriesRanksGroupedSeriesAndReportsTruncation(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 10, billable: 10, observedAt: "2026-07-26T00:10:00Z"},
		{nodeID: "node-1", userID: "user-2", direction: "uplink", rawBytes: 500, billable: 500, observedAt: "2026-07-26T00:20:00Z"},
		{nodeID: "node-2", userID: "user-2", direction: "downlink", rawBytes: 5, billable: 5, observedAt: "2026-07-26T00:30:00Z"},
	})

	all, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroupUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Series) != 2 {
		t.Fatalf("series = %d, want 2", len(all.Series))
	}
	if all.Series[0].Key != "bob" || all.Series[1].Key != "alice" {
		t.Fatalf("series order = %q, %q; want the heaviest first", all.Series[0].Key, all.Series[1].Key)
	}
	if all.Truncated {
		t.Fatal("a full result must not report truncation")
	}

	capped, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroupUser,
		Limit:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped.Series) != 1 || capped.Series[0].Key != "bob" {
		t.Fatalf("capped series = %+v", capped.Series)
	}
	if !capped.Truncated {
		t.Fatal("dropping a series must report truncation")
	}
	if capped.Series[0].Totals.UplinkRawBytes != 500 || capped.Series[0].Totals.DownlinkRawBytes != 5 {
		t.Fatalf("capped totals = %+v", capped.Series[0].Totals)
	}
}

func TestTrafficSeriesGroupsByNode(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 10, billable: 10, observedAt: "2026-07-26T00:10:00Z"},
		{nodeID: "node-2", userID: "user-1", direction: "uplink", rawBytes: 40, billable: 40, observedAt: "2026-07-26T00:20:00Z"},
	})

	result, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroupNode,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Series) != 2 {
		t.Fatalf("series = %d, want 2", len(result.Series))
	}
	if result.Series[0].Key != "edge-b" || result.Series[0].Totals.UplinkRawBytes != 40 {
		t.Fatalf("top node series = %+v", result.Series[0])
	}
}

func TestTrafficSeriesFiltersIndependentlyOfGrouping(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficSeriesFixture(t, ctx, store, []trafficSeriesDelta{
		{nodeID: "node-1", userID: "user-1", direction: "uplink", rawBytes: 10, billable: 10, observedAt: "2026-07-26T00:10:00Z"},
		{nodeID: "node-2", userID: "user-1", direction: "uplink", rawBytes: 40, billable: 40, observedAt: "2026-07-26T00:20:00Z"},
		{nodeID: "node-1", userID: "user-2", direction: "uplink", rawBytes: 70, billable: 70, observedAt: "2026-07-26T00:30:00Z"},
	})

	result, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:    mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:      mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket:   BucketHour,
		NodeName: "edge-a",
		UserName: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Series[0].Totals.UplinkRawBytes; got != 10 {
		t.Fatalf("filtered total = %d, want 10", got)
	}

	if _, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:    mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:      mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket:   BucketHour,
		NodeName: "ghost",
	}); err == nil {
		t.Fatal("an unknown node must surface as an error, not an empty chart")
	}
}

func TestTrafficSeriesRejectsAnUnknownGroup(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.TrafficSeries(ctx, TrafficSeriesFilter{
		Start:  mustParseTime(t, "2026-07-26T00:00:00Z"),
		End:    mustParseTime(t, "2026-07-26T00:59:00Z"),
		Bucket: BucketHour,
		Group:  TrafficSeriesGroup("proxy"),
	}); err == nil {
		t.Fatal("an unsupported grouping was accepted")
	}
}

func TestTrafficSeriesWindowIsBoundedByAnIndex(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	scope := trafficSeriesScope{
		StartTime: "2026-07-26T00:00:00Z",
		EndTime:   "2026-07-27T00:00:00Z",
	}

	tests := []struct {
		name          string
		scope         trafficSeriesScope
		bucket        Bucket
		offsetMinutes int
		keyColumn     string
		keyIDs        []string
		want          string
	}{
		{
			name:   "ungrouped hour buckets ride the observed-at covering index",
			scope:  scope,
			bucket: BucketHour,
			want:   "idx_traffic_usage_deltas_observed",
		},
		{
			name:          "grouped day buckets seek by user",
			scope:         scope,
			bucket:        BucketDay,
			offsetMinutes: -480,
			keyColumn:     "d.proxy_user_id",
			keyIDs:        []string{"user-1", "user-2"},
			want:          "idx_traffic_usage_user_observed",
		},
		{
			name:      "a node filter seeks by node",
			scope:     trafficSeriesScope{NodeID: "node-1", StartTime: scope.StartTime, EndTime: scope.EndTime},
			bucket:    BucketHour,
			want:      "idx_traffic_usage_node_observed",
			keyColumn: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, args := buildTrafficBucketQuery(tt.scope, tt.bucket, tt.offsetMinutes, tt.keyColumn, tt.keyIDs)
			plan := explainQueryPlan(t, ctx, store, query, args...)
			if !strings.Contains(plan, tt.want) {
				t.Fatalf("query plan does not contain %q:\n%s", tt.want, plan)
			}
			// The span ceiling only bounds the read if the time range drives an
			// index; traffic_usage_deltas is append-only and never pruned, so a
			// full scan here grows without limit.
			if strings.Contains(plan, "scan d") {
				t.Fatalf("query plan scans traffic_usage_deltas end to end:\n%s", plan)
			}
		})
	}
}
