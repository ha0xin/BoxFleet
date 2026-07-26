package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func seedNetworkSeriesEvents(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	seedAPITestNode(t, ctx, store)
	if err := store.RecordLogEvents(ctx, db.LogEventReport{
		NodeName: "azus",
		Events: []db.LogEventInput{{
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "www.youtube.com",
			TargetPort:  443,
			Action:      "connect",
			Count:       3,
			WindowStart: "2026-07-25T00:10:00Z",
			WindowEnd:   "2026-07-25T00:10:00Z",
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "I.YTimg.com",
			TargetPort:  443,
			Action:      "connect",
			Count:       2,
			WindowStart: "2026-07-25T02:40:00Z",
			WindowEnd:   "2026-07-25T02:40:00Z",
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "github.com",
			TargetPort:  443,
			Action:      "connect",
			Count:       7,
			WindowStart: "2026-07-25T02:50:00Z",
			WindowEnd:   "2026-07-25T02:50:00Z",
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func adminGetJSON(t *testing.T, router http.Handler, path string, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

func adminStatus(t *testing.T, router http.Handler, method, path string, body any) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, method, path, body))
	return rec.Code, rec.Body.String()
}

func TestAdminNetworkEventSeriesEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedNetworkSeriesEvents(t, ctx, store)

	var response adminNetworkEventSeriesResponse
	adminGetJSON(t, router, "/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z", &response)
	if response.Bucket != "hour" || response.Group != "total" || response.OffsetMinutes != 0 {
		t.Fatalf("response envelope = %#v", response)
	}
	if response.Start != "2026-07-25T00:00:00Z" || response.End != "2026-07-25T03:00:00Z" {
		t.Fatalf("window echo = (%q, %q)", response.Start, response.End)
	}
	if len(response.Series) != 1 || response.Series[0].Key != "total" {
		t.Fatalf("series = %#v", response.Series)
	}
	points := response.Series[0].Points
	if len(points) != 4 {
		t.Fatalf("points = %#v", points)
	}
	if points[0].BucketStart != "2026-07-25T00:00:00Z" || points[0].Count != 3 {
		t.Fatalf("first bucket = %#v", points[0])
	}
	// Empty buckets are present because the server, not the client, zero-fills.
	if points[1].Count != 0 || points[2].Count != 9 || points[3].Count != 0 {
		t.Fatalf("points = %#v", points)
	}
	if response.Series[0].Total != 12 {
		t.Fatalf("total = %d, want 12", response.Series[0].Total)
	}
	if len(response.Actions) != 1 || response.Actions[0].Action != "connect" || response.Actions[0].Count != 12 {
		t.Fatalf("actions = %#v", response.Actions)
	}

	adminGetJSON(t, router, "/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&group=user", &response)
	if response.Group != "user" || len(response.Series) != 1 || response.Series[0].Key != "alice" {
		t.Fatalf("grouped series = %#v", response)
	}
}

func TestAdminNetworkEventSeriesRejectsUnusableRequests(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	paths := []string{
		"/api/admin/network-events/series",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T00:00:00Z",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&bucket=minute",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&group=proxy",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&offset_minutes=5000",
		// hour buckets cap at 8 days
		"/api/admin/network-events/series?start=2026-06-25T00:00:00Z&end=2026-07-25T00:00:00Z&bucket=hour",
	}
	for _, path := range paths {
		code, body := adminStatus(t, router, http.MethodGet, path, nil)
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("GET %s status = %d, body = %s", path, code, body)
		}
	}
}

func TestAdminNetworkEventServicesEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedNetworkSeriesEvents(t, ctx, store)

	var response adminServiceUsageResponse
	adminGetJSON(t, router, "/api/admin/network-events/services", &response)
	if response.Group != "service" {
		t.Fatalf("group = %q", response.Group)
	}
	if response.TotalConnections != 12 || response.TotalHosts != 3 {
		t.Fatalf("totals = (%d, %d)", response.TotalConnections, response.TotalHosts)
	}
	if response.CatalogVersion == "" {
		t.Fatal("catalog version is required so the UI can date the classification")
	}
	byKey := make(map[string]adminServiceUsageRow, len(response.Rows))
	for _, row := range response.Rows {
		byKey[row.Key] = row
	}
	if row := byKey["youtube"]; row.Connections != 5 || row.Hosts != 2 || row.Label != "YouTube" {
		t.Fatalf("youtube = %#v", row)
	}
	if row := byKey["github"]; row.Connections != 7 {
		t.Fatalf("github = %#v", row)
	}
	if response.Other.Key != "other" || response.Other.Connections != 0 {
		t.Fatalf("other = %#v", response.Other)
	}

	adminGetJSON(t, router, "/api/admin/network-events/services?group=category&limit=1", &response)
	if response.Group != "category" || len(response.Rows) != 1 {
		t.Fatalf("category response = %#v", response)
	}
	if response.Other.Connections == 0 {
		t.Fatal("a limit of one must fold the remaining categories into other")
	}

	code, body := adminStatus(t, router, http.MethodGet, "/api/admin/network-events/services?group=host", nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", code, body)
	}
}

func TestAdminNetworkEventHostsEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedNetworkSeriesEvents(t, ctx, store)

	var response adminNetworkEventHostsResponse
	adminGetJSON(t, router, "/api/admin/network-events/hosts?service=youtube", &response)
	if response.Total != 2 || len(response.Hosts) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Limit != 50 || response.Offset != 0 {
		t.Fatalf("paging echo = %#v", response)
	}
	// target_host keeps sing-box's casing; the breakdown reports the folded key.
	if response.Hosts[0].Host != "www.youtube.com" || response.Hosts[1].Host != "i.ytimg.com" {
		t.Fatalf("hosts = %#v", response.Hosts)
	}
	if response.Hosts[0].ServiceLabel != "YouTube" || response.Hosts[0].Source == "" {
		t.Fatalf("host classification = %#v", response.Hosts[0])
	}

	adminGetJSON(t, router, "/api/admin/network-events/hosts?limit=1&offset=1", &response)
	if response.Total != 3 || len(response.Hosts) != 1 || response.Limit != 1 || response.Offset != 1 {
		t.Fatalf("paged response = %#v", response)
	}

	adminGetJSON(t, router, "/api/admin/network-events/hosts?node=azus&start=2026-07-25T02:45:00Z&end=2026-07-25T03:00:00Z", &response)
	if response.Total != 1 || response.Hosts[0].Host != "github.com" {
		t.Fatalf("filtered response = %#v", response)
	}

	code, body := adminStatus(t, router, http.MethodGet, "/api/admin/network-events/hosts?start=nonsense", nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", code, body)
	}
}
