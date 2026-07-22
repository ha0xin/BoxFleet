package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/google/renameio/v2"
	"github.com/sethvargo/go-retry"
	"golang.org/x/mod/semver"

	"github.com/haoxin/boxfleet/internal/model"
)

var (
	ErrAgentRestartRequired = errors.New("agent restart required to continue update")
	semanticVersionPattern  = regexp.MustCompile(`(?i)(v?\d+\.\d+\.\d+(?:-[0-9a-z.-]+)?(?:\+[0-9a-z.-]+)?)`)
)

type updateCheckpoint struct {
	AgentCandidate    string `json:"agent_candidate,omitempty"`
	AgentPrevious     string `json:"agent_previous,omitempty"`
	AgentSwitched     bool   `json:"agent_switched,omitempty"`
	AgentConfirmed    bool   `json:"agent_confirmed,omitempty"`
	SingBoxCandidate  string `json:"sing_box_candidate,omitempty"`
	SingBoxPrevious   string `json:"sing_box_previous,omitempty"`
	SingBoxSwitched   bool   `json:"sing_box_switched,omitempty"`
	SingBoxConfirmed  bool   `json:"sing_box_confirmed,omitempty"`
	SingBoxRolledBack bool   `json:"sing_box_rolled_back,omitempty"`
}

func (a *Agent) executeUpdateOperation(ctx context.Context, state *OperationState, cancelRequested *atomic.Bool) (map[string]any, error) {
	var payload model.NodeUpdatePayload
	if err := json.Unmarshal(state.Assignment.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode update payload: %w", err)
	}
	if err := validateUpdatePayload(state.Assignment.Kind, payload); err != nil {
		return nil, err
	}
	checkpoint, err := loadUpdateCheckpoint(state.Checkpoint)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"release": payload.Release}
	if payload.Agent != nil {
		restarted, err := a.updateAgent(ctx, state, &checkpoint, *payload.Agent, cancelRequested)
		if err != nil {
			return result, err
		}
		if restarted {
			return result, ErrAgentRestartRequired
		}
		result["agent_version"] = payload.Agent.Version
		result["agent_updated"] = true
		result["committed"] = true
	}
	if payload.SingBox != nil {
		if err := a.updateSingBox(ctx, state, &checkpoint, *payload.SingBox, cancelRequested); err != nil {
			return result, err
		}
		result["sing_box_version"] = payload.SingBox.Version
		result["sing_box_updated"] = true
		result["committed"] = true
	}
	return result, nil
}

func validateUpdatePayload(kind string, payload model.NodeUpdatePayload) error {
	if payload.Release == "" {
		return errors.New("update release is required")
	}
	if payload.Agent == nil && payload.SingBox == nil {
		return errors.New("update payload has no components")
	}
	if kind == "update.agent" && (payload.Agent == nil || payload.SingBox != nil) {
		return errors.New("update.agent payload must contain only agent")
	}
	if kind == "update.sing_box" && (payload.SingBox == nil || payload.Agent != nil) {
		return errors.New("update.sing_box payload must contain only sing_box")
	}
	if kind == "update.bundle" && (payload.Agent == nil || payload.SingBox == nil) {
		return errors.New("update.bundle payload must contain agent and sing_box")
	}
	if payload.Agent != nil {
		if err := validateUpdateAsset(*payload.Agent, "agent"); err != nil {
			return err
		}
	}
	if payload.SingBox != nil {
		if err := validateUpdateAsset(*payload.SingBox, "sing_box"); err != nil {
			return err
		}
	}
	return nil
}

func validateUpdateAsset(asset model.UpdateAsset, component string) error {
	if asset.Component != component {
		return fmt.Errorf("update asset component is %q, want %q", asset.Component, component)
	}
	if canonicalVersion(asset.Version) == "" {
		return fmt.Errorf("invalid %s version %q", component, asset.Version)
	}
	parsed, err := url.Parse(asset.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("invalid %s asset URL", component)
	}
	checksum, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(checksum) != sha256.Size {
		return fmt.Errorf("invalid %s SHA256", component)
	}
	if asset.Size <= 0 {
		return fmt.Errorf("invalid %s asset size", component)
	}
	return nil
}

func (a *Agent) updateAgent(
	ctx context.Context,
	state *OperationState,
	checkpoint *updateCheckpoint,
	asset model.UpdateAsset,
	cancelRequested *atomic.Bool,
) (bool, error) {
	guard, err := a.loadAgentUpdateGuard()
	if err != nil {
		return false, err
	}
	if guard != nil && guard.OperationID == state.Assignment.ID && guard.Status == "rolled_back" {
		_ = os.Remove(a.Config.AgentGuardStatePath)
		return false, fmt.Errorf("agent update automatically rolled back: %s", guard.Error)
	}
	if checkpoint.AgentSwitched || (checkpoint.AgentCandidate != "" && versionsEquivalent(Version, asset.Version)) {
		if !versionsEquivalent(Version, asset.Version) {
			return false, fmt.Errorf("agent restarted as %q, expected %q", Version, asset.Version)
		}
		checkpoint.AgentSwitched = true
		checkpoint.AgentConfirmed = true
		if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
			return false, err
		}
		if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
			Status: "running", Phase: "agent_confirmed", Message: "new agent heartbeat confirmed",
			Details: mustJSONObject(map[string]any{"version": Version}),
		}); err != nil {
			return false, err
		}
		return false, nil
	}
	if versionsEquivalent(Version, asset.Version) {
		checkpoint.AgentConfirmed = true
		return false, a.saveUpdateCheckpoint(state, *checkpoint)
	}
	if err := operationBoundary(ctx, cancelRequested); err != nil {
		return false, err
	}
	candidate, err := a.downloadAndInstallCandidate(ctx, state, asset)
	if err != nil {
		return false, normalizeOperationContextError(ctx, err)
	}
	checkpoint.AgentCandidate = candidate
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return false, err
	}
	output, err := a.Runner.Output(ctx, candidate, "version")
	if err != nil {
		return false, fmt.Errorf("validate candidate agent: %w", err)
	}
	if !versionsEquivalent(string(output), asset.Version) {
		return false, fmt.Errorf("candidate agent version %q does not match %q", firstLine(string(output)), asset.Version)
	}
	if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
		Status: "running", Phase: "installing_agent", Message: "installing versioned agent candidate",
		Details: mustJSONObject(map[string]any{"version": asset.Version}),
	}); err != nil {
		return false, err
	}
	if err := operationBoundary(ctx, cancelRequested); err != nil {
		return false, err
	}
	if err := a.ensureAgentGuardBinary(); err != nil {
		return false, err
	}
	previous, err := a.preserveCurrentBinary(a.Config.AgentPath, "boxfleet-agent", state.Assignment.ID)
	if err != nil {
		return false, err
	}
	checkpoint.AgentPrevious = previous
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return false, err
	}
	if err := a.writeAgentUpdateGuard(AgentUpdateGuardState{
		OperationID: state.Assignment.ID, ExpectedVersion: asset.Version,
		PreviousTarget: previous, CandidateTarget: candidate, Status: "pending",
	}); err != nil {
		return false, err
	}
	if err := a.InstallSystemdUnits(); err != nil {
		return false, err
	}
	if err := a.Runner.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return false, err
	}
	if err := switchVersionedBinary(a.Config.AgentPath, candidate, previous); err != nil {
		return false, err
	}
	checkpoint.AgentSwitched = true
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return false, err
	}
	// The symlink is already committed. Bound the best-effort status delivery;
	// its durable outbox will be retried by the new process after systemd restart.
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	_ = a.reportOperationEventWithRetry(reportCtx, state, model.NodeOperationEventReport{
		Status: "running", Phase: "restarting_agent", Message: "agent switched; restarting under systemd",
		Details: mustJSONObject(map[string]any{"version": asset.Version}),
	})
	cancel()
	return true, nil
}

func (a *Agent) updateSingBox(
	ctx context.Context,
	state *OperationState,
	checkpoint *updateCheckpoint,
	asset model.UpdateAsset,
	cancelRequested *atomic.Bool,
) error {
	if checkpoint.SingBoxConfirmed {
		return nil
	}
	if err := operationBoundary(ctx, cancelRequested); err != nil {
		return err
	}
	candidate, err := a.downloadAndInstallCandidate(ctx, state, asset)
	if err != nil {
		return normalizeOperationContextError(ctx, err)
	}
	checkpoint.SingBoxCandidate = candidate
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return err
	}
	output, err := a.Runner.Output(ctx, candidate, "version")
	if err != nil {
		return fmt.Errorf("validate candidate sing-box: %w", err)
	}
	if !versionsEquivalent(string(output), asset.Version) {
		return fmt.Errorf("candidate sing-box version %q does not match %q", firstLine(string(output)), asset.Version)
	}
	if !strings.Contains(string(output), "with_v2ray_api") {
		return errors.New("candidate sing-box was not built with with_v2ray_api")
	}
	if err := a.Runner.Run(ctx, candidate, "check", "-c", a.Config.SingBoxConfig); err != nil {
		return fmt.Errorf("candidate sing-box config check: %w", err)
	}
	if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
		Status: "running", Phase: "installing_sing_box", Message: "installing versioned sing-box candidate",
		Details: mustJSONObject(map[string]any{"version": asset.Version}),
	}); err != nil {
		return err
	}
	if err := operationBoundary(ctx, cancelRequested); err != nil {
		return err
	}
	configResponse, err := a.FetchConfigVersioned(ctx)
	if err != nil {
		return fmt.Errorf("read desired node state before sing-box update: %w", err)
	}
	disabled := configResponse.State == "disabled"
	if err := a.flushTrafficForUpdate(ctx); err != nil {
		return fmt.Errorf("flush traffic before sing-box update: %w", err)
	}
	previous, err := a.preserveCurrentBinary(a.Config.SingBoxPath, "sing-box", state.Assignment.ID)
	if err != nil {
		return err
	}
	checkpoint.SingBoxPrevious = previous
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return err
	}
	if err := switchVersionedBinary(a.Config.SingBoxPath, candidate, previous); err != nil {
		return err
	}
	checkpoint.SingBoxSwitched = true
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return err
	}

	// From this point until health verification or rollback, cancellation cannot
	// interrupt consistency restoration. The critical section is still bounded.
	criticalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	var activateErr error
	if disabled {
		activateErr = a.Runner.Run(criticalCtx, "systemctl", "stop", a.Config.SingBoxService)
		if activateErr == nil && !a.singBoxConfirmedDown(criticalCtx) {
			activateErr = errors.New("disabled sing-box service did not stop")
		}
	} else {
		activateErr = a.Runner.Run(criticalCtx, "systemctl", "restart", a.Config.SingBoxService)
		if activateErr == nil {
			activateErr = a.waitServiceActive(criticalCtx, a.Config.SingBoxService)
		}
	}
	if activateErr != nil {
		rollbackErr := switchVersionedBinary(a.Config.SingBoxPath, previous, candidate)
		if rollbackErr == nil {
			if disabled {
				rollbackErr = a.Runner.Run(criticalCtx, "systemctl", "stop", a.Config.SingBoxService)
			} else {
				rollbackErr = a.Runner.Run(criticalCtx, "systemctl", "restart", a.Config.SingBoxService)
				if rollbackErr == nil {
					rollbackErr = a.waitServiceActive(criticalCtx, a.Config.SingBoxService)
				}
			}
		}
		checkpoint.SingBoxRolledBack = rollbackErr == nil
		_ = a.saveUpdateCheckpoint(state, *checkpoint)
		if rollbackErr != nil {
			return fmt.Errorf("new sing-box failed: %v; automatic rollback also failed: %w", activateErr, rollbackErr)
		}
		return fmt.Errorf("new sing-box failed and was automatically rolled back: %w", activateErr)
	}
	checkpoint.SingBoxConfirmed = true
	if err := a.saveUpdateCheckpoint(state, *checkpoint); err != nil {
		return err
	}
	heartbeatCtx, heartbeatCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	heartbeatStatus := "ok"
	if disabled {
		heartbeatStatus = "disabled"
	}
	_ = a.ReportHeartbeat(heartbeatCtx, configResponse, heartbeatStatus)
	heartbeatCancel()
	return nil
}

func (a *Agent) flushTrafficForUpdate(ctx context.Context) error {
	if a.TrafficReporter != nil {
		return a.TrafficReporter(ctx)
	}
	return a.ReportTraffic(ctx)
}

func (a *Agent) downloadAndInstallCandidate(ctx context.Context, state *OperationState, asset model.UpdateAsset) (string, error) {
	if err := validateUpdateAsset(asset, asset.Component); err != nil {
		return "", err
	}
	versionDir, err := safeVersionDir(asset.Version)
	if err != nil {
		return "", err
	}
	componentDir, ok := map[string]string{"agent": "boxfleet-agent", "sing_box": "sing-box"}[asset.Component]
	if !ok {
		return "", fmt.Errorf("unsupported update component %q", asset.Component)
	}
	binaryName := componentDir
	finalPath := filepath.Join(a.Config.InstallDir, "releases", componentDir, versionDir, binaryName)
	if valid, _ := fileMatchesAsset(finalPath, asset); valid {
		return finalPath, nil
	}
	downloadDir := filepath.Join(a.Config.InstallDir, "downloads", state.Assignment.ID)
	if err := os.MkdirAll(downloadDir, 0o700); err != nil {
		return "", err
	}
	partialPath := filepath.Join(downloadDir, componentDir+"-"+strings.ToLower(asset.SHA256)+".partial")
	if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
		Status: "running", Phase: "downloading", Message: "streaming update asset to temporary file",
		Details: mustJSONObject(map[string]any{"component": asset.Component, "size": asset.Size}),
	}); err != nil {
		return "", err
	}
	request, err := grab.NewRequest(partialPath, asset.URL)
	if err != nil {
		return "", err
	}
	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	request = request.WithContext(downloadCtx)
	request.Size = asset.Size
	request.BufferSize = 128 * 1024
	request.IgnoreRemoteTime = true
	checksum, _ := hex.DecodeString(asset.SHA256)
	request.SetChecksum(sha256.New(), checksum, true)
	httpClient := *a.client()
	httpClient.Timeout = 0
	client := grab.NewClient()
	client.HTTPClient = &httpClient
	client.UserAgent = "boxfleet-agent/" + Version
	response := client.Do(request)
	progressTicker := time.NewTicker(2 * time.Second)
	defer progressTicker.Stop()
	for {
		select {
		case <-response.Done:
			if err := response.Err(); err != nil {
				return "", err
			}
			goto downloaded
		case <-progressTicker.C:
			if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
				Status: "running", Phase: "downloading", Message: "downloading update asset",
				Details: mustJSONObject(map[string]any{
					"component": asset.Component, "bytes_complete": response.BytesComplete(), "size": asset.Size,
				}),
			}); err != nil {
				cancel()
				_ = response.Err()
				return "", err
			}
		case <-ctx.Done():
			cancel()
			_ = response.Err()
			return "", context.Cause(ctx)
		}
	}

downloaded:
	if err := os.Chmod(partialPath, defaultBinaryFilePerm); err != nil {
		return "", err
	}
	if err := syncFile(partialPath); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(partialPath, finalPath); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		return "", err
	}
	valid, err := fileMatchesAsset(finalPath, asset)
	if err != nil || !valid {
		return "", fmt.Errorf("installed candidate failed checksum verification: %w", err)
	}
	if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
		Status: "running", Phase: "verifying", Message: "download size and SHA256 verified",
		Details: mustJSONObject(map[string]any{"component": asset.Component, "sha256": asset.SHA256}),
	}); err != nil {
		return "", err
	}
	return finalPath, nil
}

// streamUnverifiedBinary is retained for the bootstrap-only sing_box_url path,
// where no release checksum is available. It still never buffers the binary in
// memory: grab streams into a same-filesystem temporary file, which is renamed
// only after a complete transfer. Managed updates always use the stricter
// size+SHA256 path above.
func (a *Agent) streamUnverifiedBinary(ctx context.Context, sourceURL, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".bootstrap.partial")
	request, err := grab.NewRequest(temporary, sourceURL)
	if err != nil {
		return err
	}
	request = request.WithContext(ctx)
	request.NoResume = true
	request.BufferSize = 128 * 1024
	httpClient := *a.client()
	httpClient.Timeout = 0
	client := grab.NewClient()
	client.HTTPClient = &httpClient
	client.UserAgent = "boxfleet-agent/" + Version
	response := client.Do(request)
	if err := response.Err(); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Chmod(temporary, defaultBinaryFilePerm); err != nil {
		return err
	}
	if err := syncFile(temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func (a *Agent) preserveCurrentBinary(currentPath, component, operationID string) (string, error) {
	resolved, err := filepath.EvalSymlinks(currentPath)
	if err != nil {
		return "", err
	}
	releasesRoot := filepath.Join(a.Config.InstallDir, "releases")
	if pathWithin(resolved, releasesRoot) {
		return resolved, nil
	}
	legacy := filepath.Join(releasesRoot, component, "legacy-"+operationID, component)
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := atomicCopyFile(resolved, legacy, defaultBinaryFilePerm); err != nil {
		return "", err
	}
	return legacy, nil
}

func switchVersionedBinary(linkPath, nextTarget, previousTarget string) error {
	if _, err := os.Stat(nextTarget); err != nil {
		return err
	}
	if previousTarget != "" {
		if err := renameio.Symlink(previousTarget, linkPath+".previous"); err != nil {
			return err
		}
	}
	return renameio.Symlink(nextTarget, linkPath)
}

func (a *Agent) waitServiceActive(ctx context.Context, service string) error {
	backoff := retry.WithMaxDuration(20*time.Second, retry.NewConstant(time.Second))
	return retry.Do(ctx, backoff, func(ctx context.Context) error {
		output, err := a.Runner.Output(ctx, "systemctl", "show", "-p", "ActiveState", "--value", service)
		if err != nil {
			return retry.RetryableError(err)
		}
		if strings.TrimSpace(string(output)) != "active" {
			return retry.RetryableError(fmt.Errorf("service state is %q", strings.TrimSpace(string(output))))
		}
		return nil
	})
}

func loadUpdateCheckpoint(raw json.RawMessage) (updateCheckpoint, error) {
	if len(raw) == 0 {
		return updateCheckpoint{}, nil
	}
	var checkpoint updateCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return updateCheckpoint{}, fmt.Errorf("decode update checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (a *Agent) saveUpdateCheckpoint(state *OperationState, checkpoint updateCheckpoint) error {
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	state.Checkpoint = raw
	return a.SaveOperationState(*state)
}

func fileMatchesAsset(path string, asset model.UpdateAsset) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != asset.Size {
		return false, nil
	}
	hash := sha256.New()
	if _, err := io.CopyBuffer(hash, file, make([]byte, 128*1024)); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), asset.SHA256), nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func safeVersionDir(value string) (string, error) {
	canonical := canonicalVersion(value)
	if canonical == "" {
		return "", fmt.Errorf("invalid semantic version %q", value)
	}
	return strings.TrimPrefix(canonical, "v"), nil
}

func canonicalVersion(value string) string {
	match := semanticVersionPattern.FindString(strings.TrimSpace(value))
	if match == "" {
		return ""
	}
	if match[0] != 'v' && match[0] != 'V' {
		match = "v" + match
	} else if match[0] == 'V' {
		match = "v" + match[1:]
	}
	if !semver.IsValid(match) {
		return ""
	}
	return semver.Canonical(match)
}

func versionsEquivalent(actual, expected string) bool {
	a, b := canonicalVersion(actual), canonicalVersion(expected)
	return a != "" && b != "" && semver.Compare(a, b) == 0
}

func pathWithin(path, root string) bool {
	pathAbs, err1 := filepath.Abs(path)
	rootAbs, err2 := filepath.Abs(root)
	return err1 == nil && err2 == nil && (pathAbs == rootAbs || strings.HasPrefix(pathAbs, rootAbs+string(os.PathSeparator)))
}

func normalizeOperationContextError(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}

func mustJSONObject(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
