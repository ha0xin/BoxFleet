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

func TestAdminMihomoProfileSavedDocumentPreview(t *testing.T) {
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
		"description": "Saved configuration",
		"user":        "alice",
		"document": map[string]any{"rewrites": []map[string]any{
			{"id": "basic", "template_id": basic.ID, "name": basic.Name, "kind": basic.Kind, "content": basic.Content, "enabled": true},
			{"id": "mode", "name": "Mode", "kind": "javascript", "content": `function main(config) { config.mode = "global"; return config }`, "enabled": true},
		}},
	})
	if profile.ID == "" || len(profile.Document.Rewrites) != 2 {
		t.Fatalf("unexpected created profile: %#v", profile)
	}

	preview := adminJSON[mihomoPreviewResponse](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/preview", map[string]any{})
	if !strings.Contains(preview.YAML, "mode: global") || !strings.Contains(preview.YAML, "proxies:") {
		t.Fatalf("unexpected preview:\n%s", preview.YAML)
	}
}

func TestAdminMihomoProfileSaveRejectsInvalidDocument(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	profile := adminJSON[db.MihomoProfile](t, router, http.MethodPost, "/api/admin/mihomo/profiles", map[string]any{
		"name": "Validated rules", "user": "alice",
		"document": map[string]any{"rewrites": []map[string]any{
			{"id": "rules", "name": "Rules", "kind": "yaml", "content": validMihomoRules("info"), "enabled": true},
		}},
	})
	req := adminJSONRequest(t, http.MethodPatch, "/api/admin/mihomo/profiles/"+profile.ID, map[string]any{
		"document": map[string]any{"rewrites": []map[string]any{
			{"id": "rules", "name": "Rules", "kind": "yaml", "content": "proxy-groups:\n  - name: PROXY\n    type: select\n    proxies: [DIRECT]\nrules:\n  - GEOIP,CN,DIRECT\n", "enabled": true},
		}},
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "final rule must be MATCH") {
		t.Fatalf("invalid save status = %d, body = %s", rec.Code, rec.Body.String())
	}
	stored, err := store.GetMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Document.Rewrites) != 1 || !strings.Contains(stored.Document.Rewrites[0].Content, "log-level: info") {
		t.Fatalf("invalid document replaced saved configuration: %#v", stored)
	}
}

func TestAdminMihomoProfileUsesSavedDocumentAndLatestTemplate(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AdminToken: "secret"})

	template := adminJSON[db.MihomoRewriteTemplate](t, router, http.MethodPost,
		"/api/admin/mihomo/rewrite-templates", map[string]any{
			"name": "Live rules", "kind": "yaml", "content": validMihomoRules("info"),
		})
	profile := adminJSON[db.MihomoProfile](t, router, http.MethodPost, "/api/admin/mihomo/profiles", map[string]any{
		"name": "Live configuration", "user": "alice",
		"document": map[string]any{"rewrites": []map[string]any{
			{"id": "rules", "template_id": template.ID, "name": template.Name, "kind": template.Kind, "content": template.Content, "enabled": true},
		}},
	})

	issued := adminJSON[adminMihomoProfileSubscription](t, router, http.MethodPost,
		"/api/admin/mihomo/profiles/"+profile.ID+"/subscription", nil)
	assertSubscriptionContains(t, router, issued.URL, "log-level: info")

	adminJSON[db.MihomoRewriteTemplate](t, router, http.MethodPatch,
		"/api/admin/mihomo/rewrite-templates/"+template.ID, map[string]any{
			"name": template.Name, "kind": "yaml", "content": validMihomoRules("debug"),
		})
	assertSubscriptionContains(t, router, issued.URL, "log-level: debug")

	adminJSON[db.MihomoProfile](t, router, http.MethodPatch,
		"/api/admin/mihomo/profiles/"+profile.ID, map[string]any{
			"document": map[string]any{"rewrites": []map[string]any{
				{"id": "custom", "name": "Custom rules", "kind": "yaml", "content": validMihomoRules("warning"), "enabled": true},
			}},
		})
	assertSubscriptionContains(t, router, issued.URL, "log-level: warning")

	req := adminJSONRequest(t, http.MethodPost, "/api/admin/mihomo/profiles/"+profile.ID+"/publish", map[string]any{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("removed publish endpoint status = %d, want 404", rec.Code)
	}
}

func validMihomoRules(logLevel string) string {
	return "log-level: " + logLevel + `
proxy-groups:
  - name: PROXY
    type: select
    proxies: [DIRECT]
    include-all-proxies: true
rules:
  - MATCH,PROXY
`
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
