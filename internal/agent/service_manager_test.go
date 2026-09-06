package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type commandRunner struct{ commands []string }

func (r *commandRunner) Run(_ context.Context, name string, args ...string) error {
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))
	return nil
}
func (r *commandRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, fmt.Errorf("unexpected output")
}
func (r *commandRunner) StreamLines(context.Context, string, []string, func(string) bool) error {
	return nil
}

func TestOpenRCServiceCommandsStripSystemdSuffix(t *testing.T) {
	runner := &commandRunner{}
	a := &Agent{Config: Config{InitSystem: InitSystemOpenRC}, Runner: runner}
	ctx := context.Background()
	if err := a.enableService(ctx, "boxfleet-agent.service"); err != nil {
		t.Fatal(err)
	}
	if err := a.restartService(ctx, "boxfleet-agent.service"); err != nil {
		t.Fatal(err)
	}
	if err := a.stopService(ctx, "boxfleet-sing-box.service"); err != nil {
		t.Fatal(err)
	}
	want := []string{"rc-update add boxfleet-agent default", "rc-service boxfleet-agent restart", "rc-service boxfleet-sing-box stop"}
	if fmt.Sprint(runner.commands) != fmt.Sprint(want) {
		t.Fatalf("commands = %v, want %v", runner.commands, want)
	}
}
