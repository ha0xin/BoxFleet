package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const DefaultMihomoProfileID = "mhp_default"
const DefaultMihomoRewriteTemplateID = "mhrt_basic"

const maxMihomoRewriteSourceBytes = 256 << 10

type MihomoRewrite struct {
	ID         string `json:"id"`
	TemplateID string `json:"template_id,omitempty"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Enabled    bool   `json:"enabled"`
}

type MihomoProfileDocument struct {
	Rewrites []MihomoRewrite `json:"rewrites"`
}

type MihomoProfile struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	ProxyUserID   string                `json:"proxy_user_id"`
	ProxyUserName string                `json:"proxy_user_name"`
	Document      MihomoProfileDocument `json:"document"`
	CreatedAt     string                `json:"created_at"`
	UpdatedAt     string                `json:"updated_at"`
}

type CreateMihomoProfileParams struct {
	Name        string
	Description string
	UserName    string
	Document    MihomoProfileDocument
}

func (db *DB) CreateMihomoProfile(ctx context.Context, params CreateMihomoProfileParams) (MihomoProfile, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return MihomoProfile{}, errors.New("Mihomo profile name is required")
	}
	user, err := db.GetProxyUser(ctx, params.UserName)
	if err != nil {
		return MihomoProfile{}, err
	}
	document, err := db.ResolveMihomoProfileDocument(ctx, params.Document)
	if err != nil {
		return MihomoProfile{}, err
	}
	documentJSON, err := marshalMihomoProfileDocument(document)
	if err != nil {
		return MihomoProfile{}, err
	}
	profileID, err := id.New("mhp")
	if err != nil {
		return MihomoProfile{}, err
	}
	if err := db.q.CreateMihomoProfile(ctx, store.CreateMihomoProfileParams{
		ID:                profileID,
		Name:              name,
		Description:       strings.TrimSpace(params.Description),
		DraftDocumentJson: documentJSON,
		ProxyUserID:       sql.NullString{String: user.ID, Valid: true},
	}); err != nil {
		return MihomoProfile{}, err
	}
	return db.GetMihomoProfile(ctx, profileID)
}

func (db *DB) GetMihomoProfile(ctx context.Context, profileID string) (MihomoProfile, error) {
	row, err := db.q.GetMihomoProfile(ctx, strings.TrimSpace(profileID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoProfile{}, fmt.Errorf("Mihomo profile %q not found", profileID)
		}
		return MihomoProfile{}, err
	}
	profile, err := mihomoProfileFromRow(
		row.ID,
		row.Name,
		row.Description,
		row.ProxyUserID,
		row.ProxyUserName,
		row.DocumentJson,
		row.CreatedAt,
		row.UpdatedAt,
	)
	if err != nil {
		return MihomoProfile{}, err
	}
	profile.Document, err = db.ResolveMihomoProfileDocument(ctx, profile.Document)
	if err != nil {
		return MihomoProfile{}, fmt.Errorf("resolve Mihomo profile %q templates: %w", profile.Name, err)
	}
	return profile, nil
}

func (db *DB) ListMihomoProfiles(ctx context.Context) ([]MihomoProfile, error) {
	rows, err := db.q.ListMihomoProfiles(ctx)
	if err != nil {
		return nil, err
	}
	profiles := make([]MihomoProfile, 0, len(rows))
	for _, row := range rows {
		profile, err := mihomoProfileFromRow(
			row.ID,
			row.Name,
			row.Description,
			row.ProxyUserID,
			row.ProxyUserName,
			row.DocumentJson,
			row.CreatedAt,
			row.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		profile.Document, err = db.ResolveMihomoProfileDocument(ctx, profile.Document)
		if err != nil {
			return nil, fmt.Errorf("resolve Mihomo profile %q templates: %w", profile.Name, err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (db *DB) UpdateMihomoProfileDocument(
	ctx context.Context,
	profileID string,
	document MihomoProfileDocument,
) (MihomoProfile, error) {
	resolved, err := db.ResolveMihomoProfileDocument(ctx, document)
	if err != nil {
		return MihomoProfile{}, err
	}
	documentJSON, err := marshalMihomoProfileDocument(resolved)
	if err != nil {
		return MihomoProfile{}, err
	}
	affected, err := db.q.UpdateMihomoProfileDocument(ctx, store.UpdateMihomoProfileDocumentParams{
		DocumentJson: documentJSON,
		ID:           strings.TrimSpace(profileID),
	})
	if err != nil {
		return MihomoProfile{}, err
	}
	if err := requireAffected(affected, "Mihomo profile", profileID); err != nil {
		return MihomoProfile{}, err
	}
	return db.GetMihomoProfile(ctx, profileID)
}

func (db *DB) AssignMihomoProfileToUser(ctx context.Context, userName, profileID string) error {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return err
	}
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return err
	}
	return db.q.AssignMihomoProfileToUser(ctx, store.AssignMihomoProfileToUserParams{
		ProxyUserID: user.ID,
		ProfileID:   profile.ID,
	})
}

func (db *DB) GetMihomoProfileDocumentForUser(ctx context.Context, userName string) (MihomoProfileDocument, error) {
	profileID, err := db.q.GetMihomoProfileIDForUser(ctx, normalizeName(userName))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoProfileDocument{}, fmt.Errorf("proxy user %q not found", userName)
		}
		return MihomoProfileDocument{}, err
	}
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return MihomoProfileDocument{}, err
	}
	return profile.Document, nil
}

func mihomoProfileFromRow(
	id, name, description, proxyUserID, proxyUserName, documentJSON, createdAt, updatedAt string,
) (MihomoProfile, error) {
	document, err := unmarshalMihomoProfileDocument(documentJSON)
	if err != nil {
		return MihomoProfile{}, fmt.Errorf("decode Mihomo profile %q document: %w", name, err)
	}
	return MihomoProfile{
		ID: id, Name: name, Description: description,
		ProxyUserID: proxyUserID, ProxyUserName: proxyUserName, Document: document,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func marshalMihomoProfileDocument(document MihomoProfileDocument) (string, error) {
	if document.Rewrites == nil {
		document.Rewrites = []MihomoRewrite{}
	}
	seen := make(map[string]struct{}, len(document.Rewrites))
	for index, rewrite := range document.Rewrites {
		if strings.TrimSpace(rewrite.ID) == "" {
			return "", fmt.Errorf("Mihomo rewrite %d is missing an id", index+1)
		}
		if _, exists := seen[rewrite.ID]; exists {
			return "", fmt.Errorf("duplicate Mihomo rewrite id %q", rewrite.ID)
		}
		seen[rewrite.ID] = struct{}{}
		if strings.TrimSpace(rewrite.Name) == "" {
			return "", fmt.Errorf("Mihomo rewrite %q is missing a name", rewrite.ID)
		}
		if rewrite.Kind != "yaml" && rewrite.Kind != "javascript" {
			return "", fmt.Errorf("Mihomo rewrite %q has unsupported kind %q", rewrite.ID, rewrite.Kind)
		}
		if len(rewrite.Content) > maxMihomoRewriteSourceBytes {
			return "", fmt.Errorf("Mihomo rewrite %q source is too large", rewrite.ID)
		}
	}
	raw, err := json.Marshal(document)
	return string(raw), err
}

func unmarshalMihomoProfileDocument(raw string) (MihomoProfileDocument, error) {
	var document MihomoProfileDocument
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return MihomoProfileDocument{}, err
	}
	if _, err := marshalMihomoProfileDocument(document); err != nil {
		return MihomoProfileDocument{}, err
	}
	return document, nil
}
