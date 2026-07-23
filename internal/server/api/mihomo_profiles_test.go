package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestAdminMihomoProfileDraftPreviewPublishAndRollback(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})
	basic, err := store.GetMihomoRewriteTemplate(ctx, db.DefaultMihomoRewriteTemplateID)
	if err != nil {
		t.Fatal(err)
	}

	profile := adminJSON[db.MihomoProfile](t, router, http.MethodPost, "/api/admin/mihomo/profiles", map[string]any{
		"name":        "Custom",
		"description": "API lifecycle",
		"user":        "alice",
		"draft": map[string]any{"rewrites": []map[string]any{
			{"id": "basic", "template_id": basic.ID, "name": basic.Name, "kind": basic.Kind, "content": basic.Content, "enabled": true},
			{"id": "mode", "name": "Mode", "kind": "javascript", "content": `function main(config) { config.mode = "global"; return config }`, "enabled": true},
		}},
	})
	if profile.ID == "" || profile.PublishedRevisionID != "" {
		t.Fatalf("unexpected created profile: %#v", profile)
	}

	preview := adminJSON[mihomoPreviewResponse](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/preview", map[string]any{})
	if !strings.Contains(preview.YAML, "mode: global") || !strings.Contains(preview.YAML, "proxies:") {
		t.Fatalf("unexpected preview:\n%s", preview.YAML)
	}

	revision1 := adminJSON[db.MihomoProfileRevision](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/publish", map[string]any{})
	if revision1.Version != 1 {
		t.Fatalf("revision 1 = %#v", revision1)
	}
	localPreview := adminJSON[mihomoPreviewResponse](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/preview", map[string]any{
			"draft": map[string]any{"rewrites": []map[string]any{
				{"id": "basic", "template_id": basic.ID, "name": basic.Name, "kind": basic.Kind, "content": basic.Content, "enabled": true},
				{"id": "mode", "name": "Mode", "kind": "yaml", "content": "mode: direct\n", "enabled": true},
			}},
		})
	if !strings.Contains(localPreview.YAML, "mode: direct") ||
		!strings.Contains(localPreview.PublishedYAML, "mode: global") {
		t.Fatalf("preview did not compare local draft with publication: %#v", localPreview)
	}

	issued := adminJSON[adminMihomoProfileSubscription](t, router, http.MethodPost, "/api/admin/mihomo/profiles/"+profile.ID+"/subscription", nil)
	assertSubscriptionContains(t, router, issued.URL, "mode: global")

	draftV2 := map[string]any{"rewrites": []map[string]any{
		{"id": "basic", "template_id": basic.ID, "name": basic.Name, "kind": basic.Kind, "content": basic.Content, "enabled": true},
		{"id": "mode", "name": "Mode", "kind": "yaml", "content": "mode: direct\n", "enabled": true},
	}}
	adminJSON[db.MihomoProfile](t, router, http.MethodPatch, "/api/admin/mihomo/profiles/"+profile.ID, map[string]any{
		"draft": draftV2,
	})
	assertSubscriptionContains(t, router, issued.URL, "mode: global")

	revision2 := adminJSON[db.MihomoProfileRevision](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/publish", map[string]any{})
	if revision2.Version != 2 {
		t.Fatalf("revision 2 = %#v", revision2)
	}
	assertSubscriptionContains(t, router, issued.URL, "mode: direct")

	revisions := adminJSON[[]db.MihomoProfileRevision](t, router, http.MethodGet,
		"/api/admin/mihomo/profiles/"+profile.ID+"/revisions", nil)
	if len(revisions) != 2 || revisions[0].ID != revision2.ID || revisions[1].ID != revision1.ID {
		t.Fatalf("unexpected revisions: %#v", revisions)
	}
	adminJSON[db.MihomoProfile](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/rollback", map[string]any{"revision_id": revision1.ID})
	assertSubscriptionContains(t, router, issued.URL, "mode: global")
}

func TestAdminMihomoProfilePublishRejectsInvalidDraft(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	profile := adminJSON[db.MihomoProfile](t, router, http.MethodPost, "/api/admin/mihomo/profiles", map[string]any{
		"name": "Invalid rules", "user": "alice",
		"draft": map[string]any{"rewrites": []map[string]any{
			{"id": "rules", "name": "Rules", "kind": "yaml", "content": "proxy-groups:\n  - name: PROXY\n    type: select\n    proxies: [DIRECT]\nrules:\n  - GEOIP,CN,DIRECT\n", "enabled": true},
		}},
	})
	req := adminJSONRequest(t, http.MethodPost, "/api/admin/mihomo/profiles/"+profile.ID+"/publish", map[string]any{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "final rule must be MATCH") {
		t.Fatalf("invalid publish status = %d, body = %s", rec.Code, rec.Body.String())
	}
	stored, err := store.GetMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PublishedRevisionID != "" || stored.PublishedVersion != 0 {
		t.Fatalf("invalid draft was published: %#v", stored)
	}
}

func adminJSON[T any](t *testing.T, router http.Handler, method, path string, body any) T {
	t.Helper()
	req := adminJSONRequest(t, method, path, body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", method, path, rec.Code, rec.Body.String())
	}
	var result T
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSubscriptionContains(t *testing.T, router http.Handler, rawURL, pattern string) {
	t.Helper()
	path := strings.TrimPrefix(rawURL, "http://example.com")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), pattern) {
		t.Fatalf("GET %s status = %d, want body containing %q:\n%s", path, rec.Code, pattern, rec.Body.String())
	}
}
