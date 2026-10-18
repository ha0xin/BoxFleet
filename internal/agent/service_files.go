package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/renameio/v2"
	"os"
	"path/filepath"
	"time"
)

type serviceFile struct {
	Path    string
	Data    []byte
	Mode    os.FileMode
	Missing bool
}

func (a *Agent) serviceDir() string {
	if a.ServiceDir != "" {
		return a.ServiceDir
	}
	if a.Config.InitSystem == InitSystemOpenRC {
		return "/etc/init.d"
	}
	return "/etc/systemd/system"
}

func (a *Agent) serviceFiles() ([]serviceFile, error) {
	switch a.Config.InitSystem {
	case InitSystemSystemd:
		return a.systemdFiles()
	case InitSystemOpenRC:
		return a.openRCFiles()
	default:
		return nil, fmt.Errorf("unsupported init_system %q", a.Config.InitSystem)
	}
}

func writeServiceFiles(files []serviceFile) error {
	for _, file := range files {
		if file.Missing {
			if err := os.Remove(file.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
				return err
			}
			if err := writeServiceFile(file); err != nil {
				return err
			}
		}
	}
	return nil
}

// renameio.WriteFile preserves an existing file's permissions. Service scripts
// need the rendered mode, including repairing a non-executable OpenRC script.
func writeServiceFile(file serviceFile) error {
	pending, err := renameio.NewPendingFile(file.Path, renameio.WithStaticPermissions(file.Mode))
	if err != nil {
		return err
	}
	defer pending.Cleanup()
	if _, err := pending.Write(file.Data); err != nil {
		return err
	}
	return pending.CloseAtomicallyReplace()
}

func (a *Agent) serviceBackupPath() string {
	return filepath.Join(a.Config.InstallDir, "state", "service-files-backup.json")
}

// syncServiceUnits applies this binary's templates. During managed upgrades,
// retain a durable snapshot until heartbeat and migration are both confirmed.
func (a *Agent) syncServiceUnits(ctx context.Context) error {
	desired, err := a.serviceFiles()
	if err != nil {
		return err
	}
	previous := make([]serviceFile, 0, len(desired))
	changed := false
	for _, file := range desired {
		old := serviceFile{Path: file.Path}
		info, err := os.Stat(file.Path)
		if errors.Is(err, os.ErrNotExist) {
			old.Missing = true
		} else if err != nil {
			return err
		} else {
			old.Mode = info.Mode().Perm()
			old.Data, err = os.ReadFile(file.Path)
			if err != nil {
				return err
			}
		}
		changed = changed || old.Missing || old.Mode != file.Mode || !bytes.Equal(old.Data, file.Data)
		previous = append(previous, old)
	}
	guard, err := a.loadAgentUpdateGuard()
	if err != nil {
		return err
	}
	pending := guard != nil && guard.Status == "pending" && versionsEquivalent(Version, guard.ExpectedVersion)
	if !changed {
		// A previous process may have died after writing files but before reload.
		if _, err := os.Stat(a.serviceBackupPath()); err == nil {
			return a.reloadServices(ctx)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if pending {
		if _, err := os.Stat(a.serviceBackupPath()); errors.Is(err, os.ErrNotExist) {
			raw, err := json.Marshal(previous)
			if err != nil {
				return err
			}
			if err := atomicWrite(a.serviceBackupPath(), raw, defaultConfigFilePerm); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if err := writeServiceFiles(desired); err != nil {
		return a.undoServiceWrite(err, previous)
	}
	if err := a.reloadServices(ctx); err != nil {
		return a.undoServiceWrite(err, previous)
	}
	return nil
}

func (a *Agent) undoServiceWrite(cause error, previous []serviceFile) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return errors.Join(cause, writeServiceFiles(previous), a.reloadServices(ctx))
}

func (a *Agent) restoreServiceBackup(ctx context.Context) error {
	raw, err := os.ReadFile(a.serviceBackupPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var files []serviceFile
	if err := json.Unmarshal(raw, &files); err != nil {
		return err
	}
	expected := []string{
		filepath.Join(a.serviceDir(), serviceName(a.Config.InitSystem, a.Config.SingBoxService)),
		filepath.Join(a.serviceDir(), serviceName(a.Config.InitSystem, a.Config.AgentService)),
	}
	if len(files) != len(expected) {
		return errors.New("invalid service backup")
	}
	for i, file := range files {
		if file.Path != expected[i] {
			return fmt.Errorf("unexpected service backup path %q", file.Path)
		}
	}
	if err := writeServiceFiles(files); err != nil {
		return err
	}
	if err := a.reloadServices(ctx); err != nil {
		return err
	}
	return a.clearServiceBackup()
}

func (a *Agent) clearServiceBackup() error {
	err := os.Remove(a.serviceBackupPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
