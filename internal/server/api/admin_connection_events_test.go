package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

// seedConnectionEvents opts azus in and ingests one window through the real
// node endpoint's store path, so the admin reads are asserted against rows that
// went through normalisation and clamping rather than hand-written SQL.
//
// The two hosts differ in shape on purpose: example.com is byte-heavy with few
// connections, telemetry.example.net is connection-heavy with few bytes, which
// is what makes the two sort orders distinguishable.
func seedConnectionEvents(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	seedAPITestNode(t, ctx, store)
	// Keep historical query fixtures independent of the default retention window.
	if err := store.SetConnectionEventRetentionDays(ctx, db.MaxConnectionEventRetentionDays); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	report := model.ConnectionReport{
		NodeName:    "azus",
		Sequence:    1,
		AgentBootID: "boot-1",
		WindowStart: "2026-07-25T00:00:00Z",
		WindowEnd:   "2026-07-25T02:55:00Z",
		ReportedAt:  "2026-07-25T02:55:01Z",
		Coverage: model.ConnectionCoverage{
			ConnectionsObserved:     10,
			ConnectionsAttributed:   8,
			ConnectionsUnattributed: 2,
			ConnectionsOrphaned:     1,
			StreamResets:            2,
			DroppedBuckets:          3,
			BytesObserved:           1000,
			BytesAttributed:         750,
		},
		Buckets: []model.ConnectionBucket{{
			BucketStart:       "2026-07-25T00:00:00Z",
			AuthName:          "vless-39090@alice",
			SourceIP:          "198.51.100.9",
			TargetHost:        "example.com",
			TargetPort:        443,
			Network:           "tcp",
			IPVersion:         4,
			Protocol:          "tls",
			Inbound:           "vless-39090",
			InboundType:       "vless",
			Rule:              "final",
			Outbound:          "direct",
			OutboundType:      "direct",
			Chain:             []string{"vless-39090", "direct"},
			ConnectionsOpened: 2,
			ConnectionsClosed: 2,
			UplinkBytes:       4096,
			DownlinkBytes:     8192,
			DurationMsTotal:   9000,
			WindowStart:       "2026-07-25T00:00:00Z",
			WindowEnd:         "2026-07-25T00:04:59Z",
		}, {
			BucketStart:       "2026-07-25T02:00:00Z",
			AuthName:          "", // single-user Shadowsocks never attributes.
			SourceIP:          "198.51.100.11",
			TargetHost:        "TELEMETRY.Example.NET",
			TargetPort:        443,
			Network:           "tcp",
			IPVersion:         4,
			Protocol:          "tls",
			Inbound:           "ss-8388",
			InboundType:       "shadowsocks",
			Outbound:          "direct",
			OutboundType:      "direct",
			ConnectionsOpened: 40,
			ConnectionsClosed: 40,
			UplinkBytes:       512,
			DownlinkBytes:     512,
			DurationMsTotal:   4000,
			WindowStart:       "2026-07-25T02:00:00Z",
			WindowEnd:         "2026-07-25T02:04:59Z",
		}},
	}
	if err := store.RecordConnectionReport(ctx, report); err != nil {
		t.Fatal(err)
	}
}

func TestAdminConnectionEventsEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	var response adminConnectionEventsResponse
	adminGetJSON(t, router, "/api/admin/connection-events?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z", &response)
	if response.Total != 2 || len(response.Events) != 2 {
		t.Fatalf("total = %d, events = %d, want 2 and 2", response.Total, len(response.Events))
	}
	// Newest bucket first, so the 02:00 shadowsocks row leads.
	newest := response.Events[0]
	if newest.TargetHost != "telemetry.example.net" {
		t.Fatalf("target_host = %q, want the normalised lowercase host", newest.TargetHost)
	}
	if newest.UserName != "" || newest.AuthName != "" {
		t.Fatalf("unattributed row carried user %q / auth %q", newest.UserName, newest.AuthName)
	}
	oldest := response.Events[1]
	if oldest.UserName != "alice" || oldest.AuthName != "vless-39090@alice" {
		t.Fatalf("attributed row = %+v", oldest)
	}
	// The enriched dimensions are the whole point of the 1.14 stream: the
	// journal scraper can produce none of these.
	if oldest.Network != "tcp" || oldest.Protocol != "tls" || oldest.InboundType != "vless" ||
		oldest.Rule != "final" || oldest.OutboundType != "direct" || oldest.Chain != "vless-39090>direct" {
		t.Fatalf("enriched dimensions = %+v", oldest)
	}
	if oldest.DurationMsTotal != 9000 || oldest.IPVersion != 4 {
		t.Fatalf("duration/ip version = %+v", oldest)
	}
}

// The window bounds are compared as TEXT against a fixed-width millisecond
// column, so a bound of "…T00:00:00Z" would sort after "…T00:00:00.000Z" and
// drop the first bucket. This pins the normalisation that prevents it.
func TestAdminConnectionEventsIncludesBoundaryBuckets(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	var response adminConnectionEventsResponse
	adminGetJSON(t, router, "/api/admin/connection-events?start=2026-07-25T00:00:00Z&end=2026-07-25T00:00:00Z", &response)
	if response.Total != 1 || len(response.Events) != 1 {
		t.Fatalf("total = %d, events = %d, want the bucket exactly on both bounds", response.Total, len(response.Events))
	}
	if response.Events[0].TargetHost != "example.com" {
		t.Fatalf("boundary event = %+v", response.Events[0])
	}
}

func TestAdminConnectionEventsFilters(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	var byUser adminConnectionEventsResponse
	adminGetJSON(t, router, "/api/admin/connection-events?user=alice", &byUser)
	if byUser.Total != 1 || byUser.Events[0].TargetHost != "example.com" {
		t.Fatalf("user filter = %+v", byUser)
	}

	var byHost adminConnectionEventsResponse
	adminGetJSON(t, router, "/api/admin/connection-events?host=TELEMETRY.example.net", &byHost)
	if byHost.Total != 1 || byHost.Events[0].TargetHost != "telemetry.example.net" {
		t.Fatalf("host filter should normalise before matching: %+v", byHost)
	}

	// An unknown scope name is a 422, never a silently empty page.
	if code, body := adminStatus(t, router, http.MethodGet, "/api/admin/connection-events?node=ghost", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown node status = %d, body = %s", code, body)
	}
}

func TestIntervalConnectionAnalyticsRetired(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	for _, path := range []string{"/api/admin/connection-events/series", "/api/admin/connection-events/hosts?group=bytes"} {
		code, body := adminStatus(t, router, http.MethodGet, path, nil)
		if code != http.StatusGone || !strings.Contains(string(body), "lifetime totals") {
			t.Fatalf("%s: status=%d body=%s", path, code, body)
		}
	}
}

// The fleet-wide default is that nothing streams: 1.13 cannot parse the
// service.api block at all. An empty list is a normal answer the UI explains,
// not an error.
func TestAdminConnectionTelemetryNodesEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedAPITestNode(t, ctx, store)

	var empty adminConnectionTelemetryNodesResponse
	adminGetJSON(t, router, "/api/admin/connection-events/nodes", &empty)
	if len(empty.Nodes) != 0 {
		t.Fatalf("nodes = %#v, want none before opt-in", empty.Nodes)
	}

	config, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var enabled adminConnectionTelemetryNodesResponse
	adminGetJSON(t, router, "/api/admin/connection-events/nodes", &enabled)
	if len(enabled.Nodes) != 1 || enabled.Nodes[0].NodeName != "azus" {
		t.Fatalf("nodes = %#v", enabled.Nodes)
	}
	if enabled.Nodes[0].ListenAddress != "127.0.0.1" || enabled.Nodes[0].ListenPort != 9091 {
		t.Fatalf("listen endpoint = %#v", enabled.Nodes[0])
	}
	// The secret is a full control-plane credential for sing-box's daemon API.
	// It exists in the row and must never reach an admin response body.
	_, body := adminStatus(t, router, http.MethodGet, "/api/admin/connection-events/nodes", nil)
	if config.Secret == "" {
		t.Fatal("fixture did not mint a secret")
	}
	if strings.Contains(body, config.Secret) {
		t.Fatalf("response body leaked the daemon secret: %s", body)
	}

	// Opting out returns the node to the structural default.
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	var disabled adminConnectionTelemetryNodesResponse
	adminGetJSON(t, router, "/api/admin/connection-events/nodes", &disabled)
	if len(disabled.Nodes) != 0 {
		t.Fatalf("nodes = %#v after opting out", disabled.Nodes)
	}
}

func TestAdminConnectionEndpointsRequireAdminAuth(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	for _, path := range []string{
		"/api/admin/connection-events",
		"/api/admin/connection-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T01:00:00Z",
		"/api/admin/connection-events/hosts",
		"/api/admin/connection-events/nodes",
	} {
		req, err := http.NewRequest(http.MethodGet, path, nil)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a token = %d, want 401", path, rec.Code)
		}
	}
}
