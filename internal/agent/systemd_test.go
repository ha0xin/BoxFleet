package agent

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitsQuotesPaths(t *testing.T) {
	data := systemdUnitData{
		SingBoxPath:       "/opt/boxfleet/bin/sing box",
		SingBoxConfig:     "/etc/boxfleet/sing-box.json",
		AgentPath:         "/opt/boxfleet/bin/boxfleet-agent",
		AgentGuardPath:    "/opt/boxfleet/libexec/boxfleet-agent-guard",
		AgentConfigPath:   "/etc/boxfleet/agent config.json",
		Restart:           "on-failure",
		RestartSec:        "3s",
		SingBoxLimitFiles: 1048576,
	}
	unit, err := renderSystemdUnit("sing-box", singBoxUnitTemplate, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/opt/boxfleet/bin/sing box" run -c "/etc/boxfleet/sing-box.json"`) {
		t.Fatalf("sing-box unit did not quote ExecStart args:\n%s", unit)
	}

	data.Restart = "always"
	data.RestartSec = "10s"
	unit, err = renderSystemdUnit("agent", agentUnitTemplate, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/opt/boxfleet/bin/boxfleet-agent" run --config "/etc/boxfleet/agent config.json"`) {
		t.Fatalf("agent unit did not quote ExecStart args:\n%s", unit)
	}
	if !strings.Contains(unit, `ExecStartPre="/opt/boxfleet/libexec/boxfleet-agent-guard" guard --config "/etc/boxfleet/agent config.json"`) {
		t.Fatalf("agent unit did not configure rollback guard:\n%s", unit)
	}
}
