package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"

	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

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
	_, _ = ctx, report
	return nil
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
