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
	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/secret"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const NodeOperationLeaseDuration = 90 * time.Second

var (
	ErrActiveNodeOperation          = errors.New("node already has an active operation")
	ErrInvalidOperationLease        = errors.New("invalid or expired operation lease")
	ErrOperationIdempotencyConflict = errors.New("operation idempotency key was reused with different parameters")
)

var allowedNodeOperationKinds = map[string]bool{
	"update.bundle":       true,
	"update.agent":        true,
	"update.sing_box":     true,
	"config.reconcile":    true,
	"diagnostics.collect": true,
	"logs.collect":        true,
}

type NodeOperation struct {
	ID                   string          `json:"id"`
	NodeID               string          `json:"node_id"`
	Kind                 string          `json:"kind"`
	Status               string          `json:"status"`
	Phase                string          `json:"phase"`
	Payload              json.RawMessage `json:"payload"`
	Result               json.RawMessage `json:"result"`
	IdempotencyKey       string          `json:"idempotency_key"`
	RequiredCapabilities []string        `json:"required_capabilities"`
	Attempt              int64           `json:"attempt"`
	LeaseExpiresAt       string          `json:"lease_expires_at,omitempty"`
	CancelRequested      bool            `json:"cancel_requested"`
	NotBefore            string          `json:"not_before,omitempty"`
	ExpiresAt            string          `json:"expires_at,omitempty"`
	RequestedBy          string          `json:"requested_by"`
	RequestedAt          string          `json:"requested_at"`
	ClaimedAt            string          `json:"claimed_at,omitempty"`
	StartedAt            string          `json:"started_at,omitempty"`
	FinishedAt           string          `json:"finished_at,omitempty"`
	UpdatedAt            string          `json:"updated_at"`
	Error                string          `json:"error,omitempty"`
	RetryOf              string          `json:"retry_of,omitempty"`
}

type NodeOperationEvent struct {
	ID          string          `json:"id"`
	OperationID string          `json:"operation_id"`
	Attempt     int64           `json:"attempt"`
	Sequence    int64           `json:"sequence"`
	Status      string          `json:"status"`
	Phase       string          `json:"phase"`
	Message     string          `json:"message,omitempty"`
	Details     json.RawMessage `json:"details"`
	Result      json.RawMessage `json:"result"`
	Error       string          `json:"error,omitempty"`
	ReportedAt  string          `json:"reported_at"`
	CreatedAt   string          `json:"created_at"`
}

type CreateNodeOperationParams struct {
	NodeName             string
	Kind                 string
	Payload              json.RawMessage
	IdempotencyKey       string
	RequiredCapabilities []string
	NotBefore            string
	ExpiresAt            string
	RequestedBy          string
	RetryOf              string
}

type ClaimNodeOperationParams struct {
	NodeName           string
	Capabilities       []string
	CurrentOperationID string
	LeaseToken         string
}

type ClaimedNodeOperation struct {
	Operation  NodeOperation `json:"operation"`
	LeaseToken string        `json:"lease_token"`
}

type RecordNodeOperationEventParams struct {
	NodeName    string
	OperationID string
	LeaseToken  string
	Attempt     int64
	Sequence    int64
	Status      string
	Phase       string
	Message     string
	Details     json.RawMessage
	Result      json.RawMessage
	Error       string
	ReportedAt  string
}

func (db *DB) CreateNodeOperation(ctx context.Context, params CreateNodeOperationParams) (NodeOperation, bool, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return NodeOperation{}, false, err
	}
	params.Kind = strings.TrimSpace(params.Kind)
	if !allowedNodeOperationKinds[params.Kind] {
		return NodeOperation{}, false, fmt.Errorf("unsupported node operation kind %q", params.Kind)
	}
	payload, err := normalizeJSONObject(params.Payload)
	if err != nil {
		return NodeOperation{}, false, fmt.Errorf("operation payload: %w", err)
	}
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if params.IdempotencyKey == "" {
		return NodeOperation{}, false, errors.New("operation idempotency key is required")
	}
	if len(params.IdempotencyKey) > 128 {
		return NodeOperation{}, false, errors.New("operation idempotency key must be at most 128 characters")
	}
	capabilities := normalizeCapabilities(append(params.RequiredCapabilities, requiredCapabilitiesForOperation(params.Kind)...))
	if err := validateCapabilities(capabilities); err != nil {
		return NodeOperation{}, false, err
	}
	capabilitiesJSON, _ := json.Marshal(capabilities)
	notBefore, notBeforeTime, err := normalizeOptionalTimestamp(params.NotBefore, "not_before")
	if err != nil {
		return NodeOperation{}, false, err
	}
	expiresAt, expiresAtTime, err := normalizeOptionalTimestamp(params.ExpiresAt, "expires_at")
	if err != nil {
		return NodeOperation{}, false, err
	}
	params.NotBefore, params.ExpiresAt = notBefore, expiresAt
	if !notBeforeTime.IsZero() && !expiresAtTime.IsZero() && !expiresAtTime.After(notBeforeTime) {
		return NodeOperation{}, false, errors.New("operation expires_at must be after not_before")
	}
	params.RequestedBy = strings.TrimSpace(params.RequestedBy)
	if params.RequestedBy == "" {
		params.RequestedBy = "admin"
	}

	var created store.NodeOperation
	var wasCreated bool
	err = db.withTx(ctx, func(q *store.Queries) error {
		existing, err := q.GetNodeOperationByIdempotencyKey(ctx, store.GetNodeOperationByIdempotencyKeyParams{
			NodeID:         node.ID,
			IdempotencyKey: params.IdempotencyKey,
		})
		if err == nil {
			if !nodeOperationRequestMatches(existing, params.Kind, string(payload), params.IdempotencyKey, string(capabilitiesJSON), params.NotBefore, params.ExpiresAt, params.RetryOf) {
				return ErrOperationIdempotencyConflict
			}
			created = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := q.GetActiveNodeOperation(ctx, node.ID); err == nil {
			return ErrActiveNodeOperation
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if params.RetryOf != "" {
			previous, err := q.GetNodeOperationByID(ctx, params.RetryOf)
			if err != nil {
				return fmt.Errorf("retry_of: %w", err)
			}
			if previous.NodeID != node.ID || previous.Status == "queued" || previous.Status == "running" {
				return errors.New("retry_of must reference a terminal operation for the same node")
			}
		}
		operationID, err := id.New("op")
		if err != nil {
			return err
		}
		if err := q.CreateNodeOperation(ctx, store.CreateNodeOperationParams{
			ID:                       operationID,
			NodeID:                   node.ID,
			Kind:                     params.Kind,
			PayloadJson:              string(payload),
			IdempotencyKey:           params.IdempotencyKey,
			RequiredCapabilitiesJson: string(capabilitiesJSON),
			NotBefore:                nullableString(params.NotBefore),
			ExpiresAt:                nullableString(params.ExpiresAt),
			RequestedBy:              params.RequestedBy,
			RetryOf:                  nullableString(params.RetryOf),
		}); err != nil {
			return err
		}
		created, err = q.GetNodeOperationByID(ctx, operationID)
		wasCreated = err == nil
		return err
	})
	if err != nil {
		// A concurrent identical request may win the unique constraint between
		// the checks above. Return its durable row to preserve API idempotency.
		if existing, getErr := db.q.GetNodeOperationByIdempotencyKey(ctx, store.GetNodeOperationByIdempotencyKeyParams{
			NodeID: node.ID, IdempotencyKey: params.IdempotencyKey,
		}); getErr == nil {
			if !nodeOperationRequestMatches(existing, params.Kind, string(payload), params.IdempotencyKey, string(capabilitiesJSON), params.NotBefore, params.ExpiresAt, params.RetryOf) {
				return NodeOperation{}, false, ErrOperationIdempotencyConflict
			}
			op, mapErr := nodeOperationFromStore(existing)
			return op, false, mapErr
		}
		return NodeOperation{}, false, err
	}
	op, err := nodeOperationFromStore(created)
	return op, wasCreated, err
}

func requiredCapabilitiesForOperation(kind string) []string {
	switch kind {
	case "update.agent":
		return []string{
			model.CapabilityOperationsV1, model.CapabilityAgentUpdateV1, model.CapabilityStreamingDownloadV1,
			model.CapabilityVersionedInstallV1, model.CapabilityAgentRestartResumeV1,
		}
	case "update.sing_box":
		return []string{
			model.CapabilityOperationsV1, model.CapabilitySingBoxUpdateV1, model.CapabilityStreamingDownloadV1,
			model.CapabilityVersionedInstallV1, model.CapabilitySingBoxRollbackV1,
		}
	case "update.bundle":
		return []string{
			model.CapabilityOperationsV1, model.CapabilityAgentUpdateV1, model.CapabilitySingBoxUpdateV1,
			model.CapabilityStreamingDownloadV1, model.CapabilityVersionedInstallV1,
			model.CapabilityAgentRestartResumeV1, model.CapabilitySingBoxRollbackV1,
		}
	default:
		return []string{model.CapabilityOperationsV1}
	}
}

func (db *DB) GetNodeOperation(ctx context.Context, operationID string) (NodeOperation, error) {
	row, err := db.q.GetNodeOperationByID(ctx, strings.TrimSpace(operationID))
	if err != nil {
		return NodeOperation{}, err
	}
	return nodeOperationFromStore(row)
}

func (db *DB) GetActiveNodeOperation(ctx context.Context, nodeName string) (NodeOperation, bool, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return NodeOperation{}, false, err
	}
	if err := db.q.ExpireQueuedNodeOperations(ctx, node.ID); err != nil {
		return NodeOperation{}, false, err
	}
	row, err := db.q.GetActiveNodeOperation(ctx, node.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return NodeOperation{}, false, nil
	}
	if err != nil {
		return NodeOperation{}, false, err
	}
	op, err := nodeOperationFromStore(row)
	return op, true, err
}

func (db *DB) ListNodeOperations(ctx context.Context, nodeName string, limit, offset int64) ([]NodeOperation, int64, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.q.ListNodeOperations(ctx, store.ListNodeOperationsParams{
		NodeID: node.ID, ResultLimit: limit, ResultOffset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	total, err := db.q.CountNodeOperations(ctx, node.ID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]NodeOperation, 0, len(rows))
	for _, row := range rows {
		op, err := nodeOperationFromStore(row)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, op)
	}
	return out, total, nil
}

func (db *DB) ListActiveNodeOperations(ctx context.Context) ([]NodeOperation, error) {
	rows, err := db.q.ListActiveNodeOperations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NodeOperation, 0, len(rows))
	for _, row := range rows {
		operation, err := nodeOperationFromStore(row)
		if err != nil {
			return nil, err
		}
		out = append(out, operation)
	}
	return out, nil
}

func (db *DB) ClaimNodeOperation(ctx context.Context, params ClaimNodeOperationParams) (ClaimedNodeOperation, bool, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	if err := db.q.ExpireQueuedNodeOperations(ctx, node.ID); err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	row, err := db.q.GetActiveNodeOperation(ctx, node.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ClaimedNodeOperation{}, false, nil
	}
	if err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	op, err := nodeOperationFromStore(row)
	if err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	if !hasCapabilities(params.Capabilities, op.RequiredCapabilities) {
		return ClaimedNodeOperation{}, false, nil
	}
	leaseExpiresAt := time.Now().UTC().Add(NodeOperationLeaseDuration).Format(time.RFC3339Nano)
	if strings.TrimSpace(params.CurrentOperationID) == op.ID && strings.TrimSpace(params.LeaseToken) != "" {
		leaseHash := SHA256Hex([]byte(params.LeaseToken))
		resumed, err := db.q.ResumeNodeOperationLease(ctx, store.ResumeNodeOperationLeaseParams{
			LeaseExpiresAt: nullableString(leaseExpiresAt),
			ID:             op.ID,
			NodeID:         node.ID,
			LeaseTokenHash: nullableString(leaseHash),
		})
		if err == nil {
			resumedOp, mapErr := nodeOperationFromStore(resumed)
			return ClaimedNodeOperation{Operation: resumedOp, LeaseToken: params.LeaseToken}, true, mapErr
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ClaimedNodeOperation{}, false, err
		}
	}
	leaseSecret, err := secret.HexBytes(32)
	if err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	leaseToken := "bfl_" + leaseSecret
	claimed, err := db.q.ClaimNodeOperation(ctx, store.ClaimNodeOperationParams{
		LeaseTokenHash: nullableString(SHA256Hex([]byte(leaseToken))),
		LeaseExpiresAt: nullableString(leaseExpiresAt),
		ID:             op.ID,
		NodeID:         node.ID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ClaimedNodeOperation{}, false, nil
	}
	if err != nil {
		return ClaimedNodeOperation{}, false, err
	}
	claimedOp, err := nodeOperationFromStore(claimed)
	return ClaimedNodeOperation{Operation: claimedOp, LeaseToken: leaseToken}, true, err
}

func (db *DB) RenewNodeOperationLease(ctx context.Context, nodeName, operationID, leaseToken string, attempt int64) (bool, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return false, err
	}
	if operationID == "" || leaseToken == "" || attempt <= 0 {
		return false, ErrInvalidOperationLease
	}
	cancelRequested, err := db.q.RenewNodeOperationLease(ctx, store.RenewNodeOperationLeaseParams{
		LeaseExpiresAt: nullableString(time.Now().UTC().Add(NodeOperationLeaseDuration).Format(time.RFC3339Nano)),
		ID:             operationID,
		NodeID:         node.ID,
		LeaseTokenHash: nullableString(SHA256Hex([]byte(leaseToken))),
		Attempt:        attempt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrInvalidOperationLease
	}
	return cancelRequested != 0, err
}

func (db *DB) RequestNodeOperationCancel(ctx context.Context, operationID string) (NodeOperation, error) {
	row, err := db.q.RequestNodeOperationCancel(ctx, strings.TrimSpace(operationID))
	if err != nil {
		return NodeOperation{}, err
	}
	return nodeOperationFromStore(row)
}

func (db *DB) RecordNodeOperationEvent(ctx context.Context, params RecordNodeOperationEventParams) (NodeOperation, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return NodeOperation{}, err
	}
	if params.Attempt <= 0 || params.Sequence <= 0 || params.LeaseToken == "" {
		return NodeOperation{}, ErrInvalidOperationLease
	}
	if !validOperationEventStatus(params.Status) {
		return NodeOperation{}, fmt.Errorf("invalid operation event status %q", params.Status)
	}
	if !validOperationPhase(params.Phase) {
		return NodeOperation{}, fmt.Errorf("invalid operation phase %q", params.Phase)
	}
	params.Message = strings.TrimSpace(params.Message)
	if len(params.Message) > 2048 {
		return NodeOperation{}, errors.New("operation event message must be at most 2048 bytes")
	}
	params.Error = strings.TrimSpace(params.Error)
	if len(params.Error) > 8192 {
		return NodeOperation{}, errors.New("operation event error must be at most 8192 bytes")
	}
	details, err := normalizeJSONObject(params.Details)
	if err != nil {
		return NodeOperation{}, fmt.Errorf("operation event details: %w", err)
	}
	result, err := normalizeJSONObject(params.Result)
	if err != nil {
		return NodeOperation{}, fmt.Errorf("operation event result: %w", err)
	}
	if params.ReportedAt == "" {
		params.ReportedAt = time.Now().UTC().Format(time.RFC3339Nano)
	} else if _, err := time.Parse(time.RFC3339Nano, params.ReportedAt); err != nil {
		return NodeOperation{}, fmt.Errorf("invalid operation event reported_at: %w", err)
	}

	var updated store.NodeOperation
	err = db.withTx(ctx, func(q *store.Queries) error {
		current, err := q.GetNodeOperationByID(ctx, params.OperationID)
		if err != nil {
			return err
		}
		if current.NodeID != node.ID {
			return ErrInvalidOperationLease
		}
		// A terminal event may have committed even when its HTTP response was
		// lost. Accept an exact replay before checking the now-cleared lease so the
		// agent's durable outbox can converge after any response-loss window.
		existing, existingErr := q.GetNodeOperationEvent(ctx, store.GetNodeOperationEventParams{
			OperationID: params.OperationID, Attempt: params.Attempt, Sequence: params.Sequence,
		})
		if existingErr == nil {
			if existing.Status != params.Status || existing.Phase != params.Phase || existing.Message != params.Message ||
				existing.DetailsJson != string(details) || existing.ResultJson != string(result) || existing.Error != params.Error {
				return errors.New("operation event sequence was reused with different content")
			}
			updated = current
			return nil
		}
		if !errors.Is(existingErr, sql.ErrNoRows) {
			return existingErr
		}
		if current.Status != "running" || current.Attempt != params.Attempt ||
			!current.LeaseTokenHash.Valid || current.LeaseTokenHash.String != SHA256Hex([]byte(params.LeaseToken)) {
			return ErrInvalidOperationLease
		}
		if params.Status == "cancelled" && current.CancelRequested == 0 {
			return errors.New("operation was not requested to cancel")
		}
		lastSequence, err := q.LastNodeOperationEventSequence(ctx, store.LastNodeOperationEventSequenceParams{
			OperationID: params.OperationID, Attempt: params.Attempt,
		})
		if err != nil {
			return err
		}
		if params.Sequence <= lastSequence {
			return errors.New("operation event sequence already exists")
		}
		if params.Sequence != lastSequence+1 {
			return fmt.Errorf("operation event sequence must be %d", lastSequence+1)
		}
		eventID, err := id.New("opev")
		if err != nil {
			return err
		}
		if err := q.AppendNodeOperationEvent(ctx, store.AppendNodeOperationEventParams{
			ID: eventID, OperationID: params.OperationID, Attempt: params.Attempt,
			Sequence: params.Sequence, Status: params.Status, Phase: params.Phase,
			Message: params.Message, DetailsJson: string(details), ResultJson: string(result),
			Error: params.Error, ReportedAt: params.ReportedAt,
		}); err != nil {
			return err
		}
		updated, err = q.ApplyNodeOperationEvent(ctx, store.ApplyNodeOperationEventParams{
			NextStatus:     params.Status,
			Phase:          params.Phase,
			ResultJson:     string(result),
			Error:          params.Error,
			LeaseExpiresAt: nullableString(time.Now().UTC().Add(NodeOperationLeaseDuration).Format(time.RFC3339Nano)),
			NextFinishedAt: terminalFinishedAt(params.Status),
			ID:             params.OperationID,
			NodeID:         node.ID,
			LeaseTokenHash: nullableString(SHA256Hex([]byte(params.LeaseToken))),
			Attempt:        params.Attempt,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidOperationLease
		}
		return err
	})
	if err != nil {
		return NodeOperation{}, err
	}
	return nodeOperationFromStore(updated)
}

func (db *DB) ListNodeOperationEvents(ctx context.Context, operationID string) ([]NodeOperationEvent, error) {
	rows, err := db.q.ListNodeOperationEvents(ctx, strings.TrimSpace(operationID))
	if err != nil {
		return nil, err
	}
	out := make([]NodeOperationEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, nodeOperationEventFromStore(row))
	}
	return out, nil
}

func nodeOperationFromStore(row store.NodeOperation) (NodeOperation, error) {
	var capabilities []string
	if err := json.Unmarshal([]byte(row.RequiredCapabilitiesJson), &capabilities); err != nil {
		return NodeOperation{}, fmt.Errorf("decode operation capabilities: %w", err)
	}
	if !json.Valid([]byte(row.PayloadJson)) || !json.Valid([]byte(row.ResultJson)) {
		return NodeOperation{}, errors.New("operation contains invalid JSON")
	}
	return NodeOperation{
		ID: row.ID, NodeID: row.NodeID, Kind: row.Kind, Status: row.Status, Phase: row.Phase,
		Payload: json.RawMessage(row.PayloadJson), Result: json.RawMessage(row.ResultJson),
		IdempotencyKey: row.IdempotencyKey, RequiredCapabilities: capabilities,
		Attempt: row.Attempt, LeaseExpiresAt: row.LeaseExpiresAt.String,
		CancelRequested: row.CancelRequested != 0, NotBefore: row.NotBefore.String,
		ExpiresAt: row.ExpiresAt.String, RequestedBy: row.RequestedBy,
		RequestedAt: row.RequestedAt, ClaimedAt: row.ClaimedAt.String,
		StartedAt: row.StartedAt.String, FinishedAt: row.FinishedAt.String,
		UpdatedAt: row.UpdatedAt, Error: row.Error, RetryOf: row.RetryOf.String,
	}, nil
}

func nodeOperationEventFromStore(row store.NodeOperationEvent) NodeOperationEvent {
	return NodeOperationEvent{
		ID: row.ID, OperationID: row.OperationID, Attempt: row.Attempt,
		Sequence: row.Sequence, Status: row.Status, Phase: row.Phase,
		Message: row.Message, Details: json.RawMessage(row.DetailsJson),
		Result: json.RawMessage(row.ResultJson), Error: row.Error,
		ReportedAt: row.ReportedAt, CreatedAt: row.CreatedAt,
	}
}

func normalizeCapabilities(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func hasCapabilities(available, required []string) bool {
	set := make(map[string]bool, len(available))
	for _, capability := range normalizeCapabilities(available) {
		set[capability] = true
	}
	for _, capability := range required {
		if !set[capability] {
			return false
		}
	}
	return true
}

func normalizeJSONObject(raw json.RawMessage) ([]byte, error) {
	if len(raw) > 256*1024 {
		return nil, errors.New("JSON object must be at most 256 KiB")
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return []byte("{}"), nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("must be a JSON object")
	}
	return json.Marshal(object)
}

func normalizeOptionalTimestamp(value, field string) (string, time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return "", time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("invalid operation %s: %w", field, err)
	}
	parsed = parsed.UTC()
	return parsed.Format(time.RFC3339Nano), parsed, nil
}

func validateCapabilities(capabilities []string) error {
	if len(capabilities) > 128 {
		return errors.New("operation requires too many capabilities")
	}
	for _, capability := range capabilities {
		if len(capability) > 128 {
			return errors.New("operation capability must be at most 128 characters")
		}
		for _, r := range capability {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
				continue
			}
			return fmt.Errorf("invalid operation capability %q", capability)
		}
	}
	return nil
}

func nodeOperationRequestMatches(row store.NodeOperation, kind, payload, idempotencyKey, capabilities, notBefore, expiresAt, retryOf string) bool {
	return row.Kind == kind && row.PayloadJson == payload && row.IdempotencyKey == idempotencyKey &&
		row.RequiredCapabilitiesJson == capabilities && row.NotBefore.String == notBefore &&
		row.ExpiresAt.String == expiresAt && row.RetryOf.String == retryOf
}

func nullableString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func validOperationEventStatus(status string) bool {
	switch status {
	case "running", "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalFinishedAt(status string) sql.NullString {
	if status == "running" {
		return sql.NullString{}
	}
	return nullableString(time.Now().UTC().Format(time.RFC3339Nano))
}

func validOperationPhase(phase string) bool {
	phase = strings.TrimSpace(phase)
	if phase == "" || len(phase) > 64 {
		return false
	}
	for i, r := range phase {
		if (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') || (i > 0 && r == '_') {
			continue
		}
		return false
	}
	return true
}
