package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func nodeNetworkSnapshotHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		if r.Method == http.MethodGet {
			network, err := store.NodeNetwork(r.Context(), name)
			if err != nil {
				http.Error(w, "network state lookup failed", 500)
				return
			}
			writeJSON(w, model.NetworkSnapshotAck{Revision: network.Revision})
			return
		}
		var report model.NetworkSnapshot
		if !decodeNodeReport(w, r, model.MaxNetworkSnapshotBytes, &report) {
			return
		}
		if err := store.RecordNetworkSnapshot(r.Context(), name, report); err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, db.ErrNetworkRevision) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, model.NetworkSnapshotAck{Revision: report.Revision})
	}
}

func adminNodeAddressesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "node")
		if _, err := store.GetNode(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "node not found", http.StatusNotFound)
			} else {
				writeAdminError(w, err)
			}
			return
		}
		network, err := store.NodeNetwork(r.Context(), id)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, network)
	}
}
