package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NetworkEventSeriesGroup is the dimension a bucketed event series splits on.
// There is deliberately no proxy grouping: log_events carries no proxy_id, so
// only traffic can be attributed to a proxy.
type NetworkEventSeriesGroup string

const (
	NetworkEventSeriesGroupTotal  NetworkEventSeriesGroup = "total"
	NetworkEventSeriesGroupAction NetworkEventSeriesGroup = "action"
	NetworkEventSeriesGroupNode   NetworkEventSeriesGroup = "node"
	NetworkEventSeriesGroupUser   NetworkEventSeriesGroup = "user"
)

const (
	networkEventSeriesDefaultLimit = 25
	networkEventSeriesMaxLimit     = 100
	networkEventSeriesTotalKey     = "total"
	networkEventSeriesTotalLabel   = "All events"
)

// NetworkEventSeriesFilter scopes a bucketed count series. The embedded
// LogEventFilter is the same shape the paged table uses, so the chart and the
// rows beneath it can never disagree about what is being counted.
type NetworkEventSeriesFilter struct {
	LogEventFilter
	Bucket        Bucket
	OffsetMinutes int
	Group         NetworkEventSeriesGroup
	Limit         int64
}

// NetworkEventPoint is one bucket. Counts are connections, never bytes:
// log_events has no byte columns and traffic_usage_deltas has no host column,
// so bytes cannot be attributed to a destination at all.
type NetworkEventPoint struct {
	BucketStart time.Time
	Count       int64
}

type NetworkEventSeries struct {
	Key    string
	Label  string
	Points []NetworkEventPoint
	Total  int64
}

// ActionCount is one row of the unbucketed action histogram. In practice this
// is a single "connect" row: parseSingBoxLogEvent also emits invalid_connection
// and outbound_connect, but neither carries an auth name, so RecordLogEvents
// drops both before they reach the table.
type ActionCount struct {
	Action string
	Count  int64
}

type NetworkEventSeriesResult struct {
	Series    []NetworkEventSeries
	Actions   []ActionCount
	Truncated bool
}

// NetworkEventSeries buckets connection counts over the filtered window. The
// server owns bucketing and zero-fill end to end, so every bucket in the
// window is present in the result whether or not it had rows.
//
// Unlike the traffic series, this read does not exclude soft-deleted users: the
// paged event table joins proxy_users without a deleted_at filter, and a chart
// that counts fewer events than the rows below it reads as a data bug. The
// asymmetry is intentional — each aggregation matches the pipeline it extends.
func (db *DB) NetworkEventSeries(ctx context.Context, filter NetworkEventSeriesFilter) (NetworkEventSeriesResult, error) {
	group := filter.Group
	if group == "" {
		group = NetworkEventSeriesGroupTotal
	}
	bucket := filter.Bucket
	if bucket == "" {
		bucket = BucketHour
	}
	if err := ValidateBucketOffsetMinutes(filter.OffsetMinutes); err != nil {
		return NetworkEventSeriesResult{}, err
	}
	scope, err := db.resolveLogEventScope(ctx, filter.LogEventFilter)
	if err != nil {
		return NetworkEventSeriesResult{}, err
	}
	start, end, err := parseLogEventWindow(scope)
	if err != nil {
		return NetworkEventSeriesResult{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = networkEventSeriesDefaultLimit
	}
	if limit > networkEventSeriesMaxLimit {
		limit = networkEventSeriesMaxLimit
	}

	actions, err := db.networkEventActionCounts(ctx, scope)
	if err != nil {
		return NetworkEventSeriesResult{}, err
	}
	if group == NetworkEventSeriesGroupTotal {
		series, err := db.networkEventTotalSeries(ctx, scope, bucket, filter.OffsetMinutes, start, end)
		if err != nil {
			return NetworkEventSeriesResult{}, err
		}
		return NetworkEventSeriesResult{Series: []NetworkEventSeries{series}, Actions: actions}, nil
	}
	series, truncated, err := db.networkEventGroupedSeries(ctx, scope, group, bucket, filter.OffsetMinutes, start, end, limit)
	if err != nil {
		return NetworkEventSeriesResult{}, err
	}
	return NetworkEventSeriesResult{Series: series, Actions: actions, Truncated: truncated}, nil
}

// parseLogEventWindow reads the scope's window back out as instants. A series
// without both ends cannot be zero-filled, so an open window is an error here
// rather than a silently ragged chart.
func parseLogEventWindow(scope logEventScope) (time.Time, time.Time, error) {
	if scope.StartTime == "" || scope.EndTime == "" {
		return time.Time{}, time.Time{}, errors.New("start and end are required for a bucketed series")
	}
	start, err := time.Parse(time.RFC3339Nano, scope.StartTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start must be RFC3339 time: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, scope.EndTime)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end must be RFC3339 time: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, errors.New("end must not be before start")
	}
	return start.UTC(), end.UTC(), nil
}

func (db *DB) networkEventTotalSeries(
	ctx context.Context,
	scope logEventScope,
	bucket Bucket,
	offsetMinutes int,
	start, end time.Time,
) (NetworkEventSeries, error) {
	searchJoin, where, args := buildLogEventPredicates(scope)
	// Bucketing on window_start, never created_at: the aggregate upsert bumps
	// created_at on every merge, so it is last-touched rather than first-seen.
	query := `
SELECT ` + bucketExpr("e.window_start", bucket, offsetMinutes) + ` AS bucket_key, SUM(e.count) AS connections
FROM log_events e` + searchJoin + `
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY bucket_key`
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return NetworkEventSeries{}, err
	}
	defer rows.Close()
	present := make(map[string]NetworkEventPoint)
	var total int64
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return NetworkEventSeries{}, err
		}
		bucketStart, err := ParseBucketKey(key)
		if err != nil {
			return NetworkEventSeries{}, err
		}
		present[key] = NetworkEventPoint{BucketStart: bucketStart, Count: count}
		total += count
	}
	if err := rows.Err(); err != nil {
		return NetworkEventSeries{}, err
	}
	return NetworkEventSeries{
		Key:    networkEventSeriesTotalKey,
		Label:  networkEventSeriesTotalLabel,
		Points: zeroFillNetworkEventPoints(start, end, bucket, offsetMinutes, present),
		Total:  total,
	}, nil
}

func (db *DB) networkEventGroupedSeries(
	ctx context.Context,
	scope logEventScope,
	group NetworkEventSeriesGroup,
	bucket Bucket,
	offsetMinutes int,
	start, end time.Time,
	limit int64,
) ([]NetworkEventSeries, bool, error) {
	keyExpr, groupJoin, err := networkEventSeriesKeyExpr(group)
	if err != nil {
		return nil, false, err
	}
	searchJoin, where, args := buildLogEventPredicates(scope)
	whereSQL := strings.Join(where, " AND ")
	from := `
FROM log_events e` + searchJoin + groupJoin

	// Rank the series first so the bucket scan is bounded by the keys that will
	// actually be rendered, rather than by however many users a scope covers.
	topQuery := `
SELECT ` + keyExpr + ` AS series_key, SUM(e.count) AS connections` + from + `
WHERE ` + whereSQL + `
GROUP BY series_key
ORDER BY connections DESC, series_key ASC
LIMIT ?`
	topArgs := append(append([]any{}, args...), limit+1)
	topRows, err := db.sql.QueryContext(ctx, topQuery, topArgs...)
	if err != nil {
		return nil, false, err
	}
	defer topRows.Close()
	keys := make([]string, 0, limit)
	totals := make(map[string]int64)
	for topRows.Next() {
		var key string
		var count int64
		if err := topRows.Scan(&key, &count); err != nil {
			return nil, false, err
		}
		keys = append(keys, key)
		totals[key] = count
	}
	if err := topRows.Err(); err != nil {
		return nil, false, err
	}
	truncated := int64(len(keys)) > limit
	if truncated {
		keys = keys[:limit]
	}
	if len(keys) == 0 {
		return []NetworkEventSeries{}, false, nil
	}

	seriesArgs := append(append([]any{}, args...), logEventKeyArgs(keys)...)
	seriesQuery := `
SELECT ` + keyExpr + ` AS series_key, ` + bucketExpr("e.window_start", bucket, offsetMinutes) + ` AS bucket_key, SUM(e.count) AS connections` + from + `
WHERE ` + whereSQL + ` AND ` + keyExpr + ` IN (` + logEventKeyPlaceholders(len(keys)) + `)
GROUP BY series_key, bucket_key`
	seriesRows, err := db.sql.QueryContext(ctx, seriesQuery, seriesArgs...)
	if err != nil {
		return nil, false, err
	}
	defer seriesRows.Close()
	present := make(map[string]map[string]NetworkEventPoint, len(keys))
	for seriesRows.Next() {
		var seriesKey, bucketKey string
		var count int64
		if err := seriesRows.Scan(&seriesKey, &bucketKey, &count); err != nil {
			return nil, false, err
		}
		bucketStart, err := ParseBucketKey(bucketKey)
		if err != nil {
			return nil, false, err
		}
		buckets, ok := present[seriesKey]
		if !ok {
			buckets = make(map[string]NetworkEventPoint)
			present[seriesKey] = buckets
		}
		buckets[bucketKey] = NetworkEventPoint{BucketStart: bucketStart, Count: count}
	}
	if err := seriesRows.Err(); err != nil {
		return nil, false, err
	}

	series := make([]NetworkEventSeries, 0, len(keys))
	for _, key := range keys {
		series = append(series, NetworkEventSeries{
			Key:    key,
			Label:  key,
			Points: zeroFillNetworkEventPoints(start, end, bucket, offsetMinutes, present[key]),
			Total:  totals[key],
		})
	}
	return series, truncated, nil
}

// networkEventSeriesKeyExpr maps a grouping onto the column that both keys and
// labels the series. Names rather than IDs, so a series key is also a value the
// caller can hand straight back as a filter.
func networkEventSeriesKeyExpr(group NetworkEventSeriesGroup) (keyExpr string, join string, err error) {
	switch group {
	case NetworkEventSeriesGroupAction:
		return "e.action", "", nil
	case NetworkEventSeriesGroupNode:
		return "n.name", "\nJOIN nodes n ON n.id = e.node_id", nil
	case NetworkEventSeriesGroupUser:
		// No deleted_at filter, matching the paged event table.
		return "u.name", "\nJOIN proxy_users u ON u.id = e.proxy_user_id", nil
	default:
		return "", "", fmt.Errorf("unsupported network event series group %q", group)
	}
}

func (db *DB) networkEventActionCounts(ctx context.Context, scope logEventScope) ([]ActionCount, error) {
	searchJoin, where, args := buildLogEventPredicates(scope)
	query := `
SELECT e.action, SUM(e.count) AS connections
FROM log_events e` + searchJoin + `
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY e.action
ORDER BY connections DESC, e.action ASC`
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make([]ActionCount, 0, 4)
	for rows.Next() {
		var row ActionCount
		if err := rows.Scan(&row.Action, &row.Count); err != nil {
			return nil, err
		}
		counts = append(counts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func zeroFillNetworkEventPoints(
	start, end time.Time,
	bucket Bucket,
	offsetMinutes int,
	present map[string]NetworkEventPoint,
) []NetworkEventPoint {
	return ZeroFillSeries(start, end, bucket, offsetMinutes, present, func(bucketStart time.Time) NetworkEventPoint {
		return NetworkEventPoint{BucketStart: bucketStart}
	})
}

func logEventKeyPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimPrefix(strings.Repeat(", ?", count), ", ")
}

func logEventKeyArgs(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}
