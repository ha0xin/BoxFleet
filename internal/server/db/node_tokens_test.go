package db

import (
	"context"
	"strings"
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
	if !strings.HasPrefix(issued.Token, "bfnt_") {
		t.Fatalf("token = %q", issued.Token)
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
	// Pause keeps the token valid; only decommission revokes it.
	if err := store.SetNodeStatus(ctx, "azus", "disabled"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.VerifyNodeToken(ctx, "azus", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("token stopped verifying after node was paused")
	}
	if _, err := store.IssueNodeToken(ctx, "azus"); err == nil {
		t.Fatal("disabled node accepted a new token")
	}
	if _, err := store.SoftDeleteNode(ctx, "azus"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.VerifyNodeToken(ctx, "azus", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("token verified after node was decommissioned")
	}
}
