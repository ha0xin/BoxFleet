package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if a.Config.InitSystem == InitSystemOpenRC {
		return nil
	}
	return a.Runner.Run(ctx, "systemctl", "daemon-reload")
}

func (a *Agent) enableService(ctx context.Context, service string) error {
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		return a.Runner.Run(ctx, "rc-update", "add", service, "default")
	}
	return a.Runner.Run(ctx, "systemctl", "enable", service)
}

func (a *Agent) restartService(ctx context.Context, service string) error {
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		return a.Runner.Run(ctx, "rc-service", service, "restart")
	}
	return a.Runner.Run(ctx, "systemctl", "restart", service)
}

func (a *Agent) stopService(ctx context.Context, service string) error {
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		return a.Runner.Run(ctx, "rc-service", service, "stop")
	}
	return a.Runner.Run(ctx, "systemctl", "stop", service)
}

func (a *Agent) serviceActiveState(ctx context.Context, service string) (string, error) {
	service = serviceName(a.Config.InitSystem, service)
	if a.Config.InitSystem == InitSystemOpenRC {
		if err := a.Runner.Run(ctx, "rc-service", service, "status"); err != nil {
			// OpenRC returns a non-zero status for a known, stopped service.
			if _, statErr := os.Stat(filepath.Join("/etc/init.d", service)); statErr == nil {
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
