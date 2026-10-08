package db

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRunningUpdateCanRecoverAfterQueueDeadline(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	if _, err := d.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	op, _, err := d.CreateNodeOperation(ctx, CreateNodeOperationParams{NodeName: "edge", Kind: "update.agent", Payload: json.RawMessage(`{"version":"v0.2.0"}`), IdempotencyKey: "recovery", ExpiresAt: time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := d.ClaimNodeOperation(ctx, ClaimNodeOperationParams{NodeName: "edge", Capabilities: op.RequiredCapabilities})
	if err != nil || !ok {
		t.Fatalf("initial claim %v %v", ok, err)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := d.sql.ExecContext(ctx, `UPDATE node_operations SET expires_at=?, lease_expires_at=? WHERE id=?`, past, past, op.ID); err != nil {
		t.Fatal(err)
	}
	recovered, ok, err := d.ClaimNodeOperation(ctx, ClaimNodeOperationParams{NodeName: "edge", Capabilities: op.RequiredCapabilities, CurrentOperationID: op.ID, LeaseToken: claim.LeaseToken})
	if err != nil || !ok {
		t.Fatalf("cannot reclaim running operation past queue deadline: ok=%v err=%v", ok, err)
	}
	if recovered.Operation.ID != op.ID || recovered.Operation.Attempt != 2 || recovered.LeaseToken == claim.LeaseToken {
		t.Fatalf("reclaimed wrong operation: %+v", recovered)
	}
}

func TestUnclaimedUpdateExpiresAtQueueDeadline(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	if _, err := d.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	op, _, err := d.CreateNodeOperation(ctx, CreateNodeOperationParams{NodeName: "edge", Kind: "update.agent", Payload: json.RawMessage(`{"version":"v0.2.0"}`), IdempotencyKey: "expired", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.sql.ExecContext(ctx, `UPDATE node_operations SET expires_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), op.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := d.ClaimNodeOperation(ctx, ClaimNodeOperationParams{NodeName: "edge", Capabilities: op.RequiredCapabilities}); err != nil || ok {
		t.Fatalf("expired queued operation claimed: ok=%v err=%v", ok, err)
	}
	after, err := d.GetNodeOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "expired" || after.Attempt != 0 {
		t.Fatalf("unexpected expired operation: %+v", after)
	}
}
