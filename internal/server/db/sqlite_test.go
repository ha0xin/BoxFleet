package db

import (
	"context"
	"testing"
	"time"
)

func TestSQLitePoolAllowsConcurrentReads(t *testing.T) {
	store := openTestDB(t)
	if got := store.sql.Stats().MaxOpenConnections; got != sqliteMaxOpenConnections {
		t.Fatalf("MaxOpenConnections = %d, want %d", got, sqliteMaxOpenConnections)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reserved, err := store.sql.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reserved.Close()

	done := make(chan error, 1)
	go func() {
		_, err := store.ListNodes(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("read waited for an unrelated reserved SQLite connection")
	}
}

func TestSQLitePragmasApplyToEveryConnection(t *testing.T) {
	store := openTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	connections := make([]interface{ Close() error }, 0, sqliteMaxOpenConnections)
	for range sqliteMaxOpenConnections {
		conn, err := store.sql.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		var foreignKeys int
		if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatal(err)
		}
		if foreignKeys != 1 {
			t.Fatalf("foreign_keys = %d on connection %d", foreignKeys, len(connections))
		}
	}
	for _, conn := range connections {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}
}
