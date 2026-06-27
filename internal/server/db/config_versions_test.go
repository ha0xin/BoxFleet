package db

import (
	"context"
	"testing"
)

func TestPublishConfigAndApplyStatus(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishConfig(ctx, "azus", []byte(`{"inbounds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !published.Created || published.ConfigVersion.Version != 1 {
		t.Fatalf("published = %#v", published)
	}
	status, err := store.GetNodeConfigStatus(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if status.TargetVersion.Int64 != 1 || status.LastApplyStatus != "pending" {
		t.Fatalf("status = %#v", status)
	}
	if err := store.RecordApplyResult(ctx, ApplyResult{
		NodeName:        "azus",
		ConfigVersionID: published.ConfigVersion.ID,
		ConfigHash:      published.ConfigVersion.ConfigHash,
		Status:          "applied",
	}); err != nil {
		t.Fatal(err)
	}
	status, err = store.GetNodeConfigStatus(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if status.CurrentVersion.Int64 != 1 || status.LastApplyStatus != "applied" {
		t.Fatalf("status = %#v", status)
	}
}

func TestTrafficAndLogReports(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	seedTrafficFixture(t, ctx, store)
	if err := store.RecordTrafficReport(ctx, TrafficReport{
		NodeName:    "azus",
		Sequence:    1,
		AgentBootID: "boot",
		Deltas: []TrafficDelta{{
			AuthName:      "vless-39090@alice",
			Direction:     "downlink",
			RawBytesDelta: 1024,
			CounterValue:  2048,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := store.SumTrafficByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || summary[0].RawBytes != 1024 {
		t.Fatalf("summary = %#v", summary)
	}
	if err := store.RecordTrafficReport(ctx, TrafficReport{
		NodeName:    "azus",
		Sequence:    1,
		AgentBootID: "boot",
		Deltas: []TrafficDelta{{
			AuthName:      "vless-39090@alice",
			Direction:     "downlink",
			RawBytesDelta: 1024,
			CounterValue:  2048,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err = store.SumTrafficByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || summary[0].RawBytes != 1024 {
		t.Fatalf("duplicate report changed summary = %#v", summary)
	}
	if err := store.RecordTrafficReport(ctx, TrafficReport{
		NodeName:    "azus",
		Sequence:    2,
		AgentBootID: "boot",
		Deltas: []TrafficDelta{{
			AuthName:      "deleted-access",
			Direction:     "downlink",
			RawBytesDelta: 2048,
			CounterValue:  4096,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordTrafficReport(ctx, TrafficReport{
		NodeName:    "azus",
		Sequence:    3,
		AgentBootID: "boot",
		Deltas: []TrafficDelta{{
			AuthName:      "deleted-access",
			Direction:     "downlink",
			RawBytesDelta: 2048,
			CounterValue:  4096,
		}, {
			AuthName:      "vless-39090@alice",
			Direction:     "uplink",
			RawBytesDelta: 512,
			CounterValue:  512,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err = store.SumTrafficByUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 2 {
		t.Fatalf("summary after stale auth = %#v", summary)
	}
	byDirection := make(map[string]int64)
	for _, row := range summary {
		byDirection[row.Direction] = row.RawBytes
	}
	if byDirection["downlink"] != 1024 || byDirection["uplink"] != 512 {
		t.Fatalf("summary after stale auth = %#v", summary)
	}
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{{
			Action:     "sing-box",
			RawMessage: "+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 0ms] inbound/vless[vless-39090]: inbound connection from 115.27.221.55:62895",
		}, {
			Action:     "sing-box",
			RawMessage: "+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to speed.cloudflare.com:443",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListRecentLogEventsByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].AuthName != "vless-39090@alice" {
		t.Fatalf("logs = %#v", logs)
	}
	rawLogs, err := store.ListRecentRawLogEntriesByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawLogs) != 0 {
		t.Fatalf("rawLogs = %#v", rawLogs)
	}
	userLogs, err := store.ListRecentLogEventsByUser(ctx, "alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(userLogs) != 1 || userLogs[0].SourceIp != "115.27.221.55" || userLogs[0].TargetHost != "speed.cloudflare.com" || userLogs[0].TargetPort != 443 {
		t.Fatalf("userLogs = %#v", userLogs)
	}
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{{
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "speed.cloudflare.com",
			TargetPort:  443,
			Action:      "connect",
			RawMessage:  "accepted tcp connection duplicate one",
			WindowStart: "2026-05-16T03:24:01Z",
			WindowEnd:   "2026-05-16T03:24:02Z",
			Count:       1,
		}, {
			AuthName:    "vless-39090@alice",
			SourceIP:    "115.27.221.55",
			TargetHost:  "speed.cloudflare.com",
			TargetPort:  443,
			Action:      "connect",
			RawMessage:  "accepted tcp connection duplicate two",
			WindowStart: "2026-05-16T03:24:03Z",
			WindowEnd:   "2026-05-16T03:24:08Z",
			Count:       2,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	userLogs, err = store.ListRecentLogEventsByUser(ctx, "alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	var aggregated LogEvent
	for _, log := range userLogs {
		if log.WindowStart == "2026-05-16T03:24:01Z" {
			aggregated = log
			break
		}
	}
	if aggregated.Count != 3 || aggregated.WindowEnd != "2026-05-16T03:24:08Z" {
		t.Fatalf("aggregated log = %#v", aggregated)
	}
	if err := store.RecordLogEvents(ctx, LogEventReport{
		NodeName: "azus",
		Events: []LogEventInput{{
			Cursor:     "cursor-1",
			ObservedAt: "2026-05-16T03:23:43Z",
			RawMessage: "+0000 2026-05-16 03:23:43 INFO [3999106428 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to speed.cloudflare.com:443",
		}, {
			Cursor:     "cursor-1",
			ObservedAt: "2026-05-16T03:23:43Z",
			RawMessage: "+0000 2026-05-16 03:23:43 INFO [3999106428 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to speed.cloudflare.com:443",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	rawLogs, err = store.ListRecentRawLogEntriesByNode(ctx, "azus", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rawLogs) != 0 {
		t.Fatalf("dedupe failed, rawLogs = %#v", rawLogs)
	}
}

func TestParseSingBoxLogEvent(t *testing.T) {
	sources := make(map[string]string)
	if _, ok := parseSingBoxLogEvent("+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 0ms] inbound/vless[vless-39090]: inbound connection from 115.27.221.55:62895", sources); ok {
		t.Fatal("source line should not create a structured event")
	}
	event, ok := parseSingBoxLogEvent("+0000 2026-05-16 03:23:43 \x1b[36mINFO\x1b[0m [\x1b[38;5;140m3999106428\x1b[0m 236ms] inbound/vless[vless-39090]: [vless-39090@alice] inbound connection to speed.cloudflare.com:443", sources)
	if !ok {
		t.Fatal("target line was not parsed")
	}
	if event.AuthName != "vless-39090@alice" || event.SourceIP != "115.27.221.55" || event.TargetHost != "speed.cloudflare.com" || event.TargetPort != 443 || event.Action != "connect" {
		t.Fatalf("event = %#v", event)
	}
	if event.WindowStart != "2026-05-16T03:23:42.764Z" || event.WindowEnd != "2026-05-16T03:23:43Z" {
		t.Fatalf("event window = %s -> %s", event.WindowStart, event.WindowEnd)
	}
	event, ok = parseSingBoxLogEvent("+0000 2026-05-15 18:08:47 \x1b[31mERROR\x1b[0m [\x1b[38;5;38m3583260653\x1b[0m 55ms] inbound/vless[vless-39090]: process connection from 67.230.167.42:52570: TLS handshake: REALITY: processed invalid connection", sources)
	if !ok {
		t.Fatal("invalid connection line was not parsed")
	}
	if event.SourceIP != "67.230.167.42" || event.Action != "invalid_connection" {
		t.Fatalf("event = %#v", event)
	}
	if event.WindowStart != "2026-05-15T18:08:46.945Z" || event.WindowEnd != "2026-05-15T18:08:47Z" {
		t.Fatalf("invalid connection window = %s -> %s", event.WindowStart, event.WindowEnd)
	}
}

func seedTrafficFixture(t *testing.T, ctx context.Context, store *DB) {
	t.Helper()
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:   "azus",
		Name:       "vless-39090",
		Protocol:   ProtocolVLESSReality,
		Listen:     "0.0.0.0",
		ListenPort: 39090,
		Transport:  TransportTCP,
		Enabled:    true,
		SettingsJSON: `{
			"server_name": "www.amazon.com",
			"reality_private_key": "private-key",
			"reality_public_key": "public-key",
			"short_id": "01234567",
			"handshake_server": "www.amazon.com",
			"handshake_port": 443
		}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindUserToNode(ctx, "alice", "azus"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueVLESSRealityAccess(ctx, IssueAccessParams{
		UserName:  "alice",
		NodeName:  "azus",
		ProxyName: "vless-39090",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordHeartbeatActivatesPendingNode(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	// Bootstrap-enrolled nodes are pending until their agent checks in.
	if err := store.SetNodeStatus(ctx, "azus", "pending"); err != nil {
		t.Fatal(err)
	}

	// First authenticated heartbeat completes enrollment.
	if err := store.RecordHeartbeat(ctx, Heartbeat{NodeName: "azus", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" {
		t.Fatalf("status after heartbeat = %q, want active", got.Status)
	}

	// A disabled node still heartbeats (paused) and must not self-reactivate.
	if err := store.SetNodeStatus(ctx, "azus", "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordHeartbeat(ctx, Heartbeat{NodeName: "azus", Status: "disabled"}); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "disabled" {
		t.Fatalf("disabled node reactivated to %q", got.Status)
	}
}

func TestPromotePendingNodeToActiveOnlyAffectsPending(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	// CreateNode makes an active node: the conditional promote must be a no-op,
	// which is what makes RecordHeartbeat race-safe (a node disabled between the
	// GetNode read and the promote is not reactivated).
	if rows, err := store.q.PromotePendingNodeToActive(ctx, "azus"); err != nil || rows != 0 {
		t.Fatalf("promote on active node: rows=%d err=%v, want 0 rows", rows, err)
	}
	if err := store.SetNodeStatus(ctx, "azus", "pending"); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.q.PromotePendingNodeToActive(ctx, "azus"); err != nil || rows != 1 {
		t.Fatalf("promote on pending node: rows=%d err=%v, want 1 row", rows, err)
	}
}
