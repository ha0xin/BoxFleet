package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
)

func TestNetworkInventoryResyncAndFailedCollection(t *testing.T) {
	var revision atomic.Int64
	revision.Store(50)
	var getCount atomic.Int32
	reports := make(chan model.NetworkSnapshot, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("X-BoxFleet-Node") != "edge" {
			t.Error("missing node credentials")
		}
		if r.Method == http.MethodGet {
			getCount.Add(1)
			_ = json.NewEncoder(w).Encode(model.NetworkSnapshotAck{Revision: revision.Load()})
			return
		}
		var report model.NetworkSnapshot
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		// Simulate another already-committed report on the first attempt.
		if report.Revision == 51 {
			revision.Store(60)
			w.WriteHeader(409)
			return
		}
		revision.Store(report.Revision)
		reports <- report
		_ = json.NewEncoder(w).Encode(model.NetworkSnapshotAck{Revision: report.Revision})
	}))
	defer server.Close()
	a := &Agent{Config: Config{NodeName: "edge", Token: "test", ServerURL: server.URL, StatePath: filepath.Join(t.TempDir(), "state.json")}, Client: server.Client()}
	events := make(chan struct{}, 1)
	var fail atomic.Bool
	scan := func() ([]model.NodeAddress, error) {
		if fail.Load() {
			return nil, errors.New("dump interrupted")
		}
		return []model.NodeAddress{
			{Address: "10.0.0.2", Family: 4, Interface: "eth0", InterfaceIndex: 2, PrefixLength: 24},
			{Address: "10.0.0.3", Family: 4, Interface: "eth0", InterfaceIndex: 2, PrefixLength: 24},
			{Address: "2001:db8::2", Family: 6, Interface: "eth0", InterfaceIndex: 2, PrefixLength: 64},
			{Address: "2001:db8::3", Family: 6, Interface: "eth0", InterfaceIndex: 2, PrefixLength: 64},
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runNetworkInventory(ctx, scan, func(context.Context) <-chan struct{} { return events })
	}()
	await := func() model.NetworkSnapshot {
		t.Helper()
		select {
		case r := <-reports:
			return r
		case <-time.After(5 * time.Second):
			t.Fatal("missing inventory report")
			return model.NetworkSnapshot{}
		}
	}
	first := await()
	if first.Revision != 61 || !first.Complete || len(first.Addresses) != 4 || getCount.Load() != 2 {
		t.Fatalf("resync/full inventory: %+v, GET count %d", first, getCount.Load())
	}
	// Changing inventory does not touch the cumulative traffic state file.
	if _, err := os.Stat(a.Config.StatePath); !os.IsNotExist(err) {
		t.Fatalf("traffic state touched: %v", err)
	}
	fail.Store(true)
	events <- struct{}{}
	failed := await()
	if failed.Complete || failed.Error != "dump interrupted" || len(failed.Addresses) != 0 {
		t.Fatalf("failed scan: %+v", failed)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not cancel")
	}
	data, err := os.ReadFile(a.Config.StatePath + ".network")
	if err != nil {
		t.Fatal(err)
	}
	var state networkInventoryState
	if json.Unmarshal(data, &state) != nil || state.Revision != failed.Revision {
		t.Fatalf("invalid durable revision: %s", data)
	}
}

func TestNetworkInventoryCancellationDuringRequest(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	a := &Agent{Config: Config{NodeName: "edge", ServerURL: server.URL, StatePath: filepath.Join(t.TempDir(), "state")}, Client: server.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runNetworkInventory(ctx, func() ([]model.NodeAddress, error) { return []model.NodeAddress{}, nil }, func(context.Context) <-chan struct{} { return nil })
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request blocked cancellation")
	}
}
