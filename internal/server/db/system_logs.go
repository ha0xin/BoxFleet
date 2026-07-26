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

// SystemLogFilter mirrors the controls on the System Logs page. An empty field
// means "no filter"; there is no "all" sentinel, because a node may legitimately
// be named "all".
type SystemLogFilter struct {
	NodeName  string
	Service   string
	Level     string
	Search    string
	Sort      string
	Direction string
	Limit     int64
	Offset    int64
}

type SystemLogPage struct {
	Logs []SystemLog
	// Services is every service that has ever reported, not just the ones on
	// this page: a filter's options have to stay put when the filter is applied,
	// and one page of a paged table can no longer enumerate them.
	Services []string
	Total    int64
	Limit    int64
	Offset   int64
}

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

func (db *DB) ListSystemLogsPage(ctx context.Context, filter SystemLogFilter) (SystemLogPage, error) {
	nodeID := ""
	if strings.TrimSpace(filter.NodeName) != "" {
		// Resolve the name so an unknown node surfaces as an error instead of an
		// empty page, and so the filter hits idx_system_logs_node_observed.
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return SystemLogPage{}, err
		}
		nodeID = node.ID
	}
	limit := pageLimit(filter.Limit, 100)
	offset := pageOffset(filter.Offset)
	where, args := systemLogPageWhere(filter, nodeID)
	// Unlike nodes and users, system logs have no soft-delete predicate to seed
	// the clause with, so an unfiltered page needs a standing-true WHERE.
	whereSQL := "1 = 1"
	if len(where) > 0 {
		whereSQL = strings.Join(where, " AND ")
	}
	var total int64
	countQuery := `
SELECT COUNT(*)
FROM system_logs l
JOIN nodes n ON n.id = l.node_id
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return SystemLogPage{}, err
	}
	sortSQL := systemLogPageSort(filter.Sort, filter.Direction)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)
	listQuery := `
SELECT
  l.id,
  l.node_id,
  n.name AS node_name,
  l.service,
  l.journal_cursor,
  l.message_hash,
  l.level,
  l.raw_message,
  l.observed_at,
  l.ingested_at
FROM system_logs l
JOIN nodes n ON n.id = l.node_id
WHERE ` + whereSQL + `
ORDER BY ` + sortSQL + `
LIMIT ?
OFFSET ?`
	rows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return SystemLogPage{}, err
	}
	defer rows.Close()
	logs := make([]SystemLog, 0)
	for rows.Next() {
		var entry SystemLog
		if err := rows.Scan(
			&entry.ID,
			&entry.NodeID,
			&entry.NodeName,
			&entry.Service,
			&entry.JournalCursor,
			&entry.MessageHash,
			&entry.Level,
			&entry.RawMessage,
			&entry.ObservedAt,
			&entry.IngestedAt,
		); err != nil {
			return SystemLogPage{}, err
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return SystemLogPage{}, err
	}
	services, err := db.listSystemLogServices(ctx)
	if err != nil {
		return SystemLogPage{}, err
	}
	return SystemLogPage{
		Logs:     logs,
		Services: services,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func (db *DB) listSystemLogServices(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT service FROM system_logs ORDER BY service`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	services := make([]string, 0)
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return services, rows.Err()
}

func systemLogPageWhere(filter SystemLogFilter, nodeID string) ([]string, []any) {
	where := make([]string, 0, 4)
	args := make([]any, 0, 6)
	if nodeID != "" {
		where = append(where, "l.node_id = ?")
		args = append(args, nodeID)
	}
	if service := strings.TrimSpace(filter.Service); service != "" {
		where = append(where, "l.service = ?")
		args = append(args, service)
	}
	if levelSQL := systemLogLevelPredicate(filter.Level); levelSQL != "" {
		where = append(where, levelSQL)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, `(LOWER(n.name) LIKE ? OR LOWER(l.service) LIKE ? OR LOWER(l.level) LIKE ? OR LOWER(l.raw_message) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	return where, args
}

// systemLogLevelPredicate buckets journal levels the way the System Logs page
// does. Levels arrive as free text from journald ("warn", "warning", "err",
// "LEVEL_FATAL"), so each bucket is a substring test and "info" is the residual
// bucket rather than a literal match. The predicates carry no arguments, so an
// unrecognised bucket simply drops the filter instead of reaching SQL.
func systemLogLevelPredicate(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return `(LOWER(l.level) LIKE '%err%' OR LOWER(l.level) LIKE '%fatal%')`
	case "warn":
		return `LOWER(l.level) LIKE '%warn%'`
	case "debug":
		return `(LOWER(l.level) LIKE '%debug%' OR LOWER(l.level) LIKE '%trace%')`
	case "info":
		return `(LOWER(l.level) NOT LIKE '%err%'
    AND LOWER(l.level) NOT LIKE '%fatal%'
    AND LOWER(l.level) NOT LIKE '%warn%'
    AND LOWER(l.level) NOT LIKE '%debug%'
    AND LOWER(l.level) NOT LIKE '%trace%')`
	default:
		return ""
	}
}

func systemLogPageSort(sort, direction string) string {
	// Reading a journal is newest-first, so an unspecified direction means DESC
	// here — the opposite of the name-ordered node, proxy, and user pages.
	dir := "DESC"
	if strings.EqualFold(direction, "asc") {
		dir = "ASC"
	}
	sortColumn := "l.observed_at"
	switch strings.TrimSpace(sort) {
	case "node":
		sortColumn = "n.name"
	case "service":
		sortColumn = "l.service"
	case "level":
		sortColumn = "l.level"
	case "message":
		sortColumn = "l.raw_message"
	case "ingested_at":
		sortColumn = "l.ingested_at"
	}
	if sortColumn == "l.observed_at" {
		// The id tiebreaker keeps a page boundary from repeating or dropping a
		// row when a burst of entries shares one timestamp.
		return "l.observed_at " + dir + ", l.id DESC"
	}
	return sortColumn + " " + dir + ", l.observed_at DESC, l.id DESC"
}

func systemLogMessageHash(service, cursor, message string) string {
	sum := sha256.Sum256([]byte(service + "\x00" + cursor + "\x00" + message))
	return hex.EncodeToString(sum[:])
}
