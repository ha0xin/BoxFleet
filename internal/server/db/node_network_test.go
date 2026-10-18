package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
)

func TestNetworkInventoryCurrentState(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	n, err := store.CreateNode(ctx, "nat", "entry.example")
	if err != nil {
		t.Fatal(err)
	}
	report := model.NetworkSnapshot{Revision: 1, Complete: true, CollectedAt: time.Now().UTC().Format(time.RFC3339Nano), Addresses: []model.NodeAddress{
		{Address: "10.0.0.2", Interface: "eth0", InterfaceIndex: 2, PrefixLength: 24, InterfaceUp: true},
		{Address: "10.0.0.3", Interface: "eth0", InterfaceIndex: 2, PrefixLength: 24, InterfaceUp: true},
		{Address: "2001:db8::2", Interface: "eth0", InterfaceIndex: 2, PrefixLength: 64, InterfaceUp: true},
		{Address: "2001:db8::3", Interface: "eth0", InterfaceIndex: 2, PrefixLength: 64, InterfaceUp: true},
	}}
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); err != nil {
		t.Fatalf("lost ACK retry: %v", err)
	}
	network, err := store.NodeNetwork(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(network.Addresses) != 4 || network.Stale || network.AddressCount != 4 {
		t.Fatalf("network: %+v", network)
	}
	initialReceived := network.ReceivedAt
	report.Revision = 2
	report.Complete = false
	report.Error = "dump interrupted"
	report.Addresses = nil
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); err != nil {
		t.Fatal(err)
	}
	network, err = store.NodeNetwork(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(network.Addresses) != 4 || network.Error != "dump interrupted" || network.ReceivedAt != initialReceived {
		t.Fatalf("failed scan erased success: %+v", network)
	}
	report.Revision = 1
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); !errors.Is(err, ErrNetworkRevision) {
		t.Fatalf("old revision: %v", err)
	}
	report.Revision = 2
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	report.Error = "different error"
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); !errors.Is(err, ErrNetworkRevision) {
		t.Fatalf("conflicting revision: %v", err)
	}
	report.Revision = 3
	report.Error = ""
	report.Complete = true
	report.Addresses = []model.NodeAddress{}
	if err := store.RecordNetworkSnapshot(ctx, n.ID, report); err != nil {
		t.Fatal(err)
	}
	network, err = store.NodeNetwork(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(network.Addresses) != 0 || network.Error != "" || network.Revision != 3 {
		t.Fatalf("empty successful scan: %+v", network)
	}
	// Observations must not become client publication addresses.
	node, err := store.GetNode(ctx, n.ID)
	if err != nil || node.PublicHost != "entry.example" {
		t.Fatalf("entry changed: %+v %v", node, err)
	}
}

func TestNetworkMultiplePublicSourcesAndNodeIdentity(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	n, err := store.CreateNode(ctx, "nat", "entry.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"198.51.100.1", "198.51.100.2", "2001:db8::1", "2001:db8::2"} {
		if err := store.RecordObservedIP(ctx, n.ID, ObservedIP{Address: address, Source: "cloudflare"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordObservedIP(ctx, n.ID, ObservedIP{Address: "198.51.100.1", Source: "cloudflare"}); err != nil {
		t.Fatal(err)
	}
	network, err := store.NodeNetwork(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(network.ObservedIPs) != 4 {
		t.Fatalf("same-family sources overwritten: %+v", network.ObservedIPs)
	}
	// Expire only old samples when another valid observation arrives. This is
	// a bounded current set, not an ever-growing history of former NAT exits.
	if _, err := store.sql.ExecContext(ctx, "UPDATE node_observed_ips SET observed_at=? WHERE node_id=? AND address=?", time.Now().Add(-11*time.Minute).UTC().Format(time.RFC3339Nano), n.ID, "198.51.100.1"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordObservedIP(ctx, n.ID, ObservedIP{Address: "198.51.100.2", Source: "cloudflare"}); err != nil {
		t.Fatal(err)
	}
	network, err = store.NodeNetwork(ctx, n.ID)
	if err != nil || len(network.ObservedIPs) != 3 {
		t.Fatalf("expired observation retained: %+v %v", network, err)
	}
	for _, address := range []string{"127.0.0.1", "172.19.0.1", "240.0.0.1", "100.64.0.1"} {
		if err := store.RecordObservedIP(ctx, n.ID, ObservedIP{Address: address, Source: "direct"}); err == nil {
			t.Fatalf("accepted non-public %s", address)
		}
	}
	if _, err := store.SoftDeleteNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.CreateNode(ctx, "nat", "new.example")
	if err != nil {
		t.Fatal(err)
	}
	network, err = store.NodeNetwork(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if network.Revision != 0 || len(network.ObservedIPs) != 0 {
		t.Fatalf("new node inherited state: %+v", network)
	}
}

func TestNetworkInventoryValidation(t *testing.T) {
	base := model.NetworkSnapshot{Revision: 1, Complete: true, CollectedAt: "2026-10-18T00:00:00Z", Addresses: []model.NodeAddress{{Address: "2001:db8::1", PrefixLength: 64, InterfaceIndex: 2, Interface: "eth0"}}}
	for _, change := range []func(*model.NetworkSnapshot){
		func(r *model.NetworkSnapshot) { r.Revision = 0 },
		func(r *model.NetworkSnapshot) { r.CollectedAt = "bad" },
		func(r *model.NetworkSnapshot) { r.Complete = false },
		func(r *model.NetworkSnapshot) { r.Error = "not complete" },
		func(r *model.NetworkSnapshot) { r.Addresses = append(r.Addresses, r.Addresses[0]) },
		func(r *model.NetworkSnapshot) {
			r.Addresses = []model.NodeAddress{{Address: "2001:db8::1%eth0", InterfaceIndex: 2, Interface: "eth0", PrefixLength: 64}}
		},
	} {
		report := base
		change(&report)
		if _, err := normalizeNetworkSnapshot(report); err == nil {
			t.Fatalf("accepted invalid snapshot: %+v", report)
		}
	}
}
