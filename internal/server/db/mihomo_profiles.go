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
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	ProxyUserID         string                `json:"proxy_user_id"`
	ProxyUserName       string                `json:"proxy_user_name"`
	Draft               MihomoProfileDocument `json:"draft"`
	PublishedRevisionID string                `json:"published_revision_id"`
	PublishedVersion    int64                 `json:"published_version"`
	Published           MihomoProfileDocument `json:"published"`
	CreatedAt           string                `json:"created_at"`
	UpdatedAt           string                `json:"updated_at"`
}

type MihomoProfileRevision struct {
	ID        string                `json:"id"`
	ProfileID string                `json:"profile_id"`
	Version   int64                 `json:"version"`
	Document  MihomoProfileDocument `json:"document"`
	CreatedAt string                `json:"created_at"`
}

type CreateMihomoProfileParams struct {
	Name        string
	Description string
	UserName    string
	Draft       MihomoProfileDocument
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
	documentJSON, err := marshalMihomoProfileDocument(params.Draft)
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
	return mihomoProfileFromRow(
		row.ID,
		row.Name,
		row.Description,
		row.ProxyUserID,
		row.ProxyUserName,
		row.DraftDocumentJson,
		row.PublishedRevisionID,
		row.PublishedVersion,
		row.PublishedDocumentJson,
		row.CreatedAt,
		row.UpdatedAt,
	)
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
			row.DraftDocumentJson,
			row.PublishedRevisionID,
			row.PublishedVersion,
			row.PublishedDocumentJson,
			row.CreatedAt,
			row.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (db *DB) UpdateMihomoProfileDraft(
	ctx context.Context,
	profileID string,
	document MihomoProfileDocument,
) (MihomoProfile, error) {
	documentJSON, err := marshalMihomoProfileDocument(document)
	if err != nil {
		return MihomoProfile{}, err
	}
	affected, err := db.q.UpdateMihomoProfileDraft(ctx, store.UpdateMihomoProfileDraftParams{
		DraftDocumentJson: documentJSON,
		ID:                strings.TrimSpace(profileID),
	})
	if err != nil {
		return MihomoProfile{}, err
	}
	if err := requireAffected(affected, "Mihomo profile", profileID); err != nil {
		return MihomoProfile{}, err
	}
	return db.GetMihomoProfile(ctx, profileID)
}

func (db *DB) PublishMihomoProfile(ctx context.Context, profileID string) (MihomoProfileRevision, error) {
	profileID = strings.TrimSpace(profileID)
	var published MihomoProfileRevision
	err := db.withTx(ctx, func(q *store.Queries) error {
		profile, err := q.GetMihomoProfile(ctx, profileID)
		if err != nil {
			return err
		}
		document, err := unmarshalMihomoProfileDocument(profile.DraftDocumentJson)
		if err != nil {
			return err
		}
		if _, err := marshalMihomoProfileDocument(document); err != nil {
			return err
		}
		version, err := q.NextMihomoProfileVersion(ctx, profileID)
		if err != nil {
			return err
		}
		revisionID, err := id.New("mhpr")
		if err != nil {
			return err
		}
		if err := q.CreateMihomoProfileRevision(ctx, store.CreateMihomoProfileRevisionParams{
			ID: revisionID, ProfileID: profileID, Version: version, DocumentJson: profile.DraftDocumentJson,
		}); err != nil {
			return err
		}
		if err := q.PublishMihomoProfileRevision(ctx, store.PublishMihomoProfileRevisionParams{
			ProfileID: profileID, RevisionID: revisionID,
		}); err != nil {
			return err
		}
		row, err := q.GetMihomoProfileRevision(ctx, store.GetMihomoProfileRevisionParams{
			ID: revisionID, ProfileID: profileID,
		})
		if err != nil {
			return err
		}
		published, err = mihomoRevisionFromRow(row)
		return err
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoProfileRevision{}, fmt.Errorf("Mihomo profile %q not found", profileID)
		}
		return MihomoProfileRevision{}, err
	}
	return published, nil
}

func (db *DB) ListMihomoProfileRevisions(ctx context.Context, profileID string) ([]MihomoProfileRevision, error) {
	rows, err := db.q.ListMihomoProfileRevisions(ctx, strings.TrimSpace(profileID))
	if err != nil {
		return nil, err
	}
	revisions := make([]MihomoProfileRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := mihomoRevisionFromRow(row)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, nil
}

func (db *DB) RollbackMihomoProfile(ctx context.Context, profileID, revisionID string) (MihomoProfile, error) {
	profileID = strings.TrimSpace(profileID)
	revisionID = strings.TrimSpace(revisionID)
	if _, err := db.q.GetMihomoProfileRevision(ctx, store.GetMihomoProfileRevisionParams{
		ID: revisionID, ProfileID: profileID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoProfile{}, fmt.Errorf("revision %q does not belong to Mihomo profile %q", revisionID, profileID)
		}
		return MihomoProfile{}, err
	}
	if err := db.q.PublishMihomoProfileRevision(ctx, store.PublishMihomoProfileRevisionParams{
		ProfileID: profileID, RevisionID: revisionID,
	}); err != nil {
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
	if profile.PublishedRevisionID == "" {
		return fmt.Errorf("Mihomo profile %q has not been published", profile.Name)
	}
	return db.q.AssignMihomoProfileToUser(ctx, store.AssignMihomoProfileToUserParams{
		ProxyUserID: user.ID,
		ProfileID:   profile.ID,
	})
}

func (db *DB) GetPublishedMihomoProfileForUser(ctx context.Context, userName string) (MihomoProfileRevision, error) {
	profileID, err := db.q.GetMihomoProfileIDForUser(ctx, normalizeName(userName))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MihomoProfileRevision{}, fmt.Errorf("proxy user %q not found", userName)
		}
		return MihomoProfileRevision{}, err
	}
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return MihomoProfileRevision{}, err
	}
	if profile.PublishedRevisionID == "" {
		return MihomoProfileRevision{}, fmt.Errorf("Mihomo profile %q has not been published", profile.Name)
	}
	row, err := db.q.GetMihomoProfileRevision(ctx, store.GetMihomoProfileRevisionParams{
		ID: profile.PublishedRevisionID, ProfileID: profile.ID,
	})
	if err != nil {
		return MihomoProfileRevision{}, err
	}
	return mihomoRevisionFromRow(row)
}

func (db *DB) GetPublishedMihomoProfile(ctx context.Context, profileID string) (MihomoProfileRevision, error) {
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return MihomoProfileRevision{}, err
	}
	if profile.PublishedRevisionID == "" {
		return MihomoProfileRevision{}, fmt.Errorf("Mihomo profile %q has not been published", profile.Name)
	}
	row, err := db.q.GetMihomoProfileRevision(ctx, store.GetMihomoProfileRevisionParams{
		ID: profile.PublishedRevisionID, ProfileID: profile.ID,
	})
	if err != nil {
		return MihomoProfileRevision{}, err
	}
	return mihomoRevisionFromRow(row)
}

func mihomoProfileFromRow(
	id, name, description, proxyUserID, proxyUserName, draftJSON, publishedRevisionID string,
	publishedVersion int64,
	publishedJSON, createdAt, updatedAt string,
) (MihomoProfile, error) {
	draft, err := unmarshalMihomoProfileDocument(draftJSON)
	if err != nil {
		return MihomoProfile{}, fmt.Errorf("decode Mihomo profile %q draft: %w", name, err)
	}
	published, err := unmarshalMihomoProfileDocument(publishedJSON)
	if err != nil {
		return MihomoProfile{}, fmt.Errorf("decode Mihomo profile %q publication: %w", name, err)
	}
	return MihomoProfile{
		ID: id, Name: name, Description: description,
		ProxyUserID: proxyUserID, ProxyUserName: proxyUserName, Draft: draft,
		PublishedRevisionID: publishedRevisionID, PublishedVersion: publishedVersion,
		Published: published, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func mihomoRevisionFromRow(row store.MihomoProfileRevision) (MihomoProfileRevision, error) {
	document, err := unmarshalMihomoProfileDocument(row.DocumentJson)
	if err != nil {
		return MihomoProfileRevision{}, fmt.Errorf("decode Mihomo profile revision %q: %w", row.ID, err)
	}
	return MihomoProfileRevision{
		ID: row.ID, ProfileID: row.ProfileID, Version: row.Version,
		Document: document, CreatedAt: row.CreatedAt,
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
