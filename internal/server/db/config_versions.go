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
	AgentGOOS              string
	AgentGOARCH            string
	Capabilities           []string
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
	var out PublishedConfig
	err = db.withTx(ctx, func(q *store.Queries) error {
		existing, err := q.GetConfigVersionByHash(ctx, store.GetConfigVersionByHashParams{
			NodeID:     node.ID,
			ConfigHash: hash,
		})
		if err == nil {
			if err := publishConfigVersion(ctx, q, node.ID, existing.ID); err != nil {
				return err
			}
			version, err := q.GetConfigVersionByID(ctx, existing.ID)
			if err != nil {
				return err
			}
			out = PublishedConfig{ConfigVersion: version, Created: false}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		next, err := q.NextConfigVersion(ctx, node.ID)
		if err != nil {
			return err
		}
		configID, err := id.New("cfg")
		if err != nil {
			return err
		}
		if err := q.CreateConfigVersion(ctx, store.CreateConfigVersionParams{
			ID:         configID,
			NodeID:     node.ID,
			Version:    next,
			Status:     "published",
			ConfigJson: string(raw),
			ConfigHash: hash,
		}); err != nil {
			return err
		}
		if err := publishConfigVersion(ctx, q, node.ID, configID); err != nil {
			return err
		}
		version, err := q.GetConfigVersionByID(ctx, configID)
		if err != nil {
			return err
		}
		out = PublishedConfig{ConfigVersion: version, Created: true}
		return nil
	})
	if err != nil {
		return PublishedConfig{}, err
	}
	return out, nil
}

func (db *DB) GetTargetConfig(ctx context.Context, nodeName string) (ConfigVersion, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return ConfigVersion{}, err
	}
	version, err := db.q.GetTargetConfigByNodeName(ctx, node.Name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ConfigVersion{}, fmt.Errorf("node %q has no published target config", nodeName)
		}
		return ConfigVersion{}, err
	}
	return version, nil
}

func (db *DB) GetNodeConfigStatus(ctx context.Context, nodeName string) (NodeConfigStatus, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return NodeConfigStatus{}, err
	}
	row, err := db.q.GetNodeConfigStatusByNodeName(ctx, node.Name)
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
	heartbeat, err := db.q.LatestNodeHeartbeatByNodeName(ctx, node.Name)
	if err == nil {
		status.LatestHeartbeat = sql.NullString{String: heartbeat.ReportedAt, Valid: true}
		status.AgentVersion = heartbeat.AgentVersion
		status.SingBoxVersion = heartbeat.SingBoxVersion
		applyHeartbeatCapabilities(&status, heartbeat.PayloadJson)
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
		if row.HeartbeatPayloadJson.Valid {
			applyHeartbeatCapabilities(&status, row.HeartbeatPayloadJson.String)
		}
		if row.LastApplyStatus.Valid {
			status.LastApplyStatus = row.LastApplyStatus.String
		}
		out = append(out, status)
	}
	return out, nil
}

func applyHeartbeatCapabilities(status *NodeConfigStatus, payload string) {
	var heartbeat model.Heartbeat
	if json.Unmarshal([]byte(payload), &heartbeat) != nil {
		return
	}
	status.AgentGOOS = heartbeat.AgentGOOS
	status.AgentGOARCH = heartbeat.AgentGOARCH
	status.Capabilities = normalizeCapabilities(heartbeat.Capabilities)
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
	return db.withTx(ctx, func(q *store.Queries) error {
		if err := q.CreateNodeHeartbeat(ctx, store.CreateNodeHeartbeatParams{
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
		// Heartbeats are append-and-replace: delete only the row currently pointed
		// to as latest, then point at the new row. ON DELETE CASCADE temporarily
		// removes the pointer inside this transaction; readers keep seeing the old
		// committed state until the replacement pointer commits. Deleting exactly
		// one row also keeps legacy backlog cleanup out of the request path.
		if err := q.DeleteCurrentNodeHeartbeat(ctx, node.ID); err != nil {
			return err
		}
		if err := q.UpsertNodeLatestHeartbeat(ctx, store.UpsertNodeLatestHeartbeatParams{
			NodeID:      node.ID,
			HeartbeatID: heartbeatID,
		}); err != nil {
			return err
		}
		// First authenticated heartbeat completes enrollment: a pending node has
		// reported in, so promote it to active. The conditional UPDATE (WHERE
		// status='pending') makes this race-safe — if an admin disabled the node
		// between GetNode above and here, it affects 0 rows and never reactivates it.
		if node.Status == "pending" {
			if _, err := q.PromotePendingNodeToActive(ctx, node.Name); err != nil {
				return err
			}
		}
		return q.TouchNodeSeen(ctx, store.TouchNodeSeenParams{
			LastSeenAt:     sql.NullString{String: reportedAt, Valid: true},
			SingBoxVersion: heartbeat.SingBoxVersion,
			NodeID:         node.ID,
		})
	})
}

func (db *DB) publishConfigVersion(ctx context.Context, nodeID, configID string) error {
	return publishConfigVersion(ctx, db.q, nodeID, configID)
}

func publishConfigVersion(ctx context.Context, q *store.Queries, nodeID, configID string) error {
	if err := q.MarkConfigVersionPublished(ctx, configID); err != nil {
		return err
	}
	if err := q.SupersedePublishedConfigVersions(ctx, store.SupersedePublishedConfigVersionsParams{
		NodeID: nodeID,
		KeepID: configID,
	}); err != nil {
		return err
	}
	return q.UpsertNodeConfigTarget(ctx, store.UpsertNodeConfigTargetParams{
		NodeID:                nodeID,
		TargetConfigVersionID: sql.NullString{String: configID, Valid: true},
	})
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
