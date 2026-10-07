package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/mihomo"
	"github.com/haoxin/boxfleet/internal/server/render"
)

type adminMihomoProfileSubscription struct {
	Active     bool   `json:"active"`
	URL        string `json:"url"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

func subscriptionMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profileToken, profileOK, err := store.VerifyMihomoProfileSubscriptionToken(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			http.Error(w, "subscription is unavailable", http.StatusInternalServerError)
			return
		}
		if profileOK {
			profile, err := store.GetMihomoProfile(r.Context(), profileToken.ProfileID)
			if err != nil {
				http.Error(w, "subscription is unavailable", http.StatusUnprocessableEntity)
				return
			}
			result, err := render.RenderMihomoConfiguration(r.Context(), store, profileToken.ProxyUserName, profile.Document)
			if err != nil || mihomo.HasErrors(result.Diagnostics) {
				http.Error(w, "subscription is unavailable", http.StatusUnprocessableEntity)
				return
			}
			writeProviderYAML(w, r, result.YAML)
			return
		}
		http.NotFound(w, r)
	}
}

func adminMihomoProfileSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok, err := store.GetActiveMihomoProfileSubscriptionToken(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminMihomoProfileSubscriptionResponse(r, token, ok))
	}
}

func adminIssueMihomoProfileSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := store.IssueMihomoProfileSubscriptionToken(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminMihomoProfileSubscriptionResponse(r, token, true))
	}
}

func adminRotateMihomoProfileSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := store.RotateMihomoProfileSubscriptionToken(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminMihomoProfileSubscriptionResponse(r, token, true))
	}
}

func adminRevokeMihomoProfileSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.RevokeMihomoProfileSubscriptionToken(r.Context(), chi.URLParam(r, "profile")); err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminMihomoProfileSubscription{Active: false})
	}
}

func adminMihomoProfileSubscriptionResponse(r *http.Request, token db.MihomoProfileSubscriptionToken, active bool) adminMihomoProfileSubscription {
	if !active {
		return adminMihomoProfileSubscription{Active: false}
	}
	return adminMihomoProfileSubscription{
		Active: true, URL: fmt.Sprintf("%s/sub/%s/mihomo.yaml", requestBaseURL(r), token.Token),
		CreatedAt: token.CreatedAt, LastUsedAt: nullString(token.LastUsedAt),
	}
}

func writeProviderYAML(w http.ResponseWriter, r *http.Request, raw []byte) {
	etag := `"` + db.SHA256Hex(raw) + `"`
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(raw)
}
