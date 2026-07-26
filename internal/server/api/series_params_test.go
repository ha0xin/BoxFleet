package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestParseSeriesParamsAcceptsAValidWindow(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		wantBucket        db.Bucket
		wantOffsetMinutes int
	}{
		{
			name:       "a one-day span derives hour buckets",
			query:      "start=2026-07-25T00:00:00Z&end=2026-07-26T00:00:00Z",
			wantBucket: db.BucketHour,
		},
		{
			name:       "a month-long span derives day buckets",
			query:      "start=2026-06-26T00:00:00Z&end=2026-07-26T00:00:00Z",
			wantBucket: db.BucketDay,
		},
		{
			name:              "an explicit day bucket keeps its offset",
			query:             "start=2026-07-25T00:00:00Z&end=2026-07-26T00:00:00Z&bucket=day&offset_minutes=-480",
			wantBucket:        db.BucketDay,
			wantOffsetMinutes: -480,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			params, ok := parseSeriesParams(rec, httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil))
			if !ok {
				t.Fatalf("rejected: %s", rec.Body.String())
			}
			if params.Bucket != tt.wantBucket {
				t.Fatalf("bucket = %q, want %q", params.Bucket, tt.wantBucket)
			}
			if params.OffsetMinutes != tt.wantOffsetMinutes {
				t.Fatalf("offset minutes = %d, want %d", params.OffsetMinutes, tt.wantOffsetMinutes)
			}
			if !params.End.After(params.Start) {
				t.Fatalf("window = %s..%s", params.Start, params.End)
			}
			if params.StartRFC3339() != params.Start.Format(time.RFC3339Nano) {
				t.Fatalf("start rendering = %q", params.StartRFC3339())
			}
		})
	}
}

func TestParseSeriesParamsRejectsUnusableWindows(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "missing start", query: "end=2026-07-26T00:00:00Z"},
		{name: "missing end", query: "start=2026-07-25T00:00:00Z"},
		{name: "unparseable start", query: "start=yesterday&end=2026-07-26T00:00:00Z"},
		{name: "end before start", query: "start=2026-07-26T00:00:00Z&end=2026-07-25T00:00:00Z"},
		{name: "empty window", query: "start=2026-07-26T00:00:00Z&end=2026-07-26T00:00:00Z"},
		{name: "unknown bucket", query: "start=2026-07-25T00:00:00Z&end=2026-07-26T00:00:00Z&bucket=minute"},
		{name: "non-integer offset", query: "start=2026-07-25T00:00:00Z&end=2026-07-26T00:00:00Z&offset_minutes=half"},
		{name: "impossible offset", query: "start=2026-07-25T00:00:00Z&end=2026-07-26T00:00:00Z&offset_minutes=2000"},
		{name: "hour buckets past the span ceiling", query: "start=2026-06-26T00:00:00Z&end=2026-07-26T00:00:00Z&bucket=hour"},
		{name: "day buckets past the span ceiling", query: "start=2020-01-01T00:00:00Z&end=2026-07-26T00:00:00Z&bucket=day"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if _, ok := parseSeriesParams(rec, httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)); ok {
				t.Fatal("accepted an unusable window")
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Fatal("empty error body")
			}
		})
	}
}

func TestQueryBoundedLimitClampsBelowTheSharedCeiling(t *testing.T) {
	tests := []struct {
		query string
		want  int64
	}{
		{query: "", want: 25},
		{query: "limit=10", want: 10},
		{query: "limit=400", want: 100},
		{query: "limit=0", want: 25},
		{query: "limit=abc", want: 25},
	}
	for _, tt := range tests {
		got := queryBoundedLimit(httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil), 25, 100)
		if got != tt.want {
			t.Fatalf("queryBoundedLimit(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestQueryGroupRejectsUnknownDimensions(t *testing.T) {
	rec := httptest.NewRecorder()
	group, ok := queryGroup(rec, httptest.NewRequest(http.MethodGet, "/", nil), "total", "total", "user", "node")
	if !ok || group != "total" {
		t.Fatalf("default group = %q, ok = %v", group, ok)
	}

	rec = httptest.NewRecorder()
	group, ok = queryGroup(rec, httptest.NewRequest(http.MethodGet, "/?group=USER", nil), "total", "total", "user", "node")
	if !ok || group != "user" {
		t.Fatalf("group = %q, ok = %v", group, ok)
	}

	rec = httptest.NewRecorder()
	if _, ok := queryGroup(rec, httptest.NewRequest(http.MethodGet, "/?group=proxy", nil), "total", "total", "user", "node"); ok {
		t.Fatal("accepted an unsupported group")
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == "" {
		t.Fatalf("missing error message: %s", rec.Body.String())
	}
}

// The telemetry routes must resolve behind admin auth rather than falling
// through to the SPA. Per-handler behaviour is covered by the dedicated
// handler tests; this only asserts the route table wiring.
func TestTelemetryRoutesAreRegistered(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AllowInsecureAdmin: true})

	routes := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/admin/traffic/series"},
		{method: http.MethodGet, path: "/api/admin/network-events/series"},
		{method: http.MethodGet, path: "/api/admin/network-events/services"},
		{method: http.MethodGet, path: "/api/admin/network-events/hosts"},
		{method: http.MethodGet, path: "/api/admin/service-overrides"},
		{method: http.MethodPut, path: "/api/admin/service-overrides"},
		{method: http.MethodDelete, path: "/api/admin/service-overrides/example.com"},
	}
	for _, route := range routes {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(route.method, route.path, nil))
		switch {
		case rec.Code == http.StatusNotFound, rec.Code == http.StatusMethodNotAllowed:
			t.Fatalf("%s %s is not routed: status = %d", route.method, route.path, rec.Code)
		case rec.Code == http.StatusNotImplemented:
			t.Fatalf("%s %s still returns a placeholder 501", route.method, route.path)
		case strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html"):
			t.Fatalf("%s %s fell through to the SPA handler", route.method, route.path)
		}
	}

	// The existing paged endpoint must keep resolving alongside its new siblings.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/network-events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("network events status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
