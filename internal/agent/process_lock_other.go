//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package agent

import "errors"

func (a *Agent) lockFile(name string) (func(), error) {
	return nil, errors.New("node runtime requires systemd or OpenRC on a Unix host")
}
