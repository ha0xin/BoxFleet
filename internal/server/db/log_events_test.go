package db

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripANSIRequiresEscapeByte(t *testing.T) {
	tests := map[string]string{
		"\x1b[36mINFO\x1b[0m": "INFO",
		"example[0m.com":      "example[0m.com",
		"host[123;45m.test":   "host[123;45m.test",
	}
	for input, want := range tests {
		if got := stripANSI(input); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRecordLogEventsKeepsLiteralBracketSequenceInHost(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{{
			Action:     "sing-box",
			RawMessage: "+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to example[0m.com:443",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].TargetHost != "example[0m.com" {
		t.Fatalf("events = %#v", events)
	}
}

func TestCompactRawSampleTruncatesOnRuneBoundary(t *testing.T) {
	message := strings.Repeat("a", 511) + strings.Repeat("界", 4)
	sample := compactRawSample(message)
	if !utf8.ValidString(sample) {
		t.Fatalf("sample is not valid UTF-8: %q", sample)
	}
	if len(sample) > 512 {
		t.Fatalf("sample length = %d, want <= 512", len(sample))
	}
	if !strings.HasPrefix(message, sample) {
		t.Fatalf("sample %q is not a prefix of the message", sample)
	}
	if short := compactRawSample("  hello 界  "); short != "hello 界" {
		t.Fatalf("compactRawSample short message = %q", short)
	}
}

func TestRecordLogEventsClampsReportedCounts(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	event := LogEventInput{
		AuthName:    "vless-39090@alice",
		SourceIP:    "115.27.221.55",
		TargetHost:  "speed.cloudflare.com",
		TargetPort:  443,
		Action:      "connect",
		Count:       5,
		WindowStart: "2026-05-16T03:23:43Z",
		WindowEnd:   "2026-05-16T03:23:43Z",
	}
	if err := store.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: []LogEventInput{event}}); err != nil {
		t.Fatal(err)
	}
	negative := event
	negative.Count = -1000
	if err := store.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: []LogEventInput{negative}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Count != 6 {
		t.Fatalf("events after negative count = %#v", events)
	}
	huge := event
	huge.Count = math.MaxInt64
	if err := store.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: []LogEventInput{huge}}); err != nil {
		t.Fatal(err)
	}
	events, err = store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Count != 6+maxLogEventCount {
		t.Fatalf("events after huge count = %#v", events)
	}
}

func TestRecordLogEventsRejectsOutOfRangePorts(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	base := LogEventInput{
		AuthName:    "vless-39090@alice",
		SourceIP:    "115.27.221.55",
		TargetHost:  "speed.cloudflare.com",
		Action:      "connect",
		WindowStart: "2026-05-16T03:23:43Z",
		WindowEnd:   "2026-05-16T03:23:43Z",
	}
	for _, port := range []int64{-1, 65536, math.MaxInt64} {
		event := base
		event.TargetPort = port
		if err := store.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: []LogEventInput{event}}); err != nil {
			t.Fatalf("port %d: %v", port, err)
		}
	}
	events, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v", events)
	}
}

func TestRecordLogEventsBoundsEventsPerReport(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	events := make([]LogEventInput, 0, maxLogEventsPerReport+10)
	for i := 0; i < cap(events); i++ {
		events = append(events, LogEventInput{
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  fmt.Sprintf("host-%d.example.com", i),
			TargetPort:  443,
			Action:      "connect",
			WindowStart: "2026-05-16T03:23:43Z",
			WindowEnd:   "2026-05-16T03:23:43Z",
		})
	}
	if err := store.RecordLogEvents(ctx, LogEventReport{NodeName: "azus", Events: events}); err != nil {
		t.Fatal(err)
	}
	var total int64
	if err := store.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM log_events`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != maxLogEventsPerReport {
		t.Fatalf("stored events = %d, want %d", total, maxLogEventsPerReport)
	}
}
