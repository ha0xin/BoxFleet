package db

import (
	"context"
	"testing"

	"github.com/haoxin/boxfleet/internal/servicecatalog"
)

func seedServiceUsageRows(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@alice", "www.youtube.com", "2026-07-25T00:10:00Z", 3)
	// Same host, different casing: target_host keeps the casing sing-box logged,
	// so only lower() in the GROUP BY collapses these into one destination.
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@alice", "WWW.YouTube.com", "2026-07-25T00:20:00Z", 4)
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@alice", "i.ytimg.com", "2026-07-25T00:40:00Z", 2)
	recordNetworkEvent(t, ctx, store, "azus", "vless-39090@bob", "www.google.com", "2026-07-25T01:20:00Z", 5)
	recordNetworkEvent(t, ctx, store, "edge", "vless-39091@alice", "github.com", "2026-07-25T03:05:00Z", 7)
	recordNetworkEvent(t, ctx, store, "edge", "vless-39091@alice", "198.51.100.7", "2026-07-25T03:30:00Z", 1)
}

func TestNetworkEventHostCountsFoldCasingAndRankByConnections(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	hosts, truncated, err := store.NetworkEventHostCounts(ctx, LogEventFilter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if len(hosts) != 5 {
		t.Fatalf("hosts = %#v", hosts)
	}
	// Equal counts break on the host name, so paging is stable across requests.
	if hosts[0].Host != "github.com" || hosts[0].Connections != 7 {
		t.Fatalf("top host = %#v", hosts[0])
	}
	if hosts[1].Host != "www.youtube.com" || hosts[1].Connections != 7 {
		t.Fatalf("second host = %#v", hosts[1])
	}
	if hosts[1].LastSeen != "2026-07-25T00:20:00Z" {
		t.Fatalf("last seen = %q", hosts[1].LastSeen)
	}
}

func TestNetworkEventHostCountsReportTruncation(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	hosts, truncated, err := store.NetworkEventHostCounts(ctx, LogEventFilter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("five hosts over a limit of two should report truncation")
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %#v", hosts)
	}
}

func TestNetworkEventServiceUsageRollsHostsIntoServices(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	result, err := store.NetworkEventServiceUsage(ctx, LogEventFilter{}, ServiceUsageGroupService, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalConnections != 22 || result.TotalHosts != 5 {
		t.Fatalf("totals = (%d, %d), want (22, 5)", result.TotalConnections, result.TotalHosts)
	}
	if result.CatalogVersion != servicecatalog.Version() {
		t.Fatalf("catalog version = %q", result.CatalogVersion)
	}
	byKey := make(map[string]ServiceUsageRow, len(result.Rows))
	for _, row := range result.Rows {
		byKey[row.Key] = row
	}
	youtube, ok := byKey["youtube"]
	if !ok {
		t.Fatalf("rows = %#v", result.Rows)
	}
	// www.youtube.com and i.ytimg.com are two distinct hosts on one service.
	if youtube.Connections != 9 || youtube.Hosts != 2 {
		t.Fatalf("youtube = %#v", youtube)
	}
	if youtube.Label != "YouTube" {
		t.Fatalf("youtube label = %q", youtube.Label)
	}
	if row := byKey["github"]; row.Connections != 7 || row.Hosts != 1 {
		t.Fatalf("github = %#v", row)
	}
	if row := byKey["google"]; row.Connections != 5 {
		t.Fatalf("google = %#v", row)
	}
	// A bare IP literal has no service; it is reported as itself under
	// direct-ip rather than being dropped or guessed at.
	if row := byKey["198.51.100.7"]; row.Category != servicecatalog.CategoryDirectIP || row.Connections != 1 {
		t.Fatalf("direct ip = %#v", row)
	}
	if result.Rows[0].Key != "youtube" {
		t.Fatalf("rows are not ranked by connections: %#v", result.Rows)
	}
}

func TestNetworkEventServiceUsageGroupsByCategoryAndFoldsOther(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	categories, err := store.NetworkEventServiceUsage(ctx, LogEventFilter{}, ServiceUsageGroupCategory, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range categories.Rows {
		if row.Key != row.Category {
			t.Fatalf("category row keys itself as %q but reports category %q", row.Key, row.Category)
		}
	}
	if categories.TotalConnections != 22 {
		t.Fatalf("category totals = %d, want 22", categories.TotalConnections)
	}

	limited, err := store.NetworkEventServiceUsage(ctx, LogEventFilter{}, ServiceUsageGroupService, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Rows) != 1 || limited.Rows[0].Key != "youtube" {
		t.Fatalf("rows = %#v", limited.Rows)
	}
	if limited.Other.Key != "other" || limited.Other.Connections != 13 || limited.Other.Hosts != 3 {
		t.Fatalf("other = %#v", limited.Other)
	}
	if limited.Other.Connections+limited.Rows[0].Connections != limited.TotalConnections {
		t.Fatal("the remainder must account for every connection outside the top rows")
	}
}

func TestNetworkEventServiceUsageAppliesOverridesAtReadTime(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	if _, err := store.UpsertDomainServiceOverride(ctx, DomainServiceOverride{
		Suffix:   ".YTImg.com.",
		Service:  "cdn",
		Label:    "Edge CDN",
		Category: "infrastructure",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := store.NetworkEventServiceUsage(ctx, LogEventFilter{}, ServiceUsageGroupService, 0)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]ServiceUsageRow, len(result.Rows))
	for _, row := range result.Rows {
		byKey[row.Key] = row
	}
	// No log_events row was rewritten: the same stored history reclassifies.
	if row := byKey["youtube"]; row.Connections != 7 || row.Hosts != 1 {
		t.Fatalf("youtube after override = %#v", row)
	}
	cdn, ok := byKey["cdn"]
	if !ok {
		t.Fatalf("rows = %#v", result.Rows)
	}
	if cdn.Connections != 2 || cdn.Label != "Edge CDN" || cdn.Category != "infrastructure" {
		t.Fatalf("cdn = %#v", cdn)
	}
}

func TestNetworkEventHostUsagePagesWithinAService(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	page, err := store.NetworkEventHostUsage(ctx, LogEventFilter{}, "youtube", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Hosts) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Hosts[0].Host != "www.youtube.com" || page.Hosts[0].ServiceLabel != "YouTube" {
		t.Fatalf("host = %#v", page.Hosts[0])
	}
	if page.Hosts[0].Source != servicecatalog.SourceCatalog {
		t.Fatalf("source = %q", page.Hosts[0].Source)
	}

	second, err := store.NetworkEventHostUsage(ctx, LogEventFilter{}, "youtube", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 2 || len(second.Hosts) != 1 || second.Hosts[0].Host != "i.ytimg.com" {
		t.Fatalf("second page = %#v", second)
	}
	if second.Offset != 1 || second.Limit != 1 {
		t.Fatalf("paging echo = %#v", second)
	}

	beyond, err := store.NetworkEventHostUsage(ctx, LogEventFilter{}, "youtube", 10, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(beyond.Hosts) != 0 || beyond.Total != 2 {
		t.Fatalf("out-of-range page = %#v", beyond)
	}

	all, err := store.NetworkEventHostUsage(ctx, LogEventFilter{}, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 5 {
		t.Fatalf("unfiltered total = %d, want 5", all.Total)
	}
}

func TestNetworkEventHostUsageHonoursTheEventFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedNetworkEventSeriesFixture(t, ctx, store)
	seedServiceUsageRows(t, ctx, store)

	page, err := store.NetworkEventHostUsage(ctx, LogEventFilter{
		NodeName: "edge",
		Start:    "2026-07-25T03:00:00Z",
		End:      "2026-07-25T03:10:00Z",
	}, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Hosts[0].Host != "github.com" {
		t.Fatalf("page = %#v", page)
	}
}

func TestDomainServiceOverridesNormalizeAndValidate(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	created, err := store.UpsertDomainServiceOverride(ctx, DomainServiceOverride{
		Suffix:  "  .Example.COM. ",
		Service: "example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Suffix != "example.com" {
		t.Fatalf("suffix = %q, want example.com", created.Suffix)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("override = %#v", created)
	}

	updated, err := store.UpsertDomainServiceOverride(ctx, DomainServiceOverride{
		Suffix:   "example.com",
		Service:  "example",
		Label:    "Example",
		Category: "corporate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "Example" || updated.Category != "corporate" {
		t.Fatalf("updated = %#v", updated)
	}
	overrides, err := store.ListDomainServiceOverrides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 1 {
		t.Fatalf("the suffix is the primary key, so the upsert must not add a row: %#v", overrides)
	}

	invalid := []DomainServiceOverride{
		{Suffix: "  ", Service: "example"},
		{Suffix: "...", Service: "example"},
		{Suffix: "example.com", Service: "  "},
		{Suffix: "https://example.com/path", Service: "example"},
		{Suffix: "example .com", Service: "example"},
	}
	for _, override := range invalid {
		if _, err := store.UpsertDomainServiceOverride(ctx, override); err == nil {
			t.Fatalf("override %#v was accepted", override)
		}
	}

	// Delete normalizes the same way, so an operator can remove a rule using
	// whatever spelling they typed to create it.
	if err := store.DeleteDomainServiceOverride(ctx, "EXAMPLE.com."); err != nil {
		t.Fatal(err)
	}
	overrides, err = store.ListDomainServiceOverrides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(overrides) != 0 {
		t.Fatalf("overrides = %#v", overrides)
	}
	if err := store.DeleteDomainServiceOverride(ctx, "  "); err == nil {
		t.Fatal("an empty suffix must be rejected")
	}
}
