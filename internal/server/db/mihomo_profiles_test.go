package db

import (
	"context"
	"reflect"
	"testing"
)

func TestDefaultMihomoProfileHasEmptySavedDocument(t *testing.T) {
	store := openTestDB(t)
	profile, err := store.GetMihomoProfile(context.Background(), DefaultMihomoProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Default" {
		t.Fatalf("unexpected default profile: %#v", profile)
	}
	if len(profile.Document.Rewrites) != 0 {
		t.Fatalf("default profile should rely on the built-in Basic rewrite: %#v", profile)
	}
}

func TestMihomoProfileSaveAndUserBinding(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}

	documentV1 := MihomoProfileDocument{Rewrites: []MihomoRewrite{
		{ID: "dns", Name: "DNS", Kind: "yaml", Content: "dns:\n  ipv6: false\n", Enabled: true},
	}}
	profile, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{
		Name: "China optimized", Description: "test profile", UserName: "alice", Document: documentV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ProxyUserName != "alice" || !reflect.DeepEqual(profile.Document, documentV1) {
		t.Fatalf("unexpected new profile: %#v", profile)
	}
	second, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{
		Name: "Alice mobile", UserName: "alice", Document: MihomoProfileDocument{},
	})
	if err != nil || second.ProxyUserID != profile.ProxyUserID || second.ID == profile.ID {
		t.Fatalf("same user should own multiple configurations: %#v, %v", second, err)
	}

	documentV2 := MihomoProfileDocument{Rewrites: []MihomoRewrite{
		{ID: "dns", Name: "DNS", Kind: "yaml", Content: "dns:\n  ipv6: true\n", Enabled: true},
		{ID: "filter", Name: "Filter", Kind: "javascript", Content: "function main(config) { return config }", Enabled: true},
	}}
	if _, err := store.UpdateMihomoProfileDocument(ctx, profile.ID, documentV2); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Document, documentV2) {
		t.Fatalf("saved document = %#v, want %#v", saved.Document, documentV2)
	}
}

func TestMihomoProfileDocumentValidation(t *testing.T) {
	store := openTestDB(t)
	_, err := store.CreateMihomoProfile(context.Background(), CreateMihomoProfileParams{
		Name: "invalid", UserName: "missing-user",
		Document: MihomoProfileDocument{Rewrites: []MihomoRewrite{
			{ID: "same", Name: "One", Kind: "yaml", Content: "{}", Enabled: true},
			{ID: "same", Name: "Two", Kind: "lua", Content: "{}", Enabled: true},
		}},
	})
	if err == nil {
		t.Fatal("invalid rewrite document was accepted")
	}
}

func TestMihomoRewriteTemplateLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	templates, err := store.ListMihomoRewriteTemplates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) == 0 || templates[0].ID != DefaultMihomoRewriteTemplateID || !templates[0].BuiltIn {
		t.Fatalf("built-in template missing: %#v", templates)
	}
	custom, err := store.CreateMihomoRewriteTemplate(ctx, CreateMihomoRewriteTemplateParams{
		Name: "Custom routing", Description: "Scoped copy source", Kind: "yaml", Content: "mode: rule\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{
		Name: "Template reference", UserName: "alice",
		Document: MihomoProfileDocument{Rewrites: []MihomoRewrite{{
			ID: "template", TemplateID: custom.ID, Name: custom.Name, Kind: custom.Kind, Content: custom.Content, Enabled: true,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateMihomoRewriteTemplate(ctx, custom.ID, UpdateMihomoRewriteTemplateParams{
		Name: custom.Name, Description: custom.Description, Kind: custom.Kind, Content: "mode: global\n",
	})
	if err != nil || updated.Content != "mode: global\n" {
		t.Fatalf("updated template = %#v, %v", updated, err)
	}
	resolved, err := store.GetMihomoProfile(ctx, profile.ID)
	if err != nil || resolved.Document.Rewrites[0].Content != "mode: global\n" {
		t.Fatalf("profile did not resolve latest template: %#v, %v", resolved, err)
	}
	if _, err := store.UpdateMihomoRewriteTemplate(ctx, DefaultMihomoRewriteTemplateID, UpdateMihomoRewriteTemplateParams{
		Name: "changed", Kind: "yaml", Content: "{}",
	}); err == nil {
		t.Fatal("built-in template should be immutable")
	}
}
