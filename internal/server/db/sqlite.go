package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
	"github.com/haoxin/boxfleet/migrations"
)

const sqliteDriver = "sqlite3"

type DB struct {
	sql *sql.DB
	q   *store.Queries
}

func (db *DB) withTx(ctx context.Context, fn func(*store.Queries) error) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	qtx := db.q.WithTx(tx)
	if err := fn(qtx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type MigrationStatus struct {
	CurrentVersion int
	LatestVersion  int
	Pending        []Migration
}

type Migration struct {
	Version int
	Name    string
	Path    string
	SQL     string
}

func OpenSQLite(path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	sqlDB, err := sql.Open(sqliteDriver, path)
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)

	db := &DB{sql: sqlDB, q: store.New(sqlDB)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.configure(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) configure(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.sql.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return nil
}

func (db *DB) Migrate(ctx context.Context) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	return goose.UpContext(ctx, db.sql, ".")
}

func (db *DB) Status(ctx context.Context) (MigrationStatus, error) {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return MigrationStatus{}, err
	}
	all, err := LoadMigrations()
	if err != nil {
		return MigrationStatus{}, err
	}
	status := MigrationStatus{}
	if len(all) > 0 {
		status.LatestVersion = all[len(all)-1].Version
	}
	current, err := goose.GetDBVersionContext(ctx, db.sql)
	if err != nil {
		return MigrationStatus{}, err
	}
	if current < 0 {
		current = 0
	}
	status.CurrentVersion = int(current)
	for _, migration := range all {
		if migration.Version > status.CurrentVersion {
			status.Pending = append(status.Pending, migration)
		}
	}
	return status, nil
}

func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, ok := parseMigrationName(entry.Name())
		if !ok {
			continue
		}
		content, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{
			Version: version,
			Name:    name,
			Path:    entry.Name(),
			SQL:     string(content),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version < out[j].Version
	})
	return out, nil
}

func parseMigrationName(filename string) (int, string, bool) {
	base := strings.TrimSuffix(filename, ".sql")
	parts := strings.SplitN(base, "_", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", false
	}
	return version, parts[1], true
}
