package db

import (
	"context"
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
