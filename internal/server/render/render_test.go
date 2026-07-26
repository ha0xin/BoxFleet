package render

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/mihomo"
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
	if info.Proxies[0].Name != "vless-39090" ||
		info.Proxies[0].ProxyName != "vless-39090" ||
		info.Proxies[0].HostTag != "" {
		t.Fatalf("unexpected profile identity: %#v", info.Proxies[0])
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
			{Host: "203.0.113.10", Tag: "ipv4", Selected: true},
			{Host: "2606:4700::1", Tag: "ipv6", Selected: false},
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
	names := map[string]bool{}
	servers := map[string]bool{}
	for _, p := range info.Proxies {
		servers[p.Server] = true
		names[p.Name] = true
	}
	if !servers["azus.example.net"] || !servers["203.0.113.10"] || servers["2606:4700::1"] {
		t.Fatalf("unexpected servers: %#v", servers)
	}
	if !names["vless-39090"] || !names["vless-39090-ipv4"] {
		t.Fatalf("unexpected names: %#v", names)
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
	if proxy["name"] != "vless-39090" ||
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

func TestRenderShadowsocks2022DialerPath(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	if _, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	for _, node := range []struct{ name, host string }{{"la-entry", "198.51.100.10"}, {"la-home", "203.0.113.20"}} {
		if _, err := store.CreateNode(ctx, node.name, node.host, ""); err != nil {
			t.Fatal(err)
		}
	}
	dialerProxy, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName: "la-entry", Name: "optimized", Protocol: db.ProtocolVLESSReality,
		ListenPort: 443, Transport: db.TransportTCP, Enabled: true,
		SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private-key","reality_public_key":"public-key","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	exitProxy, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName: "la-home", Name: "residential", Protocol: db.ProtocolShadowsocks2022,
		ListenPort: 8443, Transport: db.TransportTCPUDP, Enabled: true,
		SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	entryNode, _ := store.GetNode(ctx, "la-entry")
	exitNode, _ := store.GetNode(ctx, "la-home")
	dialerEndpoint, err := store.EnsureEndpoint(ctx, dialerProxy.ID, entryNode.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	exitEndpoint, err := store.EnsureEndpoint(ctx, exitProxy.ID, exitNode.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	dialerPath, err := store.CreatePath(ctx, db.CreatePathParams{
		Name: "optimized-entry", DisplayName: "LA optimized", EndpointID: dialerEndpoint.ID,
		Enabled: true, Visibility: db.PathVisibilityDependency,
	})
	if err != nil {
		t.Fatal(err)
	}
	exitPath, err := store.CreatePath(ctx, db.CreatePathParams{
		Name: "home-exit", DisplayName: "LA residential via optimized", EndpointID: exitEndpoint.ID,
		DialerPathID: dialerPath.ID, Enabled: true, Visibility: db.PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", exitPath.ID); err != nil {
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
		t.Fatalf("proxies = %d, want dependency and root:\n%s", len(provider.Proxies), raw)
	}
	if provider.Proxies[0]["name"] != "LA optimized" || provider.Proxies[0]["dialer-proxy"] != nil {
		t.Fatalf("unexpected dialer: %#v", provider.Proxies[0])
	}
	exit := provider.Proxies[1]
	if exit["type"] != "ss" || exit["cipher"] != db.DefaultShadowsocks2022Method || exit["dialer-proxy"] != "LA optimized" {
		t.Fatalf("unexpected exit: %#v", exit)
	}
	if password, _ := exit["password"].(string); strings.Count(password, ":") != 1 {
		t.Fatalf("unexpected SS-2022 combined password: %q", password)
	}

	serverRaw, err := RenderNodeConfig(ctx, store, "la-home")
	if err != nil {
		t.Fatal(err)
	}
	var server map[string]any
	if err := json.Unmarshal(serverRaw, &server); err != nil {
		t.Fatal(err)
	}
	inbound := server["inbounds"].([]any)[0].(map[string]any)
	if inbound["type"] != "shadowsocks" || inbound["method"] != db.DefaultShadowsocks2022Method {
		t.Fatalf("unexpected Shadowsocks inbound: %#v", inbound)
	}
	if len(inbound["users"].([]any)) != 1 || inbound["password"] == "" {
		t.Fatalf("missing Shadowsocks multi-user keys: %#v", inbound)
	}
	if _, err := store.RevokePathAccess(ctx, "alice", exitPath.ID); err != nil {
		t.Fatal(err)
	}
	for _, nodeName := range []string{"la-entry", "la-home"} {
		raw, err := RenderNodeConfig(ctx, store, nodeName)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"inbounds"`) {
			t.Fatalf("revoked Path left credential inbound on %s: %s", nodeName, raw)
		}
	}
}

func TestRenderShadowsocks2022OnlyNodeClientProfiles(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	proxy := seedShadowsocks2022Fixture(t, ctx, store, "alice", "bob")

	var settings db.Shadowsocks2022Settings
	if err := json.Unmarshal([]byte(proxy.SettingsJSON), &settings); err != nil {
		t.Fatal(err)
	}
	aliceKey := shadowsocks2022UserKey(t, ctx, store, "alice")
	bobKey := shadowsocks2022UserKey(t, ctx, store, "bob")

	rawInfo, err := RenderNodeInfo(ctx, store, "alice", "ss-node")
	if err != nil {
		t.Fatal(err)
	}
	var info NodeInfo
	if err := json.Unmarshal(rawInfo, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Proxies) != 1 {
		t.Fatalf("proxies = %d, want the Shadowsocks 2022 profile: %s", len(info.Proxies), rawInfo)
	}
	profile := info.Proxies[0]
	if profile.Name != "ss-8443" || profile.ProxyName != "ss-8443" ||
		profile.Type != db.ProtocolShadowsocks2022 ||
		profile.Server != "198.51.100.7" || profile.ServerPort != 8443 {
		t.Fatalf("unexpected profile identity: %#v", profile)
	}
	if profile.Cipher != db.DefaultShadowsocks2022Method {
		t.Fatalf("cipher = %q", profile.Cipher)
	}
	if want := settings.ServerPassword + ":" + aliceKey; profile.Password != want {
		t.Fatalf("password = %q, want iPSK:uPSK %q", profile.Password, want)
	}

	rawClient, err := RenderClientConfig(ctx, store, ClientConfigParams{
		UserName:  "alice",
		NodeName:  "ss-node",
		ProxyName: "ss-8443",
	})
	if err != nil {
		t.Fatal(err)
	}
	var client map[string]any
	if err := json.Unmarshal(rawClient, &client); err != nil {
		t.Fatal(err)
	}
	outbound := client["outbounds"].([]any)[0].(map[string]any)
	if outbound["type"] != "shadowsocks" || outbound["tag"] != "proxy" ||
		outbound["server"] != "198.51.100.7" || outbound["server_port"].(float64) != 8443 ||
		outbound["method"] != db.DefaultShadowsocks2022Method ||
		outbound["password"] != settings.ServerPassword+":"+aliceKey {
		t.Fatalf("unexpected Shadowsocks outbound: %#v", outbound)
	}

	// The client legitimately receives its own iPSK:uPSK pair and nothing else:
	// no other user's uPSK and no server-only material.
	for _, rendered := range [][]byte{rawInfo, rawClient} {
		if strings.Contains(string(rendered), bobKey) {
			t.Fatalf("client output leaked another user's uPSK:\n%s", rendered)
		}
		if strings.Contains(string(rendered), "server_password") {
			t.Fatalf("client output leaked raw server settings:\n%s", rendered)
		}
	}
}

func TestRenderVLESSClientOutputExcludesRealityPrivateKey(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	rawInfo, err := RenderNodeInfo(ctx, store, "alice", "azus")
	if err != nil {
		t.Fatal(err)
	}
	rawClient, err := RenderClientConfig(ctx, store, ClientConfigParams{
		UserName: "alice", NodeName: "azus", ProxyName: "vless-39090",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range [][]byte{rawInfo, rawClient} {
		if strings.Contains(string(rendered), "private-key") ||
			strings.Contains(string(rendered), "reality_private_key") {
			t.Fatalf("client output leaked Reality private key:\n%s", rendered)
		}
	}
}

func TestRenderMihomoProfileUsesInlineProxies(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	result, err := RenderMihomoProfile(ctx, store, "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.YAML), "proxy-providers:") {
		t.Fatalf("full profile unexpectedly uses proxy-providers:\n%s", result.YAML)
	}

	var profile map[string]any
	if err := yaml.Unmarshal(result.YAML, &profile); err != nil {
		t.Fatal(err)
	}
	proxies, ok := profile["proxies"].([]any)
	if !ok || len(proxies) != 1 {
		t.Fatalf("inline proxies = %#v, want one", profile["proxies"])
	}
	if profile["mode"] != "rule" || profile["mixed-port"] != 7890 {
		t.Fatalf("missing basic settings: %#v", profile)
	}
	groups, ok := profile["proxy-groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("proxy-groups = %#v, want PROXY and AUTO", profile["proxy-groups"])
	}
	proxyGroup := groups[0].(map[string]any)
	if proxyGroup["name"] != "PROXY" || proxyGroup["include-all-proxies"] != true {
		t.Fatalf("unexpected primary group: %#v", proxyGroup)
	}
	rules, ok := profile["rules"].([]any)
	if !ok || len(rules) == 0 || rules[len(rules)-1] != "MATCH,PROXY" {
		t.Fatalf("unexpected rules: %#v", profile["rules"])
	}
}

func TestRenderMihomoProfileAppliesCustomRewritesAfterBasic(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	result, err := RenderMihomoProfile(ctx, store, "alice", []mihomo.Rewrite{
		{
			Name: "custom-mode",
			Kind: mihomo.RewriteJavaScript,
			Content: `function main(config) {
				config.mode = "global"
				config["proxy-groups"][0].name = "CUSTOM"
				return config
			}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := yaml.Unmarshal(result.YAML, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["mode"] != "global" {
		t.Fatalf("mode = %#v, want global", profile["mode"])
	}
	groups := profile["proxy-groups"].([]any)
	if groups[0].(map[string]any)["name"] != "CUSTOM" {
		t.Fatalf("custom rewrite did not run after basic: %#v", groups)
	}
}

func TestRenderMihomoProfileUsesSavedUserProfile(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	profile, err := store.CreateMihomoProfile(ctx, db.CreateMihomoProfileParams{
		Name: "Saved", UserName: "alice",
		Document: db.MihomoProfileDocument{Rewrites: []db.MihomoRewrite{
			{ID: "disabled", Name: "Disabled", Kind: "yaml", Content: "mode: direct\n", Enabled: false},
			{ID: "enabled", Name: "Enabled", Kind: "yaml", Content: "mode: global\n", Enabled: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AssignMihomoProfileToUser(ctx, "alice", profile.ID); err != nil {
		t.Fatal(err)
	}

	result, err := RenderMihomoProfile(ctx, store, "alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	var rendered map[string]any
	if err := yaml.Unmarshal(result.YAML, &rendered); err != nil {
		t.Fatal(err)
	}
	if rendered["mode"] != "global" {
		t.Fatalf("saved enabled rewrite was not applied: %#v", rendered)
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
			{Host: "2606:4700::1", Tag: "v6", Selected: true},
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
	if provider.Proxies[0]["name"] != "vless-39090" ||
		provider.Proxies[1]["name"] != "vless-39090-v6" {
		t.Fatalf("unexpected names: %q, %q", provider.Proxies[0]["name"], provider.Proxies[1]["name"])
	}

	if _, err := store.SetProxyCredentialEnabled(ctx, "alice", "azus", "vless-39090", false); err != nil {
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

func TestRenderMihomoProxyProviderLegacyUntaggedAdditionalHost(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "boxfleet.db")
	store, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	seedVLESSRealityFixture(t, ctx, store)

	// Existing hosts_json rows created before host tags remain renderable.
	rawDB, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.ExecContext(ctx, `
UPDATE nodes
SET hosts_json = '[{"host":"azus.example.net","selected":true},{"host":"203.0.113.10","selected":true}]'
WHERE name = 'azus'`); err != nil {
		t.Fatal(err)
	}
	// Normal node updates perform this synchronization automatically. This test
	// writes the legacy JSON directly, so invoke the compatibility sync here.
	if err := store.SyncLegacyDirectPathsForNode(ctx, "azus"); err != nil {
		t.Fatal(err)
	}

	info, err := ConnectionInfoForUser(ctx, store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Nodes[0].Proxies[1].Name; got != "vless-39090-203.0.113.10" {
		t.Fatalf("legacy profile name = %q", got)
	}
}

func TestRenderMihomoProxyProviderRejectsDuplicateFinalNames(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)

	if _, err := store.UpdateNode(ctx, db.UpdateNodeParams{
		Name:   "azus",
		Status: "active",
		Hosts: []db.NodeHost{
			{Host: "azus.example.net", Selected: true},
			{Host: "2606:4700::1", Tag: "v6", Selected: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090-v6",
		Protocol:     db.ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39091,
		Transport:    db.TransportTCP,
		Enabled:      true,
		SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private-key","reality_public_key":"public-key","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityAccess(ctx, db.IssueCredentialParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090-v6",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := RenderMihomoProxyProvider(ctx, store, "alice")
	if err == nil || !strings.Contains(err.Error(), `Mihomo profile name "vless-39090-v6" conflicts`) {
		t.Fatalf("expected final profile name conflict, got %v", err)
	}
}

func TestRenderClientConfigSelectsTaggedProfile(t *testing.T) {
	ctx := context.Background()
	store := openRenderTestDB(t)
	seedVLESSRealityFixture(t, ctx, store)
	if _, err := store.UpdateNode(ctx, db.UpdateNodeParams{
		Name:   "azus",
		Status: "active",
		Hosts: []db.NodeHost{
			{Host: "azus.example.net", Selected: true},
			{Host: "2606:4700::1", Tag: "v6", Selected: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := RenderClientConfig(ctx, store, ClientConfigParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090-v6",
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	outbound := config["outbounds"].([]any)[0].(map[string]any)
	if outbound["server"] != "2606:4700::1" {
		t.Fatalf("selected server = %v", outbound["server"])
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

// seedShadowsocks2022Fixture publishes a node whose only proxy is Shadowsocks
// 2022 and grants every named user an access on it.
func seedShadowsocks2022Fixture(t *testing.T, ctx context.Context, store *db.DB, userNames ...string) db.Proxy {
	t.Helper()
	if _, err := store.CreateNode(ctx, "ss-node", "198.51.100.7", ""); err != nil {
		t.Fatal(err)
	}
	proxy, err := store.CreateProxy(ctx, db.CreateProxyParams{
		NodeName:     "ss-node",
		Name:         "ss-8443",
		Protocol:     db.ProtocolShadowsocks2022,
		Listen:       "0.0.0.0",
		ListenPort:   8443,
		Transport:    db.TransportTCPUDP,
		Enabled:      true,
		SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, userName := range userNames {
		if _, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: userName}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindUserToNode(ctx, userName, "ss-node"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.IssueShadowsocks2022Access(ctx, db.IssueCredentialParams{
			UserName:  userName,
			NodeName:  "ss-node",
			ProxyName: "ss-8443",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return proxy
}

func shadowsocks2022UserKey(t *testing.T, ctx context.Context, store *db.DB, userName string) string {
	t.Helper()
	access, err := store.GetProxyCredential(ctx, userName, "ss-node", "ss-8443")
	if err != nil {
		t.Fatal(err)
	}
	var credential db.Shadowsocks2022Credential
	if err := json.Unmarshal([]byte(access.CredentialJSON), &credential); err != nil {
		t.Fatal(err)
	}
	return credential.Password
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
	if _, err := store.IssueVLESSRealityAccess(ctx, db.IssueCredentialParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
}
