package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	"github.com/haoxin/boxfleet/internal/model"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type ConfigVersion = store.ConfigVersion

type PublishedConfig struct {
	ConfigVersion ConfigVersion
	Created       bool
}

type NodeConfigStatus struct {
	NodeID                 string
	NodeName               string
	TargetConfigVersionID  sql.NullString
	TargetVersion          sql.NullInt64
	TargetConfigHash       sql.NullString
	CurrentConfigVersionID sql.NullString
	CurrentVersion         sql.NullInt64
	CurrentConfigHash      sql.NullString
	LastApplyStatus        string
	LastApplyError         string
	UpdatedAt              sql.NullString
	LatestHeartbeat        sql.NullString
	AgentVersion           string
	SingBoxVersion         string
}

type ApplyResult = model.ApplyResult
type Heartbeat = model.Heartbeat

func (db *DB) PublishConfig(ctx context.Context, nodeName string, raw []byte) (PublishedConfig, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return PublishedConfig{}, err
	}
	hash := SHA256Hex(raw)
	existing, err := db.q.GetConfigVersionByHash(ctx, store.GetConfigVersionByHashParams{
		NodeID:     node.ID,
		ConfigHash: hash,
	})
	if err == nil {
		if err := db.publishConfigVersion(ctx, node.ID, existing.ID); err != nil {
			return PublishedConfig{}, err
		}
		version, err := db.q.GetConfigVersionByID(ctx, existing.ID)
		if err != nil {
			return PublishedConfig{}, err
		}
		return PublishedConfig{ConfigVersion: version, Created: false}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PublishedConfig{}, err
	}
	next, err := db.q.NextConfigVersion(ctx, node.ID)
	if err != nil {
		return PublishedConfig{}, err
	}
	configID, err := id.New("cfg")
	if err != nil {
		return PublishedConfig{}, err
	}
	if err := db.q.CreateConfigVersion(ctx, store.CreateConfigVersionParams{
		ID:         configID,
		NodeID:     node.ID,
		Version:    next,
		Status:     "published",
		ConfigJson: string(raw),
		ConfigHash: hash,
	}); err != nil {
		return PublishedConfig{}, err
	}
	if err := db.publishConfigVersion(ctx, node.ID, configID); err != nil {
		return PublishedConfig{}, err
	}
	version, err := db.q.GetConfigVersionByID(ctx, configID)
	if err != nil {
		return PublishedConfig{}, err
	}
	return PublishedConfig{ConfigVersion: version, Created: true}, nil
}

func (db *DB) GetTargetConfig(ctx context.Context, nodeName string) (ConfigVersion, error) {
	version, err := db.q.GetTargetConfigByNodeName(ctx, normalizeName(nodeName))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConfigVersion{}, fmt.Errorf("node %q has no published target config", nodeName)
		}
		return ConfigVersion{}, err
	}
	return version, nil
}

func (db *DB) GetNodeConfigStatus(ctx context.Context, nodeName string) (NodeConfigStatus, error) {
	row, err := db.q.GetNodeConfigStatusByNodeName(ctx, normalizeName(nodeName))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeConfigStatus{}, fmt.Errorf("node %q not found", nodeName)
		}
		return NodeConfigStatus{}, err
	}
	status := NodeConfigStatus{
		NodeID:                 row.NodeID,
		NodeName:               row.NodeName,
		TargetConfigVersionID:  row.TargetConfigVersionID,
		TargetVersion:          row.TargetVersion,
		TargetConfigHash:       row.TargetConfigHash,
		CurrentConfigVersionID: row.CurrentConfigVersionID,
		CurrentVersion:         row.CurrentVersion,
		CurrentConfigHash:      row.CurrentConfigHash,
		LastApplyStatus:        "pending",
		LastApplyError:         row.LastApplyError.String,
		UpdatedAt:              row.UpdatedAt,
	}
	if row.LastApplyStatus.Valid {
		status.LastApplyStatus = row.LastApplyStatus.String
	}
	heartbeat, err := db.q.LatestNodeHeartbeatByNodeName(ctx, normalizeName(nodeName))
	if err == nil {
		status.LatestHeartbeat = sql.NullString{String: heartbeat.ReportedAt, Valid: true}
		status.AgentVersion = heartbeat.AgentVersion
		status.SingBoxVersion = heartbeat.SingBoxVersion
	} else if !errors.Is(err, sql.ErrNoRows) {
		return NodeConfigStatus{}, err
	}
	return status, nil
}

func (db *DB) ListNodeConfigStatuses(ctx context.Context) ([]NodeConfigStatus, error) {
	rows, err := db.q.ListNodeConfigStatuses(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NodeConfigStatus, 0, len(rows))
	for _, row := range rows {
		status := NodeConfigStatus{
			NodeID:                 row.NodeID,
			NodeName:               row.NodeName,
			TargetConfigVersionID:  row.TargetConfigVersionID,
			TargetVersion:          row.TargetVersion,
			TargetConfigHash:       row.TargetConfigHash,
			CurrentConfigVersionID: row.CurrentConfigVersionID,
			CurrentVersion:         row.CurrentVersion,
			CurrentConfigHash:      row.CurrentConfigHash,
			LastApplyStatus:        "pending",
			LastApplyError:         row.LastApplyError.String,
			UpdatedAt:              row.UpdatedAt,
			LatestHeartbeat:        row.LatestHeartbeat,
			AgentVersion:           row.AgentVersion.String,
			SingBoxVersion:         row.SingBoxVersion,
		}
		if row.LastApplyStatus.Valid {
			status.LastApplyStatus = row.LastApplyStatus.String
		}
		out = append(out, status)
	}
	return out, nil
}

func (db *DB) RecordApplyResult(ctx context.Context, result ApplyResult) error {
	node, err := db.GetNode(ctx, result.NodeName)
	if err != nil {
		return err
	}
	status := result.Status
	if status != "applied" && status != "failed" {
		return fmt.Errorf("unsupported apply status %q", status)
	}
	configID := sql.NullString{}
	if result.ConfigVersionID != "" {
		config, err := db.q.GetConfigVersionByID(ctx, result.ConfigVersionID)
		if err != nil {
			return err
		}
		if config.NodeID != node.ID {
			return fmt.Errorf("config version %q does not belong to node %q", result.ConfigVersionID, result.NodeName)
		}
		if result.ConfigHash != "" && config.ConfigHash != result.ConfigHash {
			return fmt.Errorf("config hash mismatch for %q", result.ConfigVersionID)
		}
		configID = sql.NullString{String: config.ID, Valid: true}
	}
	return db.q.UpdateNodeConfigApplyStatus(ctx, store.UpdateNodeConfigApplyStatusParams{
		LastApplyStatus:        status,
		CurrentConfigVersionID: configID,
		LastApplyError:         result.Error,
		NodeID:                 node.ID,
	})
}

func (db *DB) RecordHeartbeat(ctx context.Context, heartbeat Heartbeat) error {
	node, err := db.GetNode(ctx, heartbeat.NodeName)
	if err != nil {
		return err
	}
	reportedAt := heartbeat.ReportedAt
	if reportedAt == "" {
		reportedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	heartbeatID, err := id.New("hb")
	if err != nil {
		return err
	}
	if err := db.q.CreateNodeHeartbeat(ctx, store.CreateNodeHeartbeatParams{
		ID:             heartbeatID,
		NodeID:         node.ID,
		AgentVersion:   heartbeat.AgentVersion,
		SingBoxVersion: heartbeat.SingBoxVersion,
		Status:         heartbeat.Status,
		MemoryBytes:    heartbeat.MemoryBytes,
		RxBytes:        heartbeat.RxBytes,
		TxBytes:        heartbeat.TxBytes,
		PayloadJson:    string(payload),
		ReportedAt:     reportedAt,
	}); err != nil {
		return err
	}
	return db.q.TouchNodeSeen(ctx, store.TouchNodeSeenParams{
		LastSeenAt:     sql.NullString{String: reportedAt, Valid: true},
		SingBoxVersion: heartbeat.SingBoxVersion,
		NodeID:         node.ID,
	})
}

func (db *DB) publishConfigVersion(ctx context.Context, nodeID, configID string) error {
	if err := db.q.MarkConfigVersionPublished(ctx, configID); err != nil {
		return err
	}
	if err := db.q.SupersedePublishedConfigVersions(ctx, store.SupersedePublishedConfigVersionsParams{
		NodeID: nodeID,
		KeepID: configID,
	}); err != nil {
		return err
	}
	return db.q.UpsertNodeConfigTarget(ctx, store.UpsertNodeConfigTargetParams{
		NodeID:                nodeID,
		TargetConfigVersionID: sql.NullString{String: configID, Valid: true},
	})
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
