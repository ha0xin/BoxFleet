package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const (
	SettingNetworkEventRetentionDays = "network_event_retention_days"
	DefaultNetworkEventRetentionDays = int64(90)
	MaxNetworkEventRetentionDays     = int64(3650)
)

type AdminSettings struct {
	NetworkEventRetentionDays int64
}

func (db *DB) AdminSettings(ctx context.Context) (AdminSettings, error) {
	days, err := db.NetworkEventRetentionDays(ctx)
	if err != nil {
		return AdminSettings{}, err
	}
	return AdminSettings{
		NetworkEventRetentionDays: days,
	}, nil
}

func (db *DB) NetworkEventRetentionDays(ctx context.Context) (int64, error) {
	value, err := db.settingInt(ctx, SettingNetworkEventRetentionDays, DefaultNetworkEventRetentionDays)
	if err != nil {
		return 0, err
	}
	if err := validateNetworkEventRetentionDays(value); err != nil {
		return DefaultNetworkEventRetentionDays, nil
	}
	return value, nil
}

func (db *DB) SetNetworkEventRetentionDays(ctx context.Context, days int64) error {
	if err := validateNetworkEventRetentionDays(days); err != nil {
		return err
	}
	return db.setSettingInt(ctx, SettingNetworkEventRetentionDays, days)
}

func validateNetworkEventRetentionDays(days int64) error {
	if days < 1 || days > MaxNetworkEventRetentionDays {
		return fmt.Errorf("network event retention days must be between 1 and %d", MaxNetworkEventRetentionDays)
	}
	return nil
}

func (db *DB) settingInt(ctx context.Context, key string, fallback int64) (int64, error) {
	setting, err := db.q.GetSetting(ctx, strings.TrimSpace(key))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fallback, nil
		}
		return 0, err
	}
	var value int64
	if err := json.Unmarshal([]byte(setting.ValueJson), &value); err != nil {
		return 0, fmt.Errorf("setting %q must be an integer: %w", key, err)
	}
	return value, nil
}

func (db *DB) setSettingInt(ctx context.Context, key string, value int64) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return db.q.UpsertSetting(ctx, store.UpsertSettingParams{
		Key:       strings.TrimSpace(key),
		ValueJson: string(raw),
	})
}
