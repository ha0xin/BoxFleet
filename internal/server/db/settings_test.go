package db

import (
	"context"
	"testing"
	"time"
)

func TestNetworkEventRetentionSettings(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)

	days, err := store.NetworkEventRetentionDays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if days != DefaultNetworkEventRetentionDays {
		t.Fatalf("default retention days = %d, want %d", days, DefaultNetworkEventRetentionDays)
	}

	if err := store.SetNetworkEventRetentionDays(ctx, 30); err != nil {
		t.Fatal(err)
	}
	settings, err := store.AdminSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.NetworkEventRetentionDays != 30 {
		t.Fatalf("settings = %#v", settings)
	}

	if err := store.SetNetworkEventRetentionDays(ctx, 0); err == nil {
		t.Fatal("expected invalid retention value to fail")
	}
}

func TestDeleteExpiredLogEvents(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	if err := store.SetNetworkEventRetentionDays(ctx, 90); err != nil {
		t.Fatal(err)
	}

	oldEnd := time.Now().UTC().AddDate(0, 0, -120).Format(time.RFC3339)
	recentEnd := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{{
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "old.example.com",
			TargetPort:  443,
			Action:      "connect",
			RawMessage:  "old event",
			WindowStart: oldEnd,
			WindowEnd:   oldEnd,
			Count:       1,
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "recent.example.com",
			TargetPort:  443,
			Action:      "connect",
			RawMessage:  "recent event",
			WindowStart: recentEnd,
			WindowEnd:   recentEnd,
			Count:       1,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	logs, err := store.ListRecentLogEventsByUser(ctx, "alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].TargetHost != "recent.example.com" {
		t.Fatalf("logs after retention cleanup = %#v", logs)
	}
}
