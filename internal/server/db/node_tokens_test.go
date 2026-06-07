package db

import (
	"context"
	"testing"
)

func TestIssueAndVerifyNodeToken(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if issued.Token == "" {
		t.Fatal("token is empty")
	}
	ok, err := store.VerifyNodeToken(ctx, "azus", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("token did not verify")
	}
	ok, err = store.VerifyNodeToken(ctx, "azus", "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("wrong token verified")
	}
	if _, err := store.DisableNode(ctx, "azus"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.VerifyNodeToken(ctx, "azus", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("token verified after node was disabled")
	}
	if _, err := store.IssueNodeToken(ctx, "azus"); err == nil {
		t.Fatal("disabled node accepted a new token")
	}
}
