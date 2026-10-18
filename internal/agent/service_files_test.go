package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/renameio/v2"
)

type serviceFilesRunner struct {
	onReload     func()
	reloads      int
	failReload   bool
	blockRestart bool
	blockService string
	active       map[string]bool
	restarts     []string
	stops        []string
}

func (r *serviceFilesRunner) Run(ctx context.Context, name string, args ...string) error {
	if name == "systemctl" || name == "rc-service" {
		if args[0] == "daemon-reload" {
			r.reloads++
			if r.onReload != nil {
				r.onReload()
			}
			if r.failReload && r.reloads == 1 {
				return errors.New("reload failed")
			}
		}
		if args[0] == "restart" {
			r.restarts = append(r.restarts, args[1])
			if r.blockRestart && (r.blockService == "" || r.blockService == args[1]) {
				<-ctx.Done()
				return ctx.Err()
			}
			r.active[args[1]] = true
		}
		if args[0] == "stop" {
			r.stops = append(r.stops, args[1])
			r.active[args[1]] = false
		}
	}
	return nil
}
func (r *serviceFilesRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "systemctl" {
		if r.active[args[len(args)-1]] {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), nil
	}
	return []byte("sing-box version 1.14.2\nTags: with_v2ray_api"), nil
}
func (*serviceFilesRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func serviceTestAgent(t *testing.T) (*Agent, *serviceFilesRunner) {
	t.Helper()
	dir := t.TempDir()
	a := New(Config{NodeName: "edge", Token: "secret", ServerURL: "https://example.invalid", InstallDir: dir, V2RayAPIAddress: "127.0.0.1:1"})
	a.ServiceDir = filepath.Join(dir, "units")
	r := &serviceFilesRunner{active: map[string]bool{}}
	a.Runner = r
	return a, r
}

func TestServiceSyncMigratesBothLegacyUnitsAndIsIdempotent(t *testing.T) {
	a, r := serviceTestAgent(t)
	if err := os.MkdirAll(a.ServiceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{a.Config.AgentService, a.Config.SingBoxService} {
		if err := os.WriteFile(filepath.Join(a.ServiceDir, name), []byte("[Unit]\nAfter=network-online.target\nWants=network-online.target\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.syncServiceUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := map[string]time.Time{}
	for _, name := range []string{a.Config.AgentService, a.Config.SingBoxService} {
		path := filepath.Join(a.ServiceDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "network-online.target") || !strings.Contains(string(data), "After=network.target") || !strings.Contains(string(data), "StartLimitIntervalSec=0") {
			t.Fatalf("unexpected unit: %s", data)
		}
		info, _ := os.Stat(path)
		before[path] = info.ModTime()
	}
	if err := a.syncServiceUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.reloads != 1 {
		t.Fatalf("reloads = %d", r.reloads)
	}
	for path, mtime := range before {
		info, _ := os.Stat(path)
		if !info.ModTime().Equal(mtime) {
			t.Fatalf("unchanged unit rewritten: %s", path)
		}
	}
	if len(r.restarts) != 0 {
		t.Fatalf("template migration restarted services: %v", r.restarts)
	}
}

func TestServiceSyncRestoresFilesWhenReloadFails(t *testing.T) {
	a, r := serviceTestAgent(t)
	r.failReload = true
	if err := os.MkdirAll(a.ServiceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.ServiceDir, a.Config.AgentService)
	if err := os.WriteFile(path, []byte("old agent unit"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := a.syncServiceUnits(context.Background()); err == nil {
		t.Fatal("reload failure ignored")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "old agent unit" {
		t.Fatalf("old unit not restored: %s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o640 {
		t.Fatal("old permissions not restored")
	}
	if _, err := os.Stat(filepath.Join(a.ServiceDir, a.Config.SingBoxService)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new unit not removed: %v", err)
	}
	if r.reloads != 2 {
		t.Fatalf("restored units not reloaded: %d", r.reloads)
	}
}

func TestServiceBackupSurvivesRestartAndGuardRestoresIt(t *testing.T) {
	previousVersion := Version
	Version = "v2.0.0"
	defer func() { Version = previousVersion }()
	a, r := serviceTestAgent(t)
	previous := filepath.Join(a.Config.InstallDir, "releases", "boxfleet-agent", "1.0.0", "boxfleet-agent")
	candidate := filepath.Join(a.Config.InstallDir, "releases", "boxfleet-agent", "2.0.0", "boxfleet-agent")
	for _, path := range []string{previous, candidate} {
		if err := atomicWrite(path, []byte(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(a.Config.AgentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renameio.Symlink(candidate, a.Config.AgentPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.ServiceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.ServiceDir, a.Config.AgentService)
	if err := os.WriteFile(path, []byte("previous agent unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.writeAgentUpdateGuard(AgentUpdateGuardState{ExpectedVersion: Version, PreviousTarget: previous, CandidateTarget: candidate}); err != nil {
		t.Fatal(err)
	}
	if err := a.syncServiceUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Restarted candidate must not replace the snapshot with its own new units.
	if err := a.syncServiceUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxAgentGuardStarts; i++ {
		if err := a.RunAgentGuard(); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	if string(data) != "previous agent unit" {
		t.Fatalf("rollback left new unit: %s", data)
	}
	if _, err := os.Stat(filepath.Join(a.ServiceDir, a.Config.SingBoxService)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback failed to restore missing unit: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(a.Config.AgentPath)
	if resolved != previous {
		t.Fatalf("binary not rolled back: %s", resolved)
	}
	if r.reloads < 2 {
		t.Fatal("restored services not reloaded")
	}
}

func TestOpenRCServiceSyncUpdatesScriptsWithoutStartingSingBox(t *testing.T) {
	a, r := serviceTestAgent(t)
	a.Config.InitSystem = InitSystemOpenRC
	if err := atomicWrite(filepath.Join(a.ServiceDir, serviceName(InitSystemOpenRC, a.Config.AgentService)), []byte("old non-executable script"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := a.syncServiceUnits(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{a.Config.AgentService, a.Config.SingBoxService} {
		path := filepath.Join(a.ServiceDir, serviceName(InitSystemOpenRC, name))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "supervise-daemon") {
			t.Fatalf("unexpected script %s", data)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o755 {
			t.Fatal("OpenRC script not executable")
		}
	}
	if r.reloads != 0 || len(r.restarts) != 0 {
		t.Fatal("script sync changed runtime state")
	}
}

func TestRuntimeLockPreventsConcurrentWritersAndReleases(t *testing.T) {
	a, _ := serviceTestAgent(t)
	unlock, err := a.lockRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.lockRuntime(); err == nil {
		t.Fatal("second writer acquired lock")
	}
	unlock()
	unlock, err = a.lockRuntime()
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestHealthyCandidateRefreshesLegacyGuardBeforeMigration(t *testing.T) {
	previousVersion := Version
	Version = "v2.0.0"
	defer func() { Version = previousVersion }()
	a, r := serviceTestAgent(t)
	previous := filepath.Join(a.Config.InstallDir, "releases", "boxfleet-agent", "1.0.0", "boxfleet-agent")
	candidate := filepath.Join(a.Config.InstallDir, "releases", "boxfleet-agent", "2.0.0", "boxfleet-agent")
	for path, data := range map[string]string{previous: "previous binary", candidate: "candidate binary", a.Config.AgentGuardPath: "legacy guard"} {
		if err := atomicWrite(path, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(a.Config.AgentPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := renameio.Symlink(candidate, a.Config.AgentPath); err != nil {
		t.Fatal(err)
	}
	if err := a.writeAgentUpdateGuard(AgentUpdateGuardState{ExpectedVersion: Version, PreviousTarget: previous, CandidateTarget: candidate}); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmAgentUpdateGuard(); err == nil {
		t.Fatal("confirmation accepted without heartbeat")
	}
	guardData, _ := os.ReadFile(a.Config.AgentGuardPath)
	if string(guardData) != "legacy guard" {
		t.Fatal("unhealthy candidate replaced legacy guard")
	}
	if r.reloads != 0 {
		t.Fatal("unhealthy candidate migrated units")
	}
	a.heartbeatConfirmed.Store(true)
	r.onReload = func() {
		guardData, _ := os.ReadFile(a.Config.AgentGuardPath)
		if string(guardData) != "candidate binary" {
			t.Error("service migration preceded guard refresh")
		}
		if _, err := os.Stat(a.serviceBackupPath()); err != nil {
			t.Errorf("migration missing durable snapshot: %v", err)
		}
	}
	if err := a.ConfirmAgentUpdateGuard(); err != nil {
		t.Fatal(err)
	}
	guard, err := a.loadAgentUpdateGuard()
	if err != nil || guard != nil {
		t.Fatalf("guard not confirmed: %+v %v", guard, err)
	}
	if _, err := os.Stat(a.serviceBackupPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed backup retained: %v", err)
	}
}
