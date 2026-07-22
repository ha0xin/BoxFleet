package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/mod/semver"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

const (
	maxNodeOperationRequestBytes = 256 * 1024
	maxNodeOperationLongPoll     = 45 * time.Second
)

var apiSemanticVersionPattern = regexp.MustCompile(`(?i)(v?\d+\.\d+\.\d+(?:-[0-9a-z.-]+)?(?:\+[0-9a-z.-]+)?)`)

type adminCreateNodeOperationPayload struct {
	Kind                 string          `json:"kind"`
	Payload              json.RawMessage `json:"payload"`
	IdempotencyKey       string          `json:"idempotency_key,omitempty"`
	RequiredCapabilities []string        `json:"required_capabilities,omitempty"`
	NotBefore            string          `json:"not_before,omitempty"`
	ExpiresAt            string          `json:"expires_at,omitempty"`
	RetryOf              string          `json:"retry_of,omitempty"`
}

type adminCreateNodeUpdatePayload struct {
	Components     []string `json:"components,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

type adminNodeOperationsPage struct {
	Operations []db.NodeOperation `json:"operations"`
	Total      int64              `json:"total"`
	Limit      int64              `json:"limit"`
	Offset     int64              `json:"offset"`
}

type adminNodeOperationDetail struct {
	Operation db.NodeOperation        `json:"operation"`
	Events    []db.NodeOperationEvent `json:"events"`
}

func nodeOperationClaimHandler(store *db.DB, notifier *nodeOperationNotifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var request model.NodeOperationClaimRequest
		if !decodeBoundedJSON(w, r, &request) {
			return
		}
		if len(request.Capabilities) > 128 {
			http.Error(w, "too many capabilities", http.StatusUnprocessableEntity)
			return
		}
		wait := time.Duration(request.WaitSeconds) * time.Second
		if wait < 0 {
			wait = 0
		}
		if wait > maxNodeOperationLongPoll {
			wait = maxNodeOperationLongPoll
		}

		w.Header().Set("Cache-Control", "no-store")
		claim := func() (db.ClaimedNodeOperation, bool, error) {
			return store.ClaimNodeOperation(r.Context(), db.ClaimNodeOperationParams{
				NodeName: nodeName, Capabilities: request.Capabilities,
				CurrentOperationID: request.CurrentOperationID, LeaseToken: request.LeaseToken,
			})
		}
		if assignment, found, err := claim(); err != nil {
			writeNodeOperationError(w, err)
			return
		} else if found {
			writeNodeOperationAssignment(w, assignment)
			return
		}
		if request.CurrentOperationID != "" {
			// The supplied durable local lease could not be resumed. Tell the
			// agent to discard it before it waits or claims a newer attempt.
			w.Header().Set("X-BoxFleet-Operation-Lease", "invalid")
			http.Error(w, "current operation lease is no longer valid", http.StatusConflict)
			return
		}
		if wait == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Subscribe between two DB reads to close the classic missed-wakeup
		// window: a concurrent create either appears in the second read or closes
		// this channel.
		wake := notifier.subscribe(nodeName)
		if assignment, found, err := claim(); err != nil {
			writeNodeOperationError(w, err)
			return
		} else if found {
			writeNodeOperationAssignment(w, assignment)
			return
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-timer.C:
		}
		assignment, found, err := claim()
		if err != nil {
			writeNodeOperationError(w, err)
			return
		}
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeNodeOperationAssignment(w, assignment)
	}
}

func nodeOperationLeaseHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var request model.NodeOperationLeaseRequest
		if !decodeBoundedJSON(w, r, &request) {
			return
		}
		cancelRequested, err := store.RenewNodeOperationLease(
			r.Context(), nodeName, chi.URLParam(r, "operation"), request.LeaseToken, request.Attempt,
		)
		if err != nil {
			writeNodeOperationError(w, err)
			return
		}
		writeJSON(w, model.NodeOperationLeaseResponse{
			LeaseExpiresAt:  time.Now().UTC().Add(db.NodeOperationLeaseDuration).Format(time.RFC3339Nano),
			CancelRequested: cancelRequested,
		})
	}
}

func nodeOperationEventHandler(store *db.DB, campaigns *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var report model.NodeOperationEventReport
		if !decodeBoundedJSON(w, r, &report) {
			return
		}
		operation, err := store.RecordNodeOperationEvent(r.Context(), db.RecordNodeOperationEventParams{
			NodeName: nodeName, OperationID: chi.URLParam(r, "operation"),
			LeaseToken: report.LeaseToken, Attempt: report.Attempt, Sequence: report.Sequence,
			Status: report.Status, Phase: report.Phase, Message: report.Message,
			Details: report.Details, Result: report.Result, Error: report.Error, ReportedAt: report.ReportedAt,
		})
		if err != nil {
			writeNodeOperationError(w, err)
			return
		}
		campaigns.reconcileForOperation(r.Context(), operation.ID)
		writeJSON(w, operation)
	}
}

func adminCreateNodeOperationHandler(store *db.DB, notifier *nodeOperationNotifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminCreateNodeOperationPayload
		if !decodeBoundedJSON(w, r, &payload) {
			return
		}
		if payload.IdempotencyKey == "" {
			payload.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
		if strings.HasPrefix(strings.TrimSpace(payload.Kind), "update.") {
			http.Error(w, "update operations must be created through the fixed release update endpoint", http.StatusUnprocessableEntity)
			return
		}
		operation, created, err := store.CreateNodeOperation(r.Context(), db.CreateNodeOperationParams{
			NodeName: chi.URLParam(r, "node"), Kind: payload.Kind, Payload: payload.Payload,
			IdempotencyKey: payload.IdempotencyKey, RequiredCapabilities: payload.RequiredCapabilities,
			NotBefore: payload.NotBefore, ExpiresAt: payload.ExpiresAt,
			RequestedBy: "admin-api", RetryOf: payload.RetryOf,
		})
		if err != nil {
			if errors.Is(err, db.ErrActiveNodeOperation) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			writeAdminError(w, err)
			return
		}
		if created {
			node, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
			if err == nil {
				notifier.notify(node.Name)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
		}
		writeJSON(w, operation)
	}
}

func adminCreateNodeUpdateHandler(store *db.DB, notifier *nodeOperationNotifier, catalog *updateCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request adminCreateNodeUpdatePayload
		if !decodeBoundedJSON(w, r, &request) {
			return
		}
		if request.IdempotencyKey == "" {
			request.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
		if request.IdempotencyKey == "" {
			http.Error(w, "idempotency key is required", http.StatusUnprocessableEntity)
			return
		}
		status, err := store.GetNodeConfigStatus(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if !containsCapability(status.Capabilities, model.CapabilityOperationsV1) {
			http.Error(w, "node requires one manual upgrade to an operations-capable agent", http.StatusUnprocessableEntity)
			return
		}
		agentAsset, singBoxAsset, err := catalog.assetsForNode(r.Context(), r, status.AgentGOOS, status.AgentGOARCH)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		selected, err := selectUpdateComponents(request.Components, status, agentAsset, singBoxAsset)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		payload := model.NodeUpdatePayload{Release: strings.TrimSpace(catalog.options.Version)}
		kind := ""
		if selected["agent"] {
			payload.Agent = &agentAsset
			kind = "update.agent"
		}
		if selected["sing_box"] {
			payload.SingBox = &singBoxAsset
			kind = "update.sing_box"
		}
		if payload.Agent != nil && payload.SingBox != nil {
			kind = "update.bundle"
		}
		rawPayload, err := json.Marshal(payload)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		operation, created, err := store.CreateNodeOperation(r.Context(), db.CreateNodeOperationParams{
			NodeName: status.NodeName, Kind: kind, Payload: rawPayload,
			IdempotencyKey: request.IdempotencyKey, RequestedBy: "admin-api",
		})
		if err != nil {
			if errors.Is(err, db.ErrActiveNodeOperation) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			writeAdminError(w, err)
			return
		}
		if created {
			notifier.notify(status.NodeName)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
		}
		writeJSON(w, operation)
	}
}

func selectUpdateComponents(components []string, status db.NodeConfigStatus, agentAsset, singBoxAsset model.UpdateAsset) (map[string]bool, error) {
	selected := make(map[string]bool)
	for _, component := range components {
		component = strings.TrimSpace(component)
		if component != "agent" && component != "sing_box" {
			return nil, fmt.Errorf("unsupported update component %q", component)
		}
		selected[component] = true
	}
	if len(selected) == 0 {
		if !versionsEquivalentAPI(status.AgentVersion, agentAsset.Version) {
			selected["agent"] = true
		}
		if !versionsEquivalentAPI(status.SingBoxVersion, singBoxAsset.Version) {
			selected["sing_box"] = true
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("node is already at the target release")
	}
	if selected["agent"] && !containsCapability(status.Capabilities, model.CapabilityAgentUpdateV1) {
		return nil, errors.New("node agent does not support self-update")
	}
	if selected["sing_box"] && !containsCapability(status.Capabilities, model.CapabilitySingBoxUpdateV1) {
		return nil, errors.New("node agent does not support sing-box update")
	}
	return selected, nil
}

func containsCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

func versionsEquivalentAPI(actual, expected string) bool {
	a := canonicalSemverAPI(actual)
	b := canonicalSemverAPI(expected)
	return a != "" && b != "" && semver.Compare(a, b) == 0
}

func canonicalSemverAPI(value string) string {
	match := apiSemanticVersionPattern.FindString(value)
	if match == "" {
		return ""
	}
	if match[0] != 'v' && match[0] != 'V' {
		match = "v" + match
	} else if match[0] == 'V' {
		match = "v" + match[1:]
	}
	return semver.Canonical(match)
}

func adminListNodeOperationsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset, ok := operationPageParams(w, r)
		if !ok {
			return
		}
		operations, total, err := store.ListNodeOperations(r.Context(), chi.URLParam(r, "node"), limit, offset)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeOperationsPage{Operations: operations, Total: total, Limit: limit, Offset: offset})
	}
}

func adminCurrentNodeOperationHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operation, found, err := store.GetActiveNodeOperation(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, operation)
	}
}

func adminNodeOperationDetailHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operation, ok := adminOperationForNode(w, r, store)
		if !ok {
			return
		}
		events, err := store.ListNodeOperationEvents(r.Context(), operation.ID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeOperationDetail{Operation: operation, Events: events})
	}
}

func adminCancelNodeOperationHandler(store *db.DB, notifier *nodeOperationNotifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		operation, ok := adminOperationForNode(w, r, store)
		if !ok {
			return
		}
		operation, err := store.RequestNodeOperationCancel(r.Context(), operation.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "operation is already terminal", http.StatusConflict)
				return
			}
			writeAdminError(w, err)
			return
		}
		node, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
		if err == nil {
			notifier.notify(node.Name)
		}
		writeJSON(w, operation)
	}
}

func adminOperationForNode(w http.ResponseWriter, r *http.Request, store *db.DB) (db.NodeOperation, bool) {
	node, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
	if err != nil {
		writeAdminError(w, err)
		return db.NodeOperation{}, false
	}
	operation, err := store.GetNodeOperation(r.Context(), chi.URLParam(r, "operation"))
	if err != nil || operation.NodeID != node.ID {
		http.Error(w, "operation not found", http.StatusNotFound)
		return db.NodeOperation{}, false
	}
	return operation, true
}

func operationPageParams(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	parse := func(name string, fallback int64) (int64, error) {
		raw := strings.TrimSpace(r.URL.Query().Get(name))
		if raw == "" {
			return fallback, nil
		}
		return strconv.ParseInt(raw, 10, 64)
	}
	limit, err := parse("limit", 50)
	if err != nil || limit <= 0 || limit > 200 {
		http.Error(w, "limit must be between 1 and 200", http.StatusUnprocessableEntity)
		return 0, 0, false
	}
	offset, err := parse("offset", 0)
	if err != nil || offset < 0 {
		http.Error(w, "offset must be non-negative", http.StatusUnprocessableEntity)
		return 0, 0, false
	}
	return limit, offset, true
}

func decodeBoundedJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxNodeOperationRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json: trailing data", http.StatusBadRequest)
		return false
	}
	return true
}

func writeNodeOperationAssignment(w http.ResponseWriter, claimed db.ClaimedNodeOperation) {
	writeJSON(w, model.NodeOperationAssignment{
		ID: claimed.Operation.ID, Kind: claimed.Operation.Kind, Payload: claimed.Operation.Payload,
		Attempt: claimed.Operation.Attempt, LeaseToken: claimed.LeaseToken,
		LeaseExpiresAt: claimed.Operation.LeaseExpiresAt, CancelRequested: claimed.Operation.CancelRequested,
	})
}

func writeNodeOperationError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrInvalidOperationLease) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	http.Error(w, fmt.Sprintf("node operation: %v", err), http.StatusUnprocessableEntity)
}
