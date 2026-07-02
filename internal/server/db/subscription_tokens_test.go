package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSubscriptionTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}

	issued, err := store.IssueSubscriptionToken(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Token, "bfsub_") {
		t.Fatalf("token = %q", issued.Token)
	}
	if len(strings.TrimPrefix(issued.Token, "bfsub_")) != 43 {
		t.Fatalf("token has unexpected random payload length: %q", issued.Token)
	}

	current, exists, err := store.GetActiveSubscriptionToken(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || current.Token != issued.Token {
		t.Fatalf("current = %#v, exists = %v", current, exists)
	}
	if _, err := store.IssueSubscriptionToken(ctx, "alice"); !errors.Is(err, ErrActiveSubscriptionTokenExists) {
		t.Fatalf("duplicate issue error = %v", err)
	}

	verified, ok, err := store.VerifySubscriptionToken(ctx, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || verified.ProxyUserName != "alice" {
		t.Fatalf("verified = %#v, ok = %v", verified, ok)
	}
	current, exists, err = store.GetActiveSubscriptionToken(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !exists || !current.LastUsedAt.Valid {
		t.Fatalf("last_used_at was not recorded: %#v", current)
	}

	rotated, err := store.RotateSubscriptionToken(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token == issued.Token {
		t.Fatal("rotate reused the old token")
	}
	if _, ok, err := store.VerifySubscriptionToken(ctx, issued.Token); err != nil || ok {
		t.Fatalf("old token verified after rotate: ok = %v, err = %v", ok, err)
	}
	if token, ok, err := store.VerifySubscriptionToken(ctx, rotated.Token); err != nil || !ok || token.ProxyUserName != "alice" {
		t.Fatalf("rotated token did not verify: token = %#v, ok = %v, err = %v", token, ok, err)
	}

	revoked, err := store.RevokeSubscriptionToken(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("active token was not revoked")
	}
	if _, ok, err := store.VerifySubscriptionToken(ctx, rotated.Token); err != nil || ok {
		t.Fatalf("revoked token verified: ok = %v, err = %v", ok, err)
	}
	if revoked, err := store.RevokeSubscriptionToken(ctx, "alice"); err != nil || revoked {
		t.Fatalf("second revoke = %v, err = %v", revoked, err)
	}
}

func TestRotateSubscriptionTokenRequiresActiveToken(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateSubscriptionToken(ctx, "alice"); err == nil {
		t.Fatal("rotate without an active token succeeded")
	}
	if _, exists, err := store.GetActiveSubscriptionToken(ctx, "alice"); err != nil || exists {
		t.Fatalf("token exists after failed rotate: exists = %v, err = %v", exists, err)
	}
}

func TestSubscriptionTokenDeletedWithUser(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	user, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueSubscriptionToken(ctx, user.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.sql.ExecContext(ctx, "DELETE FROM proxy_users WHERE id = ?", user.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.VerifySubscriptionToken(ctx, issued.Token); err != nil || ok {
		t.Fatalf("token survived user deletion: ok = %v, err = %v", ok, err)
	}
}
