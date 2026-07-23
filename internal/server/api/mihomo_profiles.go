package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/mihomo"
	"github.com/haoxin/boxfleet/internal/server/render"
)

type createMihomoProfileRequest struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	User        string                   `json:"user"`
	Draft       db.MihomoProfileDocument `json:"draft"`
}

type updateMihomoProfileRequest struct {
	Draft db.MihomoProfileDocument `json:"draft"`
}

type mihomoProfileUserRequest struct {
	User  string                    `json:"user"`
	Draft *db.MihomoProfileDocument `json:"draft,omitempty"`
}

type mihomoProfileRollbackRequest struct {
	RevisionID string `json:"revision_id"`
}

type mihomoRewriteTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
}

type assignMihomoProfileRequest struct {
	ProfileID string `json:"profile_id"`
}

type mihomoPreviewResponse struct {
	YAML          string              `json:"yaml"`
	PublishedYAML string              `json:"published_yaml"`
	Logs          []mihomo.LogEntry   `json:"logs"`
	Diagnostics   []mihomo.Diagnostic `json:"diagnostics"`
}

func adminListMihomoProfilesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profiles, err := store.ListMihomoProfiles(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profiles)
	}
}

func adminListMihomoRewriteTemplatesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templates, err := store.ListMihomoRewriteTemplates(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, templates)
	}
}

func adminCreateMihomoRewriteTemplateHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoRewriteTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		template, err := store.CreateMihomoRewriteTemplate(r.Context(), db.CreateMihomoRewriteTemplateParams{
			Name: payload.Name, Description: payload.Description, Kind: payload.Kind, Content: payload.Content,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, template)
	}
}

func adminUpdateMihomoRewriteTemplateHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoRewriteTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		template, err := store.UpdateMihomoRewriteTemplate(r.Context(), chi.URLParam(r, "template"), db.UpdateMihomoRewriteTemplateParams{
			Name: payload.Name, Description: payload.Description, Kind: payload.Kind, Content: payload.Content,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, template)
	}
}

func adminCreateMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload createMihomoProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.CreateMihomoProfile(r.Context(), db.CreateMihomoProfileParams{
			Name: payload.Name, Description: payload.Description, UserName: payload.User, Draft: payload.Draft,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func adminMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, err := store.GetMihomoProfile(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func adminUpdateMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload updateMihomoProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.UpdateMihomoProfileDraft(r.Context(), chi.URLParam(r, "profile"), payload.Draft)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func adminPreviewMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoProfileUserRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.GetMihomoProfile(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		draft := profile.Draft
		if payload.Draft != nil {
			draft = *payload.Draft
		}
		result, err := render.RenderMihomoConfiguration(r.Context(), store, profile.ProxyUserName, draft)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		publishedYAML := ""
		if profile.PublishedRevisionID != "" {
			published, publishedErr := render.RenderMihomoConfiguration(r.Context(), store, profile.ProxyUserName, profile.Published)
			if publishedErr != nil {
				writeAdminError(w, publishedErr)
				return
			}
			publishedYAML = string(published.YAML)
		}
		writeJSON(w, mihomoPreviewResponse{
			YAML: string(result.YAML), PublishedYAML: publishedYAML,
			Logs: result.Logs, Diagnostics: result.Diagnostics,
		})
	}
}

func adminPublishMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoProfileUserRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profileID := chi.URLParam(r, "profile")
		profile, err := store.GetMihomoProfile(r.Context(), profileID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		result, err := render.RenderMihomoConfiguration(r.Context(), store, profile.ProxyUserName, profile.Draft)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if mihomo.HasErrors(result.Diagnostics) {
			writeAdminError(w, &mihomoProfileValidationError{Diagnostics: result.Diagnostics})
			return
		}
		revision, err := store.PublishMihomoProfile(r.Context(), profileID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, revision)
	}
}

type mihomoProfileValidationError struct {
	Diagnostics []mihomo.Diagnostic
}

func (e *mihomoProfileValidationError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "Mihomo profile validation failed"
	}
	return "Mihomo profile validation failed: " + e.Diagnostics[0].Message
}

func adminListMihomoProfileRevisionsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		revisions, err := store.ListMihomoProfileRevisions(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, revisions)
	}
}

func adminRollbackMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoProfileRollbackRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.RollbackMihomoProfile(r.Context(), chi.URLParam(r, "profile"), payload.RevisionID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func adminAssignUserMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload assignMihomoProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		userName := chi.URLParam(r, "user")
		if err := store.AssignMihomoProfileToUser(r.Context(), userName, payload.ProfileID); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.GetMihomoProfile(r.Context(), payload.ProfileID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func enabledMihomoRewrites(document db.MihomoProfileDocument) []mihomo.Rewrite {
	rewrites := make([]mihomo.Rewrite, 0, len(document.Rewrites))
	for _, rewrite := range document.Rewrites {
		if !rewrite.Enabled {
			continue
		}
		rewrites = append(rewrites, mihomo.Rewrite{
			Name: rewrite.Name, Kind: mihomo.RewriteKind(rewrite.Kind), Content: rewrite.Content,
		})
	}
	return rewrites
}
