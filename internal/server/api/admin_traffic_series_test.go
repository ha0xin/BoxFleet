package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func seedTrafficSeriesReports(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	seedAPITestNode(t, ctx, store)
	if _, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: "bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindUserToNode(ctx, "bob", "azus"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityAccess(ctx, db.IssueCredentialParams{
		UserName:  "bob",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
	reports := []db.TrafficReport{
		{
			NodeName:    "azus",
			Sequence:    1,
			AgentBootID: "boot",
			Deltas: []db.TrafficDelta{
				{AuthName: "vless-39090@alice", Direction: "uplink", RawBytesDelta: 100, CounterValue: 100, ObservedAt: "2026-07-26T00:15:00Z"},
				{AuthName: "vless-39090@alice", Direction: "downlink", RawBytesDelta: 900, CounterValue: 900, ObservedAt: "2026-07-26T00:15:00Z"},
			},
		},
		{
			NodeName:    "azus",
			Sequence:    2,
			AgentBootID: "boot",
			Deltas: []db.TrafficDelta{
				{AuthName: "vless-39090@bob", Direction: "uplink", RawBytesDelta: 25, CounterValue: 25, ObservedAt: "2026-07-26T02:30:00Z"},
			},
		},
	}
	for _, report := range reports {
		if err := store.RecordTrafficReport(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAdminTrafficSeriesReturnsZeroFilledBuckets(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedTrafficSeriesReports(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet,
		"/api/admin/traffic/series?start=2026-07-26T00:00:00Z&end=2026-07-26T02:59:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response adminTrafficSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Bucket != "hour" {
		t.Fatalf("bucket = %q, want the derived hour bucket", response.Bucket)
	}
	if response.Group != "total" {
		t.Fatalf("group = %q, want total", response.Group)
	}
	if len(response.Series) != 1 || response.Series[0].Key != "total" {
		t.Fatalf("series = %+v", response.Series)
	}
	points := response.Series[0].Points
	if len(points) != 3 {
		t.Fatalf("points = %d, want one per hour in the window", len(points))
	}
	if points[0].BucketStart != "2026-07-26T00:00:00Z" {
		t.Fatalf("first bucket start = %q", points[0].BucketStart)
	}
	if points[0].UplinkRawBytes != 100 || points[0].DownlinkRawBytes != 900 {
		t.Fatalf("first point = %+v", points[0])
	}
	if points[1].UplinkRawBytes != 0 || points[1].DownlinkRawBytes != 0 {
		t.Fatalf("empty hour is not zero-filled: %+v", points[1])
	}
	if points[2].UplinkRawBytes != 25 {
		t.Fatalf("third point = %+v", points[2])
	}
	if response.Series[0].Totals.UplinkRawBytes != 125 || response.Series[0].Totals.DownlinkRawBytes != 900 {
		t.Fatalf("totals = %+v", response.Series[0].Totals)
	}
	if response.Truncated {
		t.Fatal("an ungrouped series is never truncated")
	}
}

func TestAdminTrafficSeriesGroupsAndFilters(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedTrafficSeriesReports(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet,
		"/api/admin/traffic/series?start=2026-07-26T00:00:00Z&end=2026-07-26T02:59:00Z&group=user&limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var grouped adminTrafficSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&grouped); err != nil {
		t.Fatal(err)
	}
	if len(grouped.Series) != 1 || grouped.Series[0].Key != "alice" {
		t.Fatalf("grouped series = %+v", grouped.Series)
	}
	if !grouped.Truncated {
		t.Fatal("a capped grouping must report truncation")
	}
	if len(grouped.Series[0].Points) != 3 {
		t.Fatalf("grouped points = %d, want the same zero-filled window", len(grouped.Series[0].Points))
	}

	filtered := httptest.NewRecorder()
	router.ServeHTTP(filtered, adminJSONRequest(t, http.MethodGet,
		"/api/admin/traffic/series?start=2026-07-26T00:00:00Z&end=2026-07-26T02:59:00Z&user=bob&node=azus", nil))
	if filtered.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", filtered.Code, filtered.Body.String())
	}
	var scoped adminTrafficSeriesResponse
	if err := json.NewDecoder(filtered.Body).Decode(&scoped); err != nil {
		t.Fatal(err)
	}
	if scoped.Series[0].Totals.UplinkRawBytes != 25 || scoped.Series[0].Totals.DownlinkRawBytes != 0 {
		t.Fatalf("scoped totals = %+v", scoped.Series[0].Totals)
	}
}

func TestAdminTrafficSeriesEchoesDayBucketOffsets(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedTrafficSeriesReports(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet,
		"/api/admin/traffic/series?start=2026-07-25T08:00:00Z&end=2026-07-27T07:59:00Z&bucket=day&offset_minutes=-480", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response adminTrafficSeriesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Bucket != "day" || response.OffsetMinutes != -480 {
		t.Fatalf("bucket = %q, offset = %d", response.Bucket, response.OffsetMinutes)
	}
	if response.Start != "2026-07-25T08:00:00Z" || response.End != "2026-07-27T07:59:00Z" {
		t.Fatalf("window = %s..%s", response.Start, response.End)
	}
	points := response.Series[0].Points
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2 local days", len(points))
	}
	if points[0].BucketStart != "2026-07-25T08:00:00Z" {
		t.Fatalf("first day bucket = %q, want the UTC instant of local midnight", points[0].BucketStart)
	}
	// 00:15Z and 02:30Z on 2026-07-26 are both still 2026-07-25 in UTC-8.
	if points[0].UplinkRawBytes != 125 || points[1].UplinkRawBytes != 0 {
		t.Fatalf("day buckets ignored the offset: %+v", points)
	}
}

func TestAdminTrafficSeriesRejectsBadRequests(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedTrafficSeriesReports(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	tests := []struct {
		name  string
		query string
	}{
		{name: "missing window", query: ""},
		{name: "missing end", query: "?start=2026-07-26T00:00:00Z"},
		{name: "inverted window", query: "?start=2026-07-26T00:00:00Z&end=2026-07-25T00:00:00Z"},
		{name: "unknown bucket", query: "?start=2026-07-26T00:00:00Z&end=2026-07-26T01:00:00Z&bucket=minute"},
		{name: "unknown group", query: "?start=2026-07-26T00:00:00Z&end=2026-07-26T01:00:00Z&group=proxy"},
		{name: "impossible offset", query: "?start=2026-07-26T00:00:00Z&end=2026-07-26T01:00:00Z&offset_minutes=5000"},
		{name: "span past the hour ceiling", query: "?start=2026-06-26T00:00:00Z&end=2026-07-26T00:00:00Z&bucket=hour"},
		{name: "unknown user", query: "?start=2026-07-26T00:00:00Z&end=2026-07-26T01:00:00Z&user=ghost"},
		{name: "unknown node", query: "?start=2026-07-26T00:00:00Z&end=2026-07-26T01:00:00Z&node=ghost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/traffic/series"+tt.query, nil))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}
