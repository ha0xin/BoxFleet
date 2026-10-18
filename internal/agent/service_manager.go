package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	InitSystemSystemd = "systemd"
	InitSystemOpenRC  = "openrc"
)

func detectInitSystem() (string, error) {
	if _, err := exec.LookPath("systemctl"); err == nil {
		return InitSystemSystemd, nil
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		return InitSystemOpenRC, nil
	}
	return "", errors.New("unsupported init system: install systemd or OpenRC")
}

func serviceName(initSystem, name string) string {
	if initSystem == InitSystemOpenRC {
		return strings.TrimSuffix(name, ".service")
	}
	return name
}

func (a *Agent) installServiceUnits() error {
	switch a.Config.InitSystem {
	case InitSystemSystemd:
		return a.InstallSystemdUnits()
	case InitSystemOpenRC:
		return a.InstallOpenRCScripts()
	default:
		return fmt.Errorf("unsupported init_system %q", a.Config.InitSystem)
	}
}

func (a *Agent) reloadServices(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, a.serviceCommandTimeout())
	defer cancel()
	if a.Config.InitSystem == InitSystemOpenRC {
		return nil
	}
	return a.Runner.Run(ctx, "systemctl", "daemon-reload")
}

func (a *Agent) enableService(ctx context.Context, service string) error {
	ctx, cancel := context.WithTimeout(ctx, a.serviceCommandTimeout())
	defer cancel()
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		return a.Runner.Run(ctx, "rc-update", "add", service, "default")
	}
	return a.Runner.Run(ctx, "systemctl", "enable", service)
}

var errServiceOperationUncertain = errors.New("service operation timed out or was interrupted; its manager job may still be running")

func (a *Agent) restartService(ctx context.Context, service string) error {
	ctx, cancel := context.WithTimeout(ctx, a.serviceCommandTimeout())
	defer cancel()
	service = serviceName(a.Config.InitSystem, service)
	var err error
	if a.Config.InitSystem == InitSystemOpenRC {
		err = a.Runner.Run(ctx, "rc-service", service, "restart")
	} else {
		err = a.Runner.Run(ctx, "systemctl", "restart", service)
	}
	if ctx.Err() != nil {
		diagnosticCtx, diagnosticCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer diagnosticCancel()
		state, stateErr := a.serviceActiveState(diagnosticCtx, service)
		if stateErr != nil {
			state = "unknown"
		}
		return fmt.Errorf("%s (observed state %s): %w", service, state, errors.Join(errServiceOperationUncertain, ctx.Err(), err))
	}
	return err
}

func (a *Agent) stopService(ctx context.Context, service string) error {
	ctx, cancel := context.WithTimeout(ctx, a.serviceCommandTimeout())
	defer cancel()
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		return a.Runner.Run(ctx, "rc-service", service, "stop")
	}
	return a.Runner.Run(ctx, "systemctl", "stop", service)
}

func (a *Agent) serviceActiveState(ctx context.Context, service string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.serviceCommandTimeout())
	defer cancel()
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		if err := a.Runner.Run(ctx, "rc-service", service, "status"); err != nil {
			// OpenRC returns a non-zero status for a known, stopped service.
			if _, statErr := os.Stat(filepath.Join(a.serviceDir(), service)); statErr == nil {
				return "inactive", nil
			}
			return "", err
		}
		return "active", nil
	}
	out, err := a.Runner.Output(ctx, "systemctl", "show", "-p", "ActiveState", "--value", service)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *Agent) serviceCommandTimeout() time.Duration {
	if a.ServiceCommandTimeout > 0 {
		return a.ServiceCommandTimeout
	}
	return 30 * time.Second
}
