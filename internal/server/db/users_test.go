package db

import (
	"context"
	"strings"
	"testing"
)

func TestCreateProxyUserRejectsInvalidExpireAt(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	_, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", ExpireAt: "next tuesday"})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	if _, err := store.GetProxyUser(ctx, "alice"); err == nil {
		t.Fatal("user was created despite an invalid expire_at")
	}
}

func TestProxyUserExpireAtNormalizesToUTC(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	user, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", ExpireAt: "2026-07-26T12:00:00+02:00"})
	if err != nil {
		t.Fatal(err)
	}
	if !user.ExpireAt.Valid || user.ExpireAt.String != "2026-07-26T10:00:00Z" {
		t.Fatalf("expire_at = %#v", user.ExpireAt)
	}
	empty, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "bob", ExpireAt: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if empty.ExpireAt.Valid {
		t.Fatalf("empty expire_at stored as %#v", empty.ExpireAt)
	}
}

func TestSetProxyUserExpireRejectsInvalidExpireAt(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", "2026-13-01"); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", "2026-07-26T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	user, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !user.ExpireAt.Valid || user.ExpireAt.String != "2026-07-26T10:00:00Z" {
		t.Fatalf("expire_at = %#v", user.ExpireAt)
	}
	if err := store.SetProxyUserExpire(ctx, "alice", ""); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ExpireAt.Valid {
		t.Fatalf("expire_at was not cleared: %#v", cleared.ExpireAt)
	}
}

func TestUpdateProxyUserRejectsInvalidExpireAtBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice", DisplayName: "Alice"}); err != nil {
		t.Fatal(err)
	}
	display := "Renamed"
	expire := "garbage"
	_, err := store.UpdateProxyUser(ctx, "alice", UpdateProxyUserParams{DisplayName: &display, ExpireAt: &expire})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("expected an RFC3339 error, got %v", err)
	}
	user, err := store.GetProxyUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.DisplayName != "Alice" {
		t.Fatalf("display_name changed despite an invalid expire_at: %q", user.DisplayName)
	}
}

// seedUserPageFixture creates users covering every derived status the Users page
// filters on. Traffic is written straight to the rollup table because
// ListUsersPage reads that table, not the delta history a report would replay.
func seedUserPageFixture(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	users := []CreateProxyUserParams{
		{Name: "alice", DisplayName: "Alice Zhang", GlobalQuotaBytes: 100},
		{Name: "bob", DisplayName: "Bob Lee", GlobalQuotaBytes: 100},
		{Name: "carol", DisplayName: "Carol Wu"},
		{Name: "dave", DisplayName: "Dave Ito", ExpireAt: "2020-01-01T00:00:00Z"},
		{Name: "erin", DisplayName: "Erin Poe"},
	}
	for _, params := range users {
		if _, err := store.CreateProxyUser(ctx, params); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetProxyUserStatus(ctx, "carol", "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SoftDeleteProxyUser(ctx, "erin"); err != nil {
		t.Fatal(err)
	}
	// bob has burned his whole quota; alice is nowhere near hers.
	seedUserTrafficTotal(t, ctx, store, "alice", "uplink", 2, 3)
	seedUserTrafficTotal(t, ctx, store, "alice", "downlink", 4, 5)
	seedUserTrafficTotal(t, ctx, store, "bob", "uplink", 60, 70)
	seedUserTrafficTotal(t, ctx, store, "bob", "downlink", 30, 40)
}

func seedUserTrafficTotal(t *testing.T, ctx context.Context, store *DB, name, direction string, rawBytes, billableBytes int64) {
	t.Helper()
	user, err := store.GetProxyUser(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.sql.ExecContext(ctx,
		`INSERT INTO traffic_usage_totals (proxy_user_id, direction, raw_bytes, billable_bytes) VALUES (?, ?, ?, ?)`,
		user.ID, direction, rawBytes, billableBytes,
	); err != nil {
		t.Fatal(err)
	}
}

func userPageNames(page UserPage) []string {
	names := make([]string, 0, len(page.Users))
	for _, row := range page.Users {
		names = append(names, row.Name)
	}
	return names
}

func TestListUsersPageDerivesStatusAndTraffic(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedUserPageFixture(t, ctx, store)

	page, err := store.ListUsersPage(ctx, UserFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 {
		t.Fatalf("total = %d, want the four live users", page.Total)
	}
	if page.Limit != 50 || page.Offset != 0 {
		t.Fatalf("limit/offset = %d/%d, want the 50/0 defaults", page.Limit, page.Offset)
	}
	statuses := map[string]string{}
	for _, row := range page.Users {
		statuses[row.Name] = row.EffectiveStatus
	}
	want := map[string]string{
		"alice": "active",
		"bob":   "quota_exceeded",
		"carol": "disabled",
		"dave":  "expired",
	}
	for name, status := range want {
		if statuses[name] != status {
			t.Fatalf("effective status of %s = %q, want %q", name, statuses[name], status)
		}
	}
	for _, row := range page.Users {
		if row.Name != "alice" {
			continue
		}
		if row.UplinkRawBytes != 2 || row.UplinkBillableBytes != 3 || row.DownlinkRawBytes != 4 || row.DownlinkBillableBytes != 5 {
			t.Fatalf("alice traffic = %#v", row)
		}
	}
}

func TestListUsersPageFiltersAndPages(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedUserPageFixture(t, ctx, store)

	quota, err := store.ListUsersPage(ctx, UserFilter{Status: "quota_exceeded"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(quota); len(got) != 1 || got[0] != "bob" || quota.Total != 1 {
		t.Fatalf("quota_exceeded page = %v (total %d)", got, quota.Total)
	}

	// The stored column still says "active" for dave; only the derived status
	// knows the expiry has passed.
	expired, err := store.ListUsersPage(ctx, UserFilter{Status: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(expired); len(got) != 1 || got[0] != "dave" {
		t.Fatalf("expired page = %v", got)
	}

	deleted, err := store.ListUsersPage(ctx, UserFilter{Deleted: "only"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(deleted); len(got) != 1 || got[0] != "erin" || deleted.Total != 1 {
		t.Fatalf("deleted page = %v (total %d)", got, deleted.Total)
	}

	search, err := store.ListUsersPage(ctx, UserFilter{Search: "CAROL WU"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(search); len(got) != 1 || got[0] != "carol" {
		t.Fatalf("display-name search = %v", got)
	}

	second, err := store.ListUsersPage(ctx, UserFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(second); len(got) != 2 || got[0] != "carol" || got[1] != "dave" {
		t.Fatalf("second page = %v", got)
	}
	if second.Total != 4 {
		t.Fatalf("second page total = %d, want the unpaged count", second.Total)
	}
}

func TestListUsersPageSortsOnWhitelistedKeysOnly(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedUserPageFixture(t, ctx, store)

	byTraffic, err := store.ListUsersPage(ctx, UserFilter{Sort: "traffic", Direction: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(byTraffic); got[0] != "bob" || got[1] != "alice" {
		t.Fatalf("traffic sort = %v, want bob then alice", got)
	}

	byExpiry, err := store.ListUsersPage(ctx, UserFilter{Sort: "expire_at"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(byExpiry); got[0] != "dave" {
		t.Fatalf("expiry sort = %v, want the only expiring user first", got)
	}

	// An unknown sort key falls back to name order instead of reaching SQL.
	injected, err := store.ListUsersPage(ctx, UserFilter{Sort: "u.name; DROP TABLE proxy_users"})
	if err != nil {
		t.Fatal(err)
	}
	if got := userPageNames(injected); len(got) != 4 || got[0] != "alice" {
		t.Fatalf("unknown sort key = %v, want the default name order", got)
	}
}

func TestListUsersPageCountsLiveAccessesOnly(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)

	page, err := store.ListUsersPage(ctx, UserFilter{Search: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 1 {
		t.Fatalf("page = %v", userPageNames(page))
	}
	users, err := store.ListProxyUsersWithProxyCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var want int64
	for _, user := range users {
		if user.Name == "alice" {
			want = user.ProxyCount
		}
	}
	if page.Users[0].ProxyCount != want {
		t.Fatalf("proxy_count = %d, want the unpaged count %d", page.Users[0].ProxyCount, want)
	}
}

// The user page reads two per-row aggregates. Both must resolve through an
// index, otherwise every page rescans the access table and the traffic rollup
// once per user. The predicates are taken from the production constants so the
// plan under test cannot drift away from the query that ships.
func TestListUsersPageAggregatesUseIndexes(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedUserPageFixture(t, ctx, store)

	tests := []struct {
		name      string
		query     string
		want      string
		forbidden string
	}{
		{
			name: "name ordering reads the unique index instead of sorting",
			query: `
SELECT u.id, u.name
FROM proxy_users u
WHERE u.deleted_at IS NULL
ORDER BY ` + userPageSort("", "") + `
LIMIT 50
OFFSET 0`,
			forbidden: "temp b-tree",
		},
		{
			name: "the access count is an indexed lookup per user",
			query: `
SELECT u.id, ` + userProxyCountSQL + ` AS proxy_count
FROM proxy_users u
WHERE u.deleted_at IS NULL
LIMIT 50`,
			want:      "idx_proxy_accesses_user_id",
			forbidden: "scan a",
		},
		{
			name: "the derived status reads the traffic rollup by primary key",
			query: `
SELECT u.id, ` + userEffectiveStatusSQL + ` AS effective_status
FROM proxy_users u
WHERE u.deleted_at IS NULL
LIMIT 50`,
			want:      "sqlite_autoindex_traffic_usage_totals_1",
			forbidden: "scan t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := explainQueryPlan(t, ctx, store, tt.query)
			if tt.want != "" && !strings.Contains(plan, strings.ToLower(tt.want)) {
				t.Fatalf("query plan does not contain %q:\n%s", tt.want, plan)
			}
			if tt.forbidden != "" && strings.Contains(plan, strings.ToLower(tt.forbidden)) {
				t.Fatalf("query plan contains forbidden %q:\n%s", tt.forbidden, plan)
			}
		})
	}
}
