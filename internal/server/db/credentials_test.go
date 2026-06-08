package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestIssueVLESSRealityAccess(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	_, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateNode(ctx, "azus", "203.0.113.10", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"01234567","handshake_server":"www.amazon.com","handshake_port":443}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.BindUserToNode(ctx, "alice", "azus")
	if err != nil {
		t.Fatal(err)
	}

	access, err := store.IssueVLESSRealityAccess(ctx, IssueAccessParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	})
	if err != nil {
		t.Fatal(err)
	}
	if access.AuthName != "vless-39090@alice" {
		t.Fatalf("auth name = %q", access.AuthName)
	}
	if access.Protocol != ProtocolVLESSReality {
		t.Fatalf("protocol = %q", access.Protocol)
	}
	var vless VLESSRealityCredential
	if err := json.Unmarshal([]byte(access.CredentialJSON), &vless); err != nil {
		t.Fatal(err)
	}
	if vless.UUID == "" {
		t.Fatal("uuid is empty")
	}
	if vless.Flow != VLESSRealityFlowVision {
		t.Fatalf("flow = %q", vless.Flow)
	}

	duplicate, err := store.IssueVLESSRealityAccess(ctx, IssueAccessParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != access.ID {
		t.Fatalf("duplicate issue created new access: %s != %s", duplicate.ID, access.ID)
	}

	lookup, err := store.GetProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if lookup.ID != access.ID {
		t.Fatalf("lookup id = %q, want %q", lookup.ID, access.ID)
	}
	list, err := store.ListProxyAccessesByNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != access.ID {
		t.Fatalf("list accesses = %#v", list)
	}

	revoked, err := store.RevokeProxyAccess(ctx, "alice", "azus", "vless-39090")
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Enabled {
		t.Fatal("revoked access is still enabled")
	}
	list, err = store.ListProxyAccessesByNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("revoked access still renders = %#v", list)
	}
	reissued, err := store.IssueVLESSRealityAccess(ctx, IssueAccessParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reissued.ID != access.ID || !reissued.Enabled {
		t.Fatalf("reissued access = %#v, want same enabled access %s", reissued, access.ID)
	}
}

func TestAuthNameDelimitersRejected(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice@proxy"}); err == nil {
		t.Fatal("user name containing @ was accepted")
	}
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice>>>proxy"}); err == nil {
		t.Fatal("user name containing stats delimiter was accepted")
	}
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:   "azus",
		Name:       "vless@39090",
		Protocol:   ProtocolVLESSReality,
		ListenPort: 39090,
		Enabled:    true,
	}); err == nil {
		t.Fatal("proxy name containing @ was accepted")
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:   "azus",
		Name:       "vless>>>39090",
		Protocol:   ProtocolVLESSReality,
		ListenPort: 39090,
		Enabled:    true,
	}); err == nil {
		t.Fatal("proxy name containing stats delimiter was accepted")
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "boxfleet.db"))
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
