package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type UserNodeBinding struct {
	ID                string
	ProxyUserID       string
	ProxyUserName     string
	NodeID            string
	NodeName          string
	Enabled           bool
	NodeQuotaBytes    int64
	TrafficMultiplier sql.NullFloat64
	DisabledReason    string
	CreatedAt         string
	UpdatedAt         string
}

func (db *DB) BindUserToNode(ctx context.Context, userName, nodeName string) (UserNodeBinding, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return UserNodeBinding{}, err
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return UserNodeBinding{}, err
	}
	bindingID, err := id.New("bind")
	if err != nil {
		return UserNodeBinding{}, err
	}
	err = db.q.UpsertUserNodeBinding(ctx, store.UpsertUserNodeBindingParams{
		ID:          bindingID,
		ProxyUserID: user.ID,
		NodeID:      node.ID,
	})
	if err != nil {
		return UserNodeBinding{}, err
	}
	return db.GetUserNodeBinding(ctx, user.Name, node.Name)
}

func (db *DB) ListUserNodeBindings(ctx context.Context, userName string) ([]UserNodeBinding, error) {
	if userName != "" {
		rows, err := db.q.ListUserNodeBindingsByUserName(ctx, normalizeName(userName))
		if err != nil {
			return nil, err
		}
		bindings := make([]UserNodeBinding, 0, len(rows))
		for _, row := range rows {
			bindings = append(bindings, mapListBindingByUserRow(row))
		}
		return bindings, nil
	}
	rows, err := db.q.ListUserNodeBindings(ctx)
	if err != nil {
		return nil, err
	}
	bindings := make([]UserNodeBinding, 0, len(rows))
	for _, row := range rows {
		bindings = append(bindings, mapListBindingRow(row))
	}
	return bindings, nil
}

func (db *DB) GetUserNodeBinding(ctx context.Context, userName, nodeName string) (UserNodeBinding, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return UserNodeBinding{}, err
	}
	binding, err := db.q.GetUserNodeBinding(ctx, store.GetUserNodeBindingParams{
		UserName: normalizeName(userName),
		NodeName: node.Name,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserNodeBinding{}, fmt.Errorf("binding for user %q on node %q not found", userName, nodeName)
		}
		return UserNodeBinding{}, err
	}
	return mapGetBindingRow(binding), nil
}

func (db *DB) SetUserNodeBindingEnabled(ctx context.Context, userName, nodeName string, enabled bool) error {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return err
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	reason := "manual"
	if enabled {
		reason = ""
	}
	affected, err := db.q.SetUserNodeBindingEnabled(ctx, store.SetUserNodeBindingEnabledParams{
		Enabled:        boolToInt64(enabled),
		DisabledReason: reason,
		ProxyUserID:    user.ID,
		NodeID:         node.ID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "binding", userName+"@"+nodeName)
}

func (db *DB) SetUserNodeQuota(ctx context.Context, userName, nodeName string, quotaBytes int64) error {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return err
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	affected, err := db.q.SetUserNodeQuota(ctx, store.SetUserNodeQuotaParams{
		NodeQuotaBytes: quotaBytes,
		ProxyUserID:    user.ID,
		NodeID:         node.ID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "binding", userName+"@"+nodeName)
}

func (db *DB) SetUserNodeMultiplier(ctx context.Context, userName, nodeName string, multiplier sql.NullFloat64) error {
	if multiplier.Valid && multiplier.Float64 < 0 {
		return errors.New("traffic multiplier must be non-negative")
	}
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return err
	}
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	affected, err := db.q.SetUserNodeMultiplier(ctx, store.SetUserNodeMultiplierParams{
		TrafficMultiplier: multiplier,
		ProxyUserID:       user.ID,
		NodeID:            node.ID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "binding", userName+"@"+nodeName)
}

func mapGetBindingRow(row store.GetUserNodeBindingRow) UserNodeBinding {
	return UserNodeBinding{
		ID:                row.ID,
		ProxyUserID:       row.ProxyUserID,
		ProxyUserName:     row.ProxyUserName,
		NodeID:            row.NodeID,
		NodeName:          row.NodeName,
		Enabled:           int64ToBool(row.Enabled),
		NodeQuotaBytes:    row.NodeQuotaBytes,
		TrafficMultiplier: row.TrafficMultiplier,
		DisabledReason:    row.DisabledReason,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func mapListBindingRow(row store.ListUserNodeBindingsRow) UserNodeBinding {
	return UserNodeBinding{
		ID:                row.ID,
		ProxyUserID:       row.ProxyUserID,
		ProxyUserName:     row.ProxyUserName,
		NodeID:            row.NodeID,
		NodeName:          row.NodeName,
		Enabled:           int64ToBool(row.Enabled),
		NodeQuotaBytes:    row.NodeQuotaBytes,
		TrafficMultiplier: row.TrafficMultiplier,
		DisabledReason:    row.DisabledReason,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func mapListBindingByUserRow(row store.ListUserNodeBindingsByUserNameRow) UserNodeBinding {
	return UserNodeBinding{
		ID:                row.ID,
		ProxyUserID:       row.ProxyUserID,
		ProxyUserName:     row.ProxyUserName,
		NodeID:            row.NodeID,
		NodeName:          row.NodeName,
		Enabled:           int64ToBool(row.Enabled),
		NodeQuotaBytes:    row.NodeQuotaBytes,
		TrafficMultiplier: row.TrafficMultiplier,
		DisabledReason:    row.DisabledReason,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
