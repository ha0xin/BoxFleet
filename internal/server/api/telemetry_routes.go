package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// registerTelemetryRoutes mounts the bucketed telemetry series, the service
// classification views, and the operator override endpoints. It is called from
// inside the admin route group, so every route here inherits adminAuthMiddleware.
//
// The /network-events/* family reads unified journal and stream records.
// The /connection-events list exposes source rows for diagnostics; its former
// interval-byte series and host analytics routes return HTTP 410.
func registerTelemetryRoutes(r chi.Router, store *db.DB) {
	r.Get("/traffic/series", adminTrafficSeriesHandler(store))
	r.Get("/network-events/series", adminNetworkEventSeriesHandler(store))
	r.Get("/network-events/services", adminNetworkEventServicesHandler(store))
	r.Get("/network-events/hosts", adminNetworkEventHostsHandler(store))
	r.Get("/connection-events", adminConnectionEventsHandler(store))
	r.Get("/connection-events/series", adminConnectionAggregatesRetiredHandler)
	r.Get("/connection-events/hosts", adminConnectionAggregatesRetiredHandler)
	r.Get("/connection-events/nodes", adminConnectionTelemetryNodesHandler(store))
	r.Get("/service-overrides", adminListServiceOverridesHandler(store))
	r.Put("/service-overrides", adminUpsertServiceOverrideHandler(store))
	r.Delete("/service-overrides/{suffix}", adminDeleteServiceOverrideHandler(store))
}
