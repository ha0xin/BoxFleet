package db

import (
	"context"
	"reflect"
	"testing"
)

func TestDefaultMihomoProfileIsPublished(t *testing.T) {
	store := openTestDB(t)
	profile, err := store.GetMihomoProfile(context.Background(), DefaultMihomoProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != "Default" || profile.PublishedRevisionID == "" || profile.PublishedVersion != 1 {
		t.Fatalf("unexpected default profile: %#v", profile)
	}
	if len(profile.Draft.Rewrites) != 0 || len(profile.Published.Rewrites) != 0 {
		t.Fatalf("default profile should rely on the built-in Basic rewrite: %#v", profile)
	}
}

func TestMihomoProfilePublishRollbackAndUserBinding(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateProxyUser(ctx, CreateProxyUserParams{Name: "alice"}); err != nil {
		t.Fatal(err)
	}

	draftV1 := MihomoProfileDocument{Rewrites: []MihomoRewrite{
		{ID: "dns", Name: "DNS", Kind: "yaml", Content: "dns:\n  ipv6: false\n", Enabled: true},
	}}
	profile, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{
		Name: "China optimized", Description: "test profile", UserName: "alice", Draft: draftV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.PublishedRevisionID != "" || profile.ProxyUserName != "alice" || !reflect.DeepEqual(profile.Draft, draftV1) {
		t.Fatalf("unexpected new profile: %#v", profile)
	}
	second, err := store.CreateMihomoProfile(ctx, CreateMihomoProfileParams{
		Name: "Alice mobile", UserName: "alice", Draft: MihomoProfileDocument{},
	})
	if err != nil || second.ProxyUserID != profile.ProxyUserID || second.ID == profile.ID {
		t.Fatalf("same user should own multiple configurations: %#v, %v", second, err)
	}

	revision1, err := store.PublishMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revision1.Version != 1 || !reflect.DeepEqual(revision1.Document, draftV1) {
		t.Fatalf("unexpected revision 1: %#v", revision1)
	}

	draftV2 := MihomoProfileDocument{Rewrites: []MihomoRewrite{
		{ID: "dns", Name: "DNS", Kind: "yaml", Content: "dns:\n  ipv6: true\n", Enabled: true},
		{ID: "filter", Name: "Filter", Kind: "javascript", Content: "function main(config) { return config }", Enabled: true},
	}}
	if _, err := store.UpdateMihomoProfileDraft(ctx, profile.ID, draftV2); err != nil {
		t.Fatal(err)
	}
	beforePublish, err := store.GetMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforePublish.Published, draftV1) {
		t.Fatalf("editing draft changed published document: %#v", beforePublish)
	}

	revision2, err := store.PublishMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revision2.Version != 2 || !reflect.DeepEqual(revision2.Document, draftV2) {
		t.Fatalf("unexpected revision 2: %#v", revision2)
	}
	revisions, err := store.ListMihomoProfileRevisions(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].Version != 2 || revisions[1].Version != 1 {
		t.Fatalf("unexpected revision history: %#v", revisions)
	}

	rolledBack, err := store.RollbackMihomoProfile(ctx, profile.ID, revision1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.PublishedVersion != 1 || !reflect.DeepEqual(rolledBack.Published, draftV1) {
		t.Fatalf("unexpected rollback result: %#v", rolledBack)
	}
	resolved, err := store.GetPublishedMihomoProfile(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ProfileID != profile.ID || resolved.Version != 1 || !reflect.DeepEqual(resolved.Document, draftV1) {
		t.Fatalf("unexpected user profile: %#v", resolved)
	}
}

func TestMihomoProfileDocumentValidation(t *testing.T) {
	store := openTestDB(t)
	_, err := store.CreateMihomoProfile(context.Background(), CreateMihomoProfileParams{
		Name: "invalid", UserName: "missing-user",
		Draft: MihomoProfileDocument{Rewrites: []MihomoRewrite{
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
	updated, err := store.UpdateMihomoRewriteTemplate(ctx, custom.ID, UpdateMihomoRewriteTemplateParams{
		Name: custom.Name, Description: custom.Description, Kind: custom.Kind, Content: "mode: global\n",
	})
	if err != nil || updated.Content != "mode: global\n" {
		t.Fatalf("updated template = %#v, %v", updated, err)
	}
	if _, err := store.UpdateMihomoRewriteTemplate(ctx, DefaultMihomoRewriteTemplateID, UpdateMihomoRewriteTemplateParams{
		Name: "changed", Kind: "yaml", Content: "{}",
	}); err == nil {
		t.Fatal("built-in template should be immutable")
	}
}
