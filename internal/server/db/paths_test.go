package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestGrantPathToUserIssuesCredentialsForWholeChain(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "entry", "198.51.100.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "exit", "203.0.113.20", ""); err != nil {
		t.Fatal(err)
	}
	entryProxy, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName: "entry", Name: "entry-vless", Protocol: ProtocolVLESSReality,
		ListenPort: 443, Enabled: true, SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	exitProxy, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName: "exit", Name: "home-ss", Protocol: ProtocolShadowsocks2022,
		ListenPort: 8443, Enabled: true, SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	entryNode, _ := store.GetNode(ctx, "entry")
	exitNode, _ := store.GetNode(ctx, "exit")
	entryEndpoint, err := store.EnsureEndpoint(ctx, entryProxy.ID, entryNode.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	exitEndpoint, err := store.EnsureEndpoint(ctx, exitProxy.ID, exitNode.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	dialer, err := store.CreatePath(ctx, CreatePathParams{
		Name: "optimized", EndpointID: entryEndpoint.ID, Enabled: true, Visibility: PathVisibilityDependency,
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.CreatePath(ctx, CreatePathParams{
		Name: "residential", EndpointID: exitEndpoint.ID, DialerPathID: dialer.ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	entryRoot, err := store.CreatePath(ctx, CreatePathParams{
		Name: "entry-direct", DisplayName: "Entry selectable", EndpointID: entryEndpoint.ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", dialer.ID); err == nil || !strings.Contains(err.Error(), "dependency-only") {
		t.Fatalf("dependency Path was directly grantable: %v", err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", entryRoot.ID); err != nil {
		t.Fatal(err)
	}
	accesses, err := store.ListActivePathAccessesByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(accesses) != 2 {
		t.Fatalf("PathAccesses = %#v, want two selectable roots", accesses)
	}
	credentials, err := store.ListProxyCredentialsByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 2 {
		t.Fatalf("credentials = %d, want both hops", len(credentials))
	}
	ss, err := store.GetProxyCredential(ctx, "alice", "exit", "home-ss")
	if err != nil {
		t.Fatal(err)
	}
	var credential Shadowsocks2022Credential
	if err := json.Unmarshal([]byte(ss.CredentialJSON), &credential); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(credential.Password)
	if err != nil || len(decoded) != 16 {
		t.Fatalf("SS-2022 user key decodes to %d bytes, err=%v", len(decoded), err)
	}
	if _, err := store.RevokePathAccess(ctx, "alice", root.ID); err != nil {
		t.Fatal(err)
	}
	ss, err = store.GetProxyCredential(ctx, "alice", "exit", "home-ss")
	if err != nil || ss.Enabled {
		t.Fatalf("unused exit credential remained enabled: %#v, err=%v", ss, err)
	}
	entryCredential, err := store.GetProxyCredential(ctx, "alice", "entry", "entry-vless")
	if err != nil || !entryCredential.Enabled {
		t.Fatalf("shared entry credential was disabled: %#v, err=%v", entryCredential, err)
	}
	if _, err := store.RevokePathAccess(ctx, "alice", entryRoot.ID); err != nil {
		t.Fatal(err)
	}
	entryCredential, err = store.GetProxyCredential(ctx, "alice", "entry", "entry-vless")
	if err != nil || entryCredential.Enabled {
		t.Fatalf("unused entry credential remained enabled: %#v, err=%v", entryCredential, err)
	}
	revokedPassword := credential.Password
	if _, err := store.GrantPathToUser(ctx, "alice", root.ID); err != nil {
		t.Fatal(err)
	}
	entryCredential, _ = store.GetProxyCredential(ctx, "alice", "entry", "entry-vless")
	ss, _ = store.GetProxyCredential(ctx, "alice", "exit", "home-ss")
	if !entryCredential.Enabled || !ss.Enabled {
		t.Fatalf("regrant did not restore chain credentials: entry=%v exit=%v", entryCredential.Enabled, ss.Enabled)
	}
	if err := json.Unmarshal([]byte(ss.CredentialJSON), &credential); err != nil {
		t.Fatal(err)
	}
	if credential.Password == revokedPassword {
		t.Fatal("regrant reinstated the revoked exit password")
	}
}

func TestDeletePathDisablesCredentialsItWasTheLastConsumerOf(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "exit", "203.0.113.30", ""); err != nil {
		t.Fatal(err)
	}
	proxy, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName: "exit", Name: "exit-vless", Protocol: ProtocolVLESSReality,
		ListenPort: 443, Enabled: true, SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	node, _ := store.GetNode(ctx, "exit")
	endpoint, err := store.EnsureEndpoint(ctx, proxy.ID, node.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.CreatePath(ctx, CreatePathParams{
		Name: "custom", DisplayName: "Custom exit", EndpointID: endpoint.ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", path.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePath(ctx, path.ID); err != nil {
		t.Fatal(err)
	}
	credential, err := store.GetProxyCredential(ctx, "alice", "exit", "exit-vless")
	if err != nil {
		t.Fatal(err)
	}
	if credential.Enabled {
		t.Fatalf("credential stayed enabled after its only path was deleted: %#v", credential)
	}
	if _, err := store.GetPath(ctx, path.ID); err == nil {
		t.Fatal("deleted path is still readable")
	}
}

func TestPathGraphRejectsCycleAndExcessiveDepth(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "node", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	proxy, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName: "node", Name: "proxy", Protocol: ProtocolVLESSReality,
		ListenPort: 443, Enabled: true, SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	node, _ := store.GetNode(ctx, "node")
	endpoint, err := store.EnsureEndpoint(ctx, proxy.ID, node.Hosts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]Path, 0, MaxDialerPathDepth)
	for i := 0; i < MaxDialerPathDepth; i++ {
		dialer := ""
		if i > 0 {
			dialer = paths[i-1].ID
		}
		path, err := store.CreatePath(ctx, CreatePathParams{
			Name: string(rune('a' + i)), EndpointID: endpoint.ID, DialerPathID: dialer,
			Enabled: true, Visibility: PathVisibilitySelectable,
		})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if _, err := store.CreatePath(ctx, CreatePathParams{
		Name: "too-deep", EndpointID: endpoint.ID, DialerPathID: paths[len(paths)-1].ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	}); err == nil || !strings.Contains(err.Error(), "depth exceeds") {
		t.Fatalf("expected depth error, got %v", err)
	}
	first := paths[0]
	if _, err := store.UpdatePath(ctx, UpdatePathParams{
		ID: first.ID, Name: first.Name, EndpointID: endpoint.ID,
		DialerPathID: paths[len(paths)-1].ID, Enabled: true, Visibility: first.Visibility,
	}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	firstNamed, err := store.CreatePath(ctx, CreatePathParams{
		Name: "named-a", DisplayName: "Same published name", EndpointID: endpoint.ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondNamed, err := store.CreatePath(ctx, CreatePathParams{
		Name: "named-b", DisplayName: "Same published name", EndpointID: endpoint.ID,
		Enabled: true, Visibility: PathVisibilitySelectable,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", firstNamed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantPathToUser(ctx, "alice", secondNamed.ID); err == nil || !strings.Contains(err.Error(), "published Path name") {
		t.Fatalf("expected published name conflict before grant, got %v", err)
	}
}

func TestProxyDirectPublicationSwitchManagesDirectPaths(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "node", "192.0.2.10", ""); err != nil {
		t.Fatal(err)
	}
	direct := false
	proxy, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName: "node", Name: "private", Protocol: ProtocolVLESSReality,
		ListenPort: 443, Enabled: true, SettingsJSON: `{}`, DirectPublish: &direct,
	})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := store.ListPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("direct-disabled Proxy created Paths: %#v", paths)
	}
	if _, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name: "node", Status: "active", Hosts: []NodeHost{
			{Host: "192.0.2.10", Selected: true},
			{Host: "2001:db8::10", Tag: "ipv6", Selected: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	paths, err = store.SetProxyDirectPublication(ctx, proxy.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || !paths[0].Managed || !paths[0].Enabled || !paths[1].Enabled {
		t.Fatalf("enabled direct publication = %#v", paths)
	}
	if _, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name: "node", Status: "active", Hosts: []NodeHost{
			{Host: "192.0.2.10", Selected: true},
			{Host: "2001:db8::10", Tag: "ipv6", Selected: false},
		},
	}); err != nil {
		t.Fatal(err)
	}
	allPaths, err := store.ListPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allPaths) != 2 || !allPaths[0].Enabled || allPaths[1].Enabled {
		t.Fatalf("deselected Host managed Paths = %#v", allPaths)
	}
	paths, err = store.SetProxyDirectPublication(ctx, proxy.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0].Enabled {
		t.Fatalf("disabled direct publication = %#v", paths)
	}
	enabled, err := store.GetProxyDirectPublication(ctx, proxy.ID)
	if err != nil || enabled {
		t.Fatalf("stored direct publication = %v, err=%v", enabled, err)
	}
	if err := store.DeletePath(ctx, allPaths[0].ID); err == nil || !strings.Contains(err.Error(), "managed path") {
		t.Fatalf("managed Path delete was accepted: %v", err)
	}
	if _, err := store.UpdatePath(ctx, UpdatePathParams{
		ID: allPaths[0].ID, Name: allPaths[0].Name, DisplayName: allPaths[0].DisplayName,
		EndpointID: allPaths[0].EndpointID, Enabled: false, Visibility: allPaths[0].Visibility,
	}); err == nil || !strings.Contains(err.Error(), "managed path") {
		t.Fatalf("managed Path edit was accepted: %v", err)
	}
}
