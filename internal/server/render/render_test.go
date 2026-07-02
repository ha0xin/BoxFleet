package render

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
	"go.yaml.in/yaml/v3"
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

func TestRenderMihomoProxyProvider(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	raw, err := RenderMihomoProxyProvider(ctx, store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private-key") || strings.Contains(string(raw), "reality_private_key") {
		t.Fatalf("provider leaked Reality private key:\n%s", raw)
	}

	var provider struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider.Proxies) != 1 {
		t.Fatalf("proxies = %d, want 1:\n%s", len(provider.Proxies), raw)
	}
	proxy := provider.Proxies[0]
	if proxy["name"] != "azus / vless-39090" ||
		proxy["type"] != "vless" ||
		proxy["server"] != "203.0.113.10" ||
		proxy["port"] != 39090 ||
		proxy["uuid"] == "" ||
		proxy["flow"] != db.VLESSRealityFlowVision ||
		proxy["network"] != "tcp" ||
		proxy["tls"] != true ||
		proxy["servername"] != "www.amazon.com" ||
		proxy["client-fingerprint"] != "chrome" ||
		proxy["packet-encoding"] != "xudp" ||
		proxy["encryption"] != "" {
		t.Fatalf("unexpected proxy: %#v", proxy)
	}
	reality, ok := proxy["reality-opts"].(map[string]any)
	if !ok || reality["public-key"] != "public-key" || reality["short-id"] != "01234567" {
		t.Fatalf("unexpected reality options: %#v", proxy["reality-opts"])
	}
}

func TestRenderMihomoProxyProviderMultiHostAndDisabled(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	if _, err := store.UpdateNode(ctx, db.UpdateNodeParams{
		Name:   "azus",
		Status: "active",
		Hosts: []db.NodeHost{
			{Host: "azus.example.net", Selected: true},
			{Host: "2606:4700::1", Selected: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := RenderMihomoProxyProvider(ctx, store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var provider struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &provider); err != nil {
		t.Fatal(err)
	}
	if len(provider.Proxies) != 2 {
		t.Fatalf("proxies = %d, want 2:\n%s", len(provider.Proxies), raw)
	}
	if provider.Proxies[0]["name"] != "azus / vless-39090 @ azus.example.net" ||
		provider.Proxies[1]["name"] != "azus / vless-39090 @ 2606:4700::1" {
		t.Fatalf("unexpected names: %q, %q", provider.Proxies[0]["name"], provider.Proxies[1]["name"])
	}

	if _, err := store.SetProxyAccessEnabled(ctx, "alice", "azus", "vless-39090", false); err != nil {
		t.Fatal(err)
	}
	raw, err = RenderMihomoProxyProvider(ctx, store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "proxies: []\n" {
		t.Fatalf("disabled access provider = %q, want empty proxies", raw)
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
