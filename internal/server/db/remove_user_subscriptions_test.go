package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
)

func TestRemoveUserSubscriptionsPreservesProfileLinksAndTraffic(t *testing.T) {
	ctx := context.Background()
	d, err := OpenSQLite(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpToContext(ctx, d.sql, ".", 30); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO proxy_users(id,name) VALUES('user','alice')`,
		`INSERT INTO subscription_tokens(id,proxy_user_id,token) VALUES('legacy','user','bfsub_retired')`,
		`INSERT INTO subscription_tokens(id,proxy_user_id,token,revoked_at) VALUES('revoked','user','bfsub_revoked','2026-10-01')`,
		`INSERT INTO mihomo_profiles(id,name,proxy_user_id) VALUES('profile','Desktop','user')`,
		`INSERT INTO mihomo_profile_subscription_tokens(id,profile_id,token) VALUES('profile-token','profile','bfsub_profile')`,
		`INSERT INTO traffic_usage_totals(proxy_user_id,direction,raw_bytes,billable_bytes) VALUES('user','uplink',100,150)`,
	} {
		if _, err := d.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='subscription_tokens'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("legacy table=%d err=%v", tables, err)
	}
	token, ok, err := d.VerifyMihomoProfileSubscriptionToken(ctx, "bfsub_profile")
	if err != nil || !ok || token.ProfileID != "profile" {
		t.Fatalf("profile subscription lost: ok=%v err=%v", ok, err)
	}
	var raw, billable int64
	if err := d.sql.QueryRowContext(ctx, `SELECT raw_bytes,billable_bytes FROM traffic_usage_totals WHERE proxy_user_id='user'`).Scan(&raw, &billable); err != nil || raw != 100 || billable != 150 {
		t.Fatalf("traffic changed: %d/%d err=%v", raw, billable, err)
	}
}
