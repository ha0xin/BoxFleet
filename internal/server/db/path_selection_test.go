package db

import (
	"context"
	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
	"testing"
)

func TestPathSelectionAtomicAndIndependentOfGroups(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "shortcut", p[0], p[1])
	if err := d.ChangeUserPathSelection(ctx, u.ID, []PathSelectionChange{{p[0].ID, true}, {p[1].ID, true}}); err != nil {
		t.Fatal(err)
	}
	assertGroupPaths(t, d, u.ID, p[0], p[1])
	if err := d.ChangeUserPathSelection(ctx, u.ID, []PathSelectionChange{{p[0].ID, false}}); err != nil {
		t.Fatal(err)
	}
	assertGroupPaths(t, d, u.ID, p[1])
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: g.ID, Revision: g.Revision, Name: g.Name, Enabled: true, PathIDs: []string{p[2].ID}})
	assertGroupPaths(t, d, u.ID, p[1])
	before, err := d.q.PathGroupPreviewState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`CREATE TRIGGER selection_test_failure BEFORE INSERT ON path_accesses BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = d.ChangeUserPathSelection(ctx, u.ID, []PathSelectionChange{{p[1].ID, false}, {p[2].ID, true}}); err == nil {
		t.Fatal("accepted partially failed batch")
	}
	after, err := d.q.PathGroupPreviewState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("failed batch changed grants, projection, credentials or bindings")
	}
}

func TestPathShortcutMigrationPreservesLegacyEffectiveSelection(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	goose.SetBaseFS(migrations.FS)
	if err := goose.DownToContext(ctx, d.sql, ".", 34); err != nil {
		t.Fatal(err)
	}
	u, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "legacy", p[0], p[1])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	if _, err := d.GrantPathToUser(ctx, u.ID, p[2].ID); err != nil {
		t.Fatal(err)
	}
	before, err := d.q.ListUserDesiredPathIDs(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := d.q.ListUserDesiredPathIDs(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("migration changed selection: %v => %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("migration changed selection")
		}
	}
	sources, err := d.UserPathSources(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if !s.Direct || len(s.Groups) > 0 {
			t.Fatal("legacy sources were not flattened")
		}
	}
	applyGroup(t, d, PathGroupChange{Operation: "delete", GroupID: g.ID, Revision: g.Revision})
	assertGroupPaths(t, d, u.ID, p[0], p[1], p[2])
	groups, err := d.q.ListPathGroupGrants(ctx)
	if err != nil || len(groups) != 0 {
		t.Fatalf("group sources remain: %+v %v", groups, err)
	}
}
