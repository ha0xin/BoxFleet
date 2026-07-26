package db

import (
	"context"
	"strings"
	"testing"
	"time"

	sqlcstore "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

func TestSQLiteDSNTakesWriteLocksUpFront(t *testing.T) {
	dsn := sqliteDSN("/var/lib/boxfleet/bf.db")
	for _, want := range []string{
		"_txlock=immediate",
		"_foreign_keys=on",
		"_journal_mode=WAL",
		"_synchronous=NORMAL",
		"_busy_timeout=5000",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("DSN %q is missing %q", dsn, want)
		}
	}
}

// A deferred transaction that reads before it writes fails the write with
// SQLITE_BUSY_SNAPSHOT the moment another connection commits in between, and
// busy_timeout does not retry that. Immediate transactions make the other
// writer wait instead.
func TestWithTxSurvivesConcurrentCommitBetweenReadAndWrite(t *testing.T) {
	store := openTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.CreateNode(ctx, "edge-a", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}

	contend := make(chan struct{})
	contended := make(chan error, 1)
	go func() {
		<-contend
		contended <- store.SetNetworkEventRetentionDays(ctx, 30)
	}()

	err := store.withTx(ctx, func(q *sqlcstore.Queries) error {
		if _, err := q.ListNodes(ctx); err != nil {
			return err
		}
		close(contend)
		time.Sleep(100 * time.Millisecond)
		return q.UpsertSetting(ctx, sqlcstore.UpsertSettingParams{
			Key:       "boxfleet_tx_probe",
			ValueJson: "1",
		})
	})
	if err != nil {
		t.Fatalf("read-then-write transaction failed under a concurrent writer: %v", err)
	}
	if err := <-contended; err != nil {
		t.Fatalf("concurrent writer failed: %v", err)
	}
}

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
