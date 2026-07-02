package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/render"
)

type adminUserSubscription struct {
	Active     bool   `json:"active"`
	URL        string `json:"url"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

func adminUserSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok, err := store.GetActiveSubscriptionToken(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSubscriptionResponse(r, token, ok))
	}
}

func adminIssueUserSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := store.IssueSubscriptionToken(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSubscriptionResponse(r, token, true))
	}
}

func adminRotateUserSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := store.RotateSubscriptionToken(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSubscriptionResponse(r, token, true))
	}
}

func adminRevokeUserSubscriptionHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.RevokeSubscriptionToken(r.Context(), chi.URLParam(r, "user")); err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUserSubscription{Active: false})
	}
}

func adminUserProxyProviderHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := render.RenderMihomoProxyProvider(r.Context(), store, chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeProviderYAML(w, r, raw)
	}
}

func subscriptionProviderHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok, err := store.VerifySubscriptionToken(r.Context(), chi.URLParam(r, "token"))
		if err != nil {
			http.Error(w, "subscription is unavailable", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		raw, err := render.RenderMihomoProxyProvider(r.Context(), store, token.ProxyUserName)
		if err != nil {
			http.Error(w, "subscription is unavailable", http.StatusUnprocessableEntity)
			return
		}
		writeProviderYAML(w, r, raw)
	}
}

func adminSubscriptionResponse(r *http.Request, token db.SubscriptionToken, active bool) adminUserSubscription {
	if !active {
		return adminUserSubscription{Active: false}
	}
	return adminUserSubscription{
		Active:     true,
		URL:        fmt.Sprintf("%s/sub/%s", requestBaseURL(r), token.Token),
		CreatedAt:  token.CreatedAt,
		LastUsedAt: nullString(token.LastUsedAt),
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
