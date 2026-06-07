package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/haoxin/boxfleet/internal/model"
)

func TestConfigFromBootstrap(t *testing.T) {
	config := ConfigFromBootstrap(model.BootstrapConfig{
		NodeName:        "azus",
		Token:           "secret",
		ServerURL:       "http://100.64.0.1:18081",
		SingBoxURL:      "https://example.test/sing-box",
		InstallDir:      "/opt/test",
		AgentConfigPath: "/etc/boxfleet/agent.json",
	})
	config.ApplyDefaults()
	if config.NodeName != "azus" || config.Token != "secret" || config.ServerURL != "http://100.64.0.1:18081" {
		t.Fatalf("config = %#v", config)
	}
	if config.SingBoxPath != "/opt/test/bin/sing-box" || config.AgentPath != "/opt/test/bin/boxfleet-agent" {
		t.Fatalf("paths = %#v", config)
	}
}

func TestSamePathResolvesEquivalentPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boxfleet-agent")
	if err := os.WriteFile(path, []byte("agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !samePath(path, filepath.Join(dir, ".", "boxfleet-agent")) {
		t.Fatal("equivalent paths were not detected")
	}
	if samePath(path, filepath.Join(dir, "other")) {
		t.Fatal("different paths were treated as equivalent")
	}
}
