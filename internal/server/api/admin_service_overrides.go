package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
)

// Operator overrides are consulted ahead of the embedded service catalog when
// the host breakdown classifies a destination. There is no admin page for them
// yet; the endpoints exist so the table is never a write-less dead table.
type adminServiceOverride struct {
	Suffix    string `json:"suffix"`
	Service   string `json:"service"`
	Label     string `json:"label"`
	Category  string `json:"category"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type adminServiceOverridePayload struct {
	Suffix   string `json:"suffix"`
	Service  string `json:"service"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

func adminListServiceOverridesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		overrides, err := store.ListDomainServiceOverrides(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		rows := make([]adminServiceOverride, 0, len(overrides))
		for _, override := range overrides {
			rows = append(rows, adminServiceOverride(override))
		}
		writeJSON(w, rows)
	}
}

func adminUpsertServiceOverrideHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminServiceOverridePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		override, err := store.UpsertDomainServiceOverride(r.Context(), db.DomainServiceOverride{
			Suffix:   payload.Suffix,
			Service:  payload.Service,
			Label:    payload.Label,
			Category: payload.Category,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminServiceOverride(override))
	}
}

func adminDeleteServiceOverrideHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := store.DeleteDomainServiceOverride(r.Context(), chi.URLParam(r, "suffix")); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
