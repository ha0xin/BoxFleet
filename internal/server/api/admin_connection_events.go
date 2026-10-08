package api

import (
	"net/http"
	"strings"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// Diagnostic reads over the opt-in sing-box connection source. Session byte
// totals are lifetime estimates; interval-byte aggregate endpoints are retired.
// Unified Logs and connection-start analytics live under /network-events.

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

// Lifetime snapshots cannot answer bytes transferred within a time window.
// Keep an explicit tombstone so callers cannot mistake removal for empty data.
func adminConnectionAggregatesRetiredHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	writeJSON(w, map[string]string{"error": "Interval byte analytics are retired: connection snapshots contain lifetime totals. Use /api/admin/network-events/series and /api/admin/network-events/hosts for connection counts; use /api/admin/traffic/series for user traffic."})
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
