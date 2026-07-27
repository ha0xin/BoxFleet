package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

// Admin-facing reads over connection_events, the sing-box 1.14 daemon stream.
//
// These are deliberately separate from the log_events reads rather than folded
// into them. The two producers coexist across a mixed-version fleet — most
// nodes run 1.13 and have no connection events at all — so a union view would
// have to invent nulls for half its columns on one side and byte totals it
// cannot know on the other. Keeping them apart is what lets the admin UI say
// which source an operator is looking at.
//
// Every read here goes through buildConnectionEventPredicates, exactly as every
// log_events read goes through buildLogEventPredicates: the ranked host list,
// its window totals and the bucketed series must never disagree about what is
// in scope.

// ConnectionEventFilter scopes an admin read. It is narrower than
// LogEventFilter on purpose: the stream carries no `action` (every row is a
// real connection, not a classified log line) and there is no full-text index
// over the enriched columns, so there is no search either. Host is the
// drill-down the host ranking hands back.
type ConnectionEventFilter struct {
	NodeName string
	UserName string
	Host     string
	Start    string
	End      string
	Limit    int64
	Offset   int64
}

// ConnectionSeriesFilter adds the bucketing controls. Bucket derivation, span
// limits and zero-fill all live in series_common.go, shared with the traffic
// and network-event series.
type ConnectionSeriesFilter struct {
	ConnectionEventFilter
	Bucket        Bucket
	OffsetMinutes int
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

// ConnectionEventDetail is one aggregated (dimensions, bucket) row. Byte totals
// are summed deltas of sing-box's uplinkTotal/downlinkTotal and are an estimate
// — see ConnectionCoverage for the loss modes — so nothing here may be
// presented as a ledger. Per-user billing stays on the V2Ray counters.
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

// ConnectionVolume is the measure tuple every connection aggregation carries.
// ConnectionsOpened and ConnectionsClosed are kept apart because a long-lived
// session contributes bytes to several consecutive buckets: summing
// "connections" over a range must use ConnectionsOpened or one session is
// counted once per bucket it spanned.
type ConnectionVolume struct {
	ConnectionsOpened int64
	ConnectionsClosed int64
	UplinkBytes       int64
	DownlinkBytes     int64
	DurationMsTotal   int64
}

// TotalBytes is the figure the admin UI ranks and charts on.
func (v ConnectionVolume) TotalBytes() int64 {
	return v.UplinkBytes + v.DownlinkBytes
}

type ConnectionPoint struct {
	BucketStart time.Time
	ConnectionVolume
}

// ConnectionCoverageTotals sums connection_reports over the window. It travels
// with every byte total this file returns, so a caller physically cannot render
// an estimate without the figure that says how complete it is.
type ConnectionCoverageTotals struct {
	ConnectionCoverage
	Reports int64
}

type ConnectionSeriesResult struct {
	Points   []ConnectionPoint
	Totals   ConnectionVolume
	Coverage ConnectionCoverageTotals
}

// ConnectionHostUsage is bytes per destination host — the one read log_events
// structurally cannot answer, since it has no byte columns and
// traffic_usage_deltas has no host column.
type ConnectionHostUsage struct {
	Host string
	ConnectionVolume
}

type ConnectionHostUsageResult struct {
	Hosts         []ConnectionHostUsage
	Totals        ConnectionVolume
	DistinctHosts int64
	Truncated     bool
	Coverage      ConnectionCoverageTotals
}

// ConnectionHostSort names the ranking dimension. It is mapped to a literal
// ORDER BY inside this package — an operator string never reaches the SQL.
// Both rankings are worth having: a host can dominate by bytes with a handful
// of long transfers, or by connections with a chatty polling client.
type ConnectionHostSort string

const (
	ConnectionHostSortBytes       ConnectionHostSort = "bytes"
	ConnectionHostSortConnections ConnectionHostSort = "connections"
)

func connectionHostOrderBy(sort ConnectionHostSort) (string, error) {
	switch sort {
	case "", ConnectionHostSortBytes:
		return "SUM(e.uplink_bytes + e.downlink_bytes) DESC, e.target_host ASC", nil
	case ConnectionHostSortConnections:
		return "SUM(e.connections_opened) DESC, e.target_host ASC", nil
	default:
		return "", fmt.Errorf("unsupported connection host sort %q", sort)
	}
}

const (
	connectionEventsDefaultLimit = int64(50)
	connectionEventsMaxLimit     = int64(500)
	connectionHostsDefaultLimit  = int64(20)
	connectionHostsMaxLimit      = int64(100)
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

// ConnectionHostUsage ranks destination hosts by estimated bytes and returns the
// unclipped window totals alongside, so a share column has a denominator that
// does not change with the requested row count.
func (db *DB) ConnectionHostUsage(ctx context.Context, filter ConnectionEventFilter, sort ConnectionHostSort, limit int64) (ConnectionHostUsageResult, error) {
	orderBy, err := connectionHostOrderBy(sort)
	if err != nil {
		return ConnectionHostUsageResult{}, err
	}
	scope, err := db.resolveConnectionEventScope(ctx, filter)
	if err != nil {
		return ConnectionHostUsageResult{}, err
	}
	if limit <= 0 {
		limit = connectionHostsDefaultLimit
	}
	if limit > connectionHostsMaxLimit {
		limit = connectionHostsMaxLimit
	}
	where, args := buildConnectionEventPredicates(scope)
	whereSQL := strings.Join(where, " AND ")

	result := ConnectionHostUsageResult{Hosts: make([]ConnectionHostUsage, 0, limit)}
	totalsQuery := `
SELECT
  COALESCE(SUM(e.connections_opened), 0),
  COALESCE(SUM(e.connections_closed), 0),
  COALESCE(SUM(e.uplink_bytes), 0),
  COALESCE(SUM(e.downlink_bytes), 0),
  COALESCE(SUM(e.duration_ms_total), 0),
  COUNT(DISTINCT e.target_host)
FROM connection_events e
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, totalsQuery, args...).Scan(
		&result.Totals.ConnectionsOpened,
		&result.Totals.ConnectionsClosed,
		&result.Totals.UplinkBytes,
		&result.Totals.DownlinkBytes,
		&result.Totals.DurationMsTotal,
		&result.DistinctHosts,
	); err != nil {
		return ConnectionHostUsageResult{}, err
	}

	// One row over the limit distinguishes "this is the whole ranking" from
	// "there is more below the fold", which is the difference between an
	// honest breakdown and a misleading one.
	hostArgs := append(append([]any{}, args...), limit+1)
	hostQuery := `
SELECT
  e.target_host,
  SUM(e.connections_opened),
  SUM(e.connections_closed),
  SUM(e.uplink_bytes),
  SUM(e.downlink_bytes),
  SUM(e.duration_ms_total)
FROM connection_events e
WHERE ` + whereSQL + `
GROUP BY e.target_host
ORDER BY ` + orderBy + `
LIMIT ?`
	rows, err := db.sql.QueryContext(ctx, hostQuery, hostArgs...)
	if err != nil {
		return ConnectionHostUsageResult{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var row ConnectionHostUsage
		if err := rows.Scan(
			&row.Host,
			&row.ConnectionsOpened,
			&row.ConnectionsClosed,
			&row.UplinkBytes,
			&row.DownlinkBytes,
			&row.DurationMsTotal,
		); err != nil {
			return ConnectionHostUsageResult{}, err
		}
		result.Hosts = append(result.Hosts, row)
	}
	if err := rows.Err(); err != nil {
		return ConnectionHostUsageResult{}, err
	}
	if int64(len(result.Hosts)) > limit {
		result.Hosts = result.Hosts[:limit]
		result.Truncated = true
	}

	coverage, err := db.connectionCoverage(ctx, scope)
	if err != nil {
		return ConnectionHostUsageResult{}, err
	}
	result.Coverage = coverage
	return result, nil
}

// ConnectionSeries buckets the stream over the filtered window. Like every
// other series in this package it is fully zero-filled server-side: the client
// renders the points it is handed and never buckets.
func (db *DB) ConnectionSeries(ctx context.Context, filter ConnectionSeriesFilter) (ConnectionSeriesResult, error) {
	bucket := filter.Bucket
	if bucket == "" {
		bucket = BucketHour
	}
	if err := ValidateBucketOffsetMinutes(filter.OffsetMinutes); err != nil {
		return ConnectionSeriesResult{}, err
	}
	scope, err := db.resolveConnectionEventScope(ctx, filter.ConnectionEventFilter)
	if err != nil {
		return ConnectionSeriesResult{}, err
	}
	start, end, err := parseConnectionEventWindow(scope)
	if err != nil {
		return ConnectionSeriesResult{}, err
	}
	where, args := buildConnectionEventPredicates(scope)
	query := `
SELECT
  ` + bucketExpr("e.bucket_start", bucket, filter.OffsetMinutes) + ` AS bucket_key,
  SUM(e.connections_opened),
  SUM(e.connections_closed),
  SUM(e.uplink_bytes),
  SUM(e.downlink_bytes),
  SUM(e.duration_ms_total)
FROM connection_events e
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY bucket_key`
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return ConnectionSeriesResult{}, err
	}
	defer rows.Close()
	present := make(map[string]ConnectionPoint)
	var totals ConnectionVolume
	for rows.Next() {
		var key string
		var point ConnectionPoint
		if err := rows.Scan(
			&key,
			&point.ConnectionsOpened,
			&point.ConnectionsClosed,
			&point.UplinkBytes,
			&point.DownlinkBytes,
			&point.DurationMsTotal,
		); err != nil {
			return ConnectionSeriesResult{}, err
		}
		bucketStart, err := ParseBucketKey(key)
		if err != nil {
			return ConnectionSeriesResult{}, err
		}
		point.BucketStart = bucketStart
		present[key] = point
		totals.ConnectionsOpened += point.ConnectionsOpened
		totals.ConnectionsClosed += point.ConnectionsClosed
		totals.UplinkBytes += point.UplinkBytes
		totals.DownlinkBytes += point.DownlinkBytes
		totals.DurationMsTotal += point.DurationMsTotal
	}
	if err := rows.Err(); err != nil {
		return ConnectionSeriesResult{}, err
	}
	coverage, err := db.connectionCoverage(ctx, scope)
	if err != nil {
		return ConnectionSeriesResult{}, err
	}
	return ConnectionSeriesResult{
		Points: ZeroFillSeries(start, end, bucket, filter.OffsetMinutes, present, func(bucketStart time.Time) ConnectionPoint {
			return ConnectionPoint{BucketStart: bucketStart}
		}),
		Totals:   totals,
		Coverage: coverage,
	}, nil
}

// connectionCoverage sums the collector's own loss telemetry over the window.
// connection_reports has no user column — coverage is a property of the node's
// stream, not of one credential — so a user filter narrows the byte rankings
// but deliberately leaves the coverage figure fleet- or node-wide.
func (db *DB) connectionCoverage(ctx context.Context, scope connectionEventScope) (ConnectionCoverageTotals, error) {
	row, err := db.q.SumConnectionCoverage(ctx, store.SumConnectionCoverageParams{
		// These are interface{} because of the `sqlc.arg(x) = '' OR …` idiom.
		// They must be empty strings, never nil: `NULL = ''` is NULL, so a nil
		// would make every predicate fail and return an empty window.
		NodeID:    scope.NodeID,
		StartTime: scope.StartTime,
		EndTime:   scope.EndTime,
	})
	if err != nil {
		return ConnectionCoverageTotals{}, err
	}
	return ConnectionCoverageTotals{
		ConnectionCoverage: ConnectionCoverage{
			ConnectionsObserved:     row.ConnectionsObserved,
			ConnectionsAttributed:   row.ConnectionsAttributed,
			ConnectionsUnattributed: row.ConnectionsUnattributed,
			ConnectionsOrphaned:     row.ConnectionsOrphaned,
			StreamResets:            row.StreamResets,
			DroppedBuckets:          row.DroppedBuckets,
			BytesObserved:           row.BytesObserved,
			BytesAttributed:         row.BytesAttributed,
		},
		Reports: row.Reports,
	}, nil
}

// resolveConnectionEventScope translates names to IDs and normalises the
// window. An unknown node or user is an error rather than a silently empty
// result, matching resolveLogEventScope.
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

// parseConnectionEventWindow reads the scope's window back as instants. A
// series without both ends cannot be zero-filled, so an open window is an error
// rather than a ragged chart.
func parseConnectionEventWindow(scope connectionEventScope) (time.Time, time.Time, error) {
	if scope.StartTime == "" || scope.EndTime == "" {
		return time.Time{}, time.Time{}, errors.New("start and end are required for a bucketed series")
	}
	start, err := time.Parse(model.ConnectionInstantLayout, scope.StartTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start must be RFC3339 time: %w", err)
	}
	end, err := time.Parse(model.ConnectionInstantLayout, scope.EndTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end must be RFC3339 time: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, errors.New("end must not be before start")
	}
	return start.UTC(), end.UTC(), nil
}
