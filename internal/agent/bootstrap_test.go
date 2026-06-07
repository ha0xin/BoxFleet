package agent

import (
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
