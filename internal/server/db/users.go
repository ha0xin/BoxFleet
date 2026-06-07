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
	Name              string
	DisplayName       string
	GlobalQuotaBytes  int64
	TrafficMultiplier float64
	ExpireAt          string
}

func (db *DB) CreateProxyUser(ctx context.Context, params CreateProxyUserParams) (ProxyUser, error) {
	name := normalizeName(params.Name)
	if name == "" {
		return ProxyUser{}, errors.New("user name is required")
	}
	multiplier := params.TrafficMultiplier
	if multiplier == 0 {
		multiplier = 1.0
	}
	if multiplier < 0 {
		return ProxyUser{}, errors.New("traffic multiplier must be non-negative")
	}
	userID, err := id.New("usr")
	if err != nil {
		return ProxyUser{}, err
	}
	err = db.q.CreateProxyUser(ctx, store.CreateProxyUserParams{
		ID:                userID,
		Name:              name,
		DisplayName:       params.DisplayName,
		GlobalQuotaBytes:  params.GlobalQuotaBytes,
		TrafficMultiplier: multiplier,
		ExpireAt:          nullableTrimmedString(params.ExpireAt),
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
				ID:                row.ID,
				Name:              row.Name,
				DisplayName:       row.DisplayName,
				Status:            row.Status,
				GlobalQuotaBytes:  row.GlobalQuotaBytes,
				TrafficMultiplier: row.TrafficMultiplier,
				ExpireAt:          row.ExpireAt,
				CreatedAt:         row.CreatedAt,
				UpdatedAt:         row.UpdatedAt,
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

func (db *DB) SetProxyUserMultiplier(ctx context.Context, name string, multiplier float64) error {
	if multiplier < 0 {
		return errors.New("traffic multiplier must be non-negative")
	}
	affected, err := db.q.SetProxyUserMultiplier(ctx, store.SetProxyUserMultiplierParams{
		TrafficMultiplier: multiplier,
		Name:              normalizeName(name),
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
