package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
)

func TestRemoveNodeAPIBaseURLPreservesNodeHistory(t *testing.T) {
	ctx := context.Background()
	d, err := OpenSQLite(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	goose.SetDialect("sqlite3")
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpToContext(ctx, d.sql, ".", 32); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO nodes(id,name,public_host,hosts_json,api_base_url,status,sing_box_version,deleted_at) VALUES
		 ('active','edge','192.0.2.1','[{"id":"host","host":"192.0.2.1","selected":true}]','https://unused.example','active','1.14.2',NULL),
		 ('retired','old-edge','192.0.2.2','[]','https://old-unused.example','disabled','1.13.14','2026-01-01T00:00:00Z')`,
		`INSERT INTO node_name_aliases(alias,node_id) VALUES('edge-alias','active')`,
		`INSERT INTO node_tokens(id,node_id,token_hash) VALUES('token','active','preserved-hash')`,
		`INSERT INTO proxies(id,node_id,name,protocol,listen_port) VALUES('proxy','active','proxy','vless_reality',443)`,
		`INSERT INTO config_versions(id,node_id,version,status,config_json,config_hash) VALUES('config','retired',1,'published','{}','preserved-config-hash')`,
		`INSERT INTO proxy_users(id,name) VALUES('user','alice')`,
		`INSERT INTO traffic_usage_totals(proxy_user_id,direction,raw_bytes,billable_bytes) VALUES('user','uplink',100,150)`,
	} {
		if _, err := d.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	// Compare all retained node columns, including historical identities and timestamps.
	const nodeSnapshot = `SELECT json_group_array(json_object('id',id,'name',name,'host',public_host,'hosts',hosts_json,'status',status,'version',sing_box_version,'seen',last_seen_at,'deleted',deleted_at,'created',created_at,'updated',updated_at)) FROM (SELECT * FROM nodes ORDER BY id)`
	var before, after string
	if err := d.sql.QueryRowContext(ctx, nodeSnapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.sql.QueryRowContext(ctx, nodeSnapshot).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("node history changed: before=%s after=%s", before, after)
	}
	var removedColumns int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('nodes') WHERE name='api_base_url'`).Scan(&removedColumns); err != nil || removedColumns != 0 {
		t.Fatalf("removed column count=%d err=%v", removedColumns, err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM node_name_aliases WHERE alias='edge-alias' AND node_id='active'`,
		`SELECT COUNT(*) FROM node_tokens WHERE id='token' AND node_id='active' AND token_hash='preserved-hash'`,
		`SELECT COUNT(*) FROM proxies WHERE id='proxy' AND node_id='active' AND listen_port=443`,
		`SELECT COUNT(*) FROM config_versions WHERE id='config' AND node_id='retired' AND config_hash='preserved-config-hash'`,
		`SELECT COUNT(*) FROM traffic_usage_totals WHERE proxy_user_id='user' AND raw_bytes=100 AND billable_bytes=150`,
	} {
		var count int
		if err := d.sql.QueryRowContext(ctx, query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("history check %q: count=%d err=%v", query, count, err)
		}
	}
	var violations int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&violations); err != nil || violations != 0 {
		t.Fatalf("foreign key violations=%d err=%v", violations, err)
	}
	if err := goose.DownToContext(ctx, d.sql, ".", 32); err == nil {
		t.Fatal("downgrade must refuse to recreate an incompatible, empty column")
	}
	if err := d.sql.QueryRowContext(ctx, nodeSnapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("rejected downgrade changed node history: after=%s err=%v", after, err)
	}
}
