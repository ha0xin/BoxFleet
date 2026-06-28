package render

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestRenderVLESSRealityServerAndClientConfigs(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	serverConfig, err := RenderNodeConfig(ctx, store, "azus")
	if err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	if err := json.Unmarshal(serverConfig, &server); err != nil {
		t.Fatal(err)
	}
	inbounds := server["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("server inbounds = %d", len(inbounds))
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "vless" || inbound["tag"] != "vless-39090" {
		t.Fatalf("unexpected inbound: %#v", inbound)
	}
	users := inbound["users"].([]any)
	user := users[0].(map[string]any)
	if user["name"] != "vless-39090@alice" || user["flow"] != db.VLESSRealityFlowVision {
		t.Fatalf("unexpected vless user: %#v", user)
	}
	experimental := server["experimental"].(map[string]any)
	v2ray := experimental["v2ray_api"].(map[string]any)
	stats := v2ray["stats"].(map[string]any)
	statsUsers := stats["users"].([]any)
	if statsUsers[0] != "vless-39090@alice" {
		t.Fatalf("unexpected stats users: %#v", statsUsers)
	}

	clientConfig, err := RenderClientConfig(ctx, store, ClientConfigParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	})
	if err != nil {
		t.Fatal(err)
	}
	var client map[string]any
	if err := json.Unmarshal(clientConfig, &client); err != nil {
		t.Fatal(err)
	}
	outbounds := client["outbounds"].([]any)
	proxy := outbounds[0].(map[string]any)
	if proxy["server"] != "203.0.113.10" || proxy["server_port"].(float64) != 39090 {
		t.Fatalf("unexpected proxy outbound: %#v", proxy)
	}
	tls := proxy["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)
	if tls["server_name"] != "www.amazon.com" || reality["public_key"] != "public-key" || reality["short_id"] != "01234567" {
		t.Fatalf("unexpected outbound tls: %#v", tls)
	}
	serverTLS := inbound["tls"].(map[string]any)
	serverReality := serverTLS["reality"].(map[string]any)
	if serverReality["short_id"] != "01234567" {
		t.Fatalf("unexpected server reality: %#v", serverReality)
	}
}

func TestRenderNodeInfo(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	raw, err := RenderNodeInfo(ctx, store, "alice", "azus")
	if err != nil {
		t.Fatal(err)
	}
	var info NodeInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if info.User != "alice" || info.Node != "azus" || len(info.Proxies) != 1 {
		t.Fatalf("unexpected node info: %#v", info)
	}
	if info.Proxies[0].Flow != db.VLESSRealityFlowVision {
		t.Fatalf("flow = %q", info.Proxies[0].Flow)
	}
}

func TestRenderNodeInfoMultiHost(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	// Two selected hosts and one deselected; expect a profile per selected host.
	if _, err := store.UpdateNode(ctx, db.UpdateNodeParams{
		Name:   "azus",
		Status: "active",
		Hosts: []db.NodeHost{
			{Host: "azus.example.net", Selected: true},
			{Host: "203.0.113.10", Selected: true},
			{Host: "2606:4700::1", Selected: false},
		},
	}); err != nil {
		t.Fatal(err)
	}

	info, err := NodeInfoForUser(ctx, store, "alice", "azus")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Proxies) != 2 {
		t.Fatalf("want 2 per-host profiles, got %d: %#v", len(info.Proxies), info.Proxies)
	}
	servers := map[string]bool{}
	for _, p := range info.Proxies {
		servers[p.Server] = true
		if !strings.Contains(p.Name, "@") {
			t.Fatalf("multi-host profile name should be disambiguated, got %q", p.Name)
		}
	}
	if !servers["azus.example.net"] || !servers["203.0.113.10"] || servers["2606:4700::1"] {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func openRenderTestDB(t *testing.T) *db.DB {
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

func seedVLESSRealityFixture(t *testing.T, ctx context.Context, store *db.DB) {
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
