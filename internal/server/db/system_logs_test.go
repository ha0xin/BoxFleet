package db

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRecordSystemLogsPersistsEntries(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge-a", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if err := store.RecordSystemLogs(ctx, SystemLogReport{
		NodeName: "edge-a",
		Entries: []SystemLogInput{
			{Service: "sing-box", Level: "error", RawMessage: "start service: bind failed", ObservedAt: observedAt, Cursor: "s=1;i=1"},
			{Service: "boxfleet-agent", Level: "info", RawMessage: "applied config", ObservedAt: observedAt, Cursor: "s=1;i=2"},
			{Service: "", RawMessage: "no service", ObservedAt: observedAt},
			{Service: "sing-box", RawMessage: "   ", ObservedAt: observedAt},
		},
	}); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListRecentSystemLogs(ctx, "edge-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("stored %d system logs, want 2: %#v", len(logs), logs)
	}
	byService := map[string]SystemLog{}
	for _, entry := range logs {
		byService[entry.Service] = entry
	}
	singBox, ok := byService["sing-box"]
	if !ok {
		t.Fatalf("sing-box log missing: %#v", logs)
	}
	if singBox.RawMessage != "start service: bind failed" || singBox.Level != "error" {
		t.Fatalf("unexpected sing-box log %#v", singBox)
	}
	if singBox.NodeName != "edge-a" || !singBox.JournalCursor.Valid {
		t.Fatalf("unexpected sing-box log %#v", singBox)
	}
}

func TestRecordSystemLogsIgnoresDuplicateCursors(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge-a", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	report := SystemLogReport{
		NodeName: "edge-a",
		Entries: []SystemLogInput{
			{Service: "sing-box", RawMessage: "inbound started", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Cursor: "s=1;i=1"},
		},
	}
	for range 3 {
		if err := store.RecordSystemLogs(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	logs, err := store.ListRecentSystemLogs(ctx, "edge-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("stored %d system logs, want 1", len(logs))
	}
}

func TestRecordSystemLogsPrunesExpiredEntries(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "edge-a", "192.0.2.1", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SetNetworkEventRetentionDays(ctx, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.RecordSystemLogs(ctx, SystemLogReport{
		NodeName: "edge-a",
		Entries: []SystemLogInput{
			{Service: "sing-box", RawMessage: "stale", ObservedAt: now.AddDate(0, 0, -5).Format(time.RFC3339Nano), Cursor: "s=1;i=1"},
			{Service: "sing-box", RawMessage: "fresh", ObservedAt: now.Format(time.RFC3339Nano), Cursor: "s=1;i=2"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListRecentSystemLogs(ctx, "edge-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].RawMessage != "fresh" {
		t.Fatalf("retention did not prune stale system logs: %#v", logs)
	}
}

func TestRecordSystemLogsRejectsUnknownNode(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if err := store.RecordSystemLogs(ctx, SystemLogReport{
		NodeName: "missing",
		Entries:  []SystemLogInput{{Service: "sing-box", RawMessage: "hello"}},
	}); err == nil {
		t.Fatal("expected an error for an unknown node")
	}
}

func TestNormalizeObservedAt(t *testing.T) {
	fallback := "2026-01-01T00:00:00.000Z"
	if got := normalizeObservedAt("", fallback); got != fallback {
		t.Fatalf("empty observed_at = %q, want %q", got, fallback)
	}
	if got := normalizeObservedAt("not a timestamp", fallback); got != fallback {
		t.Fatalf("garbage observed_at = %q, want %q", got, fallback)
	}
	if got := normalizeObservedAt("2026-07-26T12:00:00+02:00", fallback); got != "2026-07-26T10:00:00.000Z" {
		t.Fatalf("offset observed_at = %q", got)
	}
}

// seedSystemLogPageFixture writes two nodes' worth of journal lines with
// distinct levels, services, and observation times so every page filter has
// something to include and something to exclude.
func seedSystemLogPageFixture(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	for _, node := range []string{"edge-a", "edge-b"} {
		if _, err := store.CreateNode(ctx, node, "192.0.2.1", ""); err != nil {
			t.Fatal(err)
		}
	}
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	entries := []struct {
		node    string
		service string
		level   string
		message string
		offset  time.Duration
	}{
		{"edge-a", "sing-box", "info", "inbound/vless started on :39090", 0},
		{"edge-a", "boxfleet-agent", "warning", "config apply pending", time.Minute},
		{"edge-a", "sing-box", "LEVEL_FATAL", "listen tcp :443: address already in use", 2 * time.Minute},
		{"edge-b", "systemd", "debug", "router matched outbound direct", 3 * time.Minute},
		{"edge-b", "boxfleet-agent", "err", "heartbeat failed: i/o timeout", 4 * time.Minute},
	}
	for index, entry := range entries {
		if err := store.RecordSystemLogs(ctx, SystemLogReport{
			NodeName: entry.node,
			Entries: []SystemLogInput{{
				Service:    entry.service,
				Level:      entry.level,
				RawMessage: entry.message,
				ObservedAt: base.Add(entry.offset).Format(time.RFC3339Nano),
				Cursor:     "s=1;i=" + strconv.Itoa(index),
			}},
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func systemLogPageMessages(page SystemLogPage) []string {
	messages := make([]string, 0, len(page.Logs))
	for _, entry := range page.Logs {
		messages = append(messages, entry.RawMessage)
	}
	return messages
}

func TestListSystemLogsPageOrdersNewestFirst(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	page, err := store.ListSystemLogsPage(ctx, SystemLogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 {
		t.Fatalf("total = %d, want 5", page.Total)
	}
	if page.Limit != 100 || page.Offset != 0 {
		t.Fatalf("limit/offset = %d/%d, want the 100/0 defaults", page.Limit, page.Offset)
	}
	messages := systemLogPageMessages(page)
	if messages[0] != "heartbeat failed: i/o timeout" {
		t.Fatalf("first row = %q, want the newest entry", messages[0])
	}
	if messages[len(messages)-1] != "inbound/vless started on :39090" {
		t.Fatalf("last row = %q, want the oldest entry", messages[len(messages)-1])
	}

	oldest, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := systemLogPageMessages(oldest)[0]; got != "inbound/vless started on :39090" {
		t.Fatalf("ascending first row = %q", got)
	}
}

// The service filter's options have to survive both paging and its own
// selection, so they come from the whole table rather than the visible page.
func TestListSystemLogsPageListsEveryServiceRegardlessOfFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	page, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Service: "systemd", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"boxfleet-agent", "sing-box", "systemd"}
	if len(page.Services) != len(want) {
		t.Fatalf("services = %v, want %v", page.Services, want)
	}
	for index, service := range want {
		if page.Services[index] != service {
			t.Fatalf("services = %v, want %v", page.Services, want)
		}
	}
}

func TestListSystemLogsPageFilters(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	node, err := store.ListSystemLogsPage(ctx, SystemLogFilter{NodeName: "edge-b"})
	if err != nil {
		t.Fatal(err)
	}
	if node.Total != 2 {
		t.Fatalf("edge-b total = %d, want 2", node.Total)
	}

	service, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Service: "boxfleet-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if service.Total != 2 {
		t.Fatalf("boxfleet-agent total = %d, want 2", service.Total)
	}

	search, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Search: "ADDRESS ALREADY"})
	if err != nil {
		t.Fatal(err)
	}
	if got := systemLogPageMessages(search); len(got) != 1 || got[0] != "listen tcp :443: address already in use" {
		t.Fatalf("search page = %v", got)
	}

	if _, err := store.ListSystemLogsPage(ctx, SystemLogFilter{NodeName: "missing"}); err == nil {
		t.Fatal("an unknown node filter returned a page instead of an error")
	}
}

// Journal levels are free text, so the page's buckets have to survive "err",
// "warning", and "LEVEL_FATAL" landing in the same column as "info".
func TestListSystemLogsPageBucketsFreeTextLevels(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	tests := []struct {
		level string
		want  []string
	}{
		{level: "error", want: []string{"heartbeat failed: i/o timeout", "listen tcp :443: address already in use"}},
		{level: "warn", want: []string{"config apply pending"}},
		{level: "debug", want: []string{"router matched outbound direct"}},
		{level: "info", want: []string{"inbound/vless started on :39090"}},
		{level: "", want: nil},
	}
	for _, tt := range tests {
		page, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Level: tt.level})
		if err != nil {
			t.Fatal(err)
		}
		if tt.want == nil {
			if page.Total != 5 {
				t.Fatalf("empty level filter returned %d rows, want all 5", page.Total)
			}
			continue
		}
		got := systemLogPageMessages(page)
		if len(got) != len(tt.want) {
			t.Fatalf("level %q returned %v, want %v", tt.level, got, tt.want)
		}
		for index, message := range tt.want {
			if got[index] != message {
				t.Fatalf("level %q returned %v, want %v", tt.level, got, tt.want)
			}
		}
	}
}

func TestListSystemLogsPagePagesAndSorts(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	page, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 || len(page.Logs) != 2 {
		t.Fatalf("page = %v (total %d)", systemLogPageMessages(page), page.Total)
	}
	if got := systemLogPageMessages(page)[0]; got != "listen tcp :443: address already in use" {
		t.Fatalf("third row = %q", got)
	}

	byService, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Sort: "service", Direction: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got := byService.Logs[0].Service; got != "boxfleet-agent" {
		t.Fatalf("service sort first row = %q", got)
	}

	// An unknown sort key falls back to the observed-at order instead of
	// reaching SQL.
	injected, err := store.ListSystemLogsPage(ctx, SystemLogFilter{Sort: "l.id; DROP TABLE system_logs"})
	if err != nil {
		t.Fatal(err)
	}
	if got := systemLogPageMessages(injected); len(got) != 5 || got[0] != "heartbeat failed: i/o timeout" {
		t.Fatalf("unknown sort key = %v, want the default newest-first order", got)
	}
}

// A node-scoped page is the one read path that can stay bounded on today's
// schema: idx_system_logs_node_observed covers both the filter and the order.
// The unfiltered page still materialises a sort and needs its own
// observed_at index — see the migration proposed alongside this change.
func TestListSystemLogsPageNodeFilterUsesTheCompositeIndex(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedSystemLogPageFixture(t, ctx, store)

	plan := explainQueryPlan(t, ctx, store, `
SELECT l.id, l.observed_at
FROM system_logs l
JOIN nodes n ON n.id = l.node_id
WHERE l.node_id = ?
ORDER BY `+systemLogPageSort("", "")+`
LIMIT 100
OFFSET 0`, "node-1")
	if !strings.Contains(plan, "idx_system_logs_node_observed") {
		t.Fatalf("node-scoped page does not use the composite index:\n%s", plan)
	}
}
