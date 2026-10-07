package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func rotationFixture(t *testing.T) (*DB, []ProxyCredential) {
	t.Helper()
	ctx := context.Background()
	d := openTestDB(t)
	for _, name := range []string{"alice", "bob"} {
		if _, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: name, GlobalQuotaBytes: 10000}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.CreateNode(ctx, "edge", "203.0.113.1", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "bob"} {
		if _, err := d.BindUserToNode(ctx, name, "edge"); err != nil {
			t.Fatal(err)
		}
	}
	for i, protocol := range []string{ProtocolVLESSReality, ProtocolShadowsocks2022} {
		name := []string{"vless", "ss"}[i]
		p, err := d.CreateProxy(ctx, CreateProxyParams{NodeName: "edge", Name: name, Protocol: protocol, ListenPort: 443 + i, Enabled: true, SettingsJSON: `{}`})
		if err != nil {
			t.Fatal(err)
		}
		for _, user := range []string{"alice", "bob"} {
			params := IssueCredentialParams{UserName: user, NodeName: "edge", ProxyName: p.Name}
			if protocol == ProtocolVLESSReality {
				_, err = d.IssueVLESSRealityAccess(ctx, params)
			} else {
				_, err = d.IssueShadowsocks2022Access(ctx, params)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err := d.ListProxyCredentialsByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	return d, rows
}

func TestRotateUserCredentialsPreservesAccessAndTraffic(t *testing.T) {
	ctx := context.Background()
	d, rows := rotationFixture(t)
	if _, err := d.RevokeProxyCredential(ctx, "alice", "edge", "ss"); err != nil {
		t.Fatal(err)
	}
	rows, _ = d.ListProxyCredentialsByUser(ctx, "alice")
	bob, _ := d.ListProxyCredentialsByUser(ctx, "bob")
	paths, _ := d.ListActivePathAccessesByUser(ctx, "alice")
	profile, err := d.CreateMihomoProfile(ctx, CreateMihomoProfileParams{Name: "Alice desktop", UserName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := d.IssueMihomoProfileSubscriptionToken(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	user, _ := d.GetProxyUser(ctx, "alice")
	bindings, err := d.q.ListUserNodeBindingsByUserName(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RecordTrafficReport(ctx, TrafficReport{NodeName: "edge", Sequence: 1, AgentBootID: "test", Deltas: []TrafficDelta{{AuthName: rows[0].AuthName, Direction: "uplink", RawBytesDelta: 100, CounterValue: 100}}}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := d.sql.QueryRowContext(ctx, "SELECT SUM(raw_bytes) FROM traffic_usage_totals").Scan(&before); err != nil {
		t.Fatal(err)
	}
	count, err := d.RotateUserCredentials(ctx, "alice")
	if err != nil || count != 2 {
		t.Fatalf("rotation count = %d, err = %v", count, err)
	}
	after, _ := d.ListProxyCredentialsByUser(ctx, "alice")
	for i := range rows {
		if rows[i].CredentialJSON == after[i].CredentialJSON {
			t.Fatal("secret did not change")
		}
		if after[i].Protocol == ProtocolVLESSReality {
			var key VLESSRealityCredential
			if err := json.Unmarshal([]byte(after[i].CredentialJSON), &key); err != nil {
				t.Fatal(err)
			}
			if _, err := uuid.Parse(key.UUID); err != nil {
				t.Fatal(err)
			}
		} else {
			var key Shadowsocks2022Credential
			if err := json.Unmarshal([]byte(after[i].CredentialJSON), &key); err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(key.Password)
			if err != nil || len(raw) != 16 {
				t.Fatal("invalid Shadowsocks key")
			}
		}
		after[i].CredentialJSON = rows[i].CredentialJSON
		after[i].UpdatedAt = rows[i].UpdatedAt
	}
	if !reflect.DeepEqual(rows, after) {
		t.Fatal("rotation changed credential identity or access settings")
	}
	bobAfter, _ := d.ListProxyCredentialsByUser(ctx, "bob")
	if !reflect.DeepEqual(bob, bobAfter) {
		t.Fatal("changed another user")
	}
	pathsAfter, _ := d.ListActivePathAccessesByUser(ctx, "alice")
	if !reflect.DeepEqual(paths, pathsAfter) {
		t.Fatal("changed PathAccess")
	}
	subscriptionAfter, found, err := d.GetActiveMihomoProfileSubscriptionToken(ctx, profile.ID)
	if err != nil || !found || !reflect.DeepEqual(subscription, subscriptionAfter) {
		t.Fatal("changed subscription token")
	}
	userAfter, _ := d.GetProxyUser(ctx, "alice")
	if !reflect.DeepEqual(user, userAfter) {
		t.Fatal("changed user quota/status")
	}
	bindingsAfter, _ := d.q.ListUserNodeBindingsByUserName(ctx, "alice")
	if !reflect.DeepEqual(bindings, bindingsAfter) {
		t.Fatal("changed node binding")
	}
	if err := d.RecordTrafficReport(ctx, TrafficReport{NodeName: "edge", Sequence: 2, AgentBootID: "test", Deltas: []TrafficDelta{{AuthName: rows[0].AuthName, Direction: "uplink", RawBytesDelta: 50, CounterValue: 150}}}); err != nil {
		t.Fatal(err)
	}
	var total int64
	if err := d.sql.QueryRowContext(ctx, "SELECT SUM(raw_bytes) FROM traffic_usage_totals").Scan(&total); err != nil {
		t.Fatal(err)
	}
	if before != 100 || total != 150 {
		t.Fatalf("traffic discontinuity: before=%d after=%d", before, total)
	}
}

func TestRotateUserCredentialsAtomicAndExcludesDeleted(t *testing.T) {
	ctx := context.Background()
	d, rows := rotationFixture(t)
	// The second write fails: the first secret must roll back too.
	if _, err := d.sql.ExecContext(ctx, `CREATE TRIGGER fail_rotation BEFORE UPDATE OF credential_json ON proxy_accesses WHEN OLD.id = '`+rows[1].ID+`' BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.RotateUserCredentials(ctx, "alice"); err == nil || n != 0 {
		t.Fatal("partial rotation succeeded")
	}
	after, _ := d.ListProxyCredentialsByUser(ctx, "alice")
	if !reflect.DeepEqual(rows, after) {
		t.Fatal("transaction failed to roll back")
	}
	if _, err := d.sql.ExecContext(ctx, "DROP TRIGGER fail_rotation"); err != nil {
		t.Fatal(err)
	}
	deleted, err := d.SoftDeleteProxyCredential(ctx, "alice", "edge", rows[1].ProxyName)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := d.RotateUserCredentials(ctx, "alice"); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	afterDeleted, err := d.getProxyCredentialByIDs(ctx, deleted.ProxyUserID, deleted.ProxyID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deleted, afterDeleted) {
		t.Fatal("changed deleted credential")
	}
	if _, err := d.RotateUserCredentials(ctx, "missing"); err == nil {
		t.Fatal("missing user accepted")
	}
	if _, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: "empty"}); err != nil {
		t.Fatal(err)
	}
	if n, err := d.RotateUserCredentials(ctx, "empty"); err != nil || n != 0 {
		t.Fatalf("empty user: n=%d err=%v", n, err)
	}
}
