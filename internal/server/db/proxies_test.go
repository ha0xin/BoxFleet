package db

import (
	"context"
	"strings"
	"testing"
)

func TestProxyListenerConflictValidation(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	settings := `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090-a",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: settings,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090-b",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: settings,
	}); err != nil {
		t.Fatalf("same VLESS Reality listener should share multi-user proxy: %v", err)
	}
	_, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "hy2-39090",
		Protocol:     ProtocolHysteria2,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCPUDP,
		Enabled:      true,
		SettingsJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected listener conflict, got %v", err)
	}
}

func TestRenameProxyAliasesGloballyAndPreservesAccessIdentity(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	for _, node := range []string{"edge-a", "edge-b"} {
		if _, err := store.CreateNode(ctx, node, "192.0.2.1", ""); err != nil {
			t.Fatal(err)
		}
	}
	original, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "edge-a",
		Name:         "primary",
		Protocol:     ProtocolVLESSReality,
		ListenPort:   443,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "edge-b",
		Name:         "other",
		Protocol:     ProtocolVLESSReality,
		ListenPort:   443,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "edge-b",
		Name:         original.Name,
		Protocol:     ProtocolVLESSReality,
		ListenPort:   8443,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{}`,
	}); err == nil {
		t.Fatal("same proxy name was accepted on another node")
	}

	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindUserToNode(ctx, "alice", "edge-a"); err != nil {
		t.Fatal(err)
	}
	access, err := store.IssueVLESSRealityAccess(ctx, IssueAccessParams{
		UserName: "alice", NodeName: "edge-a", ProxyName: original.Name,
	})
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := store.RenameProxy(ctx, "edge-a", original.Name, "home")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != original.ID || renamed.Name != "home" {
		t.Fatalf("renamed proxy = %#v", renamed)
	}
	byAliases, err := store.GetProxy(ctx, "edge-a", original.Name)
	if err != nil {
		t.Fatal(err)
	}
	if byAliases.ID != original.ID || byAliases.Name != "home" {
		t.Fatalf("old proxy alias resolved to %#v", byAliases)
	}
	accessAfter, err := store.GetProxyAccess(ctx, "alice", "edge-a", "home")
	if err != nil {
		t.Fatal(err)
	}
	if accessAfter.AuthName != access.AuthName || accessAfter.CredentialJSON != access.CredentialJSON {
		t.Fatalf("access identity changed: before %#v, after %#v", access, accessAfter)
	}

	if _, err := store.RenameProxy(ctx, "edge-a", "home", "other"); err == nil {
		t.Fatal("rename accepted another proxy's canonical name")
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "edge-b",
		Name:         original.Name,
		Protocol:     ProtocolVLESSReality,
		ListenPort:   8443,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{}`,
	}); err == nil {
		t.Fatal("create accepted a reserved proxy alias")
	}

	back, err := store.RenameProxy(ctx, "edge-a", "home", original.Name)
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != original.ID || back.Name != original.Name {
		t.Fatalf("rename back = %#v", back)
	}
}

func TestUpdateProxyByNameRenamesAndUpdatesAtomically(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	original, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "edge",
		Name:         "primary",
		Protocol:     ProtocolVLESSReality,
		ListenPort:   443,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{"short_id":"AABB"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateProxyByName(ctx, "edge", original.Name, UpdateProxyParams{
		NodeName:     "edge",
		Name:         "home",
		ListenPort:   8443,
		Transport:    TransportTCP,
		Enabled:      false,
		SettingsJSON: `{"short_id":"CCDD"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Name != "home" ||
		updated.ListenPort != 8443 || updated.Enabled {
		t.Fatalf("updated proxy = %#v", updated)
	}
	if !strings.Contains(updated.SettingsJSON, `"short_id":"ccdd"`) {
		t.Fatalf("settings_json = %s", updated.SettingsJSON)
	}
	viaOldName, err := store.GetProxy(ctx, "edge", original.Name)
	if err != nil {
		t.Fatal(err)
	}
	if viaOldName.ID != original.ID || viaOldName.Name != updated.Name {
		t.Fatalf("old name resolved to %#v", viaOldName)
	}
}
