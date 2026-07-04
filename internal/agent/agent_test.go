package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
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

func TestFetchConfigAdoptsCanonicalNodeName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "agent.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-BoxFleet-Node") != "old-name" {
			t.Fatalf("node header = %q", r.Header.Get("X-BoxFleet-Node"))
		}
		w.Header().Set(model.CanonicalNodeNameHeader, "new-name")
		_, _ = w.Write([]byte(`{"inbounds":[]}`))
	}))
	defer server.Close()

	a := New(Config{
		NodeName:        "old-name",
		Token:           "secret",
		ServerURL:       server.URL,
		AgentConfigPath: configPath,
	})
	if _, err := a.FetchConfigVersioned(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.Config.NodeName != "new-name" {
		t.Fatalf("in-memory node name = %q", a.Config.NodeName)
	}
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NodeName != "new-name" {
		t.Fatalf("persisted node name = %q", loaded.NodeName)
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

func TestExecRunnerStreamLinesDrainsLargeStderr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var lines int
	err := (ExecRunner{}).StreamLines(ctx, os.Args[0], []string{"-test.run=TestHelperProcess", "--", "stream-lines-large-stderr"}, func(line string) bool {
		if line == "stdout-ok" {
			lines++
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines != 1 {
		t.Fatalf("lines = %d, want 1", lines)
	}
}

func TestOnceRetriesRestartWhenConfigFileMatchesButApplyWasNotSaved(t *testing.T) {
	t.Parallel()
	config := []byte(`{"inbounds":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/node/config":
			w.Header().Set("X-BoxFleet-Config-Version-ID", "cfg_1")
			w.Header().Set("X-BoxFleet-Config-SHA256", bytesSHA256Hex(config))
			_, _ = w.Write(config)
		case "/api/node/apply-result", "/api/node/heartbeat", "/api/node/traffic", "/api/node/logs", "/api/node/system-logs":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "sing-box.json")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &restartFailRunner{}
	a := New(Config{
		NodeName:        "azus",
		Token:           "secret",
		ServerURL:       server.URL,
		SingBoxPath:     "sing-box",
		SingBoxConfig:   configPath,
		SingBoxService:  "sing-box.service",
		StatePath:       filepath.Join(tmp, "state.json"),
		V2RayAPIAddress: "127.0.0.1:1",
	})
	a.Runner = runner
	if err := a.Once(context.Background()); err == nil {
		t.Fatal("Once succeeded despite restart failure")
	}
	if runner.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", runner.restarts)
	}
}

func TestOnceDisabledStopsSingBoxThenRestartsOnReEnable(t *testing.T) {
	t.Parallel()
	config := []byte(`{"inbounds":[]}`)
	var disabled atomic.Bool
	disabled.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/node/config":
			if disabled.Load() {
				w.Header().Set("X-BoxFleet-Node-State", "disabled")
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("X-BoxFleet-Config-Version-ID", "cfg_1")
			w.Header().Set("X-BoxFleet-Config-SHA256", bytesSHA256Hex(config))
			_, _ = w.Write(config)
		case "/api/node/apply-result", "/api/node/heartbeat", "/api/node/traffic", "/api/node/logs", "/api/node/system-logs":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "sing-box.json")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{running: true}
	a := New(Config{
		NodeName:        "azus",
		Token:           "secret",
		ServerURL:       server.URL,
		SingBoxPath:     "sing-box",
		SingBoxConfig:   configPath,
		SingBoxService:  "sing-box.service",
		StatePath:       filepath.Join(tmp, "state.json"),
		V2RayAPIAddress: "127.0.0.1:1",
	})
	a.Runner = runner

	// Disabled while running: stops sing-box once. The next poll sees it inactive
	// and does not re-stop. Never restarts while disabled.
	if err := a.Once(context.Background()); err != nil {
		t.Fatalf("first disabled Once: %v", err)
	}
	if err := a.Once(context.Background()); err != nil {
		t.Fatalf("second disabled Once: %v", err)
	}
	if runner.stops != 1 {
		t.Fatalf("stops = %d, want 1 (second poll sees inactive)", runner.stops)
	}
	if runner.restarts != 0 {
		t.Fatalf("restarts = %d, want 0 while disabled", runner.restarts)
	}

	// Reboot of a disabled host re-starts the systemd-enabled unit; the next poll
	// detects it active and stops it again (no marker is trusted).
	runner.running = true
	if err := a.Once(context.Background()); err != nil {
		t.Fatalf("post-reboot disabled Once: %v", err)
	}
	if runner.stops != 2 {
		t.Fatalf("stops = %d, want 2 after reboot re-start", runner.stops)
	}

	// Re-enable: even though the config bytes are unchanged, sing-box is inactive
	// so it restarts.
	disabled.Store(false)
	if err := a.Once(context.Background()); err != nil {
		t.Fatalf("re-enabled Once: %v", err)
	}
	if runner.restarts != 1 {
		t.Fatalf("restarts = %d, want 1 after re-enable", runner.restarts)
	}
}

type recordingRunner struct {
	stops    int
	restarts int
	running  bool
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	if name == "systemctl" && len(args) == 2 {
		switch args[0] {
		case "stop":
			r.stops++
			r.running = false
		case "restart":
			r.restarts++
			r.running = true
		}
	}
	return nil
}

func (r *recordingRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	// `systemctl show -p ActiveState --value <unit>` succeeds for any unit state.
	if name == "systemctl" && len(args) >= 1 && args[0] == "show" {
		if r.running {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), nil
	}
	return []byte("sing-box test"), nil
}

func (r *recordingRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func TestOnceRestartsWhenServiceNotConfirmedActive(t *testing.T) {
	t.Parallel()
	config := []byte(`{"inbounds":[]}`)
	hash := bytesSHA256Hex(config)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/node/config":
			w.Header().Set("X-BoxFleet-Config-Version-ID", "cfg_1")
			w.Header().Set("X-BoxFleet-Config-SHA256", hash)
			_, _ = w.Write(config)
		case "/api/node/apply-result", "/api/node/heartbeat", "/api/node/traffic", "/api/node/logs", "/api/node/system-logs":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "sing-box.json")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &probeErrorRunner{}
	a := New(Config{
		NodeName:        "azus",
		Token:           "secret",
		ServerURL:       server.URL,
		SingBoxPath:     "sing-box",
		SingBoxConfig:   configPath,
		SingBoxService:  "sing-box.service",
		StatePath:       filepath.Join(tmp, "state.json"),
		V2RayAPIAddress: "127.0.0.1:1",
	})
	a.Runner = runner
	// Config bytes and applied hash already match, so the only thing that would
	// skip the restart is a *confirmed-active* probe. The probe errors here, so
	// the agent must restart rather than assume the re-enabled node is up.
	if err := a.SaveState(State{AppliedConfigHash: hash}); err != nil {
		t.Fatal(err)
	}
	if err := a.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if runner.restarts != 1 {
		t.Fatalf("restarts = %d, want 1 (probe unknown must not skip restart)", runner.restarts)
	}
}

// probeErrorRunner fails the `systemctl show ActiveState` probe (unknown state).
type probeErrorRunner struct{ restarts int }

func (r *probeErrorRunner) Run(_ context.Context, name string, args ...string) error {
	if name == "systemctl" && len(args) == 2 && args[0] == "restart" {
		r.restarts++
	}
	return nil
}

func (r *probeErrorRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "systemctl" && len(args) >= 1 && args[0] == "show" {
		return nil, errors.New("dbus unavailable")
	}
	return []byte("sing-box test"), nil
}

func (r *probeErrorRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func TestHelperProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "stream-lines-large-stderr":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 256*1024))
		_, _ = fmt.Fprintln(os.Stdout, "stdout-ok")
		os.Exit(0)
	default:
		return
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

type restartFailRunner struct {
	restarts int
}

func (r *restartFailRunner) Run(_ context.Context, name string, args ...string) error {
	if name == "systemctl" && len(args) == 2 && args[0] == "restart" {
		r.restarts++
		return errors.New("restart failed")
	}
	return nil
}

func (r *restartFailRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return []byte("sing-box test"), nil
}

func (r *restartFailRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}
