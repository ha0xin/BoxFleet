package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
)

const openRCLogReadLimit = 2 * 1024 * 1024

func (a *Agent) openRCLogPath(service string) string {
	return filepath.Join(a.Config.InstallDir, "log", serviceName(InitSystemOpenRC, service)+".log")
}

func readOpenRCLog(path string, offset int64) ([]string, int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, openRCLogReadLimit), journalReadBufferBytes)
	lines := make([]string, 0, 64)
	consumed := int64(0)
	for {
		raw, readErr := reader.ReadString('\n')
		if len(raw) > 0 {
			consumed += int64(len(raw))
			if line := strings.TrimRight(raw, "\r\n"); line != "" {
				lines = append(lines, line)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, offset, readErr
		}
	}
	return lines, offset + consumed, nil
}

func (a *Agent) reportOpenRCNetworkLogs(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	lines, next, err := readOpenRCLog(a.openRCLogPath(a.Config.SingBoxService), state.LastOpenRCLogOffset)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for start := 0; start < len(lines); start += journalBatchMaxEntries {
		end := start + journalBatchMaxEntries
		if end > len(lines) {
			end = len(lines)
		}
		events := make([]model.LogEventInput, 0, end-start)
		for i, line := range lines[start:end] {
			events = append(events, model.LogEventInput{Action: "sing-box", RawMessage: line, Cursor: "openrc:" + strconv.FormatInt(state.LastOpenRCLogOffset+int64(start+i), 10), ObservedAt: now, Count: 1, WindowStart: now, WindowEnd: now})
		}
		if err := a.postJSON(ctx, "/api/node/logs", model.LogEventReport{Events: events}); err != nil {
			return err
		}
	}
	state.LastOpenRCLogOffset = next
	return a.SaveState(state)
}

func (a *Agent) reportOpenRCSystemLogs(ctx context.Context) error {
	state, err := a.LoadState()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, service := range []string{a.Config.AgentService, a.Config.SingBoxService} {
		lines, next, err := readOpenRCLog(a.openRCLogPath(service), state.LastOpenRCSystemLogOffset[service])
		if err != nil {
			return err
		}
		for start := 0; start < len(lines); start += journalBatchMaxEntries {
			end := start + journalBatchMaxEntries
			if end > len(lines) {
				end = len(lines)
			}
			entries := make([]model.SystemLogInput, 0, end-start)
			for i, line := range lines[start:end] {
				entries = append(entries, model.SystemLogInput{Service: service, Level: "info", RawMessage: line, Cursor: fmt.Sprintf("openrc:%d", state.LastOpenRCSystemLogOffset[service]+int64(start+i)), ObservedAt: now})
			}
			if err := a.postJSON(ctx, "/api/node/system-logs", model.SystemLogReport{Entries: entries}); err != nil {
				return err
			}
		}
		state.LastOpenRCSystemLogOffset[service] = next
	}
	return a.SaveState(state)
}
