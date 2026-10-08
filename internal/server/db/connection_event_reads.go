package db

import (
	"context"
	"errors"
	"strings"

	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

// ConnectionEventFilter scopes diagnostic source rows. Unified connection-start
// analytics use LogEventFilter; the old interval-byte aggregate APIs are retired.
type ConnectionEventFilter struct {
	NodeName string
	UserName string
	Host     string
	Start    string
	End      string
	Limit    int64
	Offset   int64
}

// connectionEventScope is ConnectionEventFilter with names resolved to IDs and
// both window bounds re-rendered at ConnectionInstantLayout. The re-render is
// load-bearing: bucket_start is stored fixed-width with milliseconds, and the
// columns are compared as TEXT, so an RFC3339 bound of "…T00:00:00Z" would sort
// after "…T00:00:00.000Z" ('Z' > '.') and silently drop the first bucket of
// every window.
type connectionEventScope struct {
	NodeID    string
	UserID    string
	Host      string
	StartTime string
	EndTime   string
}

// ConnectionEventDetail is a diagnostic source row. Legacy rows contain summed
// deltas; session rows contain lifetime byte totals. Neither is a billing ledger
// or a reliable measure of bytes transferred inside a selected query window.
type ConnectionEventDetail struct {
	NodeName          string
	UserName          string
	AuthName          string
	SourceIP          string
	TargetHost        string
	TargetPort        int64
	Domain            string
	Network           string
	IPVersion         int64
	Protocol          string
	Inbound           string
	InboundType       string
	Rule              string
	Outbound          string
	OutboundType      string
	Chain             string
	ConnectionsOpened int64
	ConnectionsClosed int64
	UplinkBytes       int64
	DownlinkBytes     int64
	DurationMsTotal   int64
	BucketStart       string
	WindowStart       string
	WindowEnd         string
}

type ConnectionEventPage struct {
	Events []ConnectionEventDetail
	Total  int64
	Limit  int64
	Offset int64
}

const (
	connectionEventsDefaultLimit = int64(50)
	connectionEventsMaxLimit     = int64(500)
)

// ListConnectionEventsPage returns one page of aggregated connection rows,
// newest bucket first. The sqlc row type is reused as the scan target while the
// SQL is written here, matching queryLogEventsPage: the predicates are dynamic,
// so a static generated query cannot express them.
func (db *DB) ListConnectionEventsPage(ctx context.Context, filter ConnectionEventFilter) (ConnectionEventPage, error) {
	scope, err := db.resolveConnectionEventScope(ctx, filter)
	if err != nil {
		return ConnectionEventPage{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = connectionEventsDefaultLimit
	}
	if limit > connectionEventsMaxLimit {
		limit = connectionEventsMaxLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := buildConnectionEventPredicates(scope)
	whereSQL := strings.Join(where, " AND ")
	var total int64
	countQuery := `
SELECT COUNT(*)
FROM connection_events e
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ConnectionEventPage{}, err
	}

	listArgs := append(append([]any{}, args...), limit, offset)
	listQuery := `
SELECT
  e.auth_name,
  e.source_ip,
  e.target_host,
  e.target_port,
  e.domain,
  e.network,
  e.ip_version,
  e.protocol,
  e.inbound,
  e.inbound_type,
  e.rule,
  e.outbound,
  e.outbound_type,
  e.chain,
  e.connections_opened,
  e.connections_closed,
  e.uplink_bytes,
  e.downlink_bytes,
  e.duration_ms_total,
  e.bucket_start,
  e.window_start,
  e.window_end,
  n.name AS node_name,
  COALESCE(u.name, '') AS user_name
FROM connection_events e
JOIN nodes n ON n.id = e.node_id
LEFT JOIN proxy_users u ON u.id = e.proxy_user_id
WHERE ` + whereSQL + `
-- bucket_start leads idx_connection_events_node_bucket, so this page is a
-- bounded index walk rather than a full sort of the window.
ORDER BY e.bucket_start DESC, e.id DESC
LIMIT ?
OFFSET ?`
	rows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return ConnectionEventPage{}, err
	}
	defer rows.Close()
	events := make([]ConnectionEventDetail, 0, limit)
	for rows.Next() {
		var row store.ListConnectionEventsPageRow
		if err := rows.Scan(
			&row.AuthName,
			&row.SourceIp,
			&row.TargetHost,
			&row.TargetPort,
			&row.Domain,
			&row.Network,
			&row.IpVersion,
			&row.Protocol,
			&row.Inbound,
			&row.InboundType,
			&row.Rule,
			&row.Outbound,
			&row.OutboundType,
			&row.Chain,
			&row.ConnectionsOpened,
			&row.ConnectionsClosed,
			&row.UplinkBytes,
			&row.DownlinkBytes,
			&row.DurationMsTotal,
			&row.BucketStart,
			&row.WindowStart,
			&row.WindowEnd,
			&row.NodeName,
			&row.UserName,
		); err != nil {
			return ConnectionEventPage{}, err
		}
		events = append(events, ConnectionEventDetail{
			NodeName:          row.NodeName,
			UserName:          row.UserName,
			AuthName:          row.AuthName,
			SourceIP:          row.SourceIp,
			TargetHost:        row.TargetHost,
			TargetPort:        row.TargetPort,
			Domain:            row.Domain,
			Network:           row.Network,
			IPVersion:         row.IpVersion,
			Protocol:          row.Protocol,
			Inbound:           row.Inbound,
			InboundType:       row.InboundType,
			Rule:              row.Rule,
			Outbound:          row.Outbound,
			OutboundType:      row.OutboundType,
			Chain:             row.Chain,
			ConnectionsOpened: row.ConnectionsOpened,
			ConnectionsClosed: row.ConnectionsClosed,
			UplinkBytes:       row.UplinkBytes,
			DownlinkBytes:     row.DownlinkBytes,
			DurationMsTotal:   row.DurationMsTotal,
			BucketStart:       row.BucketStart,
			WindowStart:       row.WindowStart,
			WindowEnd:         row.WindowEnd,
		})
	}
	if err := rows.Err(); err != nil {
		return ConnectionEventPage{}, err
	}
	return ConnectionEventPage{Events: events, Total: total, Limit: limit, Offset: offset}, nil
}

func (db *DB) resolveConnectionEventScope(ctx context.Context, filter ConnectionEventFilter) (connectionEventScope, error) {
	scope := connectionEventScope{
		Host:      model.NormalizeConnectionHost(filter.Host),
		StartTime: normalizeConnectionWindowBound(filter.Start),
		EndTime:   normalizeConnectionWindowBound(filter.End),
	}
	if strings.TrimSpace(filter.Start) != "" && scope.StartTime == "" {
		return connectionEventScope{}, errors.New("start must be RFC3339 time")
	}
	if strings.TrimSpace(filter.End) != "" && scope.EndTime == "" {
		return connectionEventScope{}, errors.New("end must be RFC3339 time")
	}
	if strings.TrimSpace(filter.NodeName) != "" {
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return connectionEventScope{}, err
		}
		scope.NodeID = node.ID
	}
	if strings.TrimSpace(filter.UserName) != "" {
		user, err := db.GetProxyUser(ctx, filter.UserName)
		if err != nil {
			return connectionEventScope{}, err
		}
		scope.UserID = user.ID
	}
	return scope, nil
}

func normalizeConnectionWindowBound(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return model.NormalizeConnectionInstant(value)
}

// buildConnectionEventPredicates renders the shared WHERE fragment. The seed
// clause is a tautology rather than log_events' "proxy_user_id IS NOT NULL":
// unattributed rows are stored on purpose here, because dropping them would
// silently understate every bytes-per-host total.
func buildConnectionEventPredicates(scope connectionEventScope) (where []string, args []any) {
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
	if scope.Host != "" {
		// target_host is normalised on write, so no lower() is needed here and
		// the index on it stays usable.
		where = append(where, "e.target_host = ?")
		args = append(args, scope.Host)
	}
	if scope.StartTime != "" {
		where = append(where, "e.bucket_start >= ?")
		args = append(args, scope.StartTime)
	}
	if scope.EndTime != "" {
		where = append(where, "e.bucket_start <= ?")
		args = append(args, scope.EndTime)
	}
	return where, args
}
