package db

import (
	"context"
	"testing"
)

func TestUserMutationsResolveIDBeforeConflictingName(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	a, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == a.ID || b.Name != a.ID {
		t.Fatalf("create returned wrong user: %+v", b)
	}
	node, err := d.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []ProxyUser{a, b} {
		binding, err := d.BindUserToNode(ctx, user.ID, node.ID)
		if err != nil || binding.ProxyUserID != user.ID {
			t.Fatalf("wrong binding for %s: %+v %v", user.ID, binding, err)
		}
		bindings, err := d.ListUserNodeBindings(ctx, user.ID)
		if err != nil || len(bindings) != 1 || bindings[0].ProxyUserID != user.ID {
			t.Fatalf("wrong binding list for %s: %+v %v", user.ID, bindings, err)
		}
	}
	label, status, expiry, quota := "Alice edited", "disabled", "2030-01-01T00:00:00Z", int64(123)
	if _, err := d.UpdateProxyUser(ctx, a.ID, UpdateProxyUserParams{DisplayName: &label, Status: &status, ExpireAt: &expiry, GlobalQuotaBytes: &quota}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := d.GetProxyUser(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.DisplayName != b.DisplayName || unchanged.Status != b.Status || unchanged.GlobalQuotaBytes != b.GlobalQuotaBytes || unchanged.ExpireAt != b.ExpireAt {
		t.Fatalf("other user changed: %+v", unchanged)
	}
	if _, err := d.SoftDeleteProxyUser(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetProxyUser(ctx, a.ID); err == nil {
		t.Fatal("deleted ID fell through to another user's name")
	}
	remaining, err := d.ListProxyUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != b.ID {
		t.Fatalf("delete affected other user: %+v", remaining)
	}
	if _, err := d.RestoreProxyUser(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.SetProxyUserQuota(ctx, b.ID, 456); err != nil {
		t.Fatal(err)
	}
	restored, err := d.GetProxyUser(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.GlobalQuotaBytes != quota || restored.DeletedAt.Valid {
		t.Fatalf("restored wrong user: %+v", restored)
	}
}
