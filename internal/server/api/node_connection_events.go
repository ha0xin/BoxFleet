package api

import (
	"fmt"
	"net/http"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// nodeConnectionEventsHandler ingests bounded session snapshots and legacy
// aggregates from the daemon connection stream. Journal collection remains
// available; the server selects sources for unified reads.
//
// Check activation before decoding. Disabled telemetry returns 403; invalid
// reports return 422. The agent currently retries failed reports until an
// applied config without the service stops collection.
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
