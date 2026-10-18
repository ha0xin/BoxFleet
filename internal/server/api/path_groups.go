package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func adminPathGroupsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, err := store.ListPathGroups(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, groups)
	}
}

func adminPathGroupHistoryHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		history, err := store.ListPathGroupHistory(r.Context(), chi.URLParam(r, "group"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, history)
	}
}

func adminPathGroupChangeHandler(store *db.DB, preview bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var change db.PathGroupChange
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&change); err != nil {
			http.Error(w, "invalid group change", http.StatusBadRequest)
			return
		}
		if change.Operation == "grant" || change.Operation == "revoke" {
			http.Error(w, "Groups are selection shortcuts; update individual Paths instead.", http.StatusGone)
			return
		}
		impact, err := store.ChangePathGroup(r.Context(), change, preview)
		if err != nil {
			if errors.Is(err, db.ErrPathGroupConflict) {
				http.Error(w, err.Error(), http.StatusConflict)
			} else if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "group not found", http.StatusNotFound)
			} else {
				writeAdminError(w, err)
			}
			return
		}
		writeJSON(w, impact)
	}
}

func adminUserPathSourcesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sources, err := store.UserPathSources(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, sources)
	}
}

func adminChangeUserPathSelectionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Changes []db.PathSelectionChange `json:"changes"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			http.Error(w, "invalid selection", http.StatusBadRequest)
			return
		}
		if err := store.ChangeUserPathSelection(r.Context(), chi.URLParam(r, "user"), payload.Changes); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "user or Path not found", http.StatusNotFound)
			} else {
				writeAdminError(w, err)
			}
			return
		}
		writeJSON(w, map[string]int{"updated": len(payload.Changes)})
	}
}
