package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/haoxin/boxfleet/migrations"
)

func TestDeletedResourceNamesDoNotReuseIdentity(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	old, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateNode(ctx, old.Name, "192.0.2.2", ""); err == nil {
		t.Fatal("active duplicate accepted")
	}
	if _, err = store.RenameNode(ctx, old.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SoftDeleteNode(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"edge", "renamed"} {
		fresh, err := store.CreateNode(ctx, name, "192.0.2.3", "")
		if err != nil {
			t.Fatal(err)
		}
		if fresh.ID == old.ID {
			t.Fatal("reused deleted node ID")
		}
		if got, err := store.GetNode(ctx, fresh.ID); err != nil || got.ID != fresh.ID {
			t.Fatalf("ID lookup: %+v %v", got, err)
		}
		if _, err := store.GetNode(ctx, old.ID); err == nil {
			t.Fatal("old ID resolved to replacement")
		}
	}
	if _, err = store.RestoreNode(ctx, old.ID); err == nil {
		t.Fatal("deleted node revived")
	}
	user, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", GlobalQuotaBytes: 123})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SoftDeleteProxyUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	freshUser, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if freshUser.ID == user.ID || freshUser.GlobalQuotaBytes != 0 {
		t.Fatal("replacement inherited old identity/quota")
	}
	archived, err := store.getProxyUserIncludingDeleted(ctx, user.ID)
	if err != nil || archived.ID != user.ID || !archived.DeletedAt.Valid {
		t.Fatalf("lost user history: %+v %v", archived, err)
	}
	if _, err = store.RestoreProxyUser(ctx, user.ID); err == nil {
		t.Fatal("restore overwrote active same-name user")
	}
}

func TestDeletedNodeArchivesProxyAndReleasesItsName(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	old, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	params := CreateProxyParams{NodeName: old.ID, Name: "edge-proxy", Protocol: ProtocolVLESSReality, Listen: "0.0.0.0", ListenPort: 443, Transport: TransportTCP, Enabled: true, SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`}
	proxy, err := store.CreateProxy(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ListPathsForAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SoftDeleteNode(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.CreateNode(ctx, old.Name, "192.0.2.2", "")
	if err != nil {
		t.Fatal(err)
	}
	params.NodeName = fresh.ID
	replacement, err := store.CreateProxy(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if replacement.ID == proxy.ID {
		t.Fatal("proxy identity inherited")
	}
	after, err := store.ListPathsForAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range after {
		for _, b := range before {
			if a.ID == b.ID {
				t.Fatal("old path attached to replacement")
			}
		}
	}
	archived, err := store.getProxyIncludingDeleted(ctx, old.ID, proxy.ID)
	if err != nil || !archived.DeletedAt.Valid {
		t.Fatalf("lost archived proxy: %+v %v", archived, err)
	}
}

func TestIdentityMigrationKeepsPopulatedForeignKeys(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err = goose.UpToContext(ctx, store.sql, ".", 27); err != nil {
		t.Fatal(err)
	}
	node, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.IssueNodeToken(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	params := CreateProxyParams{NodeName: node.ID, Name: "edge-proxy", Protocol: ProtocolVLESSReality, Listen: "0.0.0.0", ListenPort: 443, Transport: TransportTCP, Enabled: true, SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`}
	proxy, err := store.CreateProxy(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := store.ListPaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetProxy(ctx, node.ID, proxy.ID); err != nil || got.ID != proxy.ID {
		t.Fatalf("proxy changed during migration: %+v %v", got, err)
	}
	gotPaths, err := store.ListPaths(ctx)
	if err != nil || len(gotPaths) != len(paths) {
		t.Fatal("path history changed during migration")
	}
	if ok, err := store.VerifyNodeToken(ctx, node.Name, token.Token); err != nil || !ok {
		t.Fatal("existing token changed during migration")
	}
	var count int
	if err = store.sql.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d %v", count, err)
	}
	if err = store.sql.QueryRow("PRAGMA foreign_keys").Scan(&count); err != nil || count != 1 {
		t.Fatalf("foreign keys disabled: %d %v", count, err)
	}
}

func TestSameNameReplacementUserGetsIndependentConnectionCounter(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	node, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := store.CreateProxy(ctx, CreateProxyParams{NodeName: node.ID, Name: "counter-proxy", Protocol: ProtocolVLESSReality, Listen: "0.0.0.0", ListenPort: 443, Transport: TransportTCP, Enabled: true, SettingsJSON: `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindUserToNode(ctx, user.ID, node.ID); err != nil {
		t.Fatal(err)
	}
	old, err := store.IssueVLESSRealityCredential(ctx, IssueCredentialParams{UserName: user.ID, NodeName: node.ID, ProxyName: proxy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SoftDeleteProxyUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindUserToNode(ctx, fresh.ID, node.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := store.IssueVLESSRealityCredential(ctx, IssueCredentialParams{UserName: fresh.ID, NodeName: node.ID, ProxyName: proxy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.AuthName == old.AuthName || replacement.ID == old.ID || replacement.CredentialJSON == old.CredentialJSON {
		t.Fatal("replacement inherited prior credential or counter")
	}
}
