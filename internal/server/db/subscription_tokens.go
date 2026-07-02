package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const subscriptionTokenPrefix = "bfsub_"

var ErrActiveSubscriptionTokenExists = errors.New("proxy user already has an active subscription token")

type SubscriptionToken struct {
	ID            string         `json:"id"`
	ProxyUserID   string         `json:"proxy_user_id"`
	ProxyUserName string         `json:"proxy_user_name"`
	Token         string         `json:"token"`
	CreatedAt     string         `json:"created_at"`
	LastUsedAt    sql.NullString `json:"last_used_at"`
	RevokedAt     sql.NullString `json:"revoked_at"`
}

func (db *DB) IssueSubscriptionToken(ctx context.Context, userName string) (SubscriptionToken, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return SubscriptionToken{}, err
	}
	tokenID, rawToken, err := newSubscriptionToken()
	if err != nil {
		return SubscriptionToken{}, err
	}
	var issued SubscriptionToken
	err = db.withTx(ctx, func(q *store.Queries) error {
		if _, err := q.GetActiveSubscriptionTokenByUserName(ctx, user.Name); err == nil {
			return ErrActiveSubscriptionTokenExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := q.CreateSubscriptionToken(ctx, store.CreateSubscriptionTokenParams{
			ID:          tokenID,
			ProxyUserID: user.ID,
			Token:       rawToken,
		}); err != nil {
			return err
		}
		row, err := q.GetActiveSubscriptionTokenByUserName(ctx, user.Name)
		if err != nil {
			return err
		}
		issued = subscriptionTokenFromUserRow(
			row.ID,
			row.ProxyUserID,
			row.ProxyUserName,
			row.Token,
			row.CreatedAt,
			row.LastUsedAt,
			row.RevokedAt,
		)
		return nil
	})
	if err != nil {
		return SubscriptionToken{}, err
	}
	return issued, nil
}

func (db *DB) GetActiveSubscriptionToken(ctx context.Context, userName string) (SubscriptionToken, bool, error) {
	row, err := db.q.GetActiveSubscriptionTokenByUserName(ctx, normalizeName(userName))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionToken{}, false, nil
		}
		return SubscriptionToken{}, false, err
	}
	return subscriptionTokenFromUserRow(
		row.ID,
		row.ProxyUserID,
		row.ProxyUserName,
		row.Token,
		row.CreatedAt,
		row.LastUsedAt,
		row.RevokedAt,
	), true, nil
}

func (db *DB) RotateSubscriptionToken(ctx context.Context, userName string) (SubscriptionToken, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return SubscriptionToken{}, err
	}
	tokenID, rawToken, err := newSubscriptionToken()
	if err != nil {
		return SubscriptionToken{}, err
	}
	var rotated SubscriptionToken
	err = db.withTx(ctx, func(q *store.Queries) error {
		affected, err := q.RevokeActiveSubscriptionTokenByUserID(ctx, user.ID)
		if err != nil {
			return err
		}
		if affected == 0 {
			return fmt.Errorf("proxy user %q has no active subscription token", user.Name)
		}
		if err := q.CreateSubscriptionToken(ctx, store.CreateSubscriptionTokenParams{
			ID:          tokenID,
			ProxyUserID: user.ID,
			Token:       rawToken,
		}); err != nil {
			return err
		}
		row, err := q.GetActiveSubscriptionTokenByUserName(ctx, user.Name)
		if err != nil {
			return err
		}
		rotated = subscriptionTokenFromUserRow(
			row.ID,
			row.ProxyUserID,
			row.ProxyUserName,
			row.Token,
			row.CreatedAt,
			row.LastUsedAt,
			row.RevokedAt,
		)
		return nil
	})
	if err != nil {
		return SubscriptionToken{}, err
	}
	return rotated, nil
}

func (db *DB) RevokeSubscriptionToken(ctx context.Context, userName string) (bool, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return false, err
	}
	affected, err := db.q.RevokeActiveSubscriptionTokenByUserID(ctx, user.ID)
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// VerifySubscriptionToken resolves an active subscription token and marks it
// used. The associated normalized user name is returned for provider rendering.
func (db *DB) VerifySubscriptionToken(ctx context.Context, rawToken string) (SubscriptionToken, bool, error) {
	if !strings.HasPrefix(rawToken, subscriptionTokenPrefix) {
		return SubscriptionToken{}, false, nil
	}
	row, err := db.q.GetActiveSubscriptionTokenByValue(ctx, rawToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SubscriptionToken{}, false, nil
		}
		return SubscriptionToken{}, false, err
	}
	if err := db.q.MarkSubscriptionTokenUsed(ctx, row.ID); err != nil {
		return SubscriptionToken{}, false, err
	}
	verified := subscriptionTokenFromUserRow(
		row.ID,
		row.ProxyUserID,
		row.ProxyUserName,
		row.Token,
		row.CreatedAt,
		row.LastUsedAt,
		row.RevokedAt,
	)
	return verified, true, nil
}

func subscriptionTokenFromUserRow(
	tokenID string,
	proxyUserID string,
	proxyUserName string,
	rawToken string,
	createdAt string,
	lastUsedAt sql.NullString,
	revokedAt sql.NullString,
) SubscriptionToken {
	return SubscriptionToken{
		ID:            tokenID,
		ProxyUserID:   proxyUserID,
		ProxyUserName: proxyUserName,
		Token:         rawToken,
		CreatedAt:     createdAt,
		LastUsedAt:    lastUsedAt,
		RevokedAt:     revokedAt,
	}
}

func newSubscriptionToken() (string, string, error) {
	tokenID, err := id.New("stok")
	if err != nil {
		return "", "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	return tokenID, subscriptionTokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
