package api

import (
	"fmt"
	"net/http"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// nodeConnectionEventsHandler ingests one window of aggregated connection
// telemetry from sing-box 1.14's daemon gRPC stream. It is the second network
// event producer: the journalctl scraper behind /api/node/logs stays the fleet
// default and neither path knows about the other.
//
// The opt-in is checked here rather than swallowed in the facade so the agent
// can tell "stop collecting" from a transient failure. 403 means the operator
// turned telemetry off (or never turned it on) and the collector should shut
// down until the next config apply says otherwise; every other failure keeps
// the existing node-report semantics — 401 for auth, 413 for an oversized
// body, 422 for a payload the store rejects.
func nodeConnectionEventsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		config, found, err := store.NodeConnectionTelemetryConfig(r.Context(), nodeName)
		if err != nil {
			http.Error(w, "connection telemetry lookup failed", http.StatusInternalServerError)
			return
		}
		if !found || !config.Enabled {
			http.Error(w, "connection telemetry is not enabled for this node", http.StatusForbidden)
			return
		}
		var report db.ConnectionReport
		if !decodeNodeReport(w, r, maxNodeConnectionReportBytes, &report) {
			return
		}
		// The node's own claim about its identity is discarded, exactly as on
		// every other *Report: the name comes from the bearer token.
		report.NodeName = nodeName
		if err := store.RecordConnectionReport(r.Context(), report); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}
