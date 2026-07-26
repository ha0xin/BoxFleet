package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

// systemLogTimeFormat mirrors the schema's strftime('%Y-%m-%dT%H:%M:%fZ')
// default. It is fixed width, so recent-log ordering and retention pruning stay
// correct as plain string comparisons.
const systemLogTimeFormat = "2006-01-02T15:04:05.000Z"

type SystemLog struct {
	ID            string
	NodeID        string
	NodeName      string
	Service       string
	JournalCursor sql.NullString
	MessageHash   string
	Level         string
	RawMessage    string
	ObservedAt    string
	IngestedAt    string
}

type SystemLogReport = model.SystemLogReport
type SystemLogInput = model.SystemLogInput

func (db *DB) RecordSystemLogs(ctx context.Context, report SystemLogReport) error {
	node, err := db.GetNode(ctx, report.NodeName)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(systemLogTimeFormat)
	for _, entry := range report.Entries {
		service := strings.TrimSpace(entry.Service)
		message := strings.TrimSpace(entry.RawMessage)
		if service == "" || message == "" {
			continue
		}
		cursor := strings.TrimSpace(entry.Cursor)
		observedAt := normalizeObservedAt(entry.ObservedAt, now)
		logID, err := id.New("slg")
		if err != nil {
			return err
		}
		if _, err := db.q.CreateSystemLog(ctx, store.CreateSystemLogParams{
			ID:            logID,
			NodeID:        node.ID,
			Service:       service,
			JournalCursor: nullableTrimmedString(cursor),
			MessageHash:   systemLogMessageHash(service, cursor, message),
			Level:         strings.TrimSpace(entry.Level),
			RawMessage:    message,
			ObservedAt:    observedAt,
		}); err != nil {
			return err
		}
	}
	return db.DeleteExpiredSystemLogs(ctx)
}

// DeleteExpiredSystemLogs prunes system logs on the same retention window as
// structured network events, so a single operator setting bounds all node-fed
// log storage.
func (db *DB) DeleteExpiredSystemLogs(ctx context.Context) error {
	days, err := db.NetworkEventRetentionDays(ctx)
	if err != nil {
		return err
	}
	before := time.Now().UTC().AddDate(0, 0, -int(days)).Format(systemLogTimeFormat)
	_, err = db.sql.ExecContext(ctx, "DELETE FROM system_logs WHERE observed_at < ?", before)
	return err
}

func normalizeObservedAt(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fallback
	}
	return parsed.UTC().Format(systemLogTimeFormat)
}

func (db *DB) ListRecentSystemLogs(ctx context.Context, nodeName string, limit int64) ([]SystemLog, error) {
	if nodeName != "" {
		node, err := db.GetNode(ctx, nodeName)
		if err != nil {
			return nil, err
		}
		rows, err := db.q.ListRecentSystemLogsByNode(ctx, store.ListRecentSystemLogsByNodeParams{
			NodeName: node.Name,
			Limit:    limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]SystemLog, 0, len(rows))
		for _, row := range rows {
			out = append(out, SystemLog{
				ID:            row.ID,
				NodeID:        row.NodeID,
				NodeName:      row.NodeName,
				Service:       row.Service,
				JournalCursor: row.JournalCursor,
				MessageHash:   row.MessageHash,
				Level:         row.Level,
				RawMessage:    row.RawMessage,
				ObservedAt:    row.ObservedAt,
				IngestedAt:    row.IngestedAt,
			})
		}
		return out, nil
	}
	rows, err := db.q.ListRecentSystemLogs(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]SystemLog, 0, len(rows))
	for _, row := range rows {
		out = append(out, SystemLog{
			ID:            row.ID,
			NodeID:        row.NodeID,
			NodeName:      row.NodeName,
			Service:       row.Service,
			JournalCursor: row.JournalCursor,
			MessageHash:   row.MessageHash,
			Level:         row.Level,
			RawMessage:    row.RawMessage,
			ObservedAt:    row.ObservedAt,
			IngestedAt:    row.IngestedAt,
		})
	}
	return out, nil
}

func systemLogMessageHash(service, cursor, message string) string {
	sum := sha256.Sum256([]byte(service + "\x00" + cursor + "\x00" + message))
	return hex.EncodeToString(sum[:])
}
