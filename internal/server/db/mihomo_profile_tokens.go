package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type MihomoProfileSubscriptionToken struct {
	ID            string         `json:"id"`
	ProfileID     string         `json:"profile_id"`
	ProfileName   string         `json:"profile_name"`
	ProxyUserID   string         `json:"proxy_user_id"`
	ProxyUserName string         `json:"proxy_user_name"`
	Token         string         `json:"token"`
	CreatedAt     string         `json:"created_at"`
	LastUsedAt    sql.NullString `json:"last_used_at"`
	RevokedAt     sql.NullString `json:"revoked_at"`
}

func (db *DB) IssueMihomoProfileSubscriptionToken(ctx context.Context, profileID string) (MihomoProfileSubscriptionToken, error) {
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return MihomoProfileSubscriptionToken{}, err
	}
	if profile.ProxyUserID == "" {
		return MihomoProfileSubscriptionToken{}, fmt.Errorf("Mihomo profile %q has no proxy user", profile.Name)
	}
	if profile.PublishedRevisionID == "" {
		return MihomoProfileSubscriptionToken{}, fmt.Errorf("Mihomo profile %q has not been published", profile.Name)
	}
	tokenID, rawToken, err := newSubscriptionToken()
	if err != nil {
		return MihomoProfileSubscriptionToken{}, err
	}
	var issued MihomoProfileSubscriptionToken
	err = db.withTx(ctx, func(q *store.Queries) error {
		if _, err := q.GetActiveMihomoProfileSubscriptionToken(ctx, profile.ID); err == nil {
			return ErrActiveSubscriptionTokenExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := q.CreateMihomoProfileSubscriptionToken(ctx, store.CreateMihomoProfileSubscriptionTokenParams{
			ID: tokenID, ProfileID: profile.ID, Token: rawToken,
		}); err != nil {
			return err
		}
		row, err := q.GetActiveMihomoProfileSubscriptionToken(ctx, profile.ID)
		if err != nil {
			return err
		}
		issued = mihomoProfileTokenFromFields(row.ID, row.ProfileID, row.ProfileName, row.ProxyUserID.String,
			row.ProxyUserName, row.Token, row.CreatedAt, row.LastUsedAt, row.RevokedAt)
		return nil
	})
	return issued, err
}

func (db *DB) GetActiveMihomoProfileSubscriptionToken(ctx context.Context, profileID string) (MihomoProfileSubscriptionToken, bool, error) {
	row, err := db.q.GetActiveMihomoProfileSubscriptionToken(ctx, strings.TrimSpace(profileID))
	if errors.Is(err, sql.ErrNoRows) {
		return MihomoProfileSubscriptionToken{}, false, nil
	}
	if err != nil {
		return MihomoProfileSubscriptionToken{}, false, err
	}
	return mihomoProfileTokenFromFields(row.ID, row.ProfileID, row.ProfileName, row.ProxyUserID.String,
		row.ProxyUserName, row.Token, row.CreatedAt, row.LastUsedAt, row.RevokedAt), true, nil
}

func (db *DB) RotateMihomoProfileSubscriptionToken(ctx context.Context, profileID string) (MihomoProfileSubscriptionToken, error) {
	profile, err := db.GetMihomoProfile(ctx, profileID)
	if err != nil {
		return MihomoProfileSubscriptionToken{}, err
	}
	tokenID, rawToken, err := newSubscriptionToken()
	if err != nil {
		return MihomoProfileSubscriptionToken{}, err
	}
	var rotated MihomoProfileSubscriptionToken
	err = db.withTx(ctx, func(q *store.Queries) error {
		affected, err := q.RevokeActiveMihomoProfileSubscriptionToken(ctx, profile.ID)
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("Mihomo profile %q has no active subscription token", profile.Name)
		}
		if err := q.CreateMihomoProfileSubscriptionToken(ctx, store.CreateMihomoProfileSubscriptionTokenParams{
			ID: tokenID, ProfileID: profile.ID, Token: rawToken,
		}); err != nil {
			return err
		}
		row, err := q.GetActiveMihomoProfileSubscriptionToken(ctx, profile.ID)
		if err != nil {
			return err
		}
		rotated = mihomoProfileTokenFromFields(row.ID, row.ProfileID, row.ProfileName, row.ProxyUserID.String,
			row.ProxyUserName, row.Token, row.CreatedAt, row.LastUsedAt, row.RevokedAt)
		return nil
	})
	return rotated, err
}

func (db *DB) RevokeMihomoProfileSubscriptionToken(ctx context.Context, profileID string) (bool, error) {
	affected, err := db.q.RevokeActiveMihomoProfileSubscriptionToken(ctx, strings.TrimSpace(profileID))
	return affected > 0, err
}

func (db *DB) VerifyMihomoProfileSubscriptionToken(ctx context.Context, rawToken string) (MihomoProfileSubscriptionToken, bool, error) {
	if !strings.HasPrefix(rawToken, subscriptionTokenPrefix) {
		return MihomoProfileSubscriptionToken{}, false, nil
	}
	row, err := db.q.GetActiveMihomoProfileSubscriptionTokenByValue(ctx, rawToken)
	if errors.Is(err, sql.ErrNoRows) {
		return MihomoProfileSubscriptionToken{}, false, nil
	}
	if err != nil {
		return MihomoProfileSubscriptionToken{}, false, err
	}
	if err := db.q.MarkMihomoProfileSubscriptionTokenUsed(ctx, row.ID); err != nil {
		return MihomoProfileSubscriptionToken{}, false, err
	}
	return mihomoProfileTokenFromFields(row.ID, row.ProfileID, row.ProfileName, row.ProxyUserID.String,
		row.ProxyUserName, row.Token, row.CreatedAt, row.LastUsedAt, row.RevokedAt), true, nil
}

func mihomoProfileTokenFromFields(id, profileID, profileName, userID, userName, token, createdAt string, lastUsedAt, revokedAt sql.NullString) MihomoProfileSubscriptionToken {
	return MihomoProfileSubscriptionToken{ID: id, ProfileID: profileID, ProfileName: profileName, ProxyUserID: userID,
		ProxyUserName: userName, Token: token, CreatedAt: createdAt, LastUsedAt: lastUsedAt, RevokedAt: revokedAt}
}
