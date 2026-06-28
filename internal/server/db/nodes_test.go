package db

import (
	"context"
	"testing"
)

func TestCreateNodeStartsSingleSelectedHost(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	if _, err := store.CreateNode(ctx, "edge", "1.2.3.4", ""); err != nil {
		t.Fatal(err)
	}
	node, err := store.GetNode(ctx, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if node.PublicHost != "1.2.3.4" {
		t.Fatalf("public_host = %q", node.PublicHost)
	}
	if len(node.Hosts) != 1 || node.Hosts[0].Host != "1.2.3.4" || !node.Hosts[0].Selected {
		t.Fatalf("hosts = %#v, want one selected primary", node.Hosts)
	}
}

func TestUpdateNodeMultiHostMirrorsPrimaryAndDedupes(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	if _, err := store.CreateNode(ctx, "edge", "1.2.3.4", ""); err != nil {
		t.Fatal(err)
	}
	// Blanks dropped, duplicates collapsed (first wins), order preserved, and
	// public_host mirrors the first host.
	node, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name:   "edge",
		Status: "active",
		Hosts: []NodeHost{
			{Host: "  example.net ", Selected: true},
			{Host: "", Selected: true},
			{Host: "203.0.113.5", Selected: false},
			{Host: "example.net", Selected: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.PublicHost != "example.net" {
		t.Fatalf("public_host = %q, want example.net", node.PublicHost)
	}
	if len(node.Hosts) != 2 {
		t.Fatalf("hosts = %#v, want 2 after dedup/blank drop", node.Hosts)
	}
	if node.Hosts[0].Host != "example.net" || !node.Hosts[0].Selected {
		t.Fatalf("primary host = %#v", node.Hosts[0])
	}
	if node.Hosts[1].Host != "203.0.113.5" || node.Hosts[1].Selected {
		t.Fatalf("second host = %#v", node.Hosts[1])
	}

	// Reload to confirm hosts_json persisted, not just returned.
	reloaded, err := store.GetNode(ctx, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Hosts) != 2 || reloaded.Hosts[0].Host != "example.net" {
		t.Fatalf("reloaded hosts = %#v", reloaded.Hosts)
	}
}

func TestUpdateNodeAutoSelectsFirstWhenNoneSelected(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	if _, err := store.CreateNode(ctx, "edge", "1.2.3.4", ""); err != nil {
		t.Fatal(err)
	}
	node, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name:   "edge",
		Status: "active",
		Hosts: []NodeHost{
			{Host: "a.example", Selected: false},
			{Host: "b.example", Selected: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !node.Hosts[0].Selected || node.Hosts[1].Selected {
		t.Fatalf("hosts = %#v, want only the first auto-selected", node.Hosts)
	}
}
