package bf

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/spf13/viper"
)

func withMigratedStore(ctx context.Context, fn func(context.Context, *db.DB) error) error {
	return withStore(ctx, true, func(ctx context.Context, store *db.DB) error {
		migrateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
		defer cancel()
		if err := store.Migrate(migrateCtx); err != nil {
			return err
		}
		return fn(ctx, store)
	})
}

func withStore(ctx context.Context, ensureParent bool, fn func(context.Context, *db.DB) error) error {
	dbPath := viper.GetString("db")
	if ensureParent {
		if err := ensureParentDir(dbPath); err != nil {
			return err
		}
	}
	store, err := db.OpenSQLite(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return fn(ctx, store)
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
