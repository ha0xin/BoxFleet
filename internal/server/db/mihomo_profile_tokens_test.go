package db

import (
	"context"
	"testing"
)

func TestMihomoProfileSubscriptionTokensAreIndependentPerConfiguration(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{Name: "Alice desktop", UserName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{Name: "Alice mobile", UserName: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishMihomoProfile(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishMihomoProfile(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	firstToken, err := store.IssueMihomoProfileSubscriptionToken(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondToken, err := store.IssueMihomoProfileSubscriptionToken(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstToken.Token == secondToken.Token || firstToken.ProfileID == secondToken.ProfileID {
		t.Fatalf("tokens are not configuration-scoped: %#v %#v", firstToken, secondToken)
	}
	verified, ok, err := store.VerifyMihomoProfileSubscriptionToken(ctx, secondToken.Token)
	if err != nil || !ok || verified.ProfileID != second.ID || verified.ProxyUserName != "alice" {
		t.Fatalf("verified token = %#v, %v, %v", verified, ok, err)
	}
}
