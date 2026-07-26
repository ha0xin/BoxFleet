package api

import (
	"net/http"
	"strings"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// The network-event telemetry endpoints all count connections. log_events has
// no byte columns and traffic_usage_deltas has no host column, so nothing here
// is a volume: labelling any of these fields "traffic" would be a lie about the
// schema.
type adminNetworkEventPoint struct {
	BucketStart string `json:"bucket_start"`
	Count       int64  `json:"count"`
}

type adminNetworkEventSeries struct {
	Key    string                   `json:"key"`
	Label  string                   `json:"label"`
	Points []adminNetworkEventPoint `json:"points"`
	Total  int64                    `json:"total"`
}

type adminNetworkEventActionCount struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

type adminNetworkEventSeriesResponse struct {
	Bucket        string                         `json:"bucket"`
	OffsetMinutes int                            `json:"offset_minutes"`
	Start         string                         `json:"start"`
	End           string                         `json:"end"`
	Group         string                         `json:"group"`
	Series        []adminNetworkEventSeries      `json:"series"`
	Actions       []adminNetworkEventActionCount `json:"actions"`
	Truncated     bool                           `json:"truncated"`
}

type adminServiceUsageRow struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Connections int64  `json:"connections"`
	Hosts       int64  `json:"hosts"`
}

type adminServiceUsageResponse struct {
	Start            string                 `json:"start"`
	End              string                 `json:"end"`
	Group            string                 `json:"group"`
	Rows             []adminServiceUsageRow `json:"rows"`
	Other            adminServiceUsageRow   `json:"other"`
	TotalConnections int64                  `json:"total_connections"`
	TotalHosts       int64                  `json:"total_hosts"`
	Truncated        bool                   `json:"truncated"`
	CatalogVersion   string                 `json:"catalog_version"`
}

type adminNetworkEventHost struct {
	Host         string `json:"host"`
	Service      string `json:"service"`
	ServiceLabel string `json:"service_label"`
	Category     string `json:"category"`
	Source       string `json:"source"`
	Connections  int64  `json:"connections"`
	LastSeen     string `json:"last_seen"`
}

type adminNetworkEventHostsResponse struct {
	Hosts     []adminNetworkEventHost `json:"hosts"`
	Total     int64                   `json:"total"`
	Limit     int64                   `json:"limit"`
	Offset    int64                   `json:"offset"`
	Truncated bool                    `json:"truncated"`
}

func adminNetworkEventSeriesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, ok := parseSeriesParams(w, r)
		if !ok {
			return
		}
		group, ok := queryGroup(w, r, "total", "total", "action", "node", "user")
		if !ok {
			return
		}
		filter, ok := networkEventScopeFilter(w, r)
		if !ok {
			return
		}
		filter.Start = params.StartRFC3339()
		filter.End = params.EndRFC3339()
		result, err := store.NetworkEventSeries(r.Context(), db.NetworkEventSeriesFilter{
			LogEventFilter: filter,
			Bucket:         params.Bucket,
			OffsetMinutes:  params.OffsetMinutes,
			Group:          db.NetworkEventSeriesGroup(group),
			Limit:          queryBoundedLimit(r, 25, 100),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNetworkEventSeriesResponse{
			Bucket:        string(params.Bucket),
			OffsetMinutes: params.OffsetMinutes,
			Start:         params.StartRFC3339(),
			End:           params.EndRFC3339(),
			Group:         group,
			Series:        adminNetworkEventSeriesList(result.Series),
			Actions:       adminNetworkEventActionCounts(result.Actions),
			Truncated:     result.Truncated,
		})
	}
}

func adminNetworkEventServicesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		group, ok := queryGroup(w, r, "service", "service", "category")
		if !ok {
			return
		}
		filter, ok := networkEventScopeFilter(w, r)
		if !ok {
			return
		}
		result, err := store.NetworkEventServiceUsage(r.Context(), filter, db.ServiceUsageGroup(group), queryBoundedLimit(r, 20, 100))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		rows := make([]adminServiceUsageRow, 0, len(result.Rows))
		for _, row := range result.Rows {
			rows = append(rows, adminServiceUsageRow(row))
		}
		writeJSON(w, adminServiceUsageResponse{
			Start:            filter.Start,
			End:              filter.End,
			Group:            group,
			Rows:             rows,
			Other:            adminServiceUsageRow(result.Other),
			TotalConnections: result.TotalConnections,
			TotalHosts:       result.TotalHosts,
			Truncated:        result.Truncated,
			CatalogVersion:   result.CatalogVersion,
		})
	}
}

func adminNetworkEventHostsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := networkEventScopeFilter(w, r)
		if !ok {
			return
		}
		page, err := store.NetworkEventHostUsage(
			r.Context(),
			filter,
			strings.TrimSpace(r.URL.Query().Get("service")),
			queryBoundedLimit(r, 50, 500),
			queryOffset(r),
		)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		hosts := make([]adminNetworkEventHost, 0, len(page.Hosts))
		for _, host := range page.Hosts {
			hosts = append(hosts, adminNetworkEventHost(host))
		}
		writeJSON(w, adminNetworkEventHostsResponse{
			Hosts:     hosts,
			Total:     page.Total,
			Limit:     page.Limit,
			Offset:    page.Offset,
			Truncated: page.Truncated,
		})
	}
}

// networkEventScopeFilter reads the six filters the /network-events table
// endpoint reads, with byte-identical semantics. The series endpoint overwrites
// Start and End afterwards because a zero-filled series requires both; the
// service and host breakdowns leave them optional, matching the table.
func networkEventScopeFilter(w http.ResponseWriter, r *http.Request) (db.LogEventFilter, bool) {
	start, ok := queryOptionalTime(w, r, "start")
	if !ok {
		return db.LogEventFilter{}, false
	}
	end, ok := queryOptionalTime(w, r, "end")
	if !ok {
		return db.LogEventFilter{}, false
	}
	return db.LogEventFilter{
		NodeName: strings.TrimSpace(r.URL.Query().Get("node")),
		UserName: strings.TrimSpace(r.URL.Query().Get("user")),
		Action:   strings.TrimSpace(r.URL.Query().Get("action")),
		Search:   strings.TrimSpace(r.URL.Query().Get("search")),
		Start:    start,
		End:      end,
	}, true
}

func adminNetworkEventSeriesList(series []db.NetworkEventSeries) []adminNetworkEventSeries {
	out := make([]adminNetworkEventSeries, 0, len(series))
	for _, entry := range series {
		points := make([]adminNetworkEventPoint, 0, len(entry.Points))
		for _, point := range entry.Points {
			points = append(points, adminNetworkEventPoint{
				BucketStart: db.BucketKey(point.BucketStart),
				Count:       point.Count,
			})
		}
		out = append(out, adminNetworkEventSeries{
			Key:    entry.Key,
			Label:  entry.Label,
			Points: points,
			Total:  entry.Total,
		})
	}
	return out
}

func adminNetworkEventActionCounts(counts []db.ActionCount) []adminNetworkEventActionCount {
	out := make([]adminNetworkEventActionCount, 0, len(counts))
	for _, count := range counts {
		out = append(out, adminNetworkEventActionCount(count))
	}
	return out
}
