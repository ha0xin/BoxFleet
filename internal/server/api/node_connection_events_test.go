package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestNodeConnectionEventsEndpoint(t *testing.T) {
	ctx := context.Background()
	store, raw := openConnectionAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	report := connectionReportFixture()
	// Posting the same window twice must be a no-op, because buckets are summed
	// into existing rows: a partially applied replay would inflate byte totals
	// with nothing left to detect it by.
	for attempt := 0; attempt < 2; attempt++ {
		rec := postConnectionReport(t, store, issued.Token, "azus", report)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, body = %s", attempt, rec.Code, rec.Body.String())
		}
	}
	events := connectionEventsPage(t, ctx, raw)
	if len(events.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(events.Events))
	}
	if events.Events[0].UplinkBytes != 4096 || events.Events[0].DownlinkBytes != 8192 {
		t.Fatalf("replay changed byte totals: %+v", events.Events[0])
	}
}

// The name in the body is decorative on every *Report, and this endpoint is no
// exception: a token for azus writing a report labelled "victim" must land on
// azus and never touch the other node.
func TestNodeConnectionEventsOverridesNodeName(t *testing.T) {
	ctx := context.Background()
	store, raw := openConnectionAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.CreateNode(ctx, "victim", "203.0.113.99", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	report := connectionReportFixture()
	report.NodeName = "victim"
	rec := postConnectionReport(t, store, issued.Token, "azus", report)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	azus, err := store.GetNode(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	events := connectionEventsPage(t, ctx, raw)
	if len(events.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(events.Events))
	}
	if events.Events[0].NodeID != azus.ID {
		t.Fatalf("event landed on node %q, want %q", events.Events[0].NodeID, azus.ID)
	}
}

// A body larger than the limit is refused before it is decoded, so a node
// token cannot be used to exhaust server memory.
func TestNodeConnectionEventsRejectsOversizedBody(t *testing.T) {
	ctx := context.Background()
	store, raw := openConnectionAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	report := connectionReportFixture()
	// One absurd string is enough: the limit is on the wire, not on the number
	// of buckets, and this keeps the fixture cheap to build.
	report.Buckets[0].TargetHost = strings.Repeat("a", maxNodeConnectionReportBytes+1024) + ".example.com"
	rec := postConnectionReport(t, store, issued.Token, "azus", report)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", rec.Code, rec.Body.String())
	}
	if events := connectionEventsPage(t, ctx, raw); len(events.Events) != 0 {
		t.Fatalf("oversized body stored %d events", len(events.Events))
	}
}

// Opt-in is checked in the request path, not only at render time. 403 is a
// distinct signal from a transient failure so the collector can stop instead of
// retrying forever.
func TestNodeConnectionEventsRequiresOptIn(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	rec := postConnectionReport(t, store, issued.Token, "azus", connectionReportFixture())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}

	// Enabling then disabling has to return to the refusing state, so an
	// operator turning telemetry off actually stops ingest.
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rec := postConnectionReport(t, store, issued.Token, "azus", connectionReportFixture()); rec.Code != http.StatusOK {
		t.Fatalf("status = %d after opting in, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	report := connectionReportFixture()
	report.Sequence = 99
	if rec := postConnectionReport(t, store, issued.Token, "azus", report); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d after opting out, want 403", rec.Code)
	}
}

func TestNodeConnectionEventsRequiresToken(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	body, err := json.Marshal(connectionReportFixture())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/node/connections", bytes.NewReader(body))
	req.Header.Set("X-BoxFleet-Node", "azus")
	rec := httptest.NewRecorder()
	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	if rec := postConnectionReport(t, store, "bfnt_not-a-real-token", "azus", connectionReportFixture()); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d for a bogus token, want 401", rec.Code)
	}
}

// Malformed JSON is a 400, and a structurally valid payload the store refuses
// is a 422 — the same split every other node report endpoint uses.
func TestNodeConnectionEventsRejectsBadPayloads(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if _, err := store.SetNodeConnectionTelemetry(ctx, db.SetNodeConnectionTelemetryParams{NodeName: "azus", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/node/connections", strings.NewReader("{not json"))
	req.Header.Set("X-BoxFleet-Node", "azus")
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	rec := httptest.NewRecorder()
	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d for malformed json, want 400", rec.Code)
	}

	report := connectionReportFixture()
	report.AgentBootID = ""
	if rec := postConnectionReport(t, store, issued.Token, "azus", report); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d for a report with no boot id, want 422", rec.Code)
	}
}

func postConnectionReport(t *testing.T, store *db.DB, token, nodeName string, report model.ConnectionReport) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/node/connections", bytes.NewReader(body))
	req.Header.Set("X-BoxFleet-Node", nodeName)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	NewRouter(Options{DB: store}).ServeHTTP(rec, req)
	return rec
}

func connectionReportFixture() model.ConnectionReport {
	return model.ConnectionReport{
		NodeName:    "azus",
		Sequence:    1,
		AgentBootID: "boot-1",
		WindowStart: "2026-07-26T12:00:00Z",
		WindowEnd:   "2026-07-26T12:05:00Z",
		ReportedAt:  "2026-07-26T12:05:01Z",
		Coverage: model.ConnectionCoverage{
			ConnectionsObserved:   4,
			ConnectionsAttributed: 4,
			BytesObserved:         12288,
			BytesAttributed:       12288,
		},
		Buckets: []model.ConnectionBucket{{
			BucketStart:       "2026-07-26T12:00:00Z",
			AuthName:          "vless-39090@alice",
			SourceIP:          "198.51.100.9",
			TargetHost:        "example.com",
			TargetPort:        443,
			Network:           "tcp",
			IPVersion:         4,
			Protocol:          "tls",
			Inbound:           "vless-39090",
			InboundType:       "vless",
			Outbound:          "direct",
			OutboundType:      "direct",
			Chain:             []string{"vless-39090", "direct"},
			ConnectionsOpened: 4,
			ConnectionsClosed: 4,
			UplinkBytes:       4096,
			DownlinkBytes:     8192,
			DurationMsTotal:   9000,
			WindowStart:       "2026-07-26T12:00:00Z",
			WindowEnd:         "2026-07-26T12:04:59Z",
		}},
	}
}

type connectionEventRow struct {
	NodeID        string
	TargetHost    string
	UplinkBytes   int64
	DownlinkBytes int64
}

type connectionEventList struct {
	Events []connectionEventRow
}

// openConnectionAPITestDB returns the facade plus a raw handle on the same
// file. There is no admin read endpoint for connection events on this branch,
// so ingest is asserted against the table directly rather than through a
// facade method that would exist only for tests.
func openConnectionAPITestDB(t *testing.T) (*db.DB, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boxfleet.db")
	store, err := db.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	})
	return store, raw
}

func connectionEventsPage(t *testing.T, ctx context.Context, raw *sql.DB) connectionEventList {
	t.Helper()
	rows, err := raw.QueryContext(ctx,
		`SELECT node_id, target_host, uplink_bytes, downlink_bytes FROM connection_events ORDER BY target_host`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	list := connectionEventList{}
	for rows.Next() {
		var row connectionEventRow
		if err := rows.Scan(&row.NodeID, &row.TargetHost, &row.UplinkBytes, &row.DownlinkBytes); err != nil {
			t.Fatal(err)
		}
		list.Events = append(list.Events, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return list
}
