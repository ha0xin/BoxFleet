package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/renameio/v2"
	"github.com/google/uuid"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/v2raystats"
)

var Version = "dev"

const (
	DefaultConfigPath      = "/etc/boxfleet/agent.json"
	DefaultInstallDir      = "/opt/boxfleet"
	DefaultServiceName     = "boxfleet-sing-box.service"
	DefaultAgentService    = "boxfleet-agent.service"
	DefaultPollInterval    = time.Minute
	DefaultHTTPTimeout     = 5 * time.Minute
	DefaultV2RayAPIAddress = "127.0.0.1:18082"
	defaultConfigFilePerm  = 0o600
	defaultBinaryFilePerm  = 0o755
	defaultRuntimeFilePerm = 0o644
	journalBatchMaxEntries = 100
	journalBatchMaxBytes   = 256 * 1024
	journalMaxBatches      = 8
	journalReadBufferBytes = 64 * 1024
	journalMaxLineBytes    = 1024 * 1024
	stderrCaptureLimit     = 4096
	journalSinceLayout     = "2006-01-02 15:04:05"
)

type Config struct {
	NodeName            string `json:"node_name"`
	Token               string `json:"token"`
	ServerURL           string `json:"server_url"`
	SingBoxURL          string `json:"sing_box_url"`
	SingBoxSHA256       string `json:"sing_box_sha256,omitempty"`
	InstallDir          string `json:"install_dir"`
	SingBoxPath         string `json:"sing_box_path"`
	SingBoxConfig       string `json:"sing_box_config"`
	SingBoxService      string `json:"sing_box_service"`
	AgentPath           string `json:"agent_path"`
	AgentGuardPath      string `json:"agent_guard_path"`
	AgentGuardStatePath string `json:"agent_guard_state_path"`
	AgentConfigPath     string `json:"agent_config_path"`
	AgentService        string `json:"agent_service"`
	PollInterval        string `json:"poll_interval"`
	StatePath           string `json:"state_path"`
	OperationStatePath  string `json:"operation_state_path"`
	V2RayAPIAddress     string `json:"v2ray_api_address"`
	// AllowInsecureTransport permits plaintext http server_url/sing_box_url. It
	// leaks the node token and installs unverified binaries, so it exists only
	// for local development.
	AllowInsecureTransport bool `json:"allow_insecure_transport,omitempty"`
}

type Agent struct {
	Config Config
	Runner Runner
	Client *http.Client
	// ServiceReadyWait bounds how long a restarted unit is polled for an active
	// state. Zero uses defaultServiceReadyWait.
	ServiceReadyWait time.Duration
	TrafficReporter  func(context.Context) error
	maintenanceMu    sync.Mutex
	// configMu guards the mutable parts of Config. Only the canonical node name
	// changes at runtime, and it is read by every request goroutine.
	configMu sync.RWMutex
	// connectionMu guards the opt-in sing-box 1.14 connection telemetry
	// collector, which the poll loop starts and stops as the applied config
	// gains or loses an api service. See connections.go.
	connectionMu sync.Mutex
	connections  *connectionCollectorHandle
}

type ConfigResponse struct {
	Data      []byte
	VersionID string
	Version   string
	Hash      string
	Mode      string
	// State is the server's desired node state. "disabled" means the agent
	// should stop sing-box while keeping its daemon polling; empty means serve.
	State string
}

type State struct {
	BootID              string               `json:"boot_id"`
	Sequence            int64                `json:"sequence"`
	LastCounters        map[string]int64     `json:"last_counters"`
	CounterEpoch        map[string]int64     `json:"counter_epoch"`
	LastLogLines        map[string]bool      `json:"last_log_lines"`
	LastLogSince        string               `json:"last_log_since"`
	LastLogCursor       string               `json:"last_log_cursor"`
	LastSystemLogCursor map[string]string    `json:"last_system_log_cursor"`
	LastSystemLogSince  map[string]string    `json:"last_system_log_since"`
	AppliedConfigHash   string               `json:"applied_config_hash"`
	PendingTraffic      *model.TrafficReport `json:"pending_traffic,omitempty"`
	// PendingConnections stages one connection telemetry report across the POST,
	// exactly as PendingTraffic does. ConnectionSequence is its own counter so
	// the two reports keep independent, gapless (boot id, sequence) idempotency
	// keys.
	PendingConnections *model.ConnectionReport `json:"pending_connections,omitempty"`
	ConnectionSequence int64                   `json:"connection_sequence"`
	// LastConnectionCloseMs is the newest connection close already reported. It
	// survives agent restarts so sing-box's closed-connection replay ring cannot
	// re-report what this node already sent.
	LastConnectionCloseMs int64 `json:"last_connection_close_ms"`
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	StreamLines(ctx context.Context, name string, args []string, handle func(line string) bool) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (ExecRunner) StreamLines(ctx context.Context, name string, args []string, handle func(line string) bool) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	stderrDone := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		tmp := make([]byte, 1024)
		for {
			n, err := stderr.Read(tmp)
			if n > 0 && buf.Len() < stderrCaptureLimit {
				remaining := stderrCaptureLimit - buf.Len()
				if n > remaining {
					n = remaining
				}
				_, _ = buf.Write(tmp[:n])
			}
			if err != nil {
				break
			}
		}
		stderrDone <- strings.TrimSpace(buf.String())
	}()
	stopped, readErr := readLines(stdout, handle)
	if (stopped || readErr != nil) && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	stderrText := <-stderrDone
	if readErr != nil {
		return fmt.Errorf("%s %s: read stdout: %w", name, strings.Join(args, " "), readErr)
	}
	if waitErr != nil && !stopped {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), waitErr, stderrText)
	}
	return nil
}

// readLines feeds newline-delimited output to handle without buffering unbounded
// input. A line longer than journalMaxLineBytes is discarded instead of aborting
// the stream: a single oversized journal entry would otherwise fail every cycle
// forever, because the log cursor only advances past entries that were read.
func readLines(input io.Reader, handle func(line string) bool) (bool, error) {
	reader := bufio.NewReaderSize(input, journalReadBufferBytes)
	var line []byte
	oversized := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if oversized || len(line)+len(chunk) > journalMaxLineBytes {
				oversized, line = true, line[:0]
			} else {
				line = append(line, chunk...)
			}
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		if !oversized && len(line)+len(chunk) <= journalMaxLineBytes {
			line = append(line, chunk...)
			if text := strings.TrimRight(string(line), "\r\n"); text != "" && !handle(text) {
				return true, nil
			}
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		line, oversized = line[:0], false
	}
}

func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func WriteConfig(path string, config Config) error {
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, append(raw, '\n'), defaultConfigFilePerm)
}

func (c *Config) ApplyDefaults() {
	if c.InstallDir == "" {
		c.InstallDir = DefaultInstallDir
	}
	if c.SingBoxPath == "" {
		c.SingBoxPath = filepath.Join(c.InstallDir, "bin", "sing-box")
	}
	if c.SingBoxConfig == "" {
		c.SingBoxConfig = filepath.Join(c.InstallDir, "etc", "sing-box.json")
	}
	if c.SingBoxService == "" {
		c.SingBoxService = DefaultServiceName
	}
	if c.AgentPath == "" {
		c.AgentPath = filepath.Join(c.InstallDir, "bin", "boxfleet-agent")
	}
	if c.AgentGuardPath == "" {
		c.AgentGuardPath = filepath.Join(c.InstallDir, "libexec", "boxfleet-agent-guard")
	}
	if c.AgentGuardStatePath == "" {
		c.AgentGuardStatePath = filepath.Join(c.InstallDir, "state", "agent-update-guard.json")
	}
	if c.AgentConfigPath == "" {
		c.AgentConfigPath = DefaultConfigPath
	}
	if c.AgentService == "" {
		c.AgentService = DefaultAgentService
	}
	if c.PollInterval == "" {
		c.PollInterval = DefaultPollInterval.String()
	}
	if c.StatePath == "" {
		c.StatePath = filepath.Join(c.InstallDir, "state", "agent-state.json")
	}
	if c.OperationStatePath == "" {
		c.OperationStatePath = filepath.Join(c.InstallDir, "state", "operation-state.json")
	}
	if c.V2RayAPIAddress == "" {
		c.V2RayAPIAddress = DefaultV2RayAPIAddress
	}
}

func (c Config) Validate() error {
	if c.NodeName == "" {
		return errors.New("node_name is required")
	}
	if c.Token == "" {
		return errors.New("token is required")
	}
	if c.ServerURL == "" {
		return errors.New("server_url is required")
	}
	if err := validateTransportURL("server_url", c.ServerURL, c.AllowInsecureTransport); err != nil {
		return err
	}
	if c.SingBoxURL != "" {
		if err := validateTransportURL("sing_box_url", c.SingBoxURL, c.AllowInsecureTransport); err != nil {
			return err
		}
	}
	if c.SingBoxSHA256 != "" {
		if raw, err := hex.DecodeString(c.SingBoxSHA256); err != nil || len(raw) != sha256.Size {
			return errors.New("sing_box_sha256 must be a hex-encoded SHA256 digest")
		}
	}
	return nil
}

// validateTransportURL keeps the node token and the bootstrap sing-box binary on
// https. Plaintext http is accepted only behind the explicit development-only
// allow_insecure_transport opt-out.
func validateTransportURL(field, raw string, allowInsecure bool) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s is not a valid URL", field)
	}
	if parsed.Scheme == "https" || (parsed.Scheme == "http" && allowInsecure) {
		return nil
	}
	return fmt.Errorf("%s must use https (set allow_insecure_transport for local development)", field)
}

func New(config Config) *Agent {
	config.ApplyDefaults()
	if config.AllowInsecureTransport {
		fmt.Fprintln(os.Stderr, "boxfleet-agent: WARN allow_insecure_transport is enabled; the node token travels in cleartext and bootstrap binaries are unauthenticated — development only")
	}
	return &Agent{
		Config: config,
		Runner: ExecRunner{},
		Client: &http.Client{Timeout: DefaultHTTPTimeout},
	}
}

func (a *Agent) Check(ctx context.Context) error {
	if err := a.Config.Validate(); err != nil {
		return err
	}
	if err := a.CheckSingBoxV2RayAPI(ctx); err != nil {
		return err
	}
	if err := a.ensureAgentGuardBinary(); err != nil {
		return err
	}
	if _, err := os.Stat(a.Config.SingBoxConfig); err == nil {
		return a.Runner.Run(ctx, a.Config.SingBoxPath, "check", "-c", a.Config.SingBoxConfig)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *Agent) Install(ctx context.Context) error {
	if a.Config.SingBoxURL != "" {
		if err := a.DownloadSingBox(ctx); err != nil {
			return err
		}
	}
	if err := a.CheckSingBoxV2RayAPI(ctx); err != nil {
		return err
	}
	if err := a.ensureAgentGuardBinary(); err != nil {
		return err
	}
	if err := a.InstallSystemdUnits(); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, "systemctl", "enable", a.Config.SingBoxService); err != nil {
		return err
	}
	if err := a.Once(ctx); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, "systemctl", "enable", a.Config.AgentService); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, "systemctl", "restart", a.Config.AgentService); err != nil {
		return err
	}
	return nil
}

func (a *Agent) CheckSingBoxV2RayAPI(ctx context.Context) error {
	if _, err := os.Stat(a.Config.SingBoxPath); err != nil {
		return fmt.Errorf("sing-box binary: %w", err)
	}
	output, err := a.Runner.Output(ctx, a.Config.SingBoxPath, "version")
	if err != nil {
		return err
	}
	if !strings.Contains(string(output), "with_v2ray_api") {
		return fmt.Errorf("sing-box at %s was not built with with_v2ray_api", a.Config.SingBoxPath)
	}
	return nil
}

func (a *Agent) DownloadSingBox(ctx context.Context) error {
	if err := validateTransportURL("sing_box_url", a.Config.SingBoxURL, a.Config.AllowInsecureTransport); err != nil {
		return err
	}
	return a.streamBootstrapBinary(ctx, a.Config.SingBoxURL, a.Config.SingBoxPath, a.Config.SingBoxSHA256)
}

func (a *Agent) Once(ctx context.Context) error {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	return a.once(ctx)
}

func (a *Agent) once(ctx context.Context) error {
	response, err := a.FetchConfigVersioned(ctx)
	if err != nil {
		return err
	}
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	if response.State == "disabled" {
		return a.applyDisabled(ctx, response)
	}
	config := response.Data
	configHash := response.Hash
	if configHash == "" {
		configHash = bytesSHA256Hex(config)
		response.Hash = configHash
	}
	current, currentErr := os.ReadFile(a.Config.SingBoxConfig)
	if currentErr == nil && bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(config)) {
		// Take the "nothing to do" early return only when sing-box is *confirmed*
		// running. A re-enabled node has matching applied hash and unchanged
		// bytes, so a probe error (or inactive/transitional state) must not be
		// read as "up" — otherwise a single D-Bus hiccup would leave the
		// re-enabled node stopped until a later poll. Unknown ⇒ restart (idempotent).
		if state.AppliedConfigHash == configHash && a.singBoxConfirmedActive(ctx) {
			a.reportRuntimeState(ctx, response)
			return nil
		}
		// The bytes on disk are already the desired config, so there is nothing to
		// roll back to — but a unit that stays down must still be reported as
		// failed instead of being recorded as applied on every later poll.
		if err := a.restartSingBoxVerified(ctx); err != nil {
			_ = a.ReportApplyResult(ctx, response, "failed", err.Error())
			return err
		}
		a.reportRuntimeState(ctx, response)
		return nil
	}
	candidatePath := a.Config.SingBoxConfig + ".candidate"
	if err := atomicWrite(candidatePath, config, defaultRuntimeFilePerm); err != nil {
		return err
	}
	checkErr := a.Runner.Run(ctx, a.Config.SingBoxPath, "check", "-c", candidatePath)
	_ = os.Remove(candidatePath)
	if checkErr != nil {
		_ = a.ReportApplyResult(ctx, response, "failed", checkErr.Error())
		return checkErr
	}
	if currentErr == nil {
		if err := atomicWrite(a.lastGoodConfigPath(), current, defaultRuntimeFilePerm); err != nil {
			return err
		}
	}
	if err := atomicWrite(a.Config.SingBoxConfig, config, defaultRuntimeFilePerm); err != nil {
		return err
	}
	if err := a.restartSingBoxVerified(ctx); err != nil {
		applyErr := a.rollbackToLastGoodConfig(ctx, err, currentErr == nil)
		_ = a.ReportApplyResult(ctx, response, "failed", applyErr.Error())
		return applyErr
	}
	a.reportRuntimeState(ctx, response)
	return nil
}

func (a *Agent) lastGoodConfigPath() string {
	return a.Config.SingBoxConfig + ".last-good"
}

// restartSingBoxVerified restarts sing-box and waits for the unit to report
// active. The unit is Type=simple, so `systemctl restart` returns as soon as the
// process execs: a config that passes `sing-box check` but fails at runtime (a
// bound port, a missing certificate) would otherwise look applied. An unreadable
// probe stays "unknown" and is never treated as proof of failure, so a D-Bus
// hiccup cannot trigger a rollback of a healthy config.
func (a *Agent) restartSingBoxVerified(ctx context.Context) error {
	if err := a.Runner.Run(ctx, "systemctl", "restart", a.Config.SingBoxService); err != nil {
		return err
	}
	activeErr := a.waitServiceActive(ctx, a.Config.SingBoxService)
	if activeErr == nil {
		return nil
	}
	if _, err := a.singBoxActiveState(ctx); err != nil {
		return nil
	}
	return fmt.Errorf("sing-box did not become active after applying the config: %w", activeErr)
}

// rollbackToLastGoodConfig restores the configuration sing-box was last running
// and reports what really happened, so a runtime-fatal config cannot be recorded
// as applied while the service crash-loops.
func (a *Agent) rollbackToLastGoodConfig(ctx context.Context, applyErr error, haveLastGood bool) error {
	if !haveLastGood {
		return fmt.Errorf("%w; no previous config to roll back to", applyErr)
	}
	lastGood, err := os.ReadFile(a.lastGoodConfigPath())
	if err != nil {
		return fmt.Errorf("%w; reading the last-good config failed: %v", applyErr, err)
	}
	if err := atomicWrite(a.Config.SingBoxConfig, lastGood, defaultRuntimeFilePerm); err != nil {
		return fmt.Errorf("%w; restoring the last-good config failed: %v", applyErr, err)
	}
	if err := a.restartSingBoxVerified(ctx); err != nil {
		return fmt.Errorf("%w; restarting with the last-good config also failed: %v", applyErr, err)
	}
	return fmt.Errorf("%w; rolled back to the last-good config", applyErr)
}

// singBoxActiveState returns the unit's ActiveState. `systemctl show` succeeds
// for any unit state, so an error means the probe itself failed — an unknown
// state, not a state named "unknown".
func (a *Agent) singBoxActiveState(ctx context.Context) (string, error) {
	out, err := a.Runner.Output(ctx, "systemctl", "show", "-p", "ActiveState", "--value", a.Config.SingBoxService)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *Agent) singBoxConfirmedDown(ctx context.Context) bool {
	state, err := a.singBoxActiveState(ctx)
	if err != nil {
		return false
	}
	switch state {
	case "inactive", "failed":
		return true
	default:
		// Transitional states (activating/deactivating) and unknown probes are
		// NOT confirmed down, so applyDisabled still issues an (idempotent) stop.
		return false
	}
}

// singBoxConfirmedActive reports whether sing-box is known to be running. A probe
// error or any non-"active" state returns false, so callers treat "unknown" as
// "not confirmed up" and act (restart) rather than assuming the service is fine.
func (a *Agent) singBoxConfirmedActive(ctx context.Context) bool {
	state, err := a.singBoxActiveState(ctx)
	return err == nil && state == "active"
}

// applyDisabled stops sing-box for an administratively disabled node and keeps
// reporting heartbeats so the daemon stays visible and controllable. It writes
// no agent state of its own (ReportTraffic owns the counter/pending state), so
// it can never clobber a flushed traffic report.
func (a *Agent) applyDisabled(ctx context.Context, response ConfigResponse) error {
	// Act unless the service is confirmed already stopped. An unknown probe or a
	// transitional (activating) state must not skip the stop, since the unit may
	// still be (or be coming) up.
	if !a.singBoxConfirmedDown(ctx) {
		// Flush before stopping a possibly-running service: stopping drops the
		// in-memory v2ray counters. ReportTraffic durably stages the pending
		// report before it POSTs, so a POST/server failure still preserves the
		// interval. We stop regardless (the operator disabled the node), so a
		// failure that prevented staging is surfaced as an explicit accounting
		// loss rather than silently dropped.
		if err := a.ReportTraffic(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent: stopping disabled node despite traffic flush failure; final interval may be unaccounted: %v\n", err)
		}
		if err := a.Runner.Run(ctx, "systemctl", "stop", a.Config.SingBoxService); err != nil {
			_ = a.ReportHeartbeat(ctx, response, "disabled")
			return err
		}
	}
	if err := a.ReportHeartbeat(ctx, response, "disabled"); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent disabled heartbeat failed: %v\n", err)
	}
	return nil
}

func (a *Agent) reportRuntimeState(ctx context.Context, response ConfigResponse) {
	reports := []struct {
		name string
		run  func(context.Context) error
	}{
		{"apply result", func(ctx context.Context) error { return a.ReportApplyResult(ctx, response, "applied", "") }},
		{"heartbeat", func(ctx context.Context) error { return a.ReportHeartbeat(ctx, response, "ok") }},
		{"traffic", a.ReportTraffic},
		// Connection telemetry is a no-op unless the node is opted in and the
		// collector produced something, so it costs an idle node nothing.
		{"connections", a.ReportConnections},
		{"network logs", a.ReportLogs},
		{"system logs", a.ReportSystemLogs},
	}
	for _, report := range reports {
		if err := report.run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent report %s failed: %v\n", report.name, err)
		}
	}
	if response.Hash != "" {
		state, err := a.LoadState()
		if err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent load state failed: %v\n", err)
			return
		}
		state.AppliedConfigHash = response.Hash
		if err := a.SaveState(state); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent save applied config state failed: %v\n", err)
		}
	}
}

func (a *Agent) Run(ctx context.Context) error {
	interval, err := time.ParseDuration(a.Config.PollInterval)
	if err != nil {
		return err
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	a.poll(ctx)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsOut := make(chan error, 2)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				errorsOut <- context.Cause(runCtx)
				return
			case <-ticker.C:
				a.poll(runCtx)
			}
		}
	}()
	go func() { errorsOut <- a.RunOperations(runCtx) }()
	err = <-errorsOut
	cancel()
	// Stage the collector's final window before the process exits. Connection
	// deltas live only in this process, so dropping them here would cost a whole
	// poll interval on every restart.
	a.flushConnectionsToState()
	if err == nil {
		return context.Cause(runCtx)
	}
	return err
}

// poll runs one config cycle and confirms a pending agent update guard. The
// confirmation is retried on every successful cycle: a server that is briefly
// unreachable right after an update must not leave the guard pending, or the
// next service start would silently roll the agent back to the old version.
func (a *Agent) poll(ctx context.Context) {
	err := a.Once(ctx)
	// Reconciled even when the cycle failed: the collector follows the config
	// already on disk, which a failed fetch or a rolled-back apply does not
	// change. Run is the only caller, so one-shot `boxfleet-agent once` never
	// starts a background collector.
	a.reconcileConnectionCollector(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "boxfleet-agent once failed: %v\n", err)
		}
		return
	}
	if err := a.ConfirmAgentUpdateGuard(); err != nil {
		fmt.Fprintf(os.Stderr, "boxfleet-agent confirm update guard failed: %v\n", err)
	}
}

func (a *Agent) ReportTraffic(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	if state.PendingTraffic != nil {
		if err := a.postJSON(ctx, "/api/node/traffic", state.PendingTraffic); err != nil {
			return err
		}
		state.PendingTraffic = nil
		if err := a.SaveState(state); err != nil {
			return err
		}
	}
	stats, err := v2raystats.Query(ctx, a.Config.V2RayAPIAddress, []string{"user>>>"}, false)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var deltas []model.TrafficDelta
	for _, stat := range stats {
		authName, direction, ok := parseUserTrafficStat(stat.Name)
		if !ok {
			continue
		}
		previous := state.LastCounters[stat.Name]
		epoch := state.CounterEpoch[stat.Name]
		delta := stat.Value - previous
		if delta < 0 {
			epoch++
			delta = stat.Value
		}
		state.LastCounters[stat.Name] = stat.Value
		state.CounterEpoch[stat.Name] = epoch
		if delta <= 0 {
			continue
		}
		deltas = append(deltas, model.TrafficDelta{
			AuthName:      authName,
			Direction:     direction,
			RawBytesDelta: delta,
			CounterValue:  stat.Value,
			CounterEpoch:  epoch,
			ObservedAt:    now,
		})
	}
	if len(deltas) == 0 {
		return a.SaveState(state)
	}
	state.Sequence++
	payload := model.TrafficReport{
		Sequence:    state.Sequence,
		AgentBootID: state.BootID,
		ReportedAt:  now,
		Deltas:      deltas,
	}
	state.PendingTraffic = &payload
	if err := a.SaveState(state); err != nil {
		return err
	}
	if err := a.postJSON(ctx, "/api/node/traffic", payload); err != nil {
		return err
	}
	state.PendingTraffic = nil
	return a.SaveState(state)
}

func (a *Agent) ReportLogs(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	args := []string{"-u", a.Config.SingBoxService, "--no-pager", "-o", "json"}
	if state.LastLogCursor != "" {
		args = append(args, "--after-cursor", state.LastLogCursor)
	} else if state.LastLogSince == "" {
		args = append(args, "-n", "50")
	} else {
		args = append(args, "--since", journalSinceArg(state.LastLogSince))
	}
	lastCursor := state.LastLogCursor
	batch := newJournalBatch[model.LogEventInput](journalBatchMaxEntries, journalBatchMaxBytes)
	batches := 0
	flush := func() error {
		if batch.len() == 0 {
			return nil
		}
		if err := a.postJSON(ctx, "/api/node/logs", model.LogEventReport{Events: batch.items}); err != nil {
			return err
		}
		state.LastLogSince = now
		state.LastLogCursor = lastCursor
		state.LastLogLines = nil
		if err := a.SaveState(state); err != nil {
			return err
		}
		batch.reset()
		batches++
		return nil
	}
	var streamErr error
	err = a.Runner.StreamLines(ctx, "journalctl", args, func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" {
			return true
		}
		entry, ok := parseJournalJSONLine(line)
		if !ok || entry.Message == "" {
			return true
		}
		if entry.Cursor != "" {
			lastCursor = entry.Cursor
		}
		observedAt := entry.ObservedAt
		if observedAt == "" {
			observedAt = now
		}
		batch.add(model.LogEventInput{
			Action:      "sing-box",
			RawMessage:  entry.Message,
			Cursor:      entry.Cursor,
			ObservedAt:  observedAt,
			Count:       1,
			WindowStart: observedAt,
			WindowEnd:   observedAt,
		}, len(entry.Message)+len(entry.Cursor)+len(observedAt)*3+len("sing-box"))
		if batch.full() {
			if streamErr = flush(); streamErr != nil {
				return false
			}
			if batches >= journalMaxBatches {
				return false
			}
		}
		return true
	})
	if streamErr != nil {
		return streamErr
	}
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	state.LastLogSince = now
	state.LastLogCursor = lastCursor
	state.LastLogLines = nil
	return a.SaveState(state)
}

func (a *Agent) ReportSystemLogs(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	services := []string{a.Config.AgentService, a.Config.SingBoxService}
	for _, service := range services {
		args := []string{"-u", service, "--no-pager", "-o", "json"}
		if cursor := state.LastSystemLogCursor[service]; cursor != "" {
			args = append(args, "--after-cursor", cursor)
		} else if since := state.LastSystemLogSince[service]; since != "" {
			args = append(args, "--since", journalSinceArg(since))
		} else {
			args = append(args, "-n", "50")
		}
		lastCursor := state.LastSystemLogCursor[service]
		batch := newJournalBatch[model.SystemLogInput](journalBatchMaxEntries, journalBatchMaxBytes)
		batches := 0
		flush := func() error {
			if batch.len() == 0 {
				return nil
			}
			if err := a.postJSON(ctx, "/api/node/system-logs", model.SystemLogReport{Entries: batch.items}); err != nil {
				return err
			}
			state.LastSystemLogSince[service] = now
			state.LastSystemLogCursor[service] = lastCursor
			if err := a.SaveState(state); err != nil {
				return err
			}
			batch.reset()
			batches++
			return nil
		}
		var streamErr error
		err := a.Runner.StreamLines(ctx, "journalctl", args, func(line string) bool {
			line = strings.TrimSpace(line)
			if line == "" {
				return true
			}
			entry, ok := parseJournalJSONLine(line)
			if !ok || entry.Message == "" {
				return true
			}
			if entry.Cursor != "" {
				lastCursor = entry.Cursor
			}
			observedAt := entry.ObservedAt
			if observedAt == "" {
				observedAt = now
			}
			batch.add(model.SystemLogInput{
				Service:    service,
				Level:      entry.Level,
				RawMessage: entry.Message,
				Cursor:     entry.Cursor,
				ObservedAt: observedAt,
			}, len(service)+len(entry.Level)+len(entry.Message)+len(entry.Cursor)+len(observedAt))
			if batch.full() {
				if streamErr = flush(); streamErr != nil {
					return false
				}
				if batches >= journalMaxBatches {
					return false
				}
			}
			return true
		})
		if streamErr != nil {
			return streamErr
		}
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		state.LastSystemLogSince[service] = now
		state.LastSystemLogCursor[service] = lastCursor
	}
	return a.SaveState(state)
}

type journalBatch[T any] struct {
	items    []T
	maxItems int
	maxBytes int
	bytes    int
}

func newJournalBatch[T any](maxItems, maxBytes int) *journalBatch[T] {
	return &journalBatch[T]{
		items:    make([]T, 0, maxItems),
		maxItems: maxItems,
		maxBytes: maxBytes,
	}
}

func (b *journalBatch[T]) add(item T, byteSize int) {
	b.items = append(b.items, item)
	b.bytes += byteSize
}

func (b *journalBatch[T]) full() bool {
	return len(b.items) >= b.maxItems || b.bytes >= b.maxBytes
}

func (b *journalBatch[T]) len() int {
	return len(b.items)
}

func (b *journalBatch[T]) reset() {
	b.items = b.items[:0]
	b.bytes = 0
}

// journalSinceArg renders a stored RFC3339 timestamp as the local
// "YYYY-MM-DD HH:MM:SS" form journalctl accepts on every systemd version. The
// RFC3339 T/Z form is rejected before systemd v247, which would make every log
// cycle fail on a node whose cursor was never set. Truncating to whole seconds
// only ever re-reads entries; it never skips them.
func journalSinceArg(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format(journalSinceLayout)
}

type journalEntry struct {
	Message    string
	Cursor     string
	ObservedAt string
	Level      string
}

func parseJournalJSONLine(line string) (journalEntry, bool) {
	var raw struct {
		Message           any    `json:"MESSAGE"`
		Cursor            string `json:"__CURSOR"`
		RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
		Priority          string `json:"PRIORITY"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return journalEntry{}, false
	}
	message := journalMessageString(raw.Message)
	if message == "" {
		return journalEntry{}, false
	}
	observedAt := ""
	if raw.RealtimeTimestamp != "" {
		if micros, err := strconv.ParseInt(raw.RealtimeTimestamp, 10, 64); err == nil {
			observedAt = time.UnixMicro(micros).UTC().Format(time.RFC3339Nano)
		}
	}
	return journalEntry{
		Message:    message,
		Cursor:     raw.Cursor,
		ObservedAt: observedAt,
		Level:      journaldPriorityLevel(raw.Priority),
	}, true
}

func journaldPriorityLevel(priority string) string {
	switch priority {
	case "0":
		return "emerg"
	case "1":
		return "alert"
	case "2":
		return "crit"
	case "3":
		return "err"
	case "4":
		return "warning"
	case "5":
		return "notice"
	case "6":
		return "info"
	case "7":
		return "debug"
	default:
		return ""
	}
}

func journalMessageString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		buf := make([]byte, 0, len(typed))
		for _, item := range typed {
			number, ok := item.(float64)
			if !ok || number < 0 || number > 255 {
				return ""
			}
			buf = append(buf, byte(number))
		}
		return string(buf)
	default:
		return ""
	}
}

func (a *Agent) LoadState() (State, error) {
	raw, err := os.ReadFile(a.Config.StatePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{
				BootID:              uuid.NewString(),
				LastCounters:        make(map[string]int64),
				CounterEpoch:        make(map[string]int64),
				LastLogLines:        make(map[string]bool),
				LastSystemLogCursor: make(map[string]string),
				LastSystemLogSince:  make(map[string]string),
			}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, err
	}
	if state.BootID == "" {
		state.BootID = uuid.NewString()
	}
	if state.LastCounters == nil {
		state.LastCounters = make(map[string]int64)
	}
	if state.CounterEpoch == nil {
		state.CounterEpoch = make(map[string]int64)
	}
	if state.LastLogLines == nil {
		state.LastLogLines = make(map[string]bool)
	}
	if state.LastSystemLogCursor == nil {
		state.LastSystemLogCursor = make(map[string]string)
	}
	if state.LastSystemLogSince == nil {
		state.LastSystemLogSince = make(map[string]string)
	}
	return state, nil
}

func (a *Agent) SaveState(state State) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.Config.StatePath, append(raw, '\n'), defaultConfigFilePerm)
}

func (a *Agent) FetchConfigVersioned(ctx context.Context) (ConfigResponse, error) {
	url := strings.TrimRight(a.Config.ServerURL, "/") + "/api/node/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ConfigResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Config.Token)
	req.Header.Set("X-BoxFleet-Node", a.nodeName())
	resp, err := a.client().Do(req)
	if err != nil {
		return ConfigResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ConfigResponse{}, fmt.Errorf("fetch config: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := a.adoptCanonicalNodeName(resp.Header.Get(model.CanonicalNodeNameHeader)); err != nil {
		return ConfigResponse{}, err
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ConfigResponse{}, err
	}
	return ConfigResponse{
		Data:      data,
		VersionID: resp.Header.Get("X-BoxFleet-Config-Version-ID"),
		Version:   resp.Header.Get("X-BoxFleet-Config-Version"),
		Hash:      resp.Header.Get("X-BoxFleet-Config-SHA256"),
		Mode:      resp.Header.Get("X-BoxFleet-Config-Mode"),
		State:     resp.Header.Get("X-BoxFleet-Node-State"),
	}, nil
}

func (a *Agent) ReportApplyResult(ctx context.Context, response ConfigResponse, status, message string) error {
	if response.VersionID == "" {
		return nil
	}
	payload := model.ApplyResult{
		ConfigVersionID: response.VersionID,
		ConfigHash:      response.Hash,
		Status:          status,
		Error:           message,
		ReportedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	return a.postJSON(ctx, "/api/node/apply-result", payload)
}

func (a *Agent) ReportHeartbeat(ctx context.Context, response ConfigResponse, status string) error {
	singBoxVersion := ""
	if output, err := a.Runner.Output(ctx, a.Config.SingBoxPath, "version"); err == nil {
		singBoxVersion = firstLine(string(output))
	}
	payload := model.Heartbeat{
		AgentVersion:         Version,
		AgentGOOS:            runtime.GOOS,
		AgentGOARCH:          runtime.GOARCH,
		Capabilities:         agentCapabilities(),
		SingBoxVersion:       singBoxVersion,
		Status:               status,
		CurrentConfigVersion: response.VersionID,
		CurrentConfigHash:    response.Hash,
		ReportedAt:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	return a.postJSON(ctx, "/api/node/heartbeat", payload)
}

func (a *Agent) postJSON(ctx context.Context, path string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(a.Config.ServerURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Config.Token)
	req.Header.Set("X-BoxFleet-Node", a.nodeName())
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("post %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := a.adoptCanonicalNodeName(resp.Header.Get(model.CanonicalNodeNameHeader)); err != nil {
		return err
	}
	return nil
}

// nodeName reads the canonical node name, which the server may rename while
// other goroutines (poll loop, operation executor, lease monitor) are issuing
// requests.
func (a *Agent) nodeName() string {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.Config.NodeName
}

func (a *Agent) adoptCanonicalNodeName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == a.nodeName() {
		return nil
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if name == a.Config.NodeName {
		return nil
	}
	next := a.Config
	next.NodeName = name
	if err := WriteConfig(next.AgentConfigPath, next); err != nil {
		return fmt.Errorf("persist canonical node name %q: %w", name, err)
	}
	a.Config.NodeName = name
	return nil
}

func (a *Agent) InstallSystemdUnits() error {
	if err := os.MkdirAll(filepath.Dir(a.Config.SingBoxPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Config.SingBoxConfig), 0o755); err != nil {
		return err
	}
	data := systemdUnitData{
		SingBoxPath:       a.Config.SingBoxPath,
		SingBoxConfig:     a.Config.SingBoxConfig,
		AgentPath:         a.Config.AgentPath,
		AgentGuardPath:    a.Config.AgentGuardPath,
		AgentConfigPath:   a.Config.AgentConfigPath,
		Restart:           "on-failure",
		RestartSec:        "3s",
		SingBoxLimitFiles: 1048576,
	}
	singBoxUnit, err := renderSystemdUnit("sing-box", singBoxUnitTemplate, data)
	if err != nil {
		return err
	}
	data.Restart = "always"
	data.RestartSec = "10s"
	agentUnit, err := renderSystemdUnit("boxfleet-agent", agentUnitTemplate, data)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join("/etc/systemd/system", a.Config.SingBoxService), []byte(singBoxUnit), 0o644); err != nil {
		return err
	}
	return atomicWrite(filepath.Join("/etc/systemd/system", a.Config.AgentService), []byte(agentUnit), 0o644)
}

func (a *Agent) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return http.DefaultClient
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}

func bytesSHA256Hex(data []byte) string {
	sum := sha256.Sum256(bytes.TrimSpace(data))
	return hex.EncodeToString(sum[:])
}

func parseUserTrafficStat(name string) (authName, direction string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 {
		return "", "", false
	}
	if parts[0] != "user" || parts[2] != "traffic" {
		return "", "", false
	}
	if parts[3] != "uplink" && parts[3] != "downlink" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return renameio.WriteFile(path, data, perm)
}
