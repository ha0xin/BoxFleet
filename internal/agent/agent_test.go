package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteLoadConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	config := Config{
		NodeName:  "azus",
		Token:     "secret",
		ServerURL: "http://100.72.18.128:18081",
	}
	if err := WriteConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SingBoxPath != "/opt/boxfleet/bin/sing-box" {
		t.Fatalf("SingBoxPath = %q", loaded.SingBoxPath)
	}
	if loaded.AgentConfigPath != DefaultConfigPath {
		t.Fatalf("AgentConfigPath = %q", loaded.AgentConfigPath)
	}
}

func TestFetchConfigVersioned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-BoxFleet-Node") != "azus" {
			t.Fatalf("node header = %q", r.Header.Get("X-BoxFleet-Node"))
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"inbounds":[]}`))
	}))
	defer server.Close()

	a := New(Config{NodeName: "azus", Token: "secret", ServerURL: server.URL})
	response, err := a.FetchConfigVersioned(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw := response.Data
	if string(raw) != `{"inbounds":[]}` {
		t.Fatalf("config = %s", raw)
	}
}

func TestParseUserTrafficStat(t *testing.T) {
	authName, direction, ok := parseUserTrafficStat("user>>>vless-39090@alice>>>traffic>>>downlink")
	if !ok {
		t.Fatal("stat name was not parsed")
	}
	if authName != "vless-39090@alice" || direction != "downlink" {
		t.Fatalf("authName=%q direction=%q", authName, direction)
	}
	if _, _, ok := parseUserTrafficStat("user>>>vless-39090@alice>>>traffic"); ok {
		t.Fatal("malformed stat name parsed as valid")
	}
}

func TestParseJournalJSONLine(t *testing.T) {
	entry, ok := parseJournalJSONLine(`{"__CURSOR":"cursor-1","__REALTIME_TIMESTAMP":"1715831731523456","MESSAGE":"inbound connection"}`)
	if !ok {
		t.Fatal("journal line was not parsed")
	}
	if entry.Cursor != "cursor-1" || entry.Message != "inbound connection" || entry.ObservedAt != "2024-05-16T03:55:31.523456Z" {
		t.Fatalf("entry = %#v", entry)
	}
	if _, ok := parseJournalJSONLine(`not-json`); ok {
		t.Fatal("invalid json parsed")
	}
	entry, ok = parseJournalJSONLine(`{"__CURSOR":"cursor-2","__REALTIME_TIMESTAMP":"1715831731523456","MESSAGE":[105,110,98,111,117,110,100]}`)
	if !ok {
		t.Fatal("journal byte-array line was not parsed")
	}
	if entry.Cursor != "cursor-2" || entry.Message != "inbound" {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestReportLogsStreamsAndSplitsBatches(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node/logs" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var payload struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		batchSizes = append(batchSizes, len(payload.Events))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	lines := make([]string, 0, 250)
	for i := 1; i <= 250; i++ {
		lines = append(lines, fmt.Sprintf(`{"__CURSOR":"cursor-%03d","__REALTIME_TIMESTAMP":"1715831731523456","MESSAGE":"line %03d"}`, i, i))
	}
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	a := New(Config{
		NodeName:       "azus",
		Token:          "secret",
		ServerURL:      server.URL,
		StatePath:      statePath,
		SingBoxService: "sing-box.service",
	})
	a.Runner = streamLinesRunner{lines: lines}

	if err := a.ReportLogs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(batchSizes), "[100 100 50]"; got != want {
		t.Fatalf("batch sizes = %s, want %s", got, want)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.LastLogCursor != "cursor-250" {
		t.Fatalf("LastLogCursor = %q", state.LastLogCursor)
	}
}

type streamLinesRunner struct {
	lines []string
}

func (r streamLinesRunner) Run(context.Context, string, ...string) error {
	return nil
}

func (r streamLinesRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func (r streamLinesRunner) StreamLines(_ context.Context, _ string, _ []string, handle func(line string) bool) error {
	for _, line := range r.lines {
		if !handle(line) {
			return nil
		}
	}
	return nil
}
