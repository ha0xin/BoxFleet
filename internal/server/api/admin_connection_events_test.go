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

func TestAdminConnectionSeriesEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	var response adminConnectionSeriesResponse
	adminGetJSON(t, router, "/api/admin/connection-events/series?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z", &response)
	if response.Bucket != "hour" || response.OffsetMinutes != 0 {
		t.Fatalf("envelope = %#v", response)
	}
	// Four hourly buckets, zero-filled by the server; the client never buckets.
	if len(response.Points) != 4 {
		t.Fatalf("points = %#v", response.Points)
	}
	if response.Points[0].TotalBytes != 12288 || response.Points[1].TotalBytes != 0 {
		t.Fatalf("first two buckets = %#v", response.Points[:2])
	}
	if response.Points[2].TotalBytes != 1024 || response.Points[2].ConnectionsOpened != 40 {
		t.Fatalf("02:00 bucket = %#v", response.Points[2])
	}
	if response.Totals.TotalBytes != 13312 || response.Totals.ConnectionsOpened != 42 {
		t.Fatalf("totals = %#v", response.Totals)
	}
	// Coverage rides along so a chart cannot be drawn without its caveat.
	if response.Coverage.Reports != 1 || response.Coverage.StreamResets != 2 || response.Coverage.DroppedBuckets != 3 {
		t.Fatalf("coverage = %#v", response.Coverage)
	}
	if response.Coverage.AttributionRatio != 0.75 {
		t.Fatalf("attribution ratio = %v, want 750/1000", response.Coverage.AttributionRatio)
	}
}

func TestAdminConnectionSeriesRequiresWindow(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	if code, _ := adminStatus(t, router, http.MethodGet, "/api/admin/connection-events/series", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("missing window status = %d, want 422", code)
	}
	if code, _ := adminStatus(t, router, http.MethodGet,
		"/api/admin/connection-events/series?start=2026-07-25T03:00:00Z&end=2026-07-25T00:00:00Z", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("inverted window status = %d, want 422", code)
	}
}

func TestAdminConnectionHostsEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	seedConnectionEvents(t, ctx, store)

	var byBytes adminConnectionHostsResponse
	adminGetJSON(t, router, "/api/admin/connection-events/hosts?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z", &byBytes)
	if byBytes.Sort != "bytes" || len(byBytes.Hosts) != 2 {
		t.Fatalf("hosts = %#v", byBytes)
	}
	if byBytes.Hosts[0].Host != "example.com" || byBytes.Hosts[0].TotalBytes != 12288 {
		t.Fatalf("byte ranking leader = %#v", byBytes.Hosts[0])
	}
	// The denominator is the unclipped window total, so a share column does not
	// change meaning with the requested row count.
	if byBytes.Totals.TotalBytes != 13312 || byBytes.DistinctHosts != 2 || byBytes.Truncated {
		t.Fatalf("totals = %#v", byBytes)
	}
	if byBytes.Coverage.ConnectionsObserved != 10 || byBytes.Coverage.ConnectionsUnattributed != 2 {
		t.Fatalf("coverage = %#v", byBytes.Coverage)
	}

	var byConnections adminConnectionHostsResponse
	adminGetJSON(t, router, "/api/admin/connection-events/hosts?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&group=connections", &byConnections)
	if byConnections.Sort != "connections" || byConnections.Hosts[0].Host != "telemetry.example.net" {
		t.Fatalf("connection ranking = %#v", byConnections)
	}

	// A single row requested still reports the true host count and flags that
	// the ranking is partial.
	var clipped adminConnectionHostsResponse
	adminGetJSON(t, router, "/api/admin/connection-events/hosts?start=2026-07-25T00:00:00Z&end=2026-07-25T03:00:00Z&limit=1", &clipped)
	if len(clipped.Hosts) != 1 || !clipped.Truncated || clipped.DistinctHosts != 2 {
		t.Fatalf("clipped ranking = %#v", clipped)
	}

	if code, _ := adminStatus(t, router, http.MethodGet,
		"/api/admin/connection-events/hosts?group=duration", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown sort status = %d, want 422", code)
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
