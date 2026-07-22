package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/renameio/v2"
)

const maxAgentGuardStarts = 3

type AgentUpdateGuardState struct {
	OperationID     string `json:"operation_id"`
	ExpectedVersion string `json:"expected_version"`
	PreviousTarget  string `json:"previous_target"`
	CandidateTarget string `json:"candidate_target"`
	Status          string `json:"status"`
	Attempts        int    `json:"attempts"`
	Deadline        string `json:"deadline"`
	Error           string `json:"error,omitempty"`
}

func (a *Agent) ensureAgentGuardBinary() error {
	if info, err := os.Stat(a.Config.AgentGuardPath); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("agent guard %s is not a regular file", a.Config.AgentGuardPath)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicCopyFile(a.Config.AgentPath, a.Config.AgentGuardPath, defaultBinaryFilePerm)
}

func (a *Agent) writeAgentUpdateGuard(state AgentUpdateGuardState) error {
	if state.Status == "" {
		state.Status = "pending"
	}
	if state.Deadline == "" {
		state.Deadline = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.Config.AgentGuardStatePath, append(raw, '\n'), defaultConfigFilePerm)
}

func (a *Agent) loadAgentUpdateGuard() (*AgentUpdateGuardState, error) {
	raw, err := os.ReadFile(a.Config.AgentGuardStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state AgentUpdateGuardState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode agent update guard: %w", err)
	}
	return &state, nil
}

// RunAgentGuard is invoked by systemd ExecStartPre from a stable copy of the
// last confirmed agent. Three failed starts (or the deadline) atomically point
// the service path back to the previous version before systemd starts it again.
func (a *Agent) RunAgentGuard() error {
	state, err := a.loadAgentUpdateGuard()
	if err != nil || state == nil {
		return err
	}
	if state.Status == "rolled_back" || state.Status == "confirmed" {
		return nil
	}
	if state.Status != "pending" {
		return fmt.Errorf("invalid agent guard status %q", state.Status)
	}
	if err := a.validateGuardTarget(state.PreviousTarget); err != nil {
		return err
	}
	if err := a.validateGuardTarget(state.CandidateTarget); err != nil {
		return err
	}
	current, err := filepath.EvalSymlinks(a.Config.AgentPath)
	if err != nil {
		return err
	}
	previous, _ := filepath.EvalSymlinks(state.PreviousTarget)
	if samePath(current, previous) {
		state.Status = "rolled_back"
		state.Error = "agent service path already points to the previous version"
		return a.writeAgentUpdateGuard(*state)
	}
	state.Attempts++
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, state.Deadline)
	deadlinePassed := deadlineErr == nil && !time.Now().Before(deadline)
	if state.Attempts < maxAgentGuardStarts && !deadlinePassed {
		return a.writeAgentUpdateGuard(*state)
	}
	if _, err := os.Stat(state.PreviousTarget); err != nil {
		return fmt.Errorf("agent guard previous target: %w", err)
	}
	if err := renameio.Symlink(state.PreviousTarget, a.Config.AgentPath); err != nil {
		return fmt.Errorf("agent guard rollback: %w", err)
	}
	state.Status = "rolled_back"
	state.Error = fmt.Sprintf("candidate did not confirm after %d service starts", state.Attempts)
	return a.writeAgentUpdateGuard(*state)
}

func (a *Agent) validateGuardTarget(target string) error {
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	root, err := filepath.Abs(filepath.Join(a.Config.InstallDir, "releases"))
	if err != nil {
		return err
	}
	if targetAbs != root && !strings.HasPrefix(targetAbs, root+string(os.PathSeparator)) {
		return fmt.Errorf("agent guard target %q is outside the release directory", target)
	}
	return nil
}

// ConfirmAgentUpdateGuard runs only after a successful config cycle has sent a
// heartbeat from the new binary. The new version then becomes the stable guard
// used for the next update.
func (a *Agent) ConfirmAgentUpdateGuard() error {
	state, err := a.loadAgentUpdateGuard()
	if err != nil || state == nil || state.Status != "pending" {
		return err
	}
	if !versionsEquivalent(Version, state.ExpectedVersion) {
		return nil
	}
	if err := atomicCopyFile(a.Config.AgentPath, a.Config.AgentGuardPath, defaultBinaryFilePerm); err != nil {
		return fmt.Errorf("promote confirmed agent guard: %w", err)
	}
	if err := os.Remove(a.Config.AgentGuardStatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func atomicCopyFile(source, target string, perm os.FileMode) error {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	input, err := os.Open(resolved)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	pending, err := renameio.NewPendingFile(target, renameio.WithStaticPermissions(perm))
	if err != nil {
		return err
	}
	defer pending.Cleanup()
	if _, err := io.CopyBuffer(pending, input, make([]byte, 128*1024)); err != nil {
		return err
	}
	return pending.CloseAtomicallyReplace()
}
