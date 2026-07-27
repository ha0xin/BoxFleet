package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// registerTelemetryRoutes mounts the bucketed telemetry series, the service
// classification views, and the operator override endpoints. It is called from
// inside the admin route group, so every route here inherits adminAuthMiddleware.
//
// The /network-events/* family reads the journalctl-scraped log_events that
// every node produces; the /connection-events/* family reads the sing-box 1.14
// daemon stream that only opted-in nodes produce. They are mounted side by side
// and never merged: which source a row came from is a fact the admin UI has to
// be able to state.
func registerTelemetryRoutes(r chi.Router, store *db.DB) {
	r.Get("/traffic/series", adminTrafficSeriesHandler(store))
	r.Get("/network-events/series", adminNetworkEventSeriesHandler(store))
	r.Get("/network-events/services", adminNetworkEventServicesHandler(store))
	r.Get("/network-events/hosts", adminNetworkEventHostsHandler(store))
	r.Get("/connection-events", adminConnectionEventsHandler(store))
	r.Get("/connection-events/series", adminConnectionSeriesHandler(store))
	r.Get("/connection-events/hosts", adminConnectionHostsHandler(store))
	r.Get("/connection-events/nodes", adminConnectionTelemetryNodesHandler(store))
	r.Get("/service-overrides", adminListServiceOverridesHandler(store))
	r.Put("/service-overrides", adminUpsertServiceOverrideHandler(store))
	r.Delete("/service-overrides/{suffix}", adminDeleteServiceOverrideHandler(store))
}
