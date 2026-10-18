package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Install can be resumed using the existing agent.json; it never re-enrolls the
// node or discards accounting state. Stop the daemon before one-shot mutations.
func (a *Agent) Install(ctx context.Context) (err error) {
	installationUnlock, err := a.lockFile("install.lock")
	if err != nil {
		return err
	}
	defer installationUnlock()
	defer func() {
		diagnosticCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, service := range []string{a.Config.SingBoxService, a.Config.AgentService} {
			state, stateErr := a.serviceActiveState(diagnosticCtx, service)
			if stateErr != nil {
				state = "unknown"
			}
			fmt.Fprintf(os.Stderr, "boxfleet-agent install: %s: %s\n", service, state)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "boxfleet-agent install incomplete; resume with: %q install --config %q\n", a.Config.AgentPath, a.Config.AgentConfigPath)
			if a.Config.InitSystem == InitSystemSystemd {
				fmt.Fprintln(os.Stderr, "Inspect pending jobs: systemctl list-jobs; inspect logs: journalctl -u "+a.Config.AgentService+" -u "+a.Config.SingBoxService)
			}
		}
	}()
	step := func(name string, limit time.Duration, fn func(context.Context) error) error {
		fmt.Fprintf(os.Stderr, "boxfleet-agent install: %s...\n", name)
		start := time.Now()
		stepCtx, cancel := context.WithTimeout(ctx, limit)
		defer cancel()
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					fmt.Fprintf(os.Stderr, "boxfleet-agent install: still %s (%s elapsed)\n", name, time.Since(start).Round(time.Second))
				}
			}
		}()
		if err := fn(stepCtx); err != nil {
			return fmt.Errorf("%s after %s: %w", name, time.Since(start).Round(time.Millisecond), errors.Join(err, stepCtx.Err()))
		}
		if err := stepCtx.Err(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(os.Stderr, "boxfleet-agent install: %s completed (%s)\n", name, time.Since(start).Round(time.Millisecond))
		return nil
	}
	if a.Config.SingBoxURL != "" {
		if err = step("downloading sing-box", DefaultHTTPTimeout, a.DownloadSingBox); err != nil {
			return err
		}
	}
	if err = step("checking sing-box capabilities", time.Minute, a.CheckSingBoxV2RayAPI); err != nil {
		return err
	}
	if err = step("stopping agent for installation", a.serviceCommandTimeout(), func(ctx context.Context) error {
		if _, err := os.Stat(filepath.Join(a.serviceDir(), serviceName(a.Config.InitSystem, a.Config.AgentService))); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		state, err := a.serviceActiveState(ctx, a.Config.AgentService)
		if err != nil {
			return err
		}
		if state == "inactive" || state == "failed" || state == "" {
			return nil
		}
		return a.stopService(ctx, a.Config.AgentService)
	}); err != nil {
		return err
	}
	unlock, lockErr := a.lockRuntime()
	if lockErr != nil {
		return lockErr
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	if a.prepareInstall != nil {
		if err = step("writing agent configuration", time.Minute, func(context.Context) error { return a.prepareInstall() }); err != nil {
			return err
		}
	}
	if err = step("installing service files", time.Minute, func(ctx context.Context) error {
		if err := a.ensureAgentGuardBinary(); err != nil {
			return err
		}
		return a.syncServiceUnits(ctx)
	}); err != nil {
		return err
	}
	if err = step("enabling sing-box", time.Minute, func(ctx context.Context) error { return a.enableService(ctx, a.Config.SingBoxService) }); err != nil {
		return err
	}
	if err = step("fetching and applying initial configuration", time.Minute, func(ctx context.Context) error {
		a.initialSync = true
		defer func() { a.initialSync = false }()
		return a.Once(ctx)
	}); err != nil {
		return err
	}
	unlock()
	locked = false
	if err = step("enabling and starting agent", time.Minute, func(ctx context.Context) error {
		if err := a.enableService(ctx, a.Config.AgentService); err != nil {
			return err
		}
		if err := a.restartService(ctx, a.Config.AgentService); err != nil {
			return err
		}
		return a.waitServiceActive(ctx, a.Config.AgentService)
	}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "boxfleet-agent install complete; agent will report its heartbeat and telemetry to the server")
	return nil
}

// LockForOneShot prevents a CLI config reconciliation racing the daemon.
func (a *Agent) lockRuntime() (func(), error) { return a.lockFile("runtime.lock") }

func (a *Agent) LockForOneShot() (func(), error) { return a.lockRuntime() }
