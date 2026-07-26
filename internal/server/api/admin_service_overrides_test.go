package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminServiceOverridesEndpoints(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedNetworkSeriesEvents(t, ctx, store)

	var overrides []adminServiceOverride
	adminGetJSON(t, router, "/api/admin/service-overrides", &overrides)
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v", overrides)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPut, "/api/admin/service-overrides", map[string]string{
		"suffix":   ".YTImg.com.",
		"service":  "cdn",
		"label":    "Edge CDN",
		"category": "infrastructure",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created adminServiceOverride
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Suffix != "ytimg.com" || created.Label != "Edge CDN" {
		t.Fatalf("created = %#v", created)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("created = %#v", created)
	}

	adminGetJSON(t, router, "/api/admin/service-overrides", &overrides)
	if len(overrides) != 1 || overrides[0].Suffix != "ytimg.com" {
		t.Fatalf("overrides = %#v", overrides)
	}

	// Classification is read-time, so the override reaches the breakdown with
	// no ingest change and no backfill.
	var usage adminServiceUsageResponse
	adminGetJSON(t, router, "/api/admin/network-events/services", &usage)
	byKey := make(map[string]adminServiceUsageRow, len(usage.Rows))
	for _, row := range usage.Rows {
		byKey[row.Key] = row
	}
	if row := byKey["cdn"]; row.Connections != 2 || row.Category != "infrastructure" {
		t.Fatalf("cdn = %#v", row)
	}
	if row := byKey["youtube"]; row.Connections != 3 || row.Hosts != 1 {
		t.Fatalf("youtube = %#v", row)
	}

	code, body := adminStatus(t, router, http.MethodDelete, "/api/admin/service-overrides/YTImg.com", nil)
	if code != http.StatusNoContent || body != "" {
		t.Fatalf("delete status = %d, body = %s", code, body)
	}
	adminGetJSON(t, router, "/api/admin/service-overrides", &overrides)
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v", overrides)
	}
}

func TestAdminServiceOverridesRejectUnusablePayloads(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	payloads := []map[string]string{
		{"service": "cdn"},
		{"suffix": "   ", "service": "cdn"},
		{"suffix": "ytimg.com"},
		{"suffix": "https://ytimg.com/x", "service": "cdn"},
	}
	for _, payload := range payloads {
		code, body := adminStatus(t, router, http.MethodPut, "/api/admin/service-overrides", payload)
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("payload %#v status = %d, body = %s", payload, code, body)
		}
	}

	rec := httptest.NewRecorder()
	req := adminJSONRequest(t, http.MethodPut, "/api/admin/service-overrides", nil)
	req.Body = http.NoBody
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminServiceOverridesRequireAdminAuth(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	for _, path := range []string{
		"/api/admin/service-overrides",
		"/api/admin/network-events/services",
		"/api/admin/network-events/hosts",
		"/api/admin/network-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s status = %d", path, rec.Code)
		}
	}
}
