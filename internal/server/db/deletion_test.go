package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
)

func TestDeletionCascadeAndPreviewPreserveSharedCredentials(t *testing.T) {
	for _, mode := range []string{"node", "proxy", "migration"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			var d *DB
			if mode == "migration" {
				var err error
				d, err = OpenSQLite(filepath.Join(t.TempDir(), "old.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { d.Close() })
				goose.SetDialect("sqlite3")
				goose.SetLogger(goose.NopLogger())
				goose.SetBaseFS(migrations.FS)
				if err := goose.UpToContext(ctx, d.sql, ".", 28); err != nil {
					t.Fatal(err)
				}
			} else {
				d = openTestDB(t)
			}
			nodes := []Node{}
			proxies := []Proxy{}
			endpoints := []Endpoint{}
			paths := []Path{}
			for i, name := range []string{"entry", "middle", "exit"} {
				n, err := d.CreateNode(ctx, name, "192.0.2.1", "")
				if err != nil {
					t.Fatal(err)
				}
				nodes = append(nodes, n)
				p, err := d.CreateProxy(ctx, CreateProxyParams{NodeName: n.ID, Name: name + "-proxy", Protocol: ProtocolVLESSReality, ListenPort: 443 + i, Enabled: true, SettingsJSON: `{}`})
				if err != nil {
					t.Fatal(err)
				}
				proxies = append(proxies, p)
				e, err := d.EnsureEndpoint(ctx, p.ID, n.Hosts[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				endpoints = append(endpoints, e)
				dialer := ""
				if i > 0 {
					dialer = paths[i-1].ID
				}
				path, err := d.CreatePath(ctx, CreatePathParams{Name: name + "-chain", EndpointID: e.ID, DialerPathID: dialer, Enabled: true, Visibility: PathVisibilitySelectable})
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			independent, err := d.CreatePath(ctx, CreatePathParams{Name: "independent", EndpointID: endpoints[2].ID, Enabled: true, Visibility: PathVisibilitySelectable})
			if err != nil {
				t.Fatal(err)
			}
			users := []ProxyUser{}
			for _, name := range []string{"alice", "bob"} {
				u, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: name, GlobalQuotaBytes: 1234})
				if err != nil {
					t.Fatal(err)
				}
				users = append(users, u)
				if _, err := d.GrantPathToUser(ctx, name, paths[2].ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.GrantPathToUser(ctx, "bob", independent.ID); err != nil {
				t.Fatal(err)
			}
			oldCredential, err := d.getProxyCredentialByIDs(ctx, users[0].ID, proxies[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := d.RecordTrafficReport(ctx, TrafficReport{NodeName: nodes[0].Name, Sequence: 1, AgentBootID: "test", Deltas: []TrafficDelta{{AuthName: oldCredential.AuthName, Direction: "uplink", RawBytesDelta: 100, CounterValue: 100}}}); err != nil {
				t.Fatal(err)
			}

			blocked, err := d.ResourceDeletionImpact(ctx, "path", paths[0].ID)
			if err != nil || blocked.Blocked == "" {
				t.Fatalf("dependency deletion not blocked: %+v %v", blocked, err)
			}
			kind, id := "node", nodes[0].ID
			if mode == "proxy" {
				kind, id = "proxy", proxies[0].ID
			}
			preview, err := d.ResourceDeletionImpact(ctx, kind, id)
			if err != nil {
				t.Fatal(err)
			}
			predicted := map[string]string{}
			for _, item := range preview.Items {
				predicted[item.ID] = item.Effect
			}
			for _, path := range paths {
				if predicted[path.ID] != "Disabled" {
					t.Fatalf("missing dependent path %s: %+v", path.Name, preview)
				}
			}
			if predicted[oldCredential.ID] != "Archived" {
				t.Fatal("retired credential missing from preview")
			}
			for _, u := range users {
				for i, p := range proxies {
					if i == 0 {
						continue
					}
					c, err := d.getProxyCredentialByIDs(ctx, u.ID, p.ID)
					if err != nil {
						t.Fatal(err)
					}
					_, included := predicted[c.ID]
					if included != (u.Name == "alice" || i == 1) {
						t.Fatalf("wrong credential preview for %s/%s", u.Name, p.Name)
					}
				}
			}

			if _, ok := predicted[independent.ID]; ok {
				t.Fatal("unrelated path included")
			}
			if mode != "migration" {
				if _, err := d.sql.ExecContext(ctx, `CREATE TRIGGER fail_cascade BEFORE UPDATE OF deleted_at ON path_accesses BEGIN SELECT RAISE(ABORT,'cascade failure'); END`); err != nil {
					t.Fatal(err)
				}
				if mode == "node" {
					_, err = d.SoftDeleteNode(ctx, nodes[0].ID)
				} else {
					_, err = d.SoftDeleteProxy(ctx, nodes[0].ID, proxies[0].ID)
				}
				if err == nil {
					t.Fatal("partial cascade accepted")
				}
				if _, err := d.GetNode(ctx, nodes[0].ID); err != nil {
					t.Fatal("node retirement escaped rollback")
				}
				if _, err := d.GetProxyByID(ctx, proxies[0].ID); err != nil {
					t.Fatal("proxy retirement escaped rollback")
				}
				root, err := d.GetPath(ctx, paths[2].ID)
				if err != nil || !root.Enabled {
					t.Fatal("path disable escaped rollback")
				}
				if _, err := d.sql.ExecContext(ctx, "DROP TRIGGER fail_cascade"); err != nil {
					t.Fatal(err)
				}
			}

			switch mode {
			case "node":
				_, err = d.SoftDeleteNode(ctx, nodes[0].ID)
			case "proxy":
				_, err = d.SoftDeleteProxy(ctx, nodes[0].ID, proxies[0].ID)
			case "migration":
				_, err = d.q.SoftDeleteNode(ctx, nodes[0].ID)
				if err == nil {
					err = d.Migrate(ctx)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				got, err := d.GetPath(ctx, path.ID)
				if err != nil || got.Enabled {
					t.Fatalf("path still active: %+v %v", got, err)
				}
			}
			got, err := d.GetPath(ctx, independent.ID)
			if err != nil || !got.Enabled {
				t.Fatal("independent path disabled")
			}
			for _, u := range users {
				grants, err := d.ListActivePathAccessesByUser(ctx, u.ID)
				if err != nil {
					t.Fatal(err)
				}
				expected := 0
				if u.Name == "bob" {
					expected = 1
				}
				if len(grants) != expected {
					t.Fatalf("%s grants=%d", u.Name, len(grants))
				}
				for i, p := range proxies {
					credential, err := d.getProxyCredentialByIDs(ctx, u.ID, p.ID)
					if err != nil {
						t.Fatal(err)
					}
					expectedEnabled := u.Name == "bob" && i == 2
					if credential.Enabled != expectedEnabled {
						t.Fatalf("%s/%s enabled=%v", u.Name, p.Name, credential.Enabled)
					}
					if i == 0 && !credential.DeletedAt.Valid {
						t.Fatal("retired credential not archived")
					}
					if i > 0 && credential.DeletedAt.Valid {
						t.Fatal("live proxy credential was archived")
					}
				}
				fresh, err := d.GetProxyUser(ctx, u.ID)
				if err != nil || fresh.GlobalQuotaBytes != 1234 {
					t.Fatal("user or quota changed")
				}
			}
			endpoint, err := d.GetEndpoint(ctx, endpoints[0].ID)
			if err != nil || endpoint.Enabled {
				t.Fatal("retired endpoint active")
			}
			archivedCredential, err := d.getProxyCredentialByIDs(ctx, users[0].ID, proxies[0].ID)
			if err != nil || archivedCredential.CredentialJSON != oldCredential.CredentialJSON || archivedCredential.AuthName != oldCredential.AuthName {
				t.Fatal("credential history changed")
			}
			if mode == "proxy" {
				if _, err := d.RestoreProxy(ctx, nodes[0].ID, proxies[0].ID); err != nil {
					t.Fatal(err)
				}
				restoredEndpoint, err := d.GetEndpoint(ctx, endpoints[0].ID)
				if err != nil || !restoredEndpoint.Enabled {
					t.Fatal("restored endpoint unusable")
				}
				restoredPath, err := d.GetPath(ctx, paths[0].ID)
				if err != nil || restoredPath.Enabled {
					t.Fatal("restore unexpectedly re-enabled path")
				}
				grants, err := d.ListActivePathAccessesByUser(ctx, users[0].ID)
				if err != nil || len(grants) != 0 {
					t.Fatal("restore resurrected grants")
				}
			}

			if mode != "proxy" {
				bindings, err := d.q.ListUserNodeBindings(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range bindings {
					if b.NodeID == nodes[0].ID && b.Enabled != 0 {
						t.Fatal("retired node binding active")
					}
				}
			}

			var violations int
			if err := d.sql.QueryRowContext(ctx, "SELECT count(*) FROM pragma_foreign_key_check").Scan(&violations); err != nil || violations != 0 {
				t.Fatalf("foreign key violations=%d: %v", violations, err)
			}
			var total int64
			if err := d.sql.QueryRowContext(ctx, "SELECT SUM(raw_bytes) FROM traffic_usage_totals").Scan(&total); err != nil || total != 100 {
				t.Fatalf("traffic history lost: %d %v", total, err)
			}

		})
	}
}
