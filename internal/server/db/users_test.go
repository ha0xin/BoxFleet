package db

import (
	"context"
	"strings"
	"testing"
)

func TestCreateProxyUserRejectsInvalidExpireAt(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	_, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", ExpireAt: "next tuesday"})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	if _, err := store.GetProxyUser(ctx, "alice"); err == nil {
		t.Fatal("user was created despite an invalid expire_at")
	}
}

func TestProxyUserExpireAtNormalizesToUTC(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	user, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", ExpireAt: "2026-07-26T12:00:00+02:00"})
	if err != nil {
		t.Fatal(err)
	}
	if !user.ExpireAt.Valid || user.ExpireAt.String != "2026-07-26T10:00:00Z" {
		t.Fatalf("expire_at = %#v", user.ExpireAt)
	}
	empty, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "bob", ExpireAt: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if empty.ExpireAt.Valid {
		t.Fatalf("empty expire_at stored as %#v", empty.ExpireAt)
	}
}

func TestSetProxyUserExpireRejectsInvalidExpireAt(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", "2026-13-01"); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", "2026-07-26T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	user, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !user.ExpireAt.Valid || user.ExpireAt.String != "2026-07-26T10:00:00Z" {
		t.Fatalf("expire_at = %#v", user.ExpireAt)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ExpireAt.Valid {
		t.Fatalf("expire_at was not cleared: %#v", cleared.ExpireAt)
	}
}

func TestUpdateProxyUserRejectsInvalidExpireAtBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", DisplayName: "Alice"}); err != nil {
		t.Fatal(err)
	}
	display := "Renamed"
	expire := "garbage"
	_, err := store.UpdateProxyUser(ctx, "alice", UpdateProxyUserParams{DisplayName: &display, ExpireAt: &expire})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	user, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Alice" {
		t.Fatalf("display_name changed despite an invalid expire_at: %q", user.DisplayName)
	}
}
