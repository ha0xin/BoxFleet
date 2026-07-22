package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNodeOperationCreateIsIdempotentAndSerialPerNode(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	params := CreateNodeOperationParams{
		NodeName: "edge", Kind: "update.agent", Payload: json.RawMessage(`{"version":"v0.2.0"}`),
		IdempotencyKey: "agent:v0.2.0", RequiredCapabilities: []string{"vendor.extra.v1"},
	}
	first, created, err := store.CreateNodeOperation(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if !created || first.ID == "" || first.Status != "queued" || first.Phase != "queued" {
		t.Fatalf("first operation = %#v, created=%v", first, created)
	}
	if !hasCapabilities(first.RequiredCapabilities, []string{"operations.v1", "update.agent.v1", "download.streaming.v1", "install.versioned.v1", "restart_resume.agent.v1", "vendor.extra.v1"}) {
		t.Fatalf("required capabilities = %#v", first.RequiredCapabilities)
	}
	second, created, err := store.CreateNodeOperation(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("idempotent operation = %#v, created=%v", second, created)
	}
	changed := params
	changed.Payload = json.RawMessage(`{"version":"v9.9.9"}`)
	if _, _, err := store.CreateNodeOperation(ctx, changed); !errors.Is(err, ErrOperationIdempotencyConflict) {
		t.Fatalf("changed idempotency request error = %v", err)
	}
	params.IdempotencyKey = "agent:v0.3.0"
	if _, _, err := store.CreateNodeOperation(ctx, params); !errors.Is(err, ErrActiveNodeOperation) {
		t.Fatalf("second active operation error = %v", err)
	}
}

func TestNodeOperationClaimCapabilityLeaseAndResume(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	created, _, err := store.CreateNodeOperation(ctx, CreateNodeOperationParams{
		NodeName: "edge", Kind: "update.agent", Payload: json.RawMessage(`{"version":"v0.2.0"}`),
		IdempotencyKey: "agent:v0.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: []string{"operations.v1"},
	}); err != nil || ok {
		t.Fatalf("claim without capability: ok=%v err=%v", ok, err)
	}
	claim, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: created.RequiredCapabilities,
	})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if claim.Operation.ID != created.ID || claim.Operation.Attempt != 1 || claim.LeaseToken == "" {
		t.Fatalf("claim = %#v", claim)
	}
	if _, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: created.RequiredCapabilities,
	}); err != nil || ok {
		t.Fatalf("second active claim: ok=%v err=%v", ok, err)
	}
	resumed, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: created.RequiredCapabilities,
		CurrentOperationID: claim.Operation.ID, LeaseToken: claim.LeaseToken,
	})
	if err != nil || !ok {
		t.Fatalf("resume: ok=%v err=%v", ok, err)
	}
	if resumed.Operation.Attempt != 1 || resumed.LeaseToken != claim.LeaseToken {
		t.Fatalf("resumed = %#v", resumed)
	}
	if _, err := store.RenewNodeOperationLease(ctx, "edge", claim.Operation.ID, "wrong", 1); !errors.Is(err, ErrInvalidOperationLease) {
		t.Fatalf("wrong lease error = %v", err)
	}
}

func TestNodeOperationEventsAreOrderedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.CreateNodeOperation(ctx, CreateNodeOperationParams{
		NodeName: "edge", Kind: "diagnostics.collect", Payload: json.RawMessage(`{}`),
		IdempotencyKey: "diagnostics:1", RequiredCapabilities: []string{"operations.v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: []string{"operations.v1"},
	})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	event := RecordNodeOperationEventParams{
		NodeName: "edge", OperationID: claim.Operation.ID, LeaseToken: claim.LeaseToken,
		Attempt: 1, Sequence: 1, Status: "running", Phase: "collecting",
		Details: json.RawMessage(`{"step":1}`),
	}
	updated, err := store.RecordNodeOperationEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Phase != "collecting" || updated.Status != "running" {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := store.RecordNodeOperationEvent(ctx, event); err != nil {
		t.Fatalf("identical duplicate: %v", err)
	}
	event.Message = "different"
	if _, err := store.RecordNodeOperationEvent(ctx, event); err == nil {
		t.Fatal("expected changed duplicate event to fail")
	}
	event.Message = ""
	event.Sequence = 3
	if _, err := store.RecordNodeOperationEvent(ctx, event); err == nil {
		t.Fatal("expected sequence gap to fail")
	}
	event.Sequence = 2
	event.Status = "succeeded"
	event.Phase = "complete"
	event.Result = json.RawMessage(`{"archive":"ready"}`)
	finished, err := store.RecordNodeOperationEvent(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "succeeded" || finished.LeaseExpiresAt != "" || finished.FinishedAt == "" {
		t.Fatalf("finished = %#v", finished)
	}
	if replayed, err := store.RecordNodeOperationEvent(ctx, event); err != nil || replayed.Status != "succeeded" {
		t.Fatalf("terminal response-loss replay = %#v, %v", replayed, err)
	}
	events, err := store.ListNodeOperationEvents(ctx, claim.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Status != "succeeded" {
		t.Fatalf("events = %#v", events)
	}
}

func TestNodeOperationCancelAndExpiredLeaseReclaim(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	queued, _, err := store.CreateNodeOperation(ctx, CreateNodeOperationParams{
		NodeName: "edge", Kind: "logs.collect", Payload: json.RawMessage(`{}`),
		IdempotencyKey: "logs:1", RequiredCapabilities: []string{"operations.v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.RequestNodeOperationCancel(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "cancelled" || !cancelled.CancelRequested {
		t.Fatalf("cancelled = %#v", cancelled)
	}
	_, _, err = store.CreateNodeOperation(ctx, CreateNodeOperationParams{
		NodeName: "edge", Kind: "logs.collect", Payload: json.RawMessage(`{}`),
		IdempotencyKey: "logs:2", RequiredCapabilities: []string{"operations.v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: []string{"operations.v1"},
	})
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if _, err := store.sql.ExecContext(ctx, `UPDATE node_operations SET lease_expires_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), first.Operation.ID); err != nil {
		t.Fatal(err)
	}
	second, ok, err := store.ClaimNodeOperation(ctx, ClaimNodeOperationParams{
		NodeName: "edge", Capabilities: []string{"operations.v1"},
	})
	if err != nil || !ok {
		t.Fatalf("reclaim: ok=%v err=%v", ok, err)
	}
	if second.Operation.ID != first.Operation.ID || second.Operation.Attempt != 2 || second.LeaseToken == first.LeaseToken {
		t.Fatalf("reclaim = %#v, first = %#v", second, first)
	}
	if _, err := store.RecordNodeOperationEvent(ctx, RecordNodeOperationEventParams{
		NodeName: "edge", OperationID: first.Operation.ID, LeaseToken: first.LeaseToken,
		Attempt: 1, Sequence: 1, Status: "running", Phase: "collecting",
	}); !errors.Is(err, ErrInvalidOperationLease) {
		t.Fatalf("stale lease event error = %v", err)
	}
}
