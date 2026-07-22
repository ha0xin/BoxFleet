package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type NodeUpdateCampaign struct {
	ID             string   `json:"id"`
	Release        string   `json:"release"`
	Components     []string `json:"components"`
	Status         string   `json:"status"`
	IdempotencyKey string   `json:"idempotency_key"`
	BatchSize      int64    `json:"batch_size"`
	CurrentBatch   int64    `json:"current_batch"`
	RequestedBy    string   `json:"requested_by"`
	RequestedAt    string   `json:"requested_at"`
	StartedAt      string   `json:"started_at,omitempty"`
	FinishedAt     string   `json:"finished_at,omitempty"`
	UpdatedAt      string   `json:"updated_at"`
	Error          string   `json:"error,omitempty"`
}

type NodeUpdateCampaignMember struct {
	CampaignID  string          `json:"campaign_id"`
	NodeID      string          `json:"node_id"`
	NodeName    string          `json:"node_name"`
	Position    int64           `json:"position"`
	BatchNumber int64           `json:"batch_number"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	OperationID string          `json:"operation_id,omitempty"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	StartedAt   string          `json:"started_at,omitempty"`
	FinishedAt  string          `json:"finished_at,omitempty"`
	UpdatedAt   string          `json:"updated_at"`
}

type NodeUpdateCampaignDetail struct {
	Campaign NodeUpdateCampaign         `json:"campaign"`
	Members  []NodeUpdateCampaignMember `json:"members"`
}

type CreateNodeUpdateCampaignParams struct {
	Release        string
	Components     []string
	IdempotencyKey string
	BatchSize      int64
	RequestedBy    string
	Members        []CreateNodeUpdateCampaignMemberParams
}

type CreateNodeUpdateCampaignMemberParams struct {
	NodeName    string
	Position    int64
	BatchNumber int64
	Kind        string
	Payload     json.RawMessage
}

func (db *DB) CreateNodeUpdateCampaign(ctx context.Context, params CreateNodeUpdateCampaignParams) (NodeUpdateCampaignDetail, bool, error) {
	params.Release = strings.TrimSpace(params.Release)
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	params.RequestedBy = strings.TrimSpace(params.RequestedBy)
	if params.Release == "" || params.IdempotencyKey == "" {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign release and idempotency key are required")
	}
	if len(params.IdempotencyKey) > 128 {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign idempotency key must be at most 128 characters")
	}
	if params.BatchSize <= 0 || params.BatchSize > 20 {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign batch size must be between 1 and 20")
	}
	if params.RequestedBy == "" {
		params.RequestedBy = "admin"
	}
	components := normalizeCampaignComponents(params.Components)
	if len(components) == 0 {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign must select at least one component")
	}
	componentsJSON, _ := json.Marshal(components)
	if len(params.Members) == 0 {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign has no eligible nodes")
	}
	type preparedMember struct {
		node Node
		CreateNodeUpdateCampaignMemberParams
		payload []byte
	}
	prepared := make([]preparedMember, 0, len(params.Members))
	seenNodes := make(map[string]bool, len(params.Members))
	seenPositions := make(map[int64]bool, len(params.Members))
	canaries := 0
	for _, member := range params.Members {
		node, err := db.GetNode(ctx, member.NodeName)
		if err != nil {
			return NodeUpdateCampaignDetail{}, false, err
		}
		if seenNodes[node.ID] || seenPositions[member.Position] || member.Position < 0 || member.BatchNumber < 0 {
			return NodeUpdateCampaignDetail{}, false, errors.New("campaign members must have unique nodes and non-negative positions")
		}
		seenNodes[node.ID], seenPositions[member.Position] = true, true
		if member.BatchNumber == 0 {
			canaries++
		}
		if member.Kind != "update.agent" && member.Kind != "update.sing_box" && member.Kind != "update.bundle" {
			return NodeUpdateCampaignDetail{}, false, fmt.Errorf("invalid campaign operation kind %q", member.Kind)
		}
		payload, err := normalizeJSONObject(member.Payload)
		if err != nil {
			return NodeUpdateCampaignDetail{}, false, err
		}
		prepared = append(prepared, preparedMember{node: node, CreateNodeUpdateCampaignMemberParams: member, payload: payload})
	}
	if canaries != 1 {
		return NodeUpdateCampaignDetail{}, false, errors.New("campaign must have exactly one canary in batch 0")
	}
	sort.Slice(prepared, func(i, j int) bool { return prepared[i].Position < prepared[j].Position })
	spec := struct {
		Release    string           `json:"release"`
		Components []string         `json:"components"`
		BatchSize  int64            `json:"batch_size"`
		Members    []map[string]any `json:"members"`
	}{Release: params.Release, Components: components, BatchSize: params.BatchSize}
	for _, member := range prepared {
		spec.Members = append(spec.Members, map[string]any{
			"node_id": member.node.ID, "position": member.Position, "batch": member.BatchNumber,
			"kind": member.Kind, "payload": json.RawMessage(member.payload),
		})
	}
	specJSON, _ := json.Marshal(spec)
	specHash := SHA256Hex(specJSON)

	var campaignID string
	created := false
	err := db.withTx(ctx, func(q *store.Queries) error {
		existing, err := q.GetNodeUpdateCampaignByIdempotencyKey(ctx, params.IdempotencyKey)
		if err == nil {
			if existing.SpecHash != specHash {
				return ErrOperationIdempotencyConflict
			}
			campaignID = existing.ID
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		campaignID, err = id.New("upc")
		if err != nil {
			return err
		}
		if err := q.CreateNodeUpdateCampaign(ctx, store.CreateNodeUpdateCampaignParams{
			ID: campaignID, Release: params.Release, ComponentsJson: string(componentsJSON),
			IdempotencyKey: params.IdempotencyKey, SpecHash: specHash,
			BatchSize: params.BatchSize, RequestedBy: params.RequestedBy,
		}); err != nil {
			return err
		}
		for _, member := range prepared {
			if err := q.CreateNodeUpdateCampaignMember(ctx, store.CreateNodeUpdateCampaignMemberParams{
				CampaignID: campaignID, NodeID: member.node.ID, Position: member.Position,
				BatchNumber: member.BatchNumber, Kind: member.Kind, PayloadJson: string(member.payload),
			}); err != nil {
				return err
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return NodeUpdateCampaignDetail{}, false, err
	}
	detail, err := db.GetNodeUpdateCampaign(ctx, campaignID)
	return detail, created, err
}

func (db *DB) GetNodeUpdateCampaign(ctx context.Context, campaignID string) (NodeUpdateCampaignDetail, error) {
	row, err := db.q.GetNodeUpdateCampaign(ctx, strings.TrimSpace(campaignID))
	if err != nil {
		return NodeUpdateCampaignDetail{}, err
	}
	campaign, err := nodeUpdateCampaignFromStore(row)
	if err != nil {
		return NodeUpdateCampaignDetail{}, err
	}
	rows, err := db.q.ListNodeUpdateCampaignMembers(ctx, campaign.ID)
	if err != nil {
		return NodeUpdateCampaignDetail{}, err
	}
	members := make([]NodeUpdateCampaignMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, NodeUpdateCampaignMember{
			CampaignID: row.CampaignID, NodeID: row.NodeID, NodeName: row.NodeName,
			Position: row.Position, BatchNumber: row.BatchNumber, Kind: row.Kind,
			Payload: json.RawMessage(row.PayloadJson), OperationID: row.OperationID.String,
			Status: row.Status, Error: row.Error, StartedAt: row.StartedAt.String,
			FinishedAt: row.FinishedAt.String, UpdatedAt: row.UpdatedAt,
		})
	}
	return NodeUpdateCampaignDetail{Campaign: campaign, Members: members}, nil
}

func (db *DB) GetActiveNodeUpdateCampaign(ctx context.Context) (NodeUpdateCampaignDetail, bool, error) {
	row, err := db.q.GetActiveNodeUpdateCampaign(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return NodeUpdateCampaignDetail{}, false, nil
	}
	if err != nil {
		return NodeUpdateCampaignDetail{}, false, err
	}
	detail, err := db.GetNodeUpdateCampaign(ctx, row.ID)
	return detail, err == nil, err
}

func (db *DB) AttachNodeUpdateCampaignOperation(ctx context.Context, campaignID, nodeID, operationID, status string) error {
	return db.q.AttachNodeUpdateCampaignOperation(ctx, store.AttachNodeUpdateCampaignOperationParams{
		OperationID: nullableString(operationID), Status: status, CampaignID: campaignID, NodeID: nodeID,
	})
}

func (db *DB) ReplaceNodeUpdateCampaignOperationForRetry(ctx context.Context, campaignID, nodeID, operationID, status string) error {
	return db.q.ReplaceNodeUpdateCampaignOperationForRetry(ctx, store.ReplaceNodeUpdateCampaignOperationForRetryParams{
		OperationID: nullableString(operationID), Status: status, CampaignID: campaignID, NodeID: nodeID,
	})
}

func (db *DB) UpdateNodeUpdateCampaignMemberState(ctx context.Context, campaignID, nodeID, status, message string) error {
	return db.q.UpdateNodeUpdateCampaignMemberState(ctx, store.UpdateNodeUpdateCampaignMemberStateParams{
		Status: status, Error: message, NextFinishedAt: terminalCampaignTime(status),
		CampaignID: campaignID, NodeID: nodeID,
	})
}

func (db *DB) UpdateNodeUpdateCampaignState(ctx context.Context, campaignID, status string, currentBatch int64, message string) error {
	now := nullableString(time.Now().UTC().Format(time.RFC3339Nano))
	started, finished := sql.NullString{}, sql.NullString{}
	if status == "running" {
		started = now
	}
	if status == "succeeded" || status == "cancelled" {
		finished = now
	}
	return db.q.UpdateNodeUpdateCampaignState(ctx, store.UpdateNodeUpdateCampaignStateParams{
		Status: status, CurrentBatch: currentBatch, Error: message,
		NextStartedAt: started, NextFinishedAt: finished, ID: campaignID,
	})
}

func (db *DB) GetNodeUpdateCampaignIDByOperation(ctx context.Context, operationID string) (string, bool, error) {
	id, err := db.q.GetNodeUpdateCampaignIDByOperation(ctx, nullableString(operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func nodeUpdateCampaignFromStore(row store.NodeUpdateCampaign) (NodeUpdateCampaign, error) {
	var components []string
	if err := json.Unmarshal([]byte(row.ComponentsJson), &components); err != nil {
		return NodeUpdateCampaign{}, err
	}
	return NodeUpdateCampaign{
		ID: row.ID, Release: row.Release, Components: components, Status: row.Status,
		IdempotencyKey: row.IdempotencyKey, BatchSize: row.BatchSize,
		CurrentBatch: row.CurrentBatch, RequestedBy: row.RequestedBy,
		RequestedAt: row.RequestedAt, StartedAt: row.StartedAt.String,
		FinishedAt: row.FinishedAt.String, UpdatedAt: row.UpdatedAt, Error: row.Error,
	}, nil
}

func normalizeCampaignComponents(components []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, component := range components {
		component = strings.TrimSpace(component)
		if (component == "agent" || component == "sing_box") && !seen[component] {
			seen[component] = true
			out = append(out, component)
		}
	}
	sort.Strings(out)
	return out
}

func terminalCampaignTime(status string) sql.NullString {
	switch status {
	case "succeeded", "failed", "cancelled", "skipped":
		return nullableString(time.Now().UTC().Format(time.RFC3339Nano))
	default:
		return sql.NullString{}
	}
}
