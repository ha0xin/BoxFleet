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
	Document    db.MihomoProfileDocument `json:"document"`
}

type updateMihomoProfileRequest struct {
	Document db.MihomoProfileDocument `json:"document"`
}

type mihomoProfilePreviewRequest struct {
	Document *db.MihomoProfileDocument `json:"document,omitempty"`
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
	YAML        string              `json:"yaml"`
	Logs        []mihomo.LogEntry   `json:"logs"`
	Diagnostics []mihomo.Diagnostic `json:"diagnostics"`
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
		if err := validateSavedMihomoProfile(r, store, payload.User, payload.Document); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.CreateMihomoProfile(r.Context(), db.CreateMihomoProfileParams{
			Name: payload.Name, Description: payload.Description, UserName: payload.User, Document: payload.Document,
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
		profileID := chi.URLParam(r, "profile")
		current, err := store.GetMihomoProfile(r.Context(), profileID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if err := validateSavedMihomoProfile(r, store, current.ProxyUserName, payload.Document); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.UpdateMihomoProfileDocument(r.Context(), profileID, payload.Document)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, profile)
	}
}

func adminPreviewMihomoProfileHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload mihomoProfilePreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeAdminError(w, err)
			return
		}
		profile, err := store.GetMihomoProfile(r.Context(), chi.URLParam(r, "profile"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		document := profile.Document
		if payload.Document != nil {
			document = *payload.Document
		}
		result, err := render.RenderMihomoConfiguration(r.Context(), store, profile.ProxyUserName, document)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, mihomoPreviewResponse{
			YAML: string(result.YAML), Logs: result.Logs, Diagnostics: result.Diagnostics,
		})
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

func validateSavedMihomoProfile(r *http.Request, store *db.DB, userName string, document db.MihomoProfileDocument) error {
	result, err := render.RenderMihomoConfiguration(r.Context(), store, userName, document)
	if err != nil {
		return err
	}
	if mihomo.HasErrors(result.Diagnostics) {
		return &mihomoProfileValidationError{Diagnostics: result.Diagnostics}
	}
	return nil
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
