package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The schema snapshot is what sqlc generates against, so it must describe the
// same database the migrations build. Drift silently desynchronises codegen.
func TestSchemaSnapshotMatchesMigrations(t *testing.T) {
	objects := func(path string) []string {
		conn, err := sql.Open("sqlite3", path)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		rows, err := conn.Query(`SELECT type || ' ' || name FROM sqlite_master
			WHERE name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}

	migrated := filepath.Join(t.TempDir(), "migrated.db")
	store, err := OpenSQLite(migrated)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.Close()

	snapshotSQL, err := os.ReadFile("../../../schema/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	conn, err := sql.Open("sqlite3", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(string(snapshotSQL)); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	got, want := objects(migrated), objects(snapshot)
	if len(got) != len(want) {
		t.Fatalf("object count: migrations=%d snapshot=%d\nmigrations=%v\nsnapshot=%v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("mismatch: migrations=%q snapshot=%q", got[i], want[i])
		}
	}
}
