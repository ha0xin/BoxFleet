package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/render"
)

func TestAdminPathGroupsPreviewGrantSourcesAndRevoke(t *testing.T) {
	ctx := context.Background()
	d := openAPITestDB(t)
	seedAPITestNode(t, ctx, d)
	u, err := d.CreateProxyUser(ctx, db.CreateProxyUserParams{Name: "bob-groups"})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := d.ListPathsForAdmin(ctx)
	if err != nil || len(paths) == 0 {
		t.Fatalf("paths: %v %v", paths, err)
	}
	router := NewRouter(Options{DB: d, AdminToken: "secret"})
	change := func(c db.PathGroupChange) db.PathGroupImpact {
		t.Helper()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPost, "/api/admin/path-groups/preview", c))
		if rec.Code != 200 {
			t.Fatalf("preview %d %s", rec.Code, rec.Body.String())
		}
		var p db.PathGroupImpact
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		c.PreviewToken = p.PreviewToken
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPost, "/api/admin/path-groups/changes", c))
		if rec.Code != 200 {
			t.Fatalf("change %d %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	created := change(db.PathGroupChange{Operation: "save", Name: "API group", Enabled: true, PathIDs: []string{paths[0].ID}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/path-groups", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status %d", rec.Code)
	}
	// No write without a matching reviewed token.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPost, "/api/admin/path-groups/changes", db.PathGroupChange{Operation: "save", GroupID: created.GroupID, Name: "API group", Enabled: true, Revision: 1, PathIDs: []string{paths[0].ID}}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("unreviewed mutation %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPatch, "/api/admin/users/"+u.ID+"/paths", map[string]any{"changes": []db.PathSelectionChange{{PathID: paths[0].ID, Enabled: true}}}))
	if rec.Code != 200 {
		t.Fatalf("selection %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodPost, "/api/admin/path-groups/preview", db.PathGroupChange{Operation: "grant", GroupID: created.GroupID, UserID: u.ID}))
	if rec.Code != http.StatusGone {
		t.Fatalf("legacy group grant accepted: %d", rec.Code)
	}
	config, err := render.RenderNodeConfig(ctx, d, "azus")
	if err != nil || !strings.Contains(string(config), "@bob-groups") {
		t.Fatalf("group credential not rendered: %s %v", config, err)
	}
	client, err := render.NodeInfoForUser(ctx, d, u.Name, "azus")
	if err != nil || len(client.Proxies) != 1 {
		t.Fatalf("group Path not published: %+v %v", client, err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, adminJSONRequest(t, http.MethodGet, "/api/admin/users/"+u.ID+"/paths", nil))
	var accesses []adminPathAccess
	if rec.Code != 200 {
		t.Fatalf("access status %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &accesses); err != nil {
		t.Fatal(err)
	}
	if len(accesses) != 1 || accesses[0].DirectGrant == nil || !*accesses[0].DirectGrant || len(accesses[0].Groups) != 0 {
		t.Fatalf("group sources missing: %+v", accesses)
	}
	if _, err := d.GrantPathToUser(ctx, u.ID, paths[0].ID); err != nil {
		t.Fatal(err)
	}
	change(db.PathGroupChange{Operation: "save", GroupID: created.GroupID, Name: "API group", Revision: 1, Enabled: false, PathIDs: []string{}})
	effective, err := d.ListActivePathAccessesByUser(ctx, u.ID)
	if err != nil || len(effective) != 1 {
		t.Fatalf("direct grant lost: %+v %v", effective, err)
	}
	if _, err = d.RevokePathAccess(ctx, u.ID, paths[0].ID); err != nil {
		t.Fatal(err)
	}
	config, err = render.RenderNodeConfig(ctx, d, "azus")
	if err != nil || strings.Contains(string(config), "@bob-groups") {
		t.Fatalf("revoked group/direct credential still rendered: %s %v", config, err)
	}
}
