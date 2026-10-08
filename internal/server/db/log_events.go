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
	"unicode"
	"unicode/utf8"

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
	ID                string
	EventTime         string
	NodeID            string
	NodeName          string
	ProxyUserID       sql.NullString
	UserName          string
	AuthName          string
	SourceIp          string
	TargetHost        string
	TargetPort        int64
	Action            string
	RawMessage        string
	Count             int64
	AggregateKey      string
	WindowStart       string
	WindowEnd         string
	CreatedAt         string
	Source            string
	ConnectionID      string
	StartedAt         *string
	Domain            string
	Network           string
	IPVersion         *int64
	Protocol          string
	Inbound           string
	InboundType       string
	Rule              string
	Outbound          string
	OutboundType      string
	Chain             string
	UplinkBytes       *int64
	DownlinkBytes     *int64
	DurationMs        *int64
	ConnectionsClosed *int64
}

// logEventScope is LogEventFilter with node and user names already resolved to
// IDs. Every unified read — the paged table, the bucketed series, the
// service breakdown — filters through this one shape so they cannot drift; a
// chart that filters differently from the table beneath it reads as a data bug.
type logEventScope struct {
	NodeID    string
	UserID    string
	Action    string
	Search    string
	StartTime string
	EndTime   string
}

type logEventsPageParams struct {
	logEventScope
	Limit  int64
	Offset int64
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

const (
	// Nodes are not trusted to bound their own reports: the agent batches at
	// most journalBatchMaxEntries lines, so anything beyond this is dropped.
	maxLogEventsPerReport = 500
	// One journal line collapses into one event, so a count this large can only
	// come from a broken or hostile node.
	maxLogEventCount = 10000
)

func (db *DB) RecordLogEvents(ctx context.Context, report LogEventReport) error {
	node, err := db.GetNode(ctx, report.NodeName)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	events := report.Events
	if len(events) > maxLogEventsPerReport {
		events = events[:maxLogEventsPerReport]
	}
	connectionSources := make(map[string]string)
	for _, event := range events {
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
			if event.TargetPort <= 0 {
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
		if event.AuthName == "" || event.TargetHost == "" {
			continue
		}
		if event.TargetPort <= 0 || event.TargetPort > 65535 {
			continue
		}
		count := event.Count
		if count <= 0 {
			count = 1
		}
		if count > maxLogEventCount {
			count = maxLogEventCount
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
	truncated := message[:maxRawSampleBytes]
	for len(truncated) > 0 {
		if r, size := utf8.DecodeLastRuneInString(truncated); r != utf8.RuneError || size > 1 {
			break
		}
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

func rawLogMessageHash(cursor, message string) string {
	sum := sha256.Sum256([]byte(cursor + "\x00" + message))
	return hex.EncodeToString(sum[:])
}

func (db *DB) ListRecentLogEvents(ctx context.Context, limit int64) ([]LogEvent, error) {
	return db.q.ListRecentLogEvents(ctx, limit)
}

// resolveLogEventScope translates filter names to IDs, surfacing an unknown
// node or user as an error rather than as a silently empty result.
func (db *DB) resolveLogEventScope(ctx context.Context, filter LogEventFilter) (logEventScope, error) {
	scope := logEventScope{
		Action:    strings.TrimSpace(filter.Action),
		Search:    strings.TrimSpace(filter.Search),
		StartTime: strings.TrimSpace(filter.Start),
		EndTime:   strings.TrimSpace(filter.End),
	}
	if strings.TrimSpace(filter.NodeName) != "" {
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return logEventScope{}, err
		}
		scope.NodeID = node.ID
	}
	if strings.TrimSpace(filter.UserName) != "" {
		user, err := db.GetProxyUser(ctx, filter.UserName)
		if err != nil {
			return logEventScope{}, err
		}
		scope.UserID = user.ID
	}
	return scope, nil
}

// buildLogEventPredicates renders the shared FROM/WHERE fragments for a scope.
// The returned args are ordered to match the emitted clauses, so a caller
// appends its own trailing arguments after these.
func buildLogEventPredicates(scope logEventScope) (searchJoin string, where []string, args []any) {
	where = []string{"1 = 1"}
	args = make([]any, 0, 4)
	if scope.NodeID != "" {
		where = append(where, "e.node_id = ?")
		args = append(args, scope.NodeID)
	}
	if scope.UserID != "" {
		where = append(where, "e.proxy_user_id = ?")
		args = append(args, scope.UserID)
	}
	if scope.Action != "" {
		where = append(where, "e.action = ? COLLATE NOCASE")
		args = append(args, scope.Action)
	}
	if scope.Search != "" {
		tokens := strings.FieldsFunc(strings.TrimSpace(scope.Search), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
		stream := []string{}
		searchArgs := []any{networkEventSearchQuery(scope.Search)}
		for _, token := range tokens {
			stream = append(stream, `(instr(lower(e.auth_name || ' ' || e.source_ip || ' ' || e.target_host || ' ' || e.target_port || ' ' || e.action || ' ' || COALESCE(e.network,'') || ' ' || COALESCE(e.protocol,'') || ' ' || COALESCE(e.domain,'') || ' ' || COALESCE(e.inbound,'') || ' ' || COALESCE(e.rule,'') || ' ' || COALESCE(e.outbound,'') || ' ' || COALESCE(e.chain,'')), lower(?)) > 0 OR e.node_id IN (SELECT id FROM nodes WHERE instr(lower(name), lower(?)) > 0) OR e.proxy_user_id IN (SELECT id FROM proxy_users WHERE instr(lower(name), lower(?)) > 0))`)
			searchArgs = append(searchArgs, token, token, token)
		}
		if len(stream) == 0 {
			stream = append(stream, "0")
		}
		where = append(where, `(e.id IN (SELECT search_document.event_id FROM log_event_search_documents search_document JOIN log_events_search ON log_events_search.docid = search_document.id WHERE log_events_search MATCH ?) OR (e.source = 'stream' AND `+strings.Join(stream, " AND ")+`))`)
		args = append(args, searchArgs...)
	}
	if scope.StartTime != "" {
		// Keep an indexed whole-second bound, then compare instants so journal
		// RFC3339Nano timestamps and stream millisecond timestamps agree.
		where = append(where, "e.event_time >= strftime('%Y-%m-%dT%H:%M:%S', ?) AND julianday(e.event_time) >= julianday(?)")
		args = append(args, scope.StartTime, scope.StartTime)
	}
	if scope.EndTime != "" {
		where = append(where, "e.event_time < strftime('%Y-%m-%dT%H:%M:%S', ?, '+1 second') AND julianday(e.event_time) < julianday(?)")
		args = append(args, scope.EndTime, scope.EndTime)
	}
	return searchJoin, where, args
}

func (db *DB) ListLogEventsPage(ctx context.Context, filter LogEventFilter) (LogEventPage, error) {
	scope, err := db.resolveLogEventScope(ctx, filter)
	if err != nil {
		return LogEventPage{}, err
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
		logEventScope: scope,
		Offset:        offset,
		Limit:         limit,
	}
	total, rows, err := db.queryLogEventsPage(ctx, params)
	if err != nil {
		return LogEventPage{}, err
	}
	events := rows
	return LogEventPage{
		Events: events,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (db *DB) queryLogEventsPage(ctx context.Context, params logEventsPageParams) (int64, []LogEventDetail, error) {
	searchJoin, where, args := buildLogEventPredicates(params.logEventScope)
	whereSQL := strings.Join(where, " AND ")
	countQuery := `
SELECT COUNT(*)
FROM network_event_records e` + searchJoin + `
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
  COALESCE(u.name, '') AS user_name,
 e.event_time, e.source, COALESCE(e.connection_id,''), e.started_at, COALESCE(e.domain,''), COALESCE(e.network,''), e.ip_version, COALESCE(e.protocol,''), COALESCE(e.inbound,''), COALESCE(e.inbound_type,''), COALESCE(e.rule,''), COALESCE(e.outbound,''), COALESCE(e.outbound_type,''), COALESCE(e.chain,''), e.uplink_bytes, e.downlink_bytes, e.duration_ms_total, e.connections_closed
FROM network_event_records e
` + searchJoin + `
JOIN nodes n ON n.id = e.node_id
LEFT JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE ` + whereSQL + `
-- Event time is both the operator-facing chronology and the leading range
-- column of the filter indexes. Ordering by ingestion time here forced SQLite
-- to materialize and sort every matching event before returning one page.
ORDER BY e.event_time DESC, e.id DESC
LIMIT ?
OFFSET ?`
	sqlRows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return 0, nil, err
	}
	defer sqlRows.Close()
	rows := make([]LogEventDetail, 0)
	for sqlRows.Next() {
		var row LogEventDetail
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
			&row.EventTime, &row.Source, &row.ConnectionID, &row.StartedAt, &row.Domain, &row.Network, &row.IPVersion, &row.Protocol, &row.Inbound, &row.InboundType, &row.Rule, &row.Outbound, &row.OutboundType, &row.Chain, &row.UplinkBytes, &row.DownlinkBytes, &row.DurationMs, &row.ConnectionsClosed,
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

func networkEventSearchQuery(value string) string {
	tokens := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(tokens) == 0 {
		return `"__boxfleet_no_search_tokens__"`
	}
	for i := range tokens {
		tokens[i] += "*"
	}
	return strings.Join(tokens, " ")
}

var (
	ansiEscapePattern              = regexp.MustCompile(`\x1b\[[0-9;]*m`)
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
