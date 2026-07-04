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
			{Host: "203.0.113.5", Tag: "v4", Selected: false},
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
			{Host: "b.example", Tag: "backup", Selected: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !node.Hosts[0].Selected || node.Hosts[1].Selected {
		t.Fatalf("hosts = %#v, want only the first auto-selected", node.Hosts)
	}
}

func TestRenameNodeAliasesAndConflicts(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	first, err := store.CreateNode(ctx, "edge-a", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "edge-b", "192.0.2.2", ""); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, first.Name)
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := store.RenameNode(ctx, "edge-a", "edge-primary")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != first.ID || renamed.Name != "edge-primary" {
		t.Fatalf("renamed node = %#v", renamed)
	}
	byAlias, err := store.GetNode(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	if byAlias.ID != first.ID || byAlias.Name != "edge-primary" {
		t.Fatalf("old alias resolved to %#v", byAlias)
	}
	canonical, ok, err := store.AuthenticateNodeToken(ctx, "edge-a", issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || canonical != "edge-primary" {
		t.Fatalf("authenticate = (%q, %v), want (edge-primary, true)", canonical, ok)
	}

	if _, err := store.RenameNode(ctx, "edge-primary", "edge-b"); err == nil {
		t.Fatal("rename accepted another node's canonical name")
	}
	if _, err := store.CreateNode(ctx, "edge-a", "192.0.2.3", ""); err == nil {
		t.Fatal("create accepted a reserved node alias")
	}

	renamedBack, err := store.RenameNode(ctx, "edge-primary", "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	if renamedBack.ID != first.ID || renamedBack.Name != "edge-a" {
		t.Fatalf("rename back = %#v", renamedBack)
	}
	if got, err := store.GetNode(ctx, "edge-primary"); err != nil || got.ID != first.ID {
		t.Fatalf("intermediate name did not become alias: got %#v, err %v", got, err)
	}
}

func TestUpdateNodeHostTagValidationAndLegacyRead(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	node, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name:   node.Name,
		Status: node.Status,
		Hosts: []NodeHost{
			{Host: "a.example", Selected: true},
			{Host: "b.example"},
		},
	}); err == nil {
		t.Fatal("additional host without tag was accepted")
	}
	if _, err := store.UpdateNode(ctx, UpdateNodeParams{
		Name:   node.Name,
		Status: node.Status,
		Hosts: []NodeHost{
			{Host: "a.example", Tag: "V6", Selected: true},
			{Host: "b.example", Tag: "v6"},
		},
	}); err == nil {
		t.Fatal("case-insensitive duplicate host tag was accepted")
	}

	if _, err := store.sql.ExecContext(ctx,
		`UPDATE nodes SET hosts_json = ? WHERE id = ?`,
		`[{"host":"a.example","selected":true},{"host":"b.example","selected":true}]`,
		node.ID,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetNode(ctx, node.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Hosts) != 2 || legacy.Hosts[1].Tag != "" {
		t.Fatalf("legacy hosts = %#v", legacy.Hosts)
	}
}

func TestUpdateNodeByNameRenamesAndUpdatesAtomically(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	original, err := store.CreateNode(ctx, "edge", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateNodeByName(ctx, original.Name, UpdateNodeParams{
		Name:       "edge-renamed",
		Hosts:      []NodeHost{{Host: "edge.example", Selected: true}},
		APIBaseURL: "https://edge.example/api",
		Status:     "degraded",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Name != "edge-renamed" ||
		updated.PublicHost != "edge.example" || updated.Status != "degraded" {
		t.Fatalf("updated node = %#v", updated)
	}
	viaOldName, err := store.GetNode(ctx, original.Name)
	if err != nil {
		t.Fatal(err)
	}
	if viaOldName.ID != original.ID || viaOldName.Name != updated.Name {
		t.Fatalf("old name resolved to %#v", viaOldName)
	}
}
