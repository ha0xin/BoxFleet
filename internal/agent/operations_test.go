package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
)

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
