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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	stderrCaptureLimit     = 4096
)

type Config struct {
	NodeName        string `json:"node_name"`
	Token           string `json:"token"`
	ServerURL       string `json:"server_url"`
	SingBoxURL      string `json:"sing_box_url"`
	InstallDir      string `json:"install_dir"`
	SingBoxPath     string `json:"sing_box_path"`
	SingBoxConfig   string `json:"sing_box_config"`
	SingBoxService  string `json:"sing_box_service"`
	AgentPath       string `json:"agent_path"`
	AgentConfigPath string `json:"agent_config_path"`
	AgentService    string `json:"agent_service"`
	PollInterval    string `json:"poll_interval"`
	StatePath       string `json:"state_path"`
	V2RayAPIAddress string `json:"v2ray_api_address"`
}

type Agent struct {
	Config Config
	Runner Runner
	Client *http.Client
}

type ConfigResponse struct {
	Data      []byte
	VersionID string
	Version   string
	Hash      string
	Mode      string
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
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	stopped := false
	for scanner.Scan() {
		if !handle(scanner.Text()) {
			stopped = true
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			break
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	stderrText := <-stderrDone
	if scanErr != nil {
		return fmt.Errorf("%s %s: read stdout: %w", name, strings.Join(args, " "), scanErr)
	}
	if waitErr != nil && !stopped {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), waitErr, stderrText)
	}
	return nil
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
	return nil
}

func New(config Config) *Agent {
	config.ApplyDefaults()
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Config.SingBoxURL, nil)
	if err != nil {
		return err
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download sing-box: %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return err
	}
	return atomicWrite(a.Config.SingBoxPath, buf.Bytes(), defaultBinaryFilePerm)
}

func (a *Agent) Once(ctx context.Context) error {
	response, err := a.FetchConfigVersioned(ctx)
	if err != nil {
		return err
	}
	config := response.Data
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	configHash := response.Hash
	if configHash == "" {
		configHash = bytesSHA256Hex(config)
		response.Hash = configHash
	}
	if current, err := os.ReadFile(a.Config.SingBoxConfig); err == nil && bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(config)) {
		if state.AppliedConfigHash == configHash {
			a.reportRuntimeState(ctx, response)
			return nil
		}
		if err := a.Runner.Run(ctx, "systemctl", "restart", a.Config.SingBoxService); err != nil {
			_ = a.ReportApplyResult(ctx, response, "failed", err.Error())
			return err
		}
		a.reportRuntimeState(ctx, response)
		return nil
	}
	tmp := a.Config.SingBoxConfig + ".candidate"
	if err := atomicWrite(tmp, config, defaultRuntimeFilePerm); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, a.Config.SingBoxPath, "check", "-c", tmp); err != nil {
		_ = a.ReportApplyResult(ctx, response, "failed", err.Error())
		return err
	}
	if err := atomicWrite(a.Config.SingBoxConfig, config, defaultRuntimeFilePerm); err != nil {
		return err
	}
	if err := a.Runner.Run(ctx, "systemctl", "restart", a.Config.SingBoxService); err != nil {
		_ = a.ReportApplyResult(ctx, response, "failed", err.Error())
		return err
	}
	a.reportRuntimeState(ctx, response)
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
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := a.Once(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent once failed: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
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
		args = append(args, "--since", state.LastLogSince)
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
			args = append(args, "--since", since)
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
	req.Header.Set("X-BoxFleet-Node", a.Config.NodeName)
	resp, err := a.client().Do(req)
	if err != nil {
		return ConfigResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ConfigResponse{}, fmt.Errorf("fetch config: %s: %s", resp.Status, strings.TrimSpace(string(body)))
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
	req.Header.Set("X-BoxFleet-Node", a.Config.NodeName)
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
	if err := os.WriteFile(filepath.Join("/etc/systemd/system", a.Config.SingBoxService), []byte(singBoxUnit), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("/etc/systemd/system", a.Config.AgentService), []byte(agentUnit), 0o644)
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
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
