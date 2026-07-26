package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const VLESSRealityFlowVision = "xtls-rprx-vision"

type ProxyCredential struct {
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

type Shadowsocks2022Credential struct {
	Password string `json:"password"`
}

type IssueCredentialParams struct {
	UserName  string
	NodeName  string
	ProxyName string
}

func (db *DB) IssueVLESSRealityCredential(ctx context.Context, params IssueCredentialParams) (ProxyCredential, error) {
	return db.issueProxyCredential(ctx, params, ProtocolVLESSReality, func(proxy Proxy) (string, error) {
		credentialJSON, err := json.Marshal(VLESSRealityCredential{
			UUID: uuid.NewString(),
			Flow: VLESSRealityFlowVision,
		})
		return string(credentialJSON), err
	})
}

func (db *DB) IssueShadowsocks2022Credential(ctx context.Context, params IssueCredentialParams) (ProxyCredential, error) {
	return db.issueProxyCredential(ctx, params, ProtocolShadowsocks2022, func(proxy Proxy) (string, error) {
		var settings Shadowsocks2022Settings
		if err := json.Unmarshal([]byte(proxy.SettingsJSON), &settings); err != nil {
			return "", fmt.Errorf("parse settings for %s: %w", proxy.Name, err)
		}
		keyLength, err := shadowsocks2022KeyLength(settings.Method)
		if err != nil {
			return "", err
		}
		password, err := generateShadowsocks2022Key(keyLength)
		if err != nil {
			return "", err
		}
		credentialJSON, err := json.Marshal(Shadowsocks2022Credential{Password: password})
		return string(credentialJSON), err
	})
}

func (db *DB) issueProxyCredential(
	ctx context.Context,
	params IssueCredentialParams,
	expectedProtocol string,
	generate func(Proxy) (string, error),
) (ProxyCredential, error) {
	user, err := db.GetProxyUser(ctx, params.UserName)
	if err != nil {
		return ProxyCredential{}, err
	}
	proxy, err := db.GetProxy(ctx, params.NodeName, params.ProxyName)
	if err != nil {
		return ProxyCredential{}, err
	}
	if proxy.Protocol != expectedProtocol {
		return ProxyCredential{}, fmt.Errorf("proxy %q on node %q is %s, not %s", params.ProxyName, params.NodeName, proxy.Protocol, expectedProtocol)
	}
	binding, err := db.GetUserNodeBinding(ctx, user.Name, proxy.NodeName)
	if err != nil {
		return ProxyCredential{}, err
	}
	if !binding.Enabled {
		return ProxyCredential{}, fmt.Errorf("binding for user %q on node %q is disabled", user.Name, proxy.NodeName)
	}
	existing, err := db.getProxyCredentialByIDs(ctx, user.ID, proxy.ID)
	if err == nil {
		if existing.Enabled && !existing.DeletedAt.Valid {
			return existing, nil
		}
		if _, err := db.q.RestoreProxyAccess(ctx, store.RestoreProxyAccessParams{ProxyUserID: user.ID, ProxyID: proxy.ID}); err != nil {
			return ProxyCredential{}, err
		}
		return db.GetProxyCredential(ctx, user.Name, proxy.NodeName, proxy.Name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ProxyCredential{}, err
	}
	accessID, err := id.New("acc")
	if err != nil {
		return ProxyCredential{}, err
	}
	credentialJSON, err := generate(proxy)
	if err != nil {
		return ProxyCredential{}, err
	}
	authName := proxy.Name + "@" + user.Name
	if err := db.q.CreateProxyAccess(ctx, store.CreateProxyAccessParams{
		ID:             accessID,
		ProxyID:        proxy.ID,
		ProxyUserID:    user.ID,
		AuthName:       authName,
		Enabled:        1,
		QuotaBytes:     0,
		CredentialJson: credentialJSON,
	}); err != nil {
		return ProxyCredential{}, err
	}
	return db.GetProxyCredential(ctx, user.Name, proxy.NodeName, proxy.Name)
}

// IssueVLESSRealityAccess preserves the original proxy-grant behavior for the
// legacy admin API. New flows should issue a ProxyCredential as a dependency of
// an explicitly granted Path.
func (db *DB) IssueVLESSRealityAccess(ctx context.Context, params IssueCredentialParams) (ProxyCredential, error) {
	credential, err := db.IssueVLESSRealityCredential(ctx, params)
	if err != nil {
		return ProxyCredential{}, err
	}
	if err := db.GrantDirectPathsForCredential(ctx, credential); err != nil {
		return ProxyCredential{}, err
	}
	return credential, nil
}

func (db *DB) IssueShadowsocks2022Access(ctx context.Context, params IssueCredentialParams) (ProxyCredential, error) {
	credential, err := db.IssueShadowsocks2022Credential(ctx, params)
	if err != nil {
		return ProxyCredential{}, err
	}
	if err := db.GrantDirectPathsForCredential(ctx, credential); err != nil {
		return ProxyCredential{}, err
	}
	return credential, nil
}

func (db *DB) SoftDeleteProxyCredential(ctx context.Context, userName, nodeName, proxyName string) (ProxyCredential, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyCredential{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyCredential{}, err
	}
	affected, err := db.q.SoftDeleteProxyAccess(ctx, store.SoftDeleteProxyAccessParams{
		ProxyUserID: user.ID,
		ProxyID:     proxy.ID,
	})
	if err != nil {
		return ProxyCredential{}, err
	}
	if err := requireAffected(affected, "proxy credential", userName+"@"+nodeName+"/"+proxyName); err != nil {
		return ProxyCredential{}, err
	}
	return db.getProxyCredentialByIDs(ctx, user.ID, proxy.ID)
}

func (db *DB) RevokeProxyCredential(ctx context.Context, userName, nodeName, proxyName string) (ProxyCredential, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyCredential{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyCredential{}, err
	}
	if err := db.setProxyCredentialEnabledByIDs(ctx, user.ID, proxy.ID, false); err != nil {
		return ProxyCredential{}, err
	}
	return db.GetProxyCredential(ctx, user.Name, proxy.NodeName, proxy.Name)
}

func (db *DB) SetProxyCredentialEnabled(ctx context.Context, userName, nodeName, proxyName string, enabled bool) (ProxyCredential, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return ProxyCredential{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyCredential{}, err
	}
	if err := db.setProxyCredentialEnabledByIDs(ctx, user.ID, proxy.ID, enabled); err != nil {
		return ProxyCredential{}, err
	}
	return db.GetProxyCredential(ctx, user.Name, proxy.NodeName, proxy.Name)
}

func (db *DB) GetProxyCredential(ctx context.Context, userName, nodeName, proxyName string) (ProxyCredential, error) {
	proxy, err := db.GetProxy(ctx, nodeName, proxyName)
	if err != nil {
		return ProxyCredential{}, err
	}
	row, err := db.q.GetProxyAccess(ctx, store.GetProxyAccessParams{
		UserName:  normalizeName(userName),
		NodeName:  proxy.NodeName,
		ProxyName: proxy.Name,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyCredential{}, fmt.Errorf("credential for user %q on %q/%q not found", userName, nodeName, proxyName)
		}
		return ProxyCredential{}, err
	}
	return proxyCredentialFromDetail(row), nil
}

func (db *DB) ListProxyCredentialsByNode(ctx context.Context, nodeName string) ([]ProxyCredential, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	rows, err := db.q.ListProxyAccessesByNodeName(ctx, node.Name)
	if err != nil {
		return nil, err
	}
	out := make([]ProxyCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyCredentialFromDetail(row))
	}
	return out, nil
}

func (db *DB) ListProxyCredentialsByUserNode(ctx context.Context, userName, nodeName string) ([]ProxyCredential, error) {
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
	out := make([]ProxyCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyCredentialFromDetail(row))
	}
	return out, nil
}

func (db *DB) ListProxyCredentialsByUser(ctx context.Context, userName string) ([]ProxyCredential, error) {
	rows, err := db.q.ListProxyAccessesByUserName(ctx, normalizeName(userName))
	if err != nil {
		return nil, err
	}
	out := make([]ProxyCredential, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyCredentialFromDetail(row))
	}
	return out, nil
}

func (db *DB) getProxyCredentialByIDs(ctx context.Context, userID, proxyID string) (ProxyCredential, error) {
	row, err := db.q.GetProxyAccessByIDs(ctx, store.GetProxyAccessByIDsParams{
		ProxyUserID: userID,
		ProxyID:     proxyID,
	})
	if err != nil {
		return ProxyCredential{}, err
	}
	return proxyCredentialFromDetail(row), nil
}

func (db *DB) GetProxyCredentialByIDs(ctx context.Context, userID, proxyID string) (ProxyCredential, error) {
	credential, err := db.getProxyCredentialByIDs(ctx, strings.TrimSpace(userID), strings.TrimSpace(proxyID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProxyCredential{}, fmt.Errorf("credential for user %q on proxy %q not found", userID, proxyID)
		}
		return ProxyCredential{}, err
	}
	return credential, nil
}

func (db *DB) setProxyCredentialEnabledByIDs(ctx context.Context, userID, proxyID string, enabled bool) error {
	affected, err := db.q.SetProxyAccessEnabled(ctx, store.SetProxyAccessEnabledParams{
		Enabled:     boolToInt64(enabled),
		ProxyUserID: userID,
		ProxyID:     proxyID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy credential", userID+"@"+proxyID)
}

func proxyCredentialFromDetail(row store.ProxyAccessDetail) ProxyCredential {
	return ProxyCredential{
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
