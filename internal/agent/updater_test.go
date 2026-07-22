package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/renameio/v2"

	"github.com/haoxin/boxfleet/internal/model"
)

func TestUpdateDownloadStreamsResumesAndVerifies(t *testing.T) {
	t.Parallel()
	data := bytes.Repeat([]byte("boxfleet-streaming-update\n"), 128*1024)
	checksum := testAssetSHA256(data)
	var resumed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/asset":
			w.Header().Set("Accept-Ranges", "bytes")
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				return
			}
			start := 0
			if rawRange := r.Header.Get("Range"); strings.HasPrefix(rawRange, "bytes=") {
				parsed, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rawRange, "bytes="), "-"))
				if err != nil {
					t.Errorf("invalid range %q: %v", rawRange, err)
					return
				}
				start = parsed
				resumed.Store(true)
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
				w.WriteHeader(http.StatusPartialContent)
			} else {
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			}
			_, _ = w.Write(data[start:])
		case strings.Contains(r.URL.Path, "/events"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	installDir := t.TempDir()
	operationID := "op_streaming"
	partial := filepath.Join(installDir, "downloads", operationID, "boxfleet-agent-"+checksum+".partial")
	if err := os.MkdirAll(filepath.Dir(partial), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, data[:len(data)/3], 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(Config{
		NodeName: "edge", Token: "token", ServerURL: server.URL, InstallDir: installDir,
		OperationStatePath: filepath.Join(installDir, "state", "operation.json"),
	})
	state := &OperationState{Assignment: model.NodeOperationAssignment{
		ID: operationID, Kind: "update.agent", Attempt: 1, LeaseToken: "lease",
	}}
	asset := model.UpdateAsset{
		Component: "agent", Version: "v1.2.3", URL: server.URL + "/asset",
		SHA256: checksum, Size: int64(len(data)),
	}
	path, err := a.downloadAndInstallCandidate(context.Background(), state, asset)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Load() {
		t.Fatal("download did not issue a range request for the durable partial file")
	}
	valid, err := fileMatchesAsset(path, asset)
	if err != nil || !valid {
		t.Fatalf("candidate verification = %v, %v", valid, err)
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestUpdateDownloadDeletesChecksumMismatch(t *testing.T) {
	t.Parallel()
	data := []byte("corrupt release")
	server := updaterAssetAndEventServer(data, nil)
	t.Cleanup(server.Close)
	installDir := t.TempDir()
	wrong := strings.Repeat("0", sha256.Size*2)
	a := New(Config{
		NodeName: "edge", Token: "token", ServerURL: server.URL, InstallDir: installDir,
		OperationStatePath: filepath.Join(installDir, "state", "operation.json"),
	})
	state := &OperationState{Assignment: model.NodeOperationAssignment{ID: "op_bad_hash", Attempt: 1, LeaseToken: "lease"}}
	asset := model.UpdateAsset{Component: "agent", Version: "v1.2.3", URL: server.URL + "/asset", SHA256: wrong, Size: int64(len(data))}
	if _, err := a.downloadAndInstallCandidate(context.Background(), state, asset); err == nil {
		t.Fatal("checksum mismatch unexpectedly succeeded")
	}
	partial := filepath.Join(installDir, "downloads", state.Assignment.ID, "boxfleet-agent-"+wrong+".partial")
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checksum-mismatched partial was not deleted: %v", err)
	}
}

func TestSingBoxUpdateRollsBackFailedRestart(t *testing.T) {
	t.Parallel()
	candidateData := []byte("new sing-box")
	server := updaterAssetAndEventServer(candidateData, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/node/config":
			w.Header().Set("X-BoxFleet-Config-SHA256", testAssetSHA256([]byte(`{}`)))
			_, _ = w.Write([]byte(`{}`))
			return true
		case "/api/node/traffic":
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		return false
	})
	t.Cleanup(server.Close)
	installDir := t.TempDir()
	binDir := filepath.Join(installDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(binDir, "sing-box")
	if err := os.WriteFile(currentPath, []byte("old sing-box"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(installDir, "etc", "sing-box.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &updateRunner{singBoxPath: currentPath}
	a := New(Config{
		NodeName: "edge", Token: "token", ServerURL: server.URL, InstallDir: installDir,
		SingBoxPath: currentPath, SingBoxConfig: configPath, SingBoxService: "sing-box.service",
		OperationStatePath: filepath.Join(installDir, "state", "operation.json"),
	})
	a.Runner = runner
	a.TrafficReporter = func(context.Context) error { return nil }
	state := &OperationState{Assignment: model.NodeOperationAssignment{ID: "op_rollback", Attempt: 1, LeaseToken: "lease"}}
	checkpoint := updateCheckpoint{}
	asset := model.UpdateAsset{
		Component: "sing_box", Version: "v1.13.13", URL: server.URL + "/asset",
		SHA256: testAssetSHA256(candidateData), Size: int64(len(candidateData)),
	}
	err := a.updateSingBox(context.Background(), state, &checkpoint, asset, &atomic.Bool{})
	if err == nil || !strings.Contains(err.Error(), "automatically rolled back") {
		t.Fatalf("update error = %v", err)
	}
	resolved, resolveErr := filepath.EvalSymlinks(currentPath)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if strings.Contains(resolved, "1.13.13") || !checkpoint.SingBoxRolledBack {
		t.Fatalf("rollback target = %s, checkpoint = %+v", resolved, checkpoint)
	}
	if runner.failedCandidateRestarts != 1 || runner.oldRestarts != 1 {
		t.Fatalf("runner restarts = candidate %d old %d", runner.failedCandidateRestarts, runner.oldRestarts)
	}
}

func TestSingBoxUpdateKeepsDisabledNodeStopped(t *testing.T) {
	t.Parallel()
	candidateData := []byte("new sing-box")
	server := updaterAssetAndEventServer(candidateData, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/api/node/config":
			w.Header().Set("X-BoxFleet-Node-State", "disabled")
			w.Header().Set("X-BoxFleet-Config-SHA256", testAssetSHA256([]byte(`{}`)))
			_, _ = w.Write([]byte(`{}`))
			return true
		case "/api/node/heartbeat", "/api/node/traffic":
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		return false
	})
	t.Cleanup(server.Close)
	installDir := t.TempDir()
	binDir := filepath.Join(installDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(binDir, "sing-box")
	if err := os.WriteFile(currentPath, []byte("old sing-box"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(installDir, "etc", "sing-box.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &disabledUpdateRunner{}
	a := New(Config{
		NodeName: "edge", Token: "token", ServerURL: server.URL, InstallDir: installDir,
		SingBoxPath: currentPath, SingBoxConfig: configPath, SingBoxService: "sing-box.service",
		OperationStatePath: filepath.Join(installDir, "state", "operation.json"),
	})
	a.Runner = runner
	a.TrafficReporter = func(context.Context) error { return nil }
	state := &OperationState{Assignment: model.NodeOperationAssignment{ID: "op_disabled", Attempt: 1, LeaseToken: "lease"}}
	checkpoint := updateCheckpoint{}
	asset := model.UpdateAsset{
		Component: "sing_box", Version: "v1.13.13", URL: server.URL + "/asset",
		SHA256: testAssetSHA256(candidateData), Size: int64(len(candidateData)),
	}
	if err := a.updateSingBox(context.Background(), state, &checkpoint, asset, &atomic.Bool{}); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved, "1.13.13") || !checkpoint.SingBoxConfirmed || checkpoint.SingBoxRolledBack {
		t.Fatalf("disabled update target = %s, checkpoint = %+v", resolved, checkpoint)
	}
	if runner.stops != 1 || runner.restarts != 0 {
		t.Fatalf("disabled service actions: stops=%d restarts=%d", runner.stops, runner.restarts)
	}
}

func TestAgentGuardRollsBackAfterThreeFailedStarts(t *testing.T) {
	t.Parallel()
	installDir := t.TempDir()
	previous := filepath.Join(installDir, "releases", "boxfleet-agent", "1.0.0", "boxfleet-agent")
	candidate := filepath.Join(installDir, "releases", "boxfleet-agent", "2.0.0", "boxfleet-agent")
	for _, path := range []string{previous, candidate} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	agentPath := filepath.Join(installDir, "bin", "boxfleet-agent")
	if err := os.MkdirAll(filepath.Dir(agentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renameio.Symlink(candidate, agentPath); err != nil {
		t.Fatal(err)
	}
	a := New(Config{NodeName: "edge", Token: "token", ServerURL: "https://example.invalid", InstallDir: installDir, AgentPath: agentPath})
	if err := a.writeAgentUpdateGuard(AgentUpdateGuardState{
		OperationID: "op_guard", ExpectedVersion: "v2.0.0", PreviousTarget: previous, CandidateTarget: candidate,
	}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxAgentGuardStarts; attempt++ {
		if err := a.RunAgentGuard(); err != nil {
			t.Fatalf("guard attempt %d: %v", attempt, err)
		}
		resolved, err := filepath.EvalSymlinks(agentPath)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < maxAgentGuardStarts && !samePath(resolved, candidate) {
			t.Fatalf("attempt %d rolled back too early to %s", attempt, resolved)
		}
		if attempt == maxAgentGuardStarts && !samePath(resolved, previous) {
			t.Fatalf("attempt %d did not rollback to %s; got %s", attempt, previous, resolved)
		}
	}
	guard, err := a.loadAgentUpdateGuard()
	if err != nil || guard == nil || guard.Status != "rolled_back" {
		t.Fatalf("guard state = %+v, %v", guard, err)
	}
}

type updateRunner struct {
	singBoxPath             string
	failedCandidateRestarts int
	oldRestarts             int
}

type disabledUpdateRunner struct {
	stops    int
	restarts int
}

func (r *disabledUpdateRunner) Run(_ context.Context, name string, args ...string) error {
	if name == "systemctl" && len(args) == 2 {
		switch args[0] {
		case "stop":
			r.stops++
		case "restart":
			r.restarts++
		}
	}
	return nil
}

func (*disabledUpdateRunner) Output(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name == "systemctl" {
		return []byte("inactive\n"), nil
	}
	return []byte("sing-box version 1.13.13\nTags: with_v2ray_api\n"), nil
}

func (*disabledUpdateRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func (r *updateRunner) Run(_ context.Context, name string, args ...string) error {
	if name == "systemctl" && len(args) == 2 && args[0] == "restart" {
		resolved, _ := filepath.EvalSymlinks(r.singBoxPath)
		if strings.Contains(resolved, "1.13.13") {
			r.failedCandidateRestarts++
			return errors.New("candidate service failed")
		}
		r.oldRestarts++
		return nil
	}
	return nil
}

func (r *updateRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "systemctl" {
		return []byte("active\n"), nil
	}
	return []byte("sing-box version 1.13.13\nTags: with_v2ray_api\n"), nil
}

func (r *updateRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func updaterAssetAndEventServer(data []byte, extra func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extra != nil && extra(w, r) {
			return
		}
		switch {
		case r.URL.Path == "/asset":
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			_, _ = w.Write(data)
		case strings.Contains(r.URL.Path, "/events"):
			var report model.NodeOperationEventReport
			_ = json.NewDecoder(r.Body).Decode(&report)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func testAssetSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
