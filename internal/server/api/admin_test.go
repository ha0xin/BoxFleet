package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func seedAdminUsersPageFixture(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	users := []db.CreateProxyUserParams{
		{Name: "alice", DisplayName: "Alice Zhang", GlobalQuotaBytes: 1000},
		{Name: "bob", DisplayName: "Bob Lee"},
		{Name: "carol", DisplayName: "Carol Wu"},
	}
	for _, params := range users {
		if _, err := store.CreateProxyUser(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetProxyUserStatus(ctx, "carol", "disabled"); err != nil {
		t.Fatal(err)
	}
}

func seedAdminSystemLogsFixture(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	entries := []db.SystemLogInput{
		{Service: "sing-box", Level: "info", RawMessage: "inbound started", ObservedAt: base.Format(time.RFC3339Nano), Cursor: "s=1;i=1"},
		{Service: "boxfleet-agent", Level: "warning", RawMessage: "config apply pending", ObservedAt: base.Add(time.Minute).Format(time.RFC3339Nano), Cursor: "s=1;i=2"},
		{Service: "boxfleet-agent", Level: "err", RawMessage: "heartbeat failed", ObservedAt: base.Add(2 * time.Minute).Format(time.RFC3339Nano), Cursor: "s=1;i=3"},
	}
	if err := store.RecordSystemLogs(ctx, db.SystemLogReport{NodeName: "azus", Entries: entries}); err != nil {
		t.Fatal(err)
	}
}

// The unpaged array is what the overview, traffic, network-events, and Mihomo
// pages read. Only a request that carries a page parameter may switch shape.
func TestAdminUsersKeepsTheUnpagedArrayWithoutPageParams(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAdminUsersPageFixture(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/users", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var users []adminUser
	if err := json.NewDecoder(rec.Body).Decode(&users); err != nil {
		t.Fatalf("unpaged response is not an array: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("users = %+v, want all three", users)
	}
}

func TestAdminUsersPageFiltersSortsAndCarriesTraffic(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAdminUsersPageFixture(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet,
		"/api/admin/users?limit=2&offset=0&sort=name&direction=desc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page adminUsersPage
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("envelope = %d/%d/%d, want 3/2/0", page.Total, page.Limit, page.Offset)
	}
	if len(page.Users) != 2 || page.Users[0].Name != "carol" {
		t.Fatalf("users = %+v, want a descending page starting at carol", page.Users)
	}
	if page.Users[0].EffectiveStatus != "disabled" {
		t.Fatalf("effective_status = %q, want disabled", page.Users[0].EffectiveStatus)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/users?status=disabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	page = adminUsersPage{}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Users) != 1 || page.Users[0].Name != "carol" {
		t.Fatalf("status filter = %+v (total %d)", page.Users, page.Total)
	}
}

// The paged row is consumed as an AdminUser plus two extra keys, so the
// embedded identity fields must stay flat in the JSON object.
func TestAdminUsersPageRowFlattensTheUserFields(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAdminUsersPageFixture(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/users?limit=1&search=alice", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var raw struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Users) != 1 {
		t.Fatalf("users = %+v", raw.Users)
	}
	row := raw.Users[0]
	for _, key := range []string{
		"id", "name", "display_name", "status", "global_quota_bytes",
		"expire_at", "proxy_count", "deleted_at", "effective_status", "traffic",
	} {
		if _, ok := row[key]; !ok {
			t.Fatalf("paged user row is missing %q: %+v", key, row)
		}
	}
	traffic, ok := row["traffic"].(map[string]any)
	if !ok {
		t.Fatalf("traffic = %#v, want an object", row["traffic"])
	}
	for _, key := range []string{
		"uplink_raw_bytes", "uplink_billable_bytes", "downlink_raw_bytes", "downlink_billable_bytes",
	} {
		if _, ok := traffic[key]; !ok {
			t.Fatalf("traffic is missing %q: %+v", key, traffic)
		}
	}
}

func TestAdminSystemLogsPageFiltersAndReportsTotal(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAdminSystemLogsFixture(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/system-logs?limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page adminSystemLogsResponse
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Limit != 2 || page.Offset != 0 {
		t.Fatalf("envelope = %d/%d/%d, want 3/2/0", page.Total, page.Limit, page.Offset)
	}
	if len(page.Logs) != 2 || page.Logs[0].Message != "heartbeat failed" {
		t.Fatalf("logs = %+v, want the newest two", page.Logs)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet,
		"/api/admin/system-logs?node=azus&service=boxfleet-agent&level=error", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	page = adminSystemLogsResponse{}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Logs) != 1 || page.Logs[0].Message != "heartbeat failed" {
		t.Fatalf("filtered logs = %+v (total %d)", page.Logs, page.Total)
	}
	if page.Logs[0].Node != "azus" {
		t.Fatalf("node = %q, want azus", page.Logs[0].Node)
	}
	// Filter options must not narrow when the filter is applied.
	if len(page.Services) != 2 || page.Services[0] != "boxfleet-agent" || page.Services[1] != "sing-box" {
		t.Fatalf("services = %v, want the unfiltered option list", page.Services)
	}
}

func TestAdminSystemLogsRejectsAnUnknownNode(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAdminSystemLogsFixture(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/system-logs?node=missing", nil))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
