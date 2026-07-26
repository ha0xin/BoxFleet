package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
)

func TestOperationExecutionErrorPreservesExecutorFailureDuringCleanup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	executorErr := errors.New("candidate sing-box version mismatch")

	got := operationExecutionError(ctx, executorErr)
	cancel(nil)

	if !errors.Is(got, executorErr) {
		t.Fatalf("operationExecutionError() = %v, want executor error %v", got, executorErr)
	}
}

func TestOperationExecutionErrorUsesExistingCancellationCause(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(context.Background())
	leaseErr := errors.New("operation lease lost")
	cancel(leaseErr)

	got := operationExecutionError(ctx, errors.New("command interrupted"))
	if !errors.Is(got, leaseErr) {
		t.Fatalf("operationExecutionError() = %v, want cancellation cause %v", got, leaseErr)
	}
}

func TestFinalLeaseErrorKeepsCommittedUpdateSuccessful(t *testing.T) {
	t.Parallel()
	committed := map[string]any{"committed": true}
	renewErr := errors.New("renew lease: connection refused")

	if err := finalLeaseError(committed, false, renewErr); err != nil {
		t.Fatalf("committed update reported as failed: %v", err)
	}
	if err := finalLeaseError(committed, true, nil); err != nil {
		t.Fatalf("committed update reported as cancelled: %v", err)
	}
	uncommitted := map[string]any{"committed": false}
	if err := finalLeaseError(uncommitted, false, renewErr); !errors.Is(err, renewErr) {
		t.Fatalf("uncommitted renewal failure = %v, want %v", err, renewErr)
	}
	if err := finalLeaseError(uncommitted, true, nil); !errors.Is(err, errOperationCancelled) {
		t.Fatalf("uncommitted cancellation = %v, want %v", err, errOperationCancelled)
	}
	if err := finalLeaseError(uncommitted, false, nil); err != nil {
		t.Fatalf("clean renewal = %v", err)
	}
}

func TestOperationEventOutboxRetriesExactPayload(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/operations/op_outbox/events" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), raw...))
		attempt := len(bodies)
		mu.Unlock()
		if attempt == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	statePath := filepath.Join(t.TempDir(), "operation-state.json")
	a := New(Config{
		NodeName: "edge", Token: "token", ServerURL: server.URL,
		OperationStatePath: statePath,
	})
	state := &OperationState{Assignment: model.NodeOperationAssignment{
		ID: "op_outbox", Kind: "logs.collect", Attempt: 1, LeaseToken: "lease",
	}}
	if err := a.reportOperationEventWithRetry(context.Background(), state, model.NodeOperationEventReport{
		Status: "running", Phase: "collecting", Message: "collecting logs",
		ReportedAt: "2026-07-22T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatalf("event retry bodies were not exact: %q", bodies)
	}
	loaded, err := a.LoadOperationState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.LastSequence != 1 || loaded.PendingEvent != nil || loaded.Phase != "collecting" {
		t.Fatalf("persisted outbox state = %+v", loaded)
	}
}
