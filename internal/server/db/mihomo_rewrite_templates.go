package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type MihomoRewriteTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Content     string `json:"content"`
	BuiltIn     bool   `json:"built_in"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateMihomoRewriteTemplateParams struct {
	Name        string
	Description string
	Kind        string
	Content     string
}

type UpdateMihomoRewriteTemplateParams = CreateMihomoRewriteTemplateParams

func (db *DB) CreateMihomoRewriteTemplate(ctx context.Context, params CreateMihomoRewriteTemplateParams) (MihomoRewriteTemplate, error) {
	if err := validateMihomoRewriteTemplate(params.Name, params.Kind, params.Content); err != nil {
		return MihomoRewriteTemplate{}, err
	}
	templateID, err := id.New("mhrt")
	if err != nil {
		return MihomoRewriteTemplate{}, err
	}
	if err := db.q.CreateMihomoRewriteTemplate(ctx, store.CreateMihomoRewriteTemplateParams{
		ID: templateID, Name: strings.TrimSpace(params.Name), Description: strings.TrimSpace(params.Description),
		Kind: params.Kind, Content: params.Content,
	}); err != nil {
		return MihomoRewriteTemplate{}, err
	}
	return db.GetMihomoRewriteTemplate(ctx, templateID)
}

func (db *DB) GetMihomoRewriteTemplate(ctx context.Context, templateID string) (MihomoRewriteTemplate, error) {
	row, err := db.q.GetMihomoRewriteTemplate(ctx, strings.TrimSpace(templateID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoRewriteTemplate{}, fmt.Errorf("Mihomo rewrite template %q not found", templateID)
		}
		return MihomoRewriteTemplate{}, err
	}
	return mihomoRewriteTemplateFromRow(row), nil
}

func (db *DB) ListMihomoRewriteTemplates(ctx context.Context) ([]MihomoRewriteTemplate, error) {
	rows, err := db.q.ListMihomoRewriteTemplates(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]MihomoRewriteTemplate, 0, len(rows))
	for _, row := range rows {
		result = append(result, mihomoRewriteTemplateFromRow(row))
	}
	return result, nil
}

// ResolveMihomoProfileDocument replaces every template-backed processor's
// display metadata and source with the template's current values. The profile
// retains only ordering, enabled state, and the template reference as its own
// configuration; template edits therefore affect the next preview or request.
func (db *DB) ResolveMihomoProfileDocument(
	ctx context.Context,
	document MihomoProfileDocument,
) (MihomoProfileDocument, error) {
	resolved := MihomoProfileDocument{Rewrites: make([]MihomoRewrite, len(document.Rewrites))}
	copy(resolved.Rewrites, document.Rewrites)
	for index := range resolved.Rewrites {
		rewrite := &resolved.Rewrites[index]
		if strings.TrimSpace(rewrite.TemplateID) == "" {
			continue
		}
		template, err := db.GetMihomoRewriteTemplate(ctx, rewrite.TemplateID)
		if err != nil {
			return MihomoProfileDocument{}, err
		}
		rewrite.Name = template.Name
		rewrite.Kind = template.Kind
		rewrite.Content = template.Content
	}
	return resolved, nil
}

func (db *DB) UpdateMihomoRewriteTemplate(ctx context.Context, templateID string, params UpdateMihomoRewriteTemplateParams) (MihomoRewriteTemplate, error) {
	if err := validateMihomoRewriteTemplate(params.Name, params.Kind, params.Content); err != nil {
		return MihomoRewriteTemplate{}, err
	}
	current, err := db.GetMihomoRewriteTemplate(ctx, templateID)
	if err != nil {
		return MihomoRewriteTemplate{}, err
	}
	if current.BuiltIn {
		return MihomoRewriteTemplate{}, fmt.Errorf("built-in Mihomo rewrite template %q is immutable", current.Name)
	}
	affected, err := db.q.UpdateMihomoRewriteTemplate(ctx, store.UpdateMihomoRewriteTemplateParams{
		ID: current.ID, Name: strings.TrimSpace(params.Name), Description: strings.TrimSpace(params.Description),
		Kind: params.Kind, Content: params.Content,
	})
	if err != nil {
		return MihomoRewriteTemplate{}, err
	}
	if err := requireAffected(affected, "Mihomo rewrite template", templateID); err != nil {
		return MihomoRewriteTemplate{}, err
	}
	return db.GetMihomoRewriteTemplate(ctx, current.ID)
}

func validateMihomoRewriteTemplate(name, kind, content string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("Mihomo rewrite template name is required")
	}
	if kind != "yaml" && kind != "javascript" {
		return fmt.Errorf("unsupported Mihomo rewrite template kind %q", kind)
	}
	if len(content) > maxMihomoRewriteSourceBytes {
		return errors.New("Mihomo rewrite template source is too large")
	}
	return nil
}

func mihomoRewriteTemplateFromRow(row store.MihomoRewriteTemplate) MihomoRewriteTemplate {
	return MihomoRewriteTemplate{
		ID: row.ID, Name: row.Name, Description: row.Description, Kind: row.Kind,
		Content: row.Content, BuiltIn: int64ToBool(row.BuiltIn), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
