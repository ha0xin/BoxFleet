package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TrafficSeriesGroup is the dimension a bucketed traffic series is broken down
// by. "total" always yields exactly one series so the client renders every
// grouping through one code path.
type TrafficSeriesGroup string

const (
	TrafficSeriesGroupTotal TrafficSeriesGroup = "total"
	TrafficSeriesGroupUser  TrafficSeriesGroup = "user"
	TrafficSeriesGroupNode  TrafficSeriesGroup = "node"
)

const (
	trafficSeriesTotalKey     = "total"
	trafficSeriesTotalLabel   = "All traffic"
	trafficSeriesDefaultLimit = 25
	trafficSeriesMaxLimit     = 100
)

type TrafficSeriesFilter struct {
	Start         time.Time
	End           time.Time
	Bucket        Bucket
	OffsetMinutes int
	Group         TrafficSeriesGroup
	UserName      string
	NodeName      string
	// Limit bounds the number of series returned for a grouped request; it is
	// ignored for TrafficSeriesGroupTotal.
	Limit int64
}

// TrafficPoint carries both directions for one bucket. Pivoting here rather
// than in the client means a chart never has to stitch two result sets
// together to draw one stacked bar.
type TrafficPoint struct {
	BucketStart           time.Time
	UplinkRawBytes        int64
	UplinkBillableBytes   int64
	DownlinkRawBytes      int64
	DownlinkBillableBytes int64
}

type TrafficSeries struct {
	Key    string
	Label  string
	Points []TrafficPoint
	Totals TrafficPoint
}

type TrafficSeriesResult struct {
	Series    []TrafficSeries
	Truncated bool
}

// trafficSeriesScope is TrafficSeriesFilter with names resolved to IDs and the
// window rendered the way observed_at is stored.
type trafficSeriesScope struct {
	NodeID    string
	UserID    string
	StartTime string
	EndTime   string
}

// trafficSeriesKey is one ranked series: the ID the bucket query filters and
// groups on, plus the operator-facing name the client sees.
type trafficSeriesKey struct {
	ID   string
	Name string
}

// TrafficSeries aggregates traffic_usage_deltas on the fly instead of reading a
// rollup. traffic_usage_totals is keyed (proxy_user_id, direction) and carries
// no timestamp, node or proxy column, so it cannot be decomposed into buckets
// at all; the span ceiling the API enforces plus idx_traffic_usage_deltas_observed
// are what keep this scan bounded.
//
// Soft-deleted users are excluded, matching the existing traffic summaries. The
// network-event series deliberately includes them, matching the event table it
// is drawn above. The two pipelines disagree on purpose: each stays consistent
// with the rows an operator reads next to the chart.
func (db *DB) TrafficSeries(ctx context.Context, filter TrafficSeriesFilter) (TrafficSeriesResult, error) {
	group := filter.Group
	if group == "" {
		group = TrafficSeriesGroupTotal
	}
	keyColumn, err := trafficSeriesKeyColumn(group)
	if err != nil {
		return TrafficSeriesResult{}, err
	}
	bucket := filter.Bucket
	if bucket != BucketDay {
		bucket = BucketHour
	}
	scope, err := db.resolveTrafficSeriesScope(ctx, filter)
	if err != nil {
		return TrafficSeriesResult{}, err
	}

	if group == TrafficSeriesGroupTotal {
		buckets, err := db.queryTrafficBuckets(ctx, scope, bucket, filter.OffsetMinutes, "", nil)
		if err != nil {
			return TrafficSeriesResult{}, err
		}
		series := trafficSeriesFromBuckets(
			trafficSeriesTotalKey,
			trafficSeriesTotalLabel,
			buckets[""],
			filter,
			bucket,
		)
		return TrafficSeriesResult{Series: []TrafficSeries{series}}, nil
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = trafficSeriesDefaultLimit
	}
	if limit > trafficSeriesMaxLimit {
		limit = trafficSeriesMaxLimit
	}
	// One extra row is enough to tell a full page from a truncated one.
	ranked, err := db.rankTrafficSeriesKeys(ctx, scope, group, limit+1)
	if err != nil {
		return TrafficSeriesResult{}, err
	}
	truncated := int64(len(ranked)) > limit
	if truncated {
		ranked = ranked[:limit]
	}
	if len(ranked) == 0 {
		return TrafficSeriesResult{Series: []TrafficSeries{}}, nil
	}
	ids := make([]string, 0, len(ranked))
	for _, key := range ranked {
		ids = append(ids, key.ID)
	}
	buckets, err := db.queryTrafficBuckets(ctx, scope, bucket, filter.OffsetMinutes, keyColumn, ids)
	if err != nil {
		return TrafficSeriesResult{}, err
	}
	series := make([]TrafficSeries, 0, len(ranked))
	for _, key := range ranked {
		series = append(series, trafficSeriesFromBuckets(key.Name, key.Name, buckets[key.ID], filter, bucket))
	}
	return TrafficSeriesResult{Series: series, Truncated: truncated}, nil
}

func trafficSeriesKeyColumn(group TrafficSeriesGroup) (string, error) {
	switch group {
	case TrafficSeriesGroupTotal:
		return "", nil
	case TrafficSeriesGroupUser:
		return "d.proxy_user_id", nil
	case TrafficSeriesGroupNode:
		return "d.node_id", nil
	default:
		return "", fmt.Errorf("group must be total, user or node")
	}
}

// resolveTrafficSeriesScope surfaces an unknown node or user as an error rather
// than as a silently empty chart.
func (db *DB) resolveTrafficSeriesScope(ctx context.Context, filter TrafficSeriesFilter) (trafficSeriesScope, error) {
	scope := trafficSeriesScope{
		StartTime: filter.Start.UTC().Format(time.RFC3339Nano),
		EndTime:   filter.End.UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(filter.NodeName) != "" {
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return trafficSeriesScope{}, err
		}
		scope.NodeID = node.ID
	}
	if strings.TrimSpace(filter.UserName) != "" {
		user, err := db.GetProxyUser(ctx, filter.UserName)
		if err != nil {
			return trafficSeriesScope{}, err
		}
		scope.UserID = user.ID
	}
	return scope, nil
}

// buildTrafficSeriesPredicates renders the shared WHERE fragments. observed_at
// leads so the range stays the driving constraint of
// idx_traffic_usage_deltas_observed; args are ordered to match the clauses.
func buildTrafficSeriesPredicates(scope trafficSeriesScope) (where []string, args []any) {
	where = []string{"d.observed_at >= ?", "d.observed_at <= ?"}
	args = []any{scope.StartTime, scope.EndTime}
	// Soft-deleted users are rare, so a materialized list subquery costs far
	// less than joining proxy_users into the aggregation.
	where = append(where, "d.proxy_user_id NOT IN (SELECT id FROM proxy_users WHERE deleted_at IS NOT NULL)")
	if scope.NodeID != "" {
		where = append(where, "d.node_id = ?")
		args = append(args, scope.NodeID)
	}
	if scope.UserID != "" {
		where = append(where, "d.proxy_user_id = ?")
		args = append(args, scope.UserID)
	}
	return where, args
}

// buildTrafficBucketQuery renders the bucketed aggregation. keyColumn is empty
// for an ungrouped total; otherwise it is a literal from this package, never
// operator input. Both directions are summed in one pass and pivoted in Go.
func buildTrafficBucketQuery(
	scope trafficSeriesScope,
	bucket Bucket,
	offsetMinutes int,
	keyColumn string,
	keyIDs []string,
) (string, []any) {
	where, args := buildTrafficSeriesPredicates(scope)
	selectKey := "''"
	if keyColumn != "" {
		selectKey = keyColumn
		if len(keyIDs) > 0 {
			where = append(where, keyColumn+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(keyIDs)), ",")+")")
			for _, id := range keyIDs {
				args = append(args, id)
			}
		}
	}
	query := `
SELECT
  ` + selectKey + ` AS series_key,
  ` + bucketExpr("d.observed_at", bucket, offsetMinutes) + ` AS bucket_key,
  d.direction,
  SUM(d.raw_bytes_delta) AS raw_bytes,
  SUM(d.billable_bytes_delta) AS billable_bytes
FROM traffic_usage_deltas d
WHERE ` + strings.Join(where, "\n  AND ") + `
GROUP BY series_key, bucket_key, d.direction`
	return query, args
}

func (db *DB) queryTrafficBuckets(
	ctx context.Context,
	scope trafficSeriesScope,
	bucket Bucket,
	offsetMinutes int,
	keyColumn string,
	keyIDs []string,
) (map[string]map[string]TrafficPoint, error) {
	query, args := buildTrafficBucketQuery(scope, bucket, offsetMinutes, keyColumn, keyIDs)
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := make(map[string]map[string]TrafficPoint)
	for rows.Next() {
		var seriesKey string
		var bucketKey sql.NullString
		var direction string
		var rawBytes, billableBytes int64
		if err := rows.Scan(&seriesKey, &bucketKey, &direction, &rawBytes, &billableBytes); err != nil {
			return nil, err
		}
		// observed_at is agent-supplied text with no CHECK constraint, so a
		// skewed or malformed timestamp can bucket to NULL. Drop that row
		// rather than failing the whole chart.
		if !bucketKey.Valid {
			continue
		}
		bucketStart, err := ParseBucketKey(bucketKey.String)
		if err != nil {
			continue
		}
		series, ok := buckets[seriesKey]
		if !ok {
			series = make(map[string]TrafficPoint)
			buckets[seriesKey] = series
		}
		point := series[bucketKey.String]
		point.BucketStart = bucketStart
		switch direction {
		case "uplink":
			point.UplinkRawBytes += rawBytes
			point.UplinkBillableBytes += billableBytes
		case "downlink":
			point.DownlinkRawBytes += rawBytes
			point.DownlinkBillableBytes += billableBytes
		default:
			continue
		}
		series[bucketKey.String] = point
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buckets, nil
}

// rankTrafficSeriesKeys picks the heaviest series in the window so a grouped
// request returns a readable chart instead of every user a node ever served.
func (db *DB) rankTrafficSeriesKeys(
	ctx context.Context,
	scope trafficSeriesScope,
	group TrafficSeriesGroup,
	limit int64,
) ([]trafficSeriesKey, error) {
	where, args := buildTrafficSeriesPredicates(scope)
	var keyColumn, labelJoin, labelColumn string
	switch group {
	case TrafficSeriesGroupUser:
		keyColumn = "d.proxy_user_id"
		labelColumn = "u.name"
		labelJoin = "JOIN proxy_users u ON u.id = d.proxy_user_id"
	case TrafficSeriesGroupNode:
		keyColumn = "d.node_id"
		labelColumn = "n.name"
		labelJoin = "JOIN nodes n ON n.id = d.node_id"
	default:
		return nil, fmt.Errorf("group must be user or node")
	}
	query := `
SELECT
  ` + keyColumn + ` AS series_key,
  ` + labelColumn + ` AS series_label,
  SUM(d.billable_bytes_delta) AS billable_bytes
FROM traffic_usage_deltas d
` + labelJoin + `
WHERE ` + strings.Join(where, "\n  AND ") + `
GROUP BY series_key, series_label
ORDER BY billable_bytes DESC, series_label
LIMIT ?`
	args = append(args, limit)
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]trafficSeriesKey, 0, limit)
	for rows.Next() {
		var key trafficSeriesKey
		var billableBytes int64
		if err := rows.Scan(&key.ID, &key.Name, &billableBytes); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func trafficSeriesFromBuckets(
	key, label string,
	present map[string]TrafficPoint,
	filter TrafficSeriesFilter,
	bucket Bucket,
) TrafficSeries {
	points := ZeroFillSeries(
		filter.Start,
		filter.End,
		bucket,
		filter.OffsetMinutes,
		present,
		func(bucketStart time.Time) TrafficPoint {
			return TrafficPoint{BucketStart: bucketStart}
		},
	)
	totals := TrafficPoint{}
	for _, point := range points {
		totals.UplinkRawBytes += point.UplinkRawBytes
		totals.UplinkBillableBytes += point.UplinkBillableBytes
		totals.DownlinkRawBytes += point.DownlinkRawBytes
		totals.DownlinkBillableBytes += point.DownlinkBillableBytes
	}
	return TrafficSeries{Key: key, Label: label, Points: points, Totals: totals}
}
