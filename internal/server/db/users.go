package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type ProxyUser = store.ProxyUser

type ProxyUserWithProxyCount struct {
	ProxyUser
	ProxyCount int64
}

type CreateProxyUserParams struct {
	Name             string
	DisplayName      string
	GlobalQuotaBytes int64
	ExpireAt         string
}

func (db *DB) CreateProxyUser(ctx context.Context, params CreateProxyUserParams) (ProxyUser, error) {
	name := normalizeName(params.Name)
	if name == "" {
		return ProxyUser{}, errors.New("user name is required")
	}
	if err := validateNameForAuth(name, "user"); err != nil {
		return ProxyUser{}, err
	}
	userID, err := id.New("usr")
	if err != nil {
		return ProxyUser{}, err
	}
	err = db.q.CreateProxyUser(ctx, store.CreateProxyUserParams{
		ID:               userID,
		Name:             name,
		DisplayName:      params.DisplayName,
		GlobalQuotaBytes: params.GlobalQuotaBytes,
		ExpireAt:         nullableTrimmedString(params.ExpireAt),
	})
	if err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) ListProxyUsers(ctx context.Context) ([]ProxyUser, error) {
	return db.q.ListProxyUsers(ctx)
}

func (db *DB) ListProxyUsersWithProxyCounts(ctx context.Context) ([]ProxyUserWithProxyCount, error) {
	rows, err := db.q.ListProxyUsersWithProxyCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyUserWithProxyCount, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProxyUserWithProxyCount{
			ProxyUser: ProxyUser{
				ID:               row.ID,
				Name:             row.Name,
				DisplayName:      row.DisplayName,
				Status:           row.Status,
				GlobalQuotaBytes: row.GlobalQuotaBytes,
				ExpireAt:         row.ExpireAt,
				CreatedAt:        row.CreatedAt,
				UpdatedAt:        row.UpdatedAt,
			},
			ProxyCount: row.ProxyCount,
		})
	}
	return out, nil
}

func (db *DB) GetProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	user, err := db.q.GetProxyUserByName(ctx, normalizeName(name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyUser{}, fmt.Errorf("proxy user %q not found", name)
		}
		return ProxyUser{}, err
	}
	return user, nil
}

func (db *DB) SetProxyUserStatus(ctx context.Context, name, status string) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("unsupported user status %q", status)
	}
	affected, err := db.q.SetProxyUserStatus(ctx, store.SetProxyUserStatusParams{
		Status: status,
		Name:   normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}

func (db *DB) DisableProxyUser(ctx context.Context, name string) (ProxyUser, error) {
	if err := db.SetProxyUserStatus(ctx, name, "disabled"); err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

type UpdateProxyUserParams struct {
	// Nil fields are left unchanged.
	DisplayName      *string
	Status           *string
	GlobalQuotaBytes *int64
	ExpireAt         *string
}

// UpdateProxyUser applies a partial user patch atomically: all fields are
// validated first, then written in a single transaction so a later invalid
// field never leaves an earlier one half-committed.
func (db *DB) UpdateProxyUser(ctx context.Context, name string, params UpdateProxyUserParams) (ProxyUser, error) {
	normalized := normalizeName(name)
	if normalized == "" {
		return ProxyUser{}, errors.New("user name is required")
	}
	if params.Status != nil && *params.Status != "active" && *params.Status != "disabled" {
		return ProxyUser{}, fmt.Errorf("unsupported user status %q", *params.Status)
	}
	err := db.withTx(ctx, func(q *store.Queries) error {
		if params.DisplayName != nil {
			affected, err := q.SetProxyUserDisplayName(ctx, store.SetProxyUserDisplayNameParams{DisplayName: *params.DisplayName, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.GlobalQuotaBytes != nil {
			affected, err := q.SetProxyUserQuota(ctx, store.SetProxyUserQuotaParams{GlobalQuotaBytes: *params.GlobalQuotaBytes, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.ExpireAt != nil {
			affected, err := q.SetProxyUserExpire(ctx, store.SetProxyUserExpireParams{ExpireAt: nullableTrimmedString(*params.ExpireAt), Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		if params.Status != nil {
			affected, err := q.SetProxyUserStatus(ctx, store.SetProxyUserStatusParams{Status: *params.Status, Name: normalized})
			if err != nil {
				return err
			}
			if err := requireAffected(affected, "proxy user", name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProxyUser{}, err
	}
	return db.GetProxyUser(ctx, name)
}

func (db *DB) SetProxyUserQuota(ctx context.Context, name string, quotaBytes int64) error {
	affected, err := db.q.SetProxyUserQuota(ctx, store.SetProxyUserQuotaParams{
		GlobalQuotaBytes: quotaBytes,
		Name:             normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}

func (db *DB) SetProxyUserExpire(ctx context.Context, name, expireAt string) error {
	affected, err := db.q.SetProxyUserExpire(ctx, store.SetProxyUserExpireParams{
		ExpireAt: nullableTrimmedString(expireAt),
		Name:     normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy user", name)
}
