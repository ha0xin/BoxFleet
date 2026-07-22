package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type LogEvent = store.LogEvent
type RawLogEntry = store.RawLogEntry

type LogEventReport = model.LogEventReport
type LogEventInput = model.LogEventInput

type LogEventFilter struct {
	NodeName string
	UserName string
	Action   string
	Search   string
	Start    string
	End      string
	Limit    int64
	Offset   int64
}

type LogEventPage struct {
	Events []LogEventDetail
	Total  int64
	Limit  int64
	Offset int64
}

type LogEventDetail struct {
	ID           string
	NodeID       string
	NodeName     string
	ProxyUserID  sql.NullString
	UserName     string
	AuthName     string
	SourceIp     string
	TargetHost   string
	TargetPort   int64
	Action       string
	RawMessage   string
	Count        int64
	AggregateKey string
	WindowStart  string
	WindowEnd    string
	CreatedAt    string
}

type logEventsPageParams struct {
	NodeID    string
	UserID    string
	Action    string
	Search    string
	StartTime string
	EndTime   string
	Limit     int64
	Offset    int64
}

type parsedLogEvent struct {
	AuthName    string
	SourceIP    string
	TargetHost  string
	TargetPort  int64
	Action      string
	WindowStart string
	WindowEnd   string
}

func (db *DB) RecordLogEvents(ctx context.Context, report LogEventReport) error {
	node, err := db.GetNode(ctx, report.NodeName)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	connectionSources := make(map[string]string)
	for _, event := range report.Events {
		if parsed, ok := parseSingBoxLogEvent(event.RawMessage, connectionSources); ok {
			if event.AuthName == "" {
				event.AuthName = parsed.AuthName
			}
			if event.SourceIP == "" {
				event.SourceIP = parsed.SourceIP
			}
			if event.TargetHost == "" {
				event.TargetHost = parsed.TargetHost
			}
			if event.TargetPort == 0 {
				event.TargetPort = parsed.TargetPort
			}
			if event.Action == "" || event.Action == "sing-box" {
				event.Action = parsed.Action
			}
			if event.WindowStart == "" {
				event.WindowStart = parsed.WindowStart
			}
			if event.WindowEnd == "" {
				event.WindowEnd = parsed.WindowEnd
			}
		}
		if event.AuthName == "" || event.TargetHost == "" || event.TargetPort == 0 {
			continue
		}
		count := event.Count
		if count == 0 {
			count = 1
		}
		windowStart := event.WindowStart
		if windowStart == "" {
			windowStart = now
		}
		windowEnd := event.WindowEnd
		if windowEnd == "" {
			windowEnd = windowStart
		}
		proxyUserID := sql.NullString{}
		userID, err := db.q.GetProxyUserIDByNodeAuthName(ctx, store.GetProxyUserIDByNodeAuthNameParams{
			NodeName: node.Name,
			AuthName: event.AuthName,
		})
		if err == nil {
			proxyUserID = sql.NullString{String: userID, Valid: true}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !proxyUserID.Valid {
			continue
		}
		eventID, err := id.New("log")
		if err != nil {
			return err
		}
		if err := db.q.CreateLogEvent(ctx, store.CreateLogEventParams{
			ID:           eventID,
			NodeID:       node.ID,
			ProxyUserID:  proxyUserID,
			AuthName:     event.AuthName,
			SourceIp:     event.SourceIP,
			TargetHost:   event.TargetHost,
			TargetPort:   event.TargetPort,
			Action:       event.Action,
			RawMessage:   compactRawSample(event.RawMessage),
			Count:        count,
			AggregateKey: logEventAggregateKey(node.ID, proxyUserID, event, windowStart),
			WindowStart:  windowStart,
			WindowEnd:    windowEnd,
		}); err != nil {
			return err
		}
	}
	if err := db.DeleteExpiredLogEvents(ctx); err != nil {
		return err
	}
	return nil
}

func (db *DB) DeleteExpiredLogEvents(ctx context.Context) error {
	days, err := db.NetworkEventRetentionDays(ctx)
	if err != nil {
		return err
	}
	before := time.Now().UTC().AddDate(0, 0, -int(days)).Format(time.RFC3339Nano)
	return db.q.DeleteLogEventsBefore(ctx, before)
}

func logEventAggregateKey(nodeID string, proxyUserID sql.NullString, event LogEventInput, windowStart string) string {
	bucket := aggregateMinuteBucket(windowStart)
	if nodeID == "" || bucket == "" {
		return ""
	}
	userPart := strings.TrimSpace(event.AuthName)
	if proxyUserID.Valid {
		userPart = proxyUserID.String
	}
	parts := []string{
		nodeID,
		userPart,
		strings.TrimSpace(event.AuthName),
		strings.TrimSpace(event.SourceIP),
		strings.ToLower(strings.TrimSpace(event.TargetHost)),
		strconv.FormatInt(event.TargetPort, 10),
		strings.TrimSpace(event.Action),
		bucket,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func aggregateMinuteBucket(value string) string {
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC().Truncate(time.Minute).Format(time.RFC3339)
	}
	return value
}

func compactRawSample(message string) string {
	const maxRawSampleBytes = 512
	message = strings.TrimSpace(message)
	if len(message) <= maxRawSampleBytes {
		return message
	}
	return message[:maxRawSampleBytes]
}

func rawLogMessageHash(cursor, message string) string {
	sum := sha256.Sum256([]byte(cursor + "\x00" + message))
	return hex.EncodeToString(sum[:])
}

func (db *DB) ListRecentLogEvents(ctx context.Context, limit int64) ([]LogEvent, error) {
	return db.q.ListRecentLogEvents(ctx, limit)
}

func (db *DB) ListLogEventsPage(ctx context.Context, filter LogEventFilter) (LogEventPage, error) {
	nodeID := ""
	if strings.TrimSpace(filter.NodeName) != "" {
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return LogEventPage{}, err
		}
		nodeID = node.ID
	}
	userID := ""
	if strings.TrimSpace(filter.UserName) != "" {
		user, err := db.GetProxyUser(ctx, filter.UserName)
		if err != nil {
			return LogEventPage{}, err
		}
		userID = user.ID
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	params := logEventsPageParams{
		NodeID:    nodeID,
		UserID:    userID,
		Action:    strings.TrimSpace(filter.Action),
		Search:    strings.TrimSpace(filter.Search),
		StartTime: strings.TrimSpace(filter.Start),
		EndTime:   strings.TrimSpace(filter.End),
		Offset:    offset,
		Limit:     limit,
	}
	total, rows, err := db.queryLogEventsPage(ctx, params)
	if err != nil {
		return LogEventPage{}, err
	}
	events := make([]LogEventDetail, 0, len(rows))
	for _, row := range rows {
		events = append(events, LogEventDetail{
			ID:           row.ID,
			NodeID:       row.NodeID,
			NodeName:     row.NodeName,
			ProxyUserID:  row.ProxyUserID,
			UserName:     row.UserName,
			AuthName:     row.AuthName,
			SourceIp:     row.SourceIp,
			TargetHost:   row.TargetHost,
			TargetPort:   row.TargetPort,
			Action:       row.Action,
			RawMessage:   row.RawMessage,
			Count:        row.Count,
			AggregateKey: row.AggregateKey,
			WindowStart:  row.WindowStart,
			WindowEnd:    row.WindowEnd,
			CreatedAt:    row.CreatedAt,
		})
	}
	return LogEventPage{
		Events: events,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (db *DB) queryLogEventsPage(ctx context.Context, params logEventsPageParams) (int64, []store.ListLogEventsPageRow, error) {
	where := []string{"e.proxy_user_id IS NOT NULL"}
	args := make([]any, 0, 4)
	if params.NodeID != "" {
		where = append(where, "e.node_id = ?")
		args = append(args, params.NodeID)
	}
	if params.UserID != "" {
		where = append(where, "e.proxy_user_id = ?")
		args = append(args, params.UserID)
	}
	if params.Action != "" {
		where = append(where, "e.action = ? COLLATE NOCASE")
		args = append(args, params.Action)
	}
	if params.Search != "" {
		where = append(where, `(LOWER(n.name) LIKE ? OR LOWER(u.name) LIKE ? OR LOWER(e.auth_name) LIKE ? OR LOWER(e.source_ip) LIKE ? OR LOWER(e.target_host) LIKE ? OR CAST(e.target_port AS TEXT) LIKE ? OR LOWER(e.action) LIKE ? OR LOWER(e.raw_message) LIKE ?)`)
		pattern := "%" + strings.ToLower(params.Search) + "%"
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	if params.StartTime != "" {
		where = append(where, "e.window_end >= ?")
		args = append(args, params.StartTime)
	}
	if params.EndTime != "" {
		where = append(where, "e.window_start <= ?")
		args = append(args, params.EndTime)
	}
	whereSQL := strings.Join(where, " AND ")
	countJoins := ""
	if params.Search != "" {
		countJoins = `
JOIN nodes n ON n.id = e.node_id
JOIN proxy_users u ON u.id = e.proxy_user_id`
	}
	countQuery := `
SELECT COUNT(*)
FROM log_events e` + countJoins + `
WHERE ` + whereSQL
	var total int64
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, params.Limit, params.Offset)
	listQuery := `
SELECT
  e.id,
  e.node_id,
  e.proxy_user_id,
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.action,
  e.raw_message,
  e.count,
  e.aggregate_key,
  e.window_start,
  e.window_end,
  e.created_at,
  n.name AS node_name,
  u.name AS user_name
FROM log_events e
JOIN nodes n ON n.id = e.node_id
JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE ` + whereSQL + `
ORDER BY e.created_at DESC, e.window_end DESC, e.id DESC
LIMIT ?
OFFSET ?`
	sqlRows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return 0, nil, err
	}
	defer sqlRows.Close()
	rows := make([]store.ListLogEventsPageRow, 0)
	for sqlRows.Next() {
		var row store.ListLogEventsPageRow
		if err := sqlRows.Scan(
			&row.ID,
			&row.NodeID,
			&row.ProxyUserID,
			&row.AuthName,
			&row.SourceIp,
			&row.TargetHost,
			&row.TargetPort,
			&row.Action,
			&row.RawMessage,
			&row.Count,
			&row.AggregateKey,
			&row.WindowStart,
			&row.WindowEnd,
			&row.CreatedAt,
			&row.NodeName,
			&row.UserName,
		); err != nil {
			return 0, nil, err
		}
		rows = append(rows, row)
	}
	if err := sqlRows.Err(); err != nil {
		return 0, nil, err
	}
	return total, rows, nil
}

var (
	ansiEscapePattern              = regexp.MustCompile(`(?:\x1b)?\[[0-9;]*m`)
	singBoxConnectionPattern       = regexp.MustCompile(`(?:^|\s)(\[[+-]\d{4}\])?\s*([+-]\d{4})?\s*(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2})?\s*\S+\s+\[(\d+)\s+([^\]]+)\]\s+([^:]+):\s+(.*)$`)
	authInboundConnectionToPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+inbound connection to (.+)$`)
)

func parseSingBoxLogEvent(line string, connectionSources map[string]string) (parsedLogEvent, bool) {
	cleaned := stripANSI(line)
	match := singBoxConnectionPattern.FindStringSubmatch(cleaned)
	if match == nil {
		return parsedLogEvent{}, false
	}
	eventTime := parseSingBoxConnectionTime(match[2], match[3], match[5])
	connectionID := match[4]
	component := match[6]
	message := match[7]
	if strings.HasPrefix(component, "inbound/") && strings.HasPrefix(message, "inbound connection from ") {
		source := strings.TrimPrefix(message, "inbound connection from ")
		host, _, ok := splitHostPort(source)
		if ok {
			connectionSources[connectionID] = host
		}
		return parsedLogEvent{}, false
	}
	if strings.HasPrefix(component, "inbound/") && strings.Contains(message, "processed invalid connection") {
		source := ""
		if idx := strings.Index(message, "process connection from "); idx >= 0 {
			rest := strings.TrimPrefix(message[idx:], "process connection from ")
			if cut := strings.Index(rest, ": TLS handshake:"); cut >= 0 {
				host, _, ok := splitHostPort(rest[:cut])
				if ok {
					source = host
				}
			}
		}
		return parsedLogEvent{
			SourceIP:    source,
			Action:      "invalid_connection",
			WindowStart: eventTime.start,
			WindowEnd:   eventTime.end,
		}, true
	}
	if strings.HasPrefix(component, "inbound/") {
		authMatch := authInboundConnectionToPattern.FindStringSubmatch(message)
		if authMatch != nil {
			host, port, ok := splitHostPort(authMatch[2])
			if !ok {
				return parsedLogEvent{}, false
			}
			return parsedLogEvent{
				AuthName:    authMatch[1],
				SourceIP:    connectionSources[connectionID],
				TargetHost:  host,
				TargetPort:  int64(port),
				Action:      "connect",
				WindowStart: eventTime.start,
				WindowEnd:   eventTime.end,
			}, true
		}
	}
	if strings.HasPrefix(component, "outbound/") && strings.HasPrefix(message, "outbound connection to ") {
		host, port, ok := splitHostPort(strings.TrimPrefix(message, "outbound connection to "))
		if !ok {
			return parsedLogEvent{}, false
		}
		return parsedLogEvent{
			SourceIP:    connectionSources[connectionID],
			TargetHost:  host,
			TargetPort:  int64(port),
			Action:      "outbound_connect",
			WindowStart: eventTime.start,
			WindowEnd:   eventTime.end,
		}, true
	}
	return parsedLogEvent{}, false
}

type singBoxEventTime struct {
	start string
	end   string
}

func parseSingBoxConnectionTime(offset, timestamp, elapsedText string) singBoxEventTime {
	if offset == "" || timestamp == "" || elapsedText == "" {
		return singBoxEventTime{}
	}
	observedAt, err := time.Parse("-0700 2006-01-02 15:04:05", offset+" "+timestamp)
	if err != nil {
		return singBoxEventTime{}
	}
	elapsed, err := time.ParseDuration(elapsedText)
	if err != nil {
		return singBoxEventTime{}
	}
	end := observedAt.UTC()
	start := end.Add(-elapsed)
	return singBoxEventTime{
		start: start.Format(time.RFC3339Nano),
		end:   end.Format(time.RFC3339Nano),
	}
}

func stripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func splitHostPort(value string) (string, int, bool) {
	idx := strings.LastIndex(value, ":")
	if idx <= 0 || idx == len(value)-1 {
		return "", 0, false
	}
	port, err := strconv.Atoi(value[idx+1:])
	if err != nil || port < 0 || port > 65535 {
		return "", 0, false
	}
	host := strings.Trim(value[:idx], "[]")
	if host == "" {
		return "", 0, false
	}
	return host, port, true
}

func (db *DB) ListRecentLogEventsByNode(ctx context.Context, nodeName string, limit int64) ([]LogEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	return db.q.ListRecentLogEventsByNode(ctx, store.ListRecentLogEventsByNodeParams{
		NodeName: node.Name,
		Limit:    limit,
	})
}

func (db *DB) ListRecentLogEventsByUser(ctx context.Context, userName string, limit int64) ([]LogEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	return db.q.ListRecentLogEventsByUser(ctx, store.ListRecentLogEventsByUserParams{
		UserName: normalizeName(userName),
		Limit:    limit,
	})
}

func (db *DB) ListRecentRawLogEntriesByNode(ctx context.Context, nodeName string, limit int64) ([]RawLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	return db.q.ListRecentRawLogEntriesByNode(ctx, store.ListRecentRawLogEntriesByNodeParams{
		NodeName: node.Name,
		Limit:    limit,
	})
}
