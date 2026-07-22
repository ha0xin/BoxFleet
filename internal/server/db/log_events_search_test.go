package db

import (
	"context"
	"testing"
)

func TestNetworkEventSearchIndexTracksLifecycleAndRenames(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	statements := []string{
		`INSERT INTO proxy_users (id, name) VALUES ('user-search', 'alice-search')`,
		`INSERT INTO nodes (id, name, public_host) VALUES ('node-search', 'edge-search', '192.0.2.1')`,
		`INSERT INTO log_events (id, node_id, proxy_user_id, auth_name, source_ip, target_host, target_port, action, raw_message, aggregate_key, window_start, window_end) VALUES ('event-search', 'node-search', 'user-search', 'alice-auth', '198.51.100.7', 'api.example.com', 443, 'outbound_connect', 'connection accepted by gateway', 'search-key', '2026-07-22T00:00:00Z', '2026-07-22T00:00:01Z')`,
	}
	for _, statement := range statements {
		if _, err := db.sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	assertNetworkEventSearchTotal(t, db, "example.com", 1)
	assertNetworkEventSearchTotal(t, db, "exam", 1)
	assertNetworkEventSearchTotal(t, db, "edge-search", 1)
	assertNetworkEventSearchTotal(t, db, "alice-search", 1)
	assertNetworkEventSearchTotal(t, db, "198.51.100.7", 1)
	assertNetworkEventSearchTotal(t, db, "connection accepted", 1)
	assertNetworkEventSearchTotal(t, db, "---", 0)

	if _, err := db.sql.ExecContext(ctx, `UPDATE nodes SET name = 'edge-renamed' WHERE id = 'node-search'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE proxy_users SET name = 'alice-renamed' WHERE id = 'user-search'`); err != nil {
		t.Fatal(err)
	}
	assertNetworkEventSearchTotal(t, db, "edge-renamed", 1)
	assertNetworkEventSearchTotal(t, db, "alice-renamed", 1)
	assertNetworkEventSearchTotal(t, db, "edge-search", 0)

	if _, err := db.sql.ExecContext(ctx, `UPDATE log_events SET target_host = 'new.example.net' WHERE id = 'event-search'`); err != nil {
		t.Fatal(err)
	}
	assertNetworkEventSearchTotal(t, db, "new.example.net", 1)
	assertNetworkEventSearchTotal(t, db, "api.example.com", 0)

	if _, err := db.sql.ExecContext(ctx, `DELETE FROM log_events WHERE id = 'event-search'`); err != nil {
		t.Fatal(err)
	}
	assertNetworkEventSearchTotal(t, db, "new.example.net", 0)
}

func TestNetworkEventSearchQuery(t *testing.T) {
	tests := map[string]string{
		"api.example.com":  `api* example* com*`,
		"198.51.100.7":     `198* 51* 100* 7*`,
		"connect accepted": `connect* accepted*`,
		"---":              `"__boxfleet_no_search_tokens__"`,
	}
	for input, want := range tests {
		if got := networkEventSearchQuery(input); got != want {
			t.Errorf("networkEventSearchQuery(%q) = %q, want %q", input, got, want)
		}
	}
}

func assertNetworkEventSearchTotal(t *testing.T, db *DB, search string, want int64) {
	t.Helper()
	page, err := db.ListLogEventsPage(context.Background(), LogEventFilter{Search: search, Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != want {
		t.Fatalf("search %q total = %d, want %d", search, page.Total, want)
	}
}
