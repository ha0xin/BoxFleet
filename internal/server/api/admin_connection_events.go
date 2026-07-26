package api

import (
	"net/http"
	"strings"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// Admin reads over the sing-box 1.14 daemon connection stream.
//
// These sit beside the /network-events endpoints rather than replacing them.
// The production fleet runs 1.13, where the `service.api` config block does not
// even parse, so most nodes produce journal-scraped log_events and nothing
// here. The split is deliberate and visible: /connection-events/nodes tells the
// admin UI exactly which nodes stream, so a mixed-version fleet reads as "this
// node has a richer source" instead of as columns that are mysteriously empty.
//
// Every byte figure returned here is an estimate. sing-box drops silently when
// a subscriber buffer fills, evicts its closed-connection ring at 1000 entries,
// and resets connection ids on restart, so the coverage block travels with each
// aggregate and callers must render it. Nothing here is "traffic": per-user
// billing stays on the V2Ray counters that /traffic/series reads.

// adminConnectionVolume is the measure tuple every aggregate carries. Opened
// and closed are separate because a long-lived session contributes bytes to
// several consecutive buckets — summing "connections" must use opened.
type adminConnectionVolume struct {
	ConnectionsOpened int64 `json:"connections_opened"`
	ConnectionsClosed int64 `json:"connections_closed"`
	UplinkBytes       int64 `json:"uplink_bytes"`
	DownlinkBytes     int64 `json:"downlink_bytes"`
	TotalBytes        int64 `json:"total_bytes"`
	DurationMsTotal   int64 `json:"duration_ms_total"`
}

// adminConnectionCoverage is the collector's own loss telemetry. It is on every
// response that carries bytes so a client cannot render the estimate without
// the figure that qualifies it.
type adminConnectionCoverage struct {
	ConnectionsObserved     int64   `json:"connections_observed"`
	ConnectionsAttributed   int64   `json:"connections_attributed"`
	ConnectionsUnattributed int64   `json:"connections_unattributed"`
	ConnectionsOrphaned     int64   `json:"connections_orphaned"`
	StreamResets            int64   `json:"stream_resets"`
	DroppedBuckets          int64   `json:"dropped_buckets"`
	BytesObserved           int64   `json:"bytes_observed"`
	BytesAttributed         int64   `json:"bytes_attributed"`
	AttributionRatio        float64 `json:"attribution_ratio"`
	Reports                 int64   `json:"reports"`
}

type adminConnectionEvent struct {
	NodeName          string `json:"node_name"`
	UserName          string `json:"user_name"`
	AuthName          string `json:"auth_name"`
	SourceIP          string `json:"source_ip"`
	TargetHost        string `json:"target_host"`
	TargetPort        int64  `json:"target_port"`
	Domain            string `json:"domain"`
	Network           string `json:"network"`
	IPVersion         int64  `json:"ip_version"`
	Protocol          string `json:"protocol"`
	Inbound           string `json:"inbound"`
	InboundType       string `json:"inbound_type"`
	Rule              string `json:"rule"`
	Outbound          string `json:"outbound"`
	OutboundType      string `json:"outbound_type"`
	Chain             string `json:"chain"`
	ConnectionsOpened int64  `json:"connections_opened"`
	ConnectionsClosed int64  `json:"connections_closed"`
	UplinkBytes       int64  `json:"uplink_bytes"`
	DownlinkBytes     int64  `json:"downlink_bytes"`
	DurationMsTotal   int64  `json:"duration_ms_total"`
	BucketStart       string `json:"bucket_start"`
	WindowStart       string `json:"window_start"`
	WindowEnd         string `json:"window_end"`
}

type adminConnectionEventsResponse struct {
	Events []adminConnectionEvent `json:"events"`
	Total  int64                  `json:"total"`
	Limit  int64                  `json:"limit"`
	Offset int64                  `json:"offset"`
}

type adminConnectionPoint struct {
	BucketStart string `json:"bucket_start"`
	adminConnectionVolume
}

type adminConnectionSeriesResponse struct {
	Bucket        string                  `json:"bucket"`
	OffsetMinutes int                     `json:"offset_minutes"`
	Start         string                  `json:"start"`
	End           string                  `json:"end"`
	Points        []adminConnectionPoint  `json:"points"`
	Totals        adminConnectionVolume   `json:"totals"`
	Coverage      adminConnectionCoverage `json:"coverage"`
}

type adminConnectionHost struct {
	Host string `json:"host"`
	adminConnectionVolume
}

type adminConnectionHostsResponse struct {
	Sort          string                  `json:"sort"`
	Hosts         []adminConnectionHost   `json:"hosts"`
	Totals        adminConnectionVolume   `json:"totals"`
	DistinctHosts int64                   `json:"distinct_hosts"`
	Limit         int64                   `json:"limit"`
	Truncated     bool                    `json:"truncated"`
	Coverage      adminConnectionCoverage `json:"coverage"`
}

// adminConnectionTelemetryNode never carries the secret. The renderer emits it
// into the node config and it has to be recoverable server-side, but an admin
// API response is not a place it belongs.
type adminConnectionTelemetryNode struct {
	NodeName      string `json:"node_name"`
	ListenAddress string `json:"listen_address"`
	ListenPort    int64  `json:"listen_port"`
}

type adminConnectionTelemetryNodesResponse struct {
	Nodes []adminConnectionTelemetryNode `json:"nodes"`
}

func adminConnectionEventsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, ok := connectionEventScopeFilter(w, r)
		if !ok {
			return
		}
		filter.Limit = queryLimit(r, 100)
		filter.Offset = queryOffset(r)
		page, err := store.ListConnectionEventsPage(r.Context(), filter)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		events := make([]adminConnectionEvent, 0, len(page.Events))
		for _, event := range page.Events {
			events = append(events, adminConnectionEvent(event))
		}
		writeJSON(w, adminConnectionEventsResponse{
			Events: events,
			Total:  page.Total,
			Limit:  page.Limit,
			Offset: page.Offset,
		})
	}
}

func adminConnectionSeriesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params, ok := parseSeriesParams(w, r)
		if !ok {
			return
		}
		filter, ok := connectionEventScopeFilter(w, r)
		if !ok {
			return
		}
		filter.Start = params.StartRFC3339()
		filter.End = params.EndRFC3339()
		result, err := store.ConnectionSeries(r.Context(), db.ConnectionSeriesFilter{
			ConnectionEventFilter: filter,
			Bucket:                params.Bucket,
			OffsetMinutes:         params.OffsetMinutes,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		points := make([]adminConnectionPoint, 0, len(result.Points))
		for _, point := range result.Points {
			points = append(points, adminConnectionPoint{
				BucketStart:           db.BucketKey(point.BucketStart),
				adminConnectionVolume: adminConnectionVolumeOf(point.ConnectionVolume),
			})
		}
		writeJSON(w, adminConnectionSeriesResponse{
			Bucket:        string(params.Bucket),
			OffsetMinutes: params.OffsetMinutes,
			Start:         params.StartRFC3339(),
			End:           params.EndRFC3339(),
			Points:        points,
			Totals:        adminConnectionVolumeOf(result.Totals),
			Coverage:      adminConnectionCoverageOf(result.Coverage),
		})
	}
}

func adminConnectionHostsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// queryGroup is the shared whitelist mapper; reading `sort` through it
		// keeps an unknown value a 422 instead of a silent fallback to bytes.
		sort, ok := queryGroup(w, r, "bytes", "bytes", "connections")
		if !ok {
			return
		}
		filter, ok := connectionEventScopeFilter(w, r)
		if !ok {
			return
		}
		result, err := store.ConnectionHostUsage(
			r.Context(),
			filter,
			db.ConnectionHostSort(sort),
			queryBoundedLimit(r, 20, 100),
		)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		hosts := make([]adminConnectionHost, 0, len(result.Hosts))
		for _, host := range result.Hosts {
			hosts = append(hosts, adminConnectionHost{
				Host:                  host.Host,
				adminConnectionVolume: adminConnectionVolumeOf(host.ConnectionVolume),
			})
		}
		writeJSON(w, adminConnectionHostsResponse{
			Sort:          sort,
			Hosts:         hosts,
			Totals:        adminConnectionVolumeOf(result.Totals),
			DistinctHosts: result.DistinctHosts,
			Limit:         int64(len(hosts)),
			Truncated:     result.Truncated,
			Coverage:      adminConnectionCoverageOf(result.Coverage),
		})
	}
}

// adminConnectionTelemetryNodesHandler is what makes the mixed-version fleet
// legible: it lists the nodes that actually stream. An empty list is the normal
// fleet-wide answer today and the admin UI renders it as an explanation rather
// than as an error or an empty table.
func adminConnectionTelemetryNodesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, err := store.ListEnabledConnectionTelemetryNodes(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		rows := make([]adminConnectionTelemetryNode, 0, len(nodes))
		for _, node := range nodes {
			rows = append(rows, adminConnectionTelemetryNode{
				NodeName:      node.NodeName,
				ListenAddress: node.ListenAddress,
				ListenPort:    node.ListenPort,
			})
		}
		writeJSON(w, adminConnectionTelemetryNodesResponse{Nodes: rows})
	}
}

// connectionEventScopeFilter reads the filters every connection endpoint shares.
// There is no `action` and no `search`: the stream carries no classified action
// and connection_events has no full-text index. `host` is the drill-down the
// host ranking hands back.
func connectionEventScopeFilter(w http.ResponseWriter, r *http.Request) (db.ConnectionEventFilter, bool) {
	start, ok := queryOptionalTime(w, r, "start")
	if !ok {
		return db.ConnectionEventFilter{}, false
	}
	end, ok := queryOptionalTime(w, r, "end")
	if !ok {
		return db.ConnectionEventFilter{}, false
	}
	return db.ConnectionEventFilter{
		NodeName: strings.TrimSpace(r.URL.Query().Get("node")),
		UserName: strings.TrimSpace(r.URL.Query().Get("user")),
		Host:     strings.TrimSpace(r.URL.Query().Get("host")),
		Start:    start,
		End:      end,
	}, true
}

func adminConnectionVolumeOf(volume db.ConnectionVolume) adminConnectionVolume {
	return adminConnectionVolume{
		ConnectionsOpened: volume.ConnectionsOpened,
		ConnectionsClosed: volume.ConnectionsClosed,
		UplinkBytes:       volume.UplinkBytes,
		DownlinkBytes:     volume.DownlinkBytes,
		// Precomputed so no client has to re-derive the figure it ranks on.
		TotalBytes:      volume.TotalBytes(),
		DurationMsTotal: volume.DurationMsTotal,
	}
}

func adminConnectionCoverageOf(coverage db.ConnectionCoverageTotals) adminConnectionCoverage {
	return adminConnectionCoverage{
		ConnectionsObserved:     coverage.ConnectionsObserved,
		ConnectionsAttributed:   coverage.ConnectionsAttributed,
		ConnectionsUnattributed: coverage.ConnectionsUnattributed,
		ConnectionsOrphaned:     coverage.ConnectionsOrphaned,
		StreamResets:            coverage.StreamResets,
		DroppedBuckets:          coverage.DroppedBuckets,
		BytesObserved:           coverage.BytesObserved,
		BytesAttributed:         coverage.BytesAttributed,
		// Computed here, not in the browser: the "empty window reports 1"
		// convention lives with the coverage type and must not be re-invented
		// per client.
		AttributionRatio: coverage.ConnectionAttributionRatio(),
		Reports:          coverage.Reports,
	}
}
