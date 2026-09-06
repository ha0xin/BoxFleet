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

func TestRenderOpenRCScriptsQuotesPathsAndUsesSupervisor(t *testing.T) {
	script, err := renderOpenRCScript("sing-box", singBoxOpenRCTemplate, openRCData{
		Command: "/opt/boxfleet/bin/sing box", Arguments: `run -c "/etc/boxfleet/sing box.json"`,
		PIDFile: "/run/boxfleet-sing-box.pid", OutputLog: "/opt/boxfleet/log/boxfleet-sing-box.log", ErrorLog: "/opt/boxfleet/log/boxfleet-sing-box.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`command="/opt/boxfleet/bin/sing box"`, `supervisor="supervise-daemon"`, `respawn_max=0`, `rc_ulimit="-n 1048576"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("OpenRC script missing %q:\n%s", want, script)
		}
	}
	agentScript, err := renderOpenRCScript("agent", agentOpenRCTemplate, openRCData{
		Command: "/opt/boxfleet/bin/boxfleet-agent", Arguments: `run --config "/etc/boxfleet/agent.json"`, Guard: `"/opt/boxfleet/libexec/boxfleet-agent-guard" guard --config "/etc/boxfleet/agent.json"`, PIDFile: "/run/boxfleet-agent.pid", OutputLog: "/opt/boxfleet/log/boxfleet-agent.log", ErrorLog: "/opt/boxfleet/log/boxfleet-agent.log",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agentScript, "start_pre()") || !strings.Contains(agentScript, "boxfleet-agent-guard") {
		t.Fatalf("agent OpenRC script lacks update guard:\n%s", agentScript)
	}
}
