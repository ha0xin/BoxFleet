package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestNodeConfigEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/node/config", nil)
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()

	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-BoxFleet-Config-SHA256") == "" {
		t.Fatal("missing config hash header")
	}
	if rec.Header().Get("X-BoxFleet-Config-Mode") != "rendered" {
		t.Fatalf("config mode = %q", rec.Header().Get("X-BoxFleet-Config-Mode"))
	}
	if body := rec.Body.String(); !strings.Contains(body, `"tag": "vless-39090"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestNodeConfigEndpointSignalsDisabled(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	// Disable via status only (token stays valid, unlike the decommission path).
	if err := store.SetNodeStatus(ctx, "azus", "disabled"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/node/config", nil)
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()

	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-BoxFleet-Node-State") != "disabled" {
		t.Fatalf("node state = %q, want disabled", rec.Header().Get("X-BoxFleet-Node-State"))
	}
	// Body is a valid no-inbound config (for legacy agents); it must not carry
	// any inbounds.
	if body := rec.Body.String(); strings.Contains(body, `"inbounds"`) {
		t.Fatalf("disabled config should have no inbounds: %s", body)
	}
	if rec.Header().Get("X-BoxFleet-Config-SHA256") == "" {
		t.Fatal("disabled config missing hash header")
	}
}

func TestNodeConfigEndpointServesPublishedConfig(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishConfig(ctx, "azus", []byte(`{"published":true}`))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/node/config", nil)
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()

	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-BoxFleet-Config-Mode") != "published" {
		t.Fatalf("config mode = %q", rec.Header().Get("X-BoxFleet-Config-Mode"))
	}
	if rec.Header().Get("X-BoxFleet-Config-Version-ID") != published.ConfigVersion.ID {
		t.Fatalf("version id = %q", rec.Header().Get("X-BoxFleet-Config-Version-ID"))
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"published":true}` {
		t.Fatalf("body = %s", body)
	}
}

func TestNodeReportEndpointsPersistState(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishConfig(ctx, "azus", []byte(`{"inbounds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store})

	postNodeJSON(t, router, issued.Token, "/api/node/apply-result", db.ApplyResult{
		ConfigVersionID: published.ConfigVersion.ID,
		ConfigHash:      published.ConfigVersion.ConfigHash,
		Status:          "applied",
	})
	postNodeJSON(t, router, issued.Token, "/api/node/heartbeat", db.Heartbeat{
		AgentVersion:   "test-agent",
		SingBoxVersion: "test-sing-box",
	})
	postNodeJSON(t, router, issued.Token, "/api/node/traffic", db.TrafficReport{
		Sequence:    1,
		AgentBootID: "boot",
		Deltas: []db.TrafficDelta{{
			AuthName:      "vless-39090@alice",
			Direction:     "downlink",
			RawBytesDelta: 2048,
			CounterValue:  4096,
		}},
	})
	postNodeJSON(t, router, issued.Token, "/api/node/logs", db.LogEventReport{
		Events: []db.LogEventInput{{
			AuthName:   "vless-39090@alice",
			SourceIP:   "115.27.221.55",
			TargetHost: "speed.cloudflare.com",
			TargetPort: 443,
			Action:     "connect",
			RawMessage: "accepted tcp connection",
		}},
	})

	status, err := store.GetNodeConfigStatus(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if status.CurrentVersion.Int64 != published.ConfigVersion.Version || status.LastApplyStatus != "applied" {
		t.Fatalf("status = %#v", status)
	}
	traffic, err := store.SumTrafficByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 1 || traffic[0].RawBytes != 2048 {
		t.Fatalf("traffic = %#v", traffic)
	}
	logs, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].TargetHost != "speed.cloudflare.com" {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestNodeConfigEndpointRejectsBadToken(t *testing.T) {
	store := openAPITestDB(t)
	seedAPITestNode(t, context.Background(), store)

	req := httptest.NewRequest(http.MethodGet, "/api/node/config", nil)
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer bad")
	rec := httptest.NewRecorder()

	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAdminSubscriptionLifecycleAndDynamicProvider(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodGet, "/api/admin/users/alice/subscription", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("initial get status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var subscription adminUserSubscription
	if err := json.NewDecoder(rec.Body).Decode(&subscription); err != nil {
		t.Fatal(err)
	}
	if subscription.Active || subscription.URL != "" {
		t.Fatalf("initial subscription = %#v", subscription)
	}

	req = adminJSONRequest(t, http.MethodPost, "/api/admin/users/alice/subscription", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("issue status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&subscription); err != nil {
		t.Fatal(err)
	}
	if !subscription.Active || !strings.HasPrefix(subscription.URL, "http://example.com/sub/bfsub_") {
		t.Fatalf("issued subscription = %#v", subscription)
	}
	oldPath := strings.TrimPrefix(subscription.URL, "http://example.com")

	req = httptest.NewRequest(http.MethodGet, oldPath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("provider status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/yaml; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	if body := rec.Body.String(); !strings.Contains(body, "proxies:") ||
		!strings.Contains(body, "name: vless-39090") ||
		!strings.Contains(body, "type: vless") {
		t.Fatalf("provider body = %s", body)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("provider missing ETag")
	}
	req = httptest.NewRequest(http.MethodGet, oldPath, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("conditional status = %d, body = %q", rec.Code, rec.Body.String())
	}

	req = adminJSONRequest(t, http.MethodGet, "/api/admin/users/alice/subscription", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if err := json.NewDecoder(rec.Body).Decode(&subscription); err != nil {
		t.Fatal(err)
	}
	if subscription.LastUsedAt == "" {
		t.Fatalf("last_used_at was not exposed: %#v", subscription)
	}

	proxy, err := store.GetProxy(ctx, "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProxy(ctx, db.UpdateProxyParams{
		NodeName:          proxy.NodeName,
		Name:              proxy.Name,
		Listen:            proxy.Listen,
		ListenPort:        39091,
		Transport:         proxy.Transport,
		Enabled:           proxy.Enabled,
		TrafficMultiplier: proxy.TrafficMultiplier,
		SettingsJSON:      proxy.SettingsJSON,
		InboundRulesJSON:  proxy.InboundRulesJSON,
		OutboundRulesJSON: proxy.OutboundRulesJSON,
		RouteRulesJSON:    proxy.RouteRulesJSON,
	}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, oldPath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "port: 39091") {
		t.Fatalf("modified provider status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == etag {
		t.Fatal("provider ETag did not change after proxy edit")
	}

	if _, err := store.SetProxyAccessEnabled(ctx, "alice", "azus", "vless-39090", false); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, oldPath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "proxies: []\n" {
		t.Fatalf("updated provider status = %d, body = %q", rec.Code, rec.Body.String())
	}

	req = adminJSONRequest(t, http.MethodPost, "/api/admin/users/alice/subscription/rotate", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&subscription); err != nil {
		t.Fatal(err)
	}
	newPath := strings.TrimPrefix(subscription.URL, "http://example.com")
	if newPath == oldPath {
		t.Fatal("rotate kept the old URL")
	}
	req = httptest.NewRequest(http.MethodGet, oldPath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("old token status after rotate = %d", rec.Code)
	}

	req = adminJSONRequest(t, http.MethodDelete, "/api/admin/users/alice/subscription", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body = %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, newPath, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoked token status = %d", rec.Code)
	}
}

func TestAdminProxyProviderRequiresAuth(t *testing.T) {
	store := openAPITestDB(t)
	seedAPITestNode(t, context.Background(), store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users/alice/proxy-provider", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}

	req = adminJSONRequest(t, http.MethodGet, "/api/admin/users/alice/proxy-provider", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "proxies:") {
		t.Fatalf("authenticated status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminNetworkEventsPaginationAndFilters(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	if err := store.RecordLogEvents(ctx, db.LogEventReport{
		NodeName: "azus",
		Events: []db.LogEventInput{{
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "one.example",
			TargetPort:  443,
			Action:      "connect",
			WindowStart: "2026-05-16T03:20:00Z",
			WindowEnd:   "2026-05-16T03:20:05Z",
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "two.example",
			TargetPort:  443,
			Action:      "connect",
			WindowStart: "2026-05-16T03:25:00Z",
			WindowEnd:   "2026-05-16T03:25:05Z",
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "three.example",
			TargetPort:  443,
			Action:      "connect",
			WindowStart: "2026-05-16T03:30:00Z",
			WindowEnd:   "2026-05-16T03:30:05Z",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	req := adminJSONRequest(t, http.MethodGet, "/api/admin/network-events?limit=2&offset=1&node=azus&user=alice&start=2026-05-16T03:24:00Z&end=2026-05-16T03:31:00Z", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var page adminNetworkEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Limit != 2 || page.Offset != 1 || len(page.Events) != 1 {
		t.Fatalf("page = %#v", page)
	}
	if page.Events[0].NodeName != "azus" || page.Events[0].UserName != "alice" || page.Events[0].TargetHost != "two.example" {
		t.Fatalf("event = %#v", page.Events[0])
	}

	req = adminJSONRequest(t, http.MethodGet, "/api/admin/network-events?limit=10&action=connect&search=three.example", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Events) != 1 || page.Events[0].TargetHost != "three.example" {
		t.Fatalf("search page = %#v", page)
	}

	req = adminJSONRequest(t, http.MethodGet, "/api/admin/network-events?start=bad", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad time status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminSettingsEndpoint(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodGet, "/api/admin/settings", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var settings adminSettings
	if err := json.NewDecoder(rec.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.NetworkEventRetentionDays != db.DefaultNetworkEventRetentionDays {
		t.Fatalf("settings = %#v", settings)
	}

	req = adminJSONRequest(t, http.MethodPatch, "/api/admin/settings", map[string]any{
		"network_event_retention_days": 30,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch settings status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.NetworkEventRetentionDays != 30 {
		t.Fatalf("settings after patch = %#v", settings)
	}

	req = adminJSONRequest(t, http.MethodPatch, "/api/admin/settings", map[string]any{
		"network_event_retention_days": 0,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid settings status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminEndpointsRequireToken(t *testing.T) {
	store := openAPITestDB(t)
	seedAPITestNode(t, context.Background(), store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAdminPathTokenMovesAdminRoutes(t *testing.T) {
	store := openAPITestDB(t)
	seedAPITestNode(t, context.Background(), store)
	router := NewRouter(Options{DB: store, AdminToken: "secret", AdminPathToken: "panel-secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed admin API status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed admin UI status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/panel-secret/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed admin API status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/panel-secret/admin", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed admin UI status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/panel-secret/admin/assets/") {
		t.Fatalf("admin asset paths were not rewritten: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/panel-secret/admin/network-events", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed admin deep link status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/panel-secret/admin/assets/") {
		t.Fatalf("admin deep link asset paths were not rewritten: %s", rec.Body.String())
	}
}

func TestAdminNodeBootstrapCreatesNodeAndToken(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodPost, "/api/admin/nodes/bootstrap", map[string]string{
		"name": "edge-a",
	})
	req.Host = "boxfleet.example"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Node             adminNode `json:"node"`
		BootstrapString  string    `json:"bootstrap_string"`
		InstallScriptURL string    `json:"install_script_url"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Node.Name != "edge-a" || payload.BootstrapString == "" {
		t.Fatalf("payload = %#v", payload)
	}
	bootstrapConfig, err := model.DecodeBootstrap(payload.BootstrapString)
	if err != nil {
		t.Fatal(err)
	}
	if bootstrapConfig.NodeName != "edge-a" ||
		bootstrapConfig.ServerURL != "http://boxfleet.example" ||
		bootstrapConfig.SingBoxURL != "" ||
		bootstrapConfig.Token == "" {
		t.Fatalf("bootstrap config = %#v", bootstrapConfig)
	}
	if payload.InstallScriptURL != "http://boxfleet.example/install.sh" {
		t.Fatalf("install script url = %q", payload.InstallScriptURL)
	}
	ok, err := store.VerifyNodeToken(context.Background(), "edge-a", bootstrapConfig.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("issued bootstrap token did not verify")
	}
}

func TestAdminNodeReenroll(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	ctx := context.Background()

	decodeBootstrap := func(t *testing.T, rec *httptest.ResponseRecorder) (adminNode, model.BootstrapConfig) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Node            adminNode `json:"node"`
			BootstrapString string    `json:"bootstrap_string"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		cfg, err := model.DecodeBootstrap(payload.BootstrapString)
		if err != nil {
			t.Fatal(err)
		}
		return payload.Node, cfg
	}
	reenroll := func() *httptest.ResponseRecorder {
		req := adminJSONRequest(t, http.MethodPost, "/api/admin/nodes/edge-r/reenroll", nil)
		req.Host = "boxfleet.example"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// Enroll a pending node and capture its first token.
	bootReq := adminJSONRequest(t, http.MethodPost, "/api/admin/nodes/bootstrap", map[string]string{"name": "edge-r"})
	bootReq.Host = "boxfleet.example"
	bootRec := httptest.NewRecorder()
	router.ServeHTTP(bootRec, bootReq)
	_, firstCfg := decodeBootstrap(t, bootRec)

	// Pending re-enroll rotates the token: the new one verifies, the old is revoked.
	node, secondCfg := decodeBootstrap(t, reenroll())
	if node.Status != "pending" {
		t.Fatalf("status after pending re-enroll = %q, want pending", node.Status)
	}
	if ok, _ := store.VerifyNodeToken(ctx, "edge-r", secondCfg.Token); !ok {
		t.Fatal("re-enrolled token did not verify")
	}
	if ok, _ := store.VerifyNodeToken(ctx, "edge-r", firstCfg.Token); ok {
		t.Fatal("old token should be revoked after re-enroll")
	}

	// An active node refuses re-enroll (keeps its working token untouched).
	if err := store.SetNodeStatus(ctx, "edge-r", "active"); err != nil {
		t.Fatal(err)
	}
	if rec := reenroll(); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("active re-enroll status = %d, want 422", rec.Code)
	}
	if ok, _ := store.VerifyNodeToken(ctx, "edge-r", secondCfg.Token); !ok {
		t.Fatal("active node's token should survive a rejected re-enroll")
	}

	// Decommission revokes the token; re-enroll restores the node to pending.
	delReq := adminJSONRequest(t, http.MethodDelete, "/api/admin/nodes/edge-r", nil)
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("decommission status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
	restored, thirdCfg := decodeBootstrap(t, reenroll())
	if restored.Status != "pending" {
		t.Fatalf("status after restore re-enroll = %q, want pending", restored.Status)
	}
	if ok, _ := store.VerifyNodeToken(ctx, "edge-r", thirdCfg.Token); !ok {
		t.Fatal("restored token did not verify")
	}
}

func TestAdminNodePatchMultiHost(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	createReq := adminJSONRequest(t, http.MethodPost, "/api/admin/nodes", map[string]string{
		"name":        "edge",
		"public_host": "1.2.3.4",
	})
	createRec := httptest.NewRecorder()
	router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	patchReq := adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/edge", map[string]any{
		"hosts": []map[string]any{
			{"host": "example.net", "selected": true},
			{"host": "203.0.113.5", "tag": "backup", "selected": false},
		},
	})
	patchRec := httptest.NewRecorder()
	router.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}
	var node adminNode
	if err := json.NewDecoder(patchRec.Body).Decode(&node); err != nil {
		t.Fatal(err)
	}
	// public_host mirrors the first host; the full list round-trips.
	if node.PublicHost != "example.net" {
		t.Fatalf("public_host = %q, want example.net", node.PublicHost)
	}
	if len(node.Hosts) != 2 || node.Hosts[0].Host != "example.net" || !node.Hosts[0].Selected {
		t.Fatalf("hosts = %#v", node.Hosts)
	}
	if node.Hosts[1].Host != "203.0.113.5" || node.Hosts[1].Tag != "backup" || node.Hosts[1].Selected {
		t.Fatalf("second host = %#v", node.Hosts[1])
	}

	// The list endpoint must carry hosts too, or the edit dialog falls back to the
	// single public_host and a later save silently drops the extra hosts.
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, adminJSONRequest(t, http.MethodGet, "/api/admin/nodes", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	var listed []adminNode
	if err := json.NewDecoder(listRec.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	var fromList *adminNode
	for i := range listed {
		if listed[i].Name == "edge" {
			fromList = &listed[i]
		}
	}
	if fromList == nil || len(fromList.Hosts) != 2 {
		t.Fatalf("list response dropped hosts: %#v", fromList)
	}

	// An explicit empty host replacement is rejected, not silently collapsed back
	// to the primary host.
	emptyRec := httptest.NewRecorder()
	router.ServeHTTP(emptyRec, adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/edge", map[string]any{
		"hosts": []map[string]any{},
	}))
	if emptyRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty-hosts patch status = %d, want 422", emptyRec.Code)
	}
	// And the prior host list is unchanged.
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, adminJSONRequest(t, http.MethodGet, "/api/admin/nodes/edge", nil))
	var after adminNode
	if err := json.NewDecoder(getRec.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	if len(after.Hosts) != 2 {
		t.Fatalf("hosts changed after rejected empty patch: %#v", after.Hosts)
	}
}

func TestInstallScriptEndpoint(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{
		DB:             store,
		Version:        "v0.1.0",
		Repo:           "ha0xin/BoxFleet",
		SingBoxVersion: "v1.13.13",
	})

	req := httptest.NewRequest(http.MethodGet, "/install.sh", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`REPO="${BOXFLEET_REPO:-ha0xin/BoxFleet}"`,
		`BOXFLEET_VERSION="${BOXFLEET_VERSION_OVERRIDE:-v0.1.0}"`,
		`SING_BOX_VERSION="${BOXFLEET_SING_BOX_VERSION:-v1.13.13}"`,
		`agent_asset="boxfleet-agent-${BOXFLEET_VERSION}-linux-amd64"`,
		`sing_box_asset="sing-box-${SING_BOX_VERSION}-linux-amd64"`,
		"boxfleet-agent\" bootstrap \"$BOOTSTRAP_STRING\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install script missing %q:\n%s", want, body)
		}
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/x-shellscript") {
		t.Fatalf("content type = %q", got)
	}
}

func TestAdminEndpointsRejectEmptyTokenByDefault(t *testing.T) {
	store := openAPITestDB(t)
	seedAPITestNode(t, context.Background(), store)
	router := NewRouter(Options{DB: store})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAdminOverviewEndpoint(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var overview struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
		Users []struct {
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Nodes) != 1 || overview.Nodes[0].Name != "azus" {
		t.Fatalf("nodes = %#v", overview.Nodes)
	}
	if len(overview.Users) != 1 || overview.Users[0].Name != "alice" {
		t.Fatalf("users = %#v", overview.Users)
	}
}

func TestAdminNodeAndProxyManagement(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodPost, "/api/admin/nodes", map[string]any{
		"name":        "node-a",
		"public_host": "203.0.113.10",
		"status":      "active",
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create node status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/node-a", map[string]any{
		"public_host":  "203.0.113.11",
		"api_base_url": "http://node-a.local",
		"status":       "degraded",
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update node status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = adminJSONRequest(t, http.MethodPost, "/api/admin/nodes/node-a/proxies", map[string]any{
		"name":        "vless-39090",
		"protocol":    "vless_reality",
		"listen":      "0.0.0.0",
		"listen_port": 39090,
		"transport":   "tcp",
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create proxy status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/node-a/proxies/vless-39090", map[string]any{
		"listen":             "::",
		"listen_port":        39091,
		"transport":          "tcp",
		"enabled":            false,
		"traffic_multiplier": 1.5,
		"settings_json":      `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`,
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update proxy status = %d, body = %s", rec.Code, rec.Body.String())
	}
	proxy, err := store.GetProxy(context.Background(), "node-a", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if proxy.ListenPort != 39091 || proxy.Enabled || proxy.TrafficMultiplier != 1.5 {
		t.Fatalf("proxy = %#v", proxy)
	}
}

func TestAdminRenamesNodeAndProxyWithoutBreakingAgentIdentity(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	accessBefore, err := store.GetProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	nodeReq := adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/azus", map[string]any{
		"name": "edge-primary",
	})
	nodeRec := httptest.NewRecorder()
	router.ServeHTTP(nodeRec, nodeReq)
	if nodeRec.Code != http.StatusOK {
		t.Fatalf("rename node status = %d, body = %s", nodeRec.Code, nodeRec.Body.String())
	}
	var renamedNode adminNode
	if err := json.NewDecoder(nodeRec.Body).Decode(&renamedNode); err != nil {
		t.Fatal(err)
	}
	if renamedNode.Name != "edge-primary" {
		t.Fatalf("renamed node = %#v", renamedNode)
	}

	proxyReq := adminJSONRequest(t, http.MethodPatch, "/api/admin/nodes/azus/proxies/vless-39090", map[string]any{
		"name":     "home",
		"short_id": " A1B2 ",
	})
	proxyRec := httptest.NewRecorder()
	router.ServeHTTP(proxyRec, proxyReq)
	if proxyRec.Code != http.StatusOK {
		t.Fatalf("rename proxy status = %d, body = %s", proxyRec.Code, proxyRec.Body.String())
	}
	var renamedProxy adminProxy
	if err := json.NewDecoder(proxyRec.Body).Decode(&renamedProxy); err != nil {
		t.Fatal(err)
	}
	if renamedProxy.NodeName != "edge-primary" || renamedProxy.Name != "home" || renamedProxy.ShortID != "a1b2" {
		t.Fatalf("renamed proxy = %#v", renamedProxy)
	}
	accessAfter, err := store.GetProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if accessAfter.AuthName != accessBefore.AuthName || accessAfter.CredentialJSON != accessBefore.CredentialJSON {
		t.Fatalf("access identity changed: before=%#v after=%#v", accessBefore, accessAfter)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/node/config", nil)
	configReq.Header.Set("X-BoxFleet-Node", "azus")
	configReq.Header.Set("Authorization", "Bearer "+issued.Token)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("old agent identity status = %d, body = %s", configRec.Code, configRec.Body.String())
	}
	if got := configRec.Header().Get(model.CanonicalNodeNameHeader); got != "edge-primary" {
		t.Fatalf("canonical node header = %q", got)
	}
}

func TestAdminNodeStatusTogglePreservesAPIURL(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	serve := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		req := adminJSONRequest(t, method, path, body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodPost, "/api/admin/nodes", map[string]any{
		"name":        "node-a",
		"public_host": "203.0.113.10",
		"status":      "active",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create node status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodPatch, "/api/admin/nodes/node-a", map[string]any{
		"name":         "node-a",
		"api_base_url": "http://node-a.local",
		"status":       "active",
	}); rec.Code != http.StatusOK {
		t.Fatalf("set api url status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Status-only PATCH (no api_base_url) must not wipe the configured URL.
	if rec := serve(http.MethodPatch, "/api/admin/nodes/node-a", map[string]any{
		"name":   "node-a",
		"status": "disabled",
	}); rec.Code != http.StatusOK {
		t.Fatalf("toggle status = %d, body = %s", rec.Code, rec.Body.String())
	}
	node, err := store.GetNode(context.Background(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.APIBaseURL != "http://node-a.local" || node.Status != "disabled" {
		t.Fatalf("node = %#v", node)
	}

	// An explicit empty api_base_url (edit dialog clearing the field) clears it,
	// unlike an omitted field which is preserved above.
	if rec := serve(http.MethodPatch, "/api/admin/nodes/node-a", map[string]any{
		"name":         "node-a",
		"api_base_url": "",
		"status":       "active",
	}); rec.Code != http.StatusOK {
		t.Fatalf("clear api url status = %d, body = %s", rec.Code, rec.Body.String())
	}
	node, err = store.GetNode(context.Background(), "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.APIBaseURL != "" {
		t.Fatalf("api_base_url = %q, want cleared", node.APIBaseURL)
	}
}

func TestAdminUserPatchIsAtomic(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	serve := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		req := adminJSONRequest(t, method, path, body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodPost, "/api/admin/users", map[string]any{
		"name": "alice", "display_name": "Alice",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Valid display_name + invalid status: the whole patch must roll back, so the
	// display_name change does not persist despite being processed first.
	if rec := serve(http.MethodPatch, "/api/admin/users/alice", map[string]any{
		"display_name": "Changed",
		"status":       "bogus",
	}); rec.Code == http.StatusOK {
		t.Fatalf("patch with invalid status unexpectedly succeeded: %s", rec.Body.String())
	}
	user, err := store.GetProxyUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Alice" {
		t.Fatalf("display_name = %q, want unchanged (atomic rollback)", user.DisplayName)
	}
}

func TestAdminProxyEditPreservesRealityKeys(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	serve := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		req := adminJSONRequest(t, method, path, body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodPost, "/api/admin/nodes", map[string]any{
		"name": "node-a", "public_host": "203.0.113.10", "status": "active",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create node status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodPost, "/api/admin/nodes/node-a/proxies", map[string]any{
		"name": "vless-1", "protocol": "vless_reality", "listen": "0.0.0.0", "listen_port": 443, "transport": "tcp",
	}); rec.Code != http.StatusOK {
		t.Fatalf("create proxy status = %d, body = %s", rec.Code, rec.Body.String())
	}

	before, err := store.GetProxy(context.Background(), "node-a", "vless-1")
	if err != nil {
		t.Fatal(err)
	}
	var seed map[string]any
	if err := json.Unmarshal([]byte(before.SettingsJSON), &seed); err != nil {
		t.Fatalf("seed settings: %v (%s)", err, before.SettingsJSON)
	}

	// Mirror the edit dialog: re-send the existing settings with an overridden
	// SNI. The server must keep the original Reality key pair / short_id.
	seed["server_name"] = "www.apple.com"
	merged, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if rec := serve(http.MethodPatch, "/api/admin/nodes/node-a/proxies/vless-1", map[string]any{
		"listen_port":   8443,
		"settings_json": string(merged),
	}); rec.Code != http.StatusOK {
		t.Fatalf("patch proxy status = %d, body = %s", rec.Code, rec.Body.String())
	}

	after, err := store.GetProxy(context.Background(), "node-a", "vless-1")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(after.SettingsJSON), &got); err != nil {
		t.Fatalf("after settings: %v (%s)", err, after.SettingsJSON)
	}
	if got["reality_private_key"] != seed["reality_private_key"] || got["reality_public_key"] != seed["reality_public_key"] {
		t.Fatalf("reality keys rotated: before=%v/%v after=%v/%v",
			seed["reality_private_key"], seed["reality_public_key"], got["reality_private_key"], got["reality_public_key"])
	}
	if got["server_name"] != "www.apple.com" {
		t.Fatalf("server_name = %v", got["server_name"])
	}
}

func TestConfigChangesIncludesPendingNodes(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	// Bootstrap leaves a freshly enrolled node pending; it must still appear in
	// the change set so Apply does not silently skip it.
	if err := store.SetNodeStatus(ctx, "azus", "pending"); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodGet, "/api/admin/config/changes", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Changed []struct {
			Node string `json:"node"`
		} `json:"changed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range resp.Changed {
		if c.Node == "azus" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending node azus missing from changed set: %+v", resp.Changed)
	}
}

func TestAdminUserManagement(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	serve := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		req := adminJSONRequest(t, method, path, body)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodPost, "/api/admin/users", map[string]any{
		"name":               "alice",
		"display_name":       "Alice",
		"global_quota_bytes": 1000,
	}); rec.Code != http.StatusOK {
		t.Fatalf("create user status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// PATCH dispatches each non-nil field to its setter and leaves omitted ones alone.
	if rec := serve(http.MethodPatch, "/api/admin/users/alice", map[string]any{
		"display_name":       "Alice Z",
		"global_quota_bytes": 2048,
		"status":             "disabled",
	}); rec.Code != http.StatusOK {
		t.Fatalf("patch user status = %d, body = %s", rec.Code, rec.Body.String())
	}

	user, err := store.GetProxyUser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Alice Z" || user.GlobalQuotaBytes != 2048 || user.Status != "disabled" {
		t.Fatalf("user = %#v", user)
	}
}

func TestAdminNodeAndProxyPagination(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.CreateNode(ctx, "beta", "198.51.100.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:     "beta",
		Name:         "vless-443",
		Protocol:     db.ProtocolVLESSReality,
		Listen:       "::",
		ListenPort:   443,
		Transport:    db.TransportTCP,
		Enabled:      true,
		SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"01234567","handshake_server":"www.amazon.com","handshake_port":443}`,
	}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/nodes?limit=1&offset=1&sort=name", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes page status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var nodesPage struct {
		Nodes  []adminNode `json:"nodes"`
		Total  int64       `json:"total"`
		Limit  int64       `json:"limit"`
		Offset int64       `json:"offset"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&nodesPage); err != nil {
		t.Fatal(err)
	}
	if nodesPage.Total != 2 || nodesPage.Limit != 1 || nodesPage.Offset != 1 || len(nodesPage.Nodes) != 1 || nodesPage.Nodes[0].Name != "beta" {
		t.Fatalf("nodes page = %#v", nodesPage)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/proxies?limit=1&offset=1&sort=name", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxies page status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var proxiesPage struct {
		Proxies []adminProxy `json:"proxies"`
		Total   int64        `json:"total"`
		Limit   int64        `json:"limit"`
		Offset  int64        `json:"offset"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&proxiesPage); err != nil {
		t.Fatal(err)
	}
	if proxiesPage.Total != 2 || proxiesPage.Limit != 1 || proxiesPage.Offset != 1 || len(proxiesPage.Proxies) != 1 || proxiesPage.Proxies[0].Name != "vless-443" {
		t.Fatalf("proxies page = %#v", proxiesPage)
	}
}

func TestAdminDeleteResourceEndpointsDisableResources(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	req := adminJSONRequest(t, http.MethodDelete, "/api/admin/users/alice/proxies/azus/vless-39090", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete access status = %d, body = %s", rec.Code, rec.Body.String())
	}
	access, err := store.GetProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if access.Enabled {
		t.Fatal("access was not disabled")
	}

	req = adminJSONRequest(t, http.MethodPost, "/api/admin/users/alice/proxies", map[string]string{
		"node_name":  "azus",
		"proxy_name": "vless-39090",
	})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reissue access status = %d, body = %s", rec.Code, rec.Body.String())
	}
	access, err = store.GetProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if !access.Enabled {
		t.Fatal("reissued access was not enabled")
	}

	req = adminJSONRequest(t, http.MethodDelete, "/api/admin/nodes/azus/proxies/vless-39090", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete proxy status = %d, body = %s", rec.Code, rec.Body.String())
	}
	proxy, err := store.GetProxy(ctx, "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Enabled {
		t.Fatal("proxy was not disabled")
	}

	req = adminJSONRequest(t, http.MethodDelete, "/api/admin/users/alice", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete user status = %d, body = %s", rec.Code, rec.Body.String())
	}
	user, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.Status != "disabled" {
		t.Fatalf("user status = %q", user.Status)
	}

	req = adminJSONRequest(t, http.MethodDelete, "/api/admin/nodes/azus", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete node status = %d, body = %s", rec.Code, rec.Body.String())
	}
	node, err := store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != "disabled" {
		t.Fatalf("node status = %q", node.Status)
	}
	ok, err := store.VerifyNodeToken(ctx, "azus", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("node token still verifies after node delete")
	}
}

func TestNodeSystemLogsEndpointAndAdminQuery(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	postNodeJSON(t, router, issued.Token, "/api/node/system-logs", db.SystemLogReport{
		Entries: []db.SystemLogInput{{
			Service:    "boxfleet-agent.service",
			Level:      "info",
			RawMessage: "agent started",
			ObservedAt: "2026-05-16T00:00:00Z",
			Cursor:     "cursor-system-1",
		}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/system-logs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"logs":[]`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAdminUIRouteServesEmbeddedIndex(t *testing.T) {
	store := openAPITestDB(t)
	router := NewRouter(Options{DB: store})

	for _, path := range []string{"/admin", "/admin/network-events"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "BoxFleet Admin") {
			t.Fatalf("%s unexpected body: %s", path, rec.Body.String())
		}
	}
}

func adminJSONRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func postNodeJSON(t *testing.T, handler http.Handler, token string, path string, body any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
}

func openAPITestDB(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.OpenSQLite(filepath.Join(t.TempDir(), "boxfleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func seedAPITestNode(t *testing.T, ctx context.Context, store *db.DB) {
	t.Helper()
	if _, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:   "azus",
		Name:       "vless-39090",
		Protocol:   db.ProtocolVLESSReality,
		Listen:     "0.0.0.0",
		ListenPort: 39090,
		Transport:  db.TransportTCP,
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
	if _, err := store.BindUserToNode(ctx, "alice", "azus"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityAccess(ctx, db.IssueAccessParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
}
