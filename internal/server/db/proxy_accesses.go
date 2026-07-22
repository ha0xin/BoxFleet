package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const VLESSRealityFlowVision = "xtls-rprx-vision"

type ProxyAccess struct {
	ID                     string
	ProxyID                string
	ProxyUserID            string
	ProxyUserName          string
	NodeName               string
	NodePublicHost         string
	ProxyName              string
	Protocol               string
	Listen                 string
	ListenPort             int
	Transport              string
	ProxyTrafficMultiplier float64
	SettingsJSON           string
	AuthName               string
	Enabled                bool
	QuotaBytes             int64
	TrafficMultiplier      sql.NullFloat64
	CredentialJSON         string
	DeletedAt              sql.NullString
	CreatedAt              string
	UpdatedAt              string
}

type VLESSRealityCredential struct {
	UUID string `json:"uuid"`
	Flow string `json:"flow"`
}

type IssueAccessParams struct {
	UserName  string
	NodeName  string
	ProxyName string
}

func (db *DB) IssueVLESSRealityAccess(ctx context.Context, params IssueAccessParams) (ProxyAccess, error) {
	user, err := db.GetProxyUser(ctx, params.UserName)
	if err != nil {
		return ProxyAccess{}, err
	}
	proxy, err := db.GetProxy(ctx, params.NodeName, params.ProxyName)
	if err != nil {
		return ProxyAccess{}, err
	}
	if proxy.Protocol != ProtocolVLESSReality {
		return ProxyAccess{}, fmt.Errorf("proxy %q on node %q is %s, not %s", params.ProxyName, params.NodeName, proxy.Protocol, ProtocolVLESSReality)
	}
	binding, err := db.GetUserNodeBinding(ctx, user.Name, proxy.NodeName)
	if err != nil {
		return ProxyAccess{}, err
	}
	if !binding.Enabled {
		return ProxyAccess{}, fmt.Errorf("binding for user %q on node %q is disabled", user.Name, proxy.NodeName)
	}
	existing, err := db.getProxyAccessByIDs(ctx, user.ID, proxy.ID)
	if err == nil {
		if existing.Enabled && !existing.DeletedAt.Valid {
			return existing, nil
		}
		if _, err := db.q.RestoreProxyAccess(ctx, store.RestoreProxyAccessParams{ProxyUserID: user.ID, ProxyID: proxy.ID}); err != nil {
			return ProxyAccess{}, err
		}
		return db.GetProxyAccess(ctx, user.Name, proxy.NodeName, proxy.Name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProxyAccess{}, err
	}
	accessID, err := id.New("acc")
	if err != nil {
		return ProxyAccess{}, err
	}
	credentialJSON, err := json.Marshal(VLESSRealityCredential{
		UUID: uuid.NewString(),
		Flow: VLESSRealityFlowVision,
	})
	if err != nil {
		return ProxyAccess{}, err
	}
	authName := proxy.Name + "@" + user.Name
	if err := db.q.CreateProxyAccess(ctx, store.CreateProxyAccessParams{
		ID:             accessID,
		ProxyID:        proxy.ID,
		ProxyUserID:    user.ID,
		AuthName:       authName,
		Enabled:        1,
		QuotaBytes:     0,
		CredentialJson: string(credentialJSON),
	}); err != nil {
		return ProxyAccess{}, err
	}
	return db.GetProxyAccess(ctx, user.Name, proxy.NodeName, proxy.Name)
}

func (db *DB) SoftDeleteProxyAccess(ctx context.Context, userName, nodeName, proxyName string) (ProxyAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyAccess{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyAccess{}, err
	}
	affected, err := db.q.SoftDeleteProxyAccess(ctx, store.SoftDeleteProxyAccessParams{
		ProxyUserID: user.ID,
		ProxyID:     proxy.ID,
	})
	if err != nil {
		return ProxyAccess{}, err
	}
	if err := requireAffected(affected, "proxy access", userName+"@"+nodeName+"/"+proxyName); err != nil {
		return ProxyAccess{}, err
	}
	return db.getProxyAccessByIDs(ctx, user.ID, proxy.ID)
}

func (db *DB) RevokeProxyAccess(ctx context.Context, userName, nodeName, proxyName string) (ProxyAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyAccess{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyAccess{}, err
	}
	if err := db.setProxyAccessEnabledByIDs(ctx, user.ID, proxy.ID, false); err != nil {
		return ProxyAccess{}, err
	}
	return db.GetProxyAccess(ctx, user.Name, proxy.NodeName, proxy.Name)
}

func (db *DB) SetProxyAccessEnabled(ctx context.Context, userName, nodeName, proxyName string, enabled bool) (ProxyAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyAccess{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyAccess{}, err
	}
	if err := db.setProxyAccessEnabledByIDs(ctx, user.ID, proxy.ID, enabled); err != nil {
		return ProxyAccess{}, err
	}
	return db.GetProxyAccess(ctx, user.Name, proxy.NodeName, proxy.Name)
}

func (db *DB) GetProxyAccess(ctx context.Context, userName, nodeName, proxyName string) (ProxyAccess, error) {
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyAccess{}, err
	}
	row, err := db.q.GetProxyAccess(ctx, store.GetProxyAccessParams{
		UserName:  normalizeName(userName),
		NodeName:  proxy.NodeName,
		ProxyName: proxy.Name,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyAccess{}, fmt.Errorf("access for user %q on %q/%q not found", userName, nodeName, proxyName)
		}
		return ProxyAccess{}, err
	}
	return proxyAccessFromDetail(row), nil
}

func (db *DB) ListProxyAccessesByNode(ctx context.Context, nodeName string) ([]ProxyAccess, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	rows, err := db.q.ListProxyAccessesByNodeName(ctx, node.Name)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyAccessFromDetail(row))
	}
	return out, nil
}

func (db *DB) ListProxyAccessesByUserNode(ctx context.Context, userName, nodeName string) ([]ProxyAccess, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	rows, err := db.q.ListProxyAccessesByUserNode(ctx, store.ListProxyAccessesByUserNodeParams{
		UserName: normalizeName(userName),
		NodeName: node.Name,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ProxyAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyAccessFromDetail(row))
	}
	return out, nil
}

func (db *DB) ListProxyAccessesByUser(ctx context.Context, userName string) ([]ProxyAccess, error) {
	rows, err := db.q.ListProxyAccessesByUserName(ctx, normalizeName(userName))
	if err != nil {
		return nil, err
	}
	out := make([]ProxyAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyAccessFromDetail(row))
	}
	return out, nil
}

func (db *DB) getProxyAccessByIDs(ctx context.Context, userID, proxyID string) (ProxyAccess, error) {
	row, err := db.q.GetProxyAccessByIDs(ctx, store.GetProxyAccessByIDsParams{
		ProxyUserID: userID,
		ProxyID:     proxyID,
	})
	if err != nil {
		return ProxyAccess{}, err
	}
	return proxyAccessFromDetail(row), nil
}

func (db *DB) setProxyAccessEnabledByIDs(ctx context.Context, userID, proxyID string, enabled bool) error {
	affected, err := db.q.SetProxyAccessEnabled(ctx, store.SetProxyAccessEnabledParams{
		Enabled:     boolToInt64(enabled),
		ProxyUserID: userID,
		ProxyID:     proxyID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy access", userID+"@"+proxyID)
}

func proxyAccessFromDetail(row store.ProxyAccessDetail) ProxyAccess {
	return ProxyAccess{
		ID:                     row.ID,
		ProxyID:                row.ProxyID,
		ProxyUserID:            row.ProxyUserID,
		ProxyUserName:          row.ProxyUserName,
		NodeName:               row.NodeName,
		NodePublicHost:         row.NodePublicHost,
		ProxyName:              row.ProxyName,
		Protocol:               row.Protocol,
		Listen:                 row.Listen,
		ListenPort:             int(row.ListenPort),
		Transport:              row.Transport,
		ProxyTrafficMultiplier: row.ProxyTrafficMultiplier,
		SettingsJSON:           row.SettingsJson,
		AuthName:               row.AuthName,
		Enabled:                int64ToBool(row.Enabled),
		QuotaBytes:             row.QuotaBytes,
		TrafficMultiplier:      row.TrafficMultiplier,
		CredentialJSON:         row.CredentialJson,
		DeletedAt:              row.DeletedAt,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}
