package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInstallResumesWithoutWaitingForTelemetry(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "disabled"}[disabled], func(t *testing.T) {
			a, r := serviceTestAgent(t)
			reports := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/node/config" {
					if disabled {
						w.Header().Set("X-BoxFleet-Node-State", "disabled")
					}
					_, _ = w.Write([]byte(`{"inbounds":[]}`))
					return
				}
				reports++
				http.Error(w, "telemetry unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			a.Config.ServerURL = server.URL
			if err := atomicWrite(a.Config.SingBoxPath, []byte("fake sing-box"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(a.Config.AgentPath, []byte("fake agent"), 0o755); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := a.Install(context.Background()); err != nil {
					t.Fatalf("install %d: %v", i, err)
				}
				if !r.active[a.Config.AgentService] {
					t.Fatal("agent not started")
				}
				if r.active[a.Config.SingBoxService] == disabled {
					t.Fatal("sing-box state disagrees with node state")
				}
			}
			if reports != 0 {
				t.Fatalf("initial installation waited for %d telemetry reports", reports)
			}
			if len(r.stops) == 0 {
				t.Fatal("recovery did not stop the existing daemon")
			}
		})
	}
}

func TestInterruptedRestartDoesNotRollbackConfig(t *testing.T) {
	a, r := serviceTestAgent(t)
	r.blockRestart = true
	a.ServiceCommandTimeout = 10 * time.Millisecond
	old := []byte(`{"old":true}`)
	desired := []byte(`{"new":true}`)
	if err := atomicWrite(a.Config.SingBoxConfig, old, 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { _, _ = w.Write(desired) }))
	defer server.Close()
	a.Config.ServerURL = server.URL
	err := a.Once(context.Background())
	if !errors.Is(err, errServiceOperationUncertain) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(a.Config.SingBoxConfig)
	if string(data) != string(desired) {
		t.Fatalf("uncertain job caused rollback: %s", data)
	}
	if len(r.restarts) != 1 {
		t.Fatalf("issued conflicting restarts: %v", r.restarts)
	}
}

func TestFailedHeartbeatDoesNotConfirmAgentUpdate(t *testing.T) {
	previousVersion := Version
	Version = "v2.0.0"
	defer func() { Version = previousVersion }()
	a, _ := serviceTestAgent(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/node/config" {
			_, _ = w.Write([]byte(`{"inbounds":[]}`))
			return
		}
		if strings.Contains(req.URL.Path, "heartbeat") {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	a.Config.ServerURL = server.URL
	if err := a.writeAgentUpdateGuard(AgentUpdateGuardState{ExpectedVersion: Version}); err != nil {
		t.Fatal(err)
	}
	if err := a.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	guard, err := a.loadAgentUpdateGuard()
	if err != nil {
		t.Fatal(err)
	}
	if guard == nil || guard.Status != "pending" {
		t.Fatalf("failed heartbeat confirmed upgrade: %+v", guard)
	}
}

func TestInterruptedAgentStartCanResumeHalfInstallation(t *testing.T) {
	a, r := serviceTestAgent(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { _, _ = w.Write([]byte(`{"inbounds":[]}`)) }))
	defer server.Close()
	a.Config.ServerURL = server.URL
	for _, path := range []string{a.Config.AgentPath, a.Config.SingBoxPath} {
		if err := atomicWrite(path, []byte("fake binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.blockRestart = true
	r.blockService = a.Config.AgentService
	a.ServiceCommandTimeout = 10 * time.Millisecond
	if err := a.Install(context.Background()); !errors.Is(err, errServiceOperationUncertain) {
		t.Fatalf("expected interrupted service start: %v", err)
	}
	if !r.active[a.Config.SingBoxService] || r.active[a.Config.AgentService] {
		t.Fatalf("unexpected partial install state: %v", r.active)
	}
	r.blockRestart = false
	if err := a.Install(context.Background()); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if !r.active[a.Config.AgentService] || !r.active[a.Config.SingBoxService] {
		t.Fatalf("recovery left partial state: %v", r.active)
	}
}

func TestConcurrentInstallerDoesNotStopRunningAgent(t *testing.T) {
	a, r := serviceTestAgent(t)
	if err := a.InstallSystemdUnits(); err != nil {
		t.Fatal(err)
	}
	r.active[a.Config.AgentService] = true
	unlock, err := a.lockFile("install.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := a.Install(context.Background()); err == nil {
		t.Fatal("concurrent installation was allowed")
	}
	if len(r.stops) != 0 || !r.active[a.Config.AgentService] {
		t.Fatal("rejected installer stopped the existing agent")
	}
}
