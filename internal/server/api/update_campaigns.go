package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

type updateCampaignController struct {
	store    *db.DB
	notifier *nodeOperationNotifier
	mu       sync.Mutex
}

type adminCreateUpdateCampaignPayload struct {
	Nodes          []string `json:"nodes,omitempty"`
	Components     []string `json:"components,omitempty"`
	BatchSize      int64    `json:"batch_size,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

func newUpdateCampaignController(store *db.DB, notifier *nodeOperationNotifier) *updateCampaignController {
	return &updateCampaignController{store: store, notifier: notifier}
}

func (c *updateCampaignController) reconcile(ctx context.Context, campaignID string) (db.NodeUpdateCampaignDetail, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for iteration := 0; iteration < 4; iteration++ {
		detail, err := c.store.GetNodeUpdateCampaign(ctx, campaignID)
		if err != nil {
			return db.NodeUpdateCampaignDetail{}, err
		}
		if detail.Campaign.Status == "paused" || detail.Campaign.Status == "succeeded" || detail.Campaign.Status == "cancelled" {
			return detail, nil
		}
		for _, member := range detail.Members {
			if member.OperationID == "" {
				continue
			}
			operation, err := c.store.GetNodeOperation(ctx, member.OperationID)
			if err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			status := operation.Status
			if status == "expired" {
				status = "failed"
			}
			if member.Status != status || member.Error != operation.Error {
				if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, status, operation.Error); err != nil {
					return db.NodeUpdateCampaignDetail{}, err
				}
			}
		}
		detail, err = c.store.GetNodeUpdateCampaign(ctx, campaignID)
		if err != nil {
			return db.NodeUpdateCampaignDetail{}, err
		}
		if detail.Campaign.Status == "queued" {
			if err := c.store.UpdateNodeUpdateCampaignState(ctx, campaignID, "running", 0, ""); err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			continue
		}

		current := detail.Campaign.CurrentBatch
		var currentMembers []db.NodeUpdateCampaignMember
		for _, member := range detail.Members {
			if member.BatchNumber == current {
				currentMembers = append(currentMembers, member)
			}
		}
		if len(currentMembers) == 0 {
			return db.NodeUpdateCampaignDetail{}, errors.New("campaign current batch has no members")
		}
		for _, member := range currentMembers {
			if member.Status == "failed" || member.Status == "cancelled" {
				message := fmt.Sprintf("batch %d paused after %s failed: %s", current, member.NodeName, member.Error)
				if err := c.store.UpdateNodeUpdateCampaignState(ctx, campaignID, "paused", current, message); err != nil {
					return db.NodeUpdateCampaignDetail{}, err
				}
				return c.store.GetNodeUpdateCampaign(ctx, campaignID)
			}
		}

		createdAny := false
		waiting := false
		for _, member := range currentMembers {
			if member.Status != "pending" || member.OperationID != "" {
				continue
			}
			operation, created, err := c.store.CreateNodeOperation(ctx, db.CreateNodeOperationParams{
				NodeName: member.NodeName, Kind: member.Kind, Payload: member.Payload,
				IdempotencyKey: "campaign:" + campaignID + ":" + member.NodeID,
				RequestedBy:    "update-campaign:" + campaignID,
			})
			if errors.Is(err, db.ErrActiveNodeOperation) {
				waiting = true
				continue
			}
			if err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			if err := c.store.AttachNodeUpdateCampaignOperation(ctx, campaignID, member.NodeID, operation.ID, operation.Status); err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			if created {
				c.notifier.notify(member.NodeName)
			}
			createdAny = true
		}
		if createdAny || waiting {
			return c.store.GetNodeUpdateCampaign(ctx, campaignID)
		}

		allCompleted := true
		for _, member := range currentMembers {
			if member.Status != "succeeded" && member.Status != "skipped" {
				allCompleted = false
				break
			}
		}
		if !allCompleted {
			return detail, nil
		}
		nextBatch := int64(-1)
		for _, member := range detail.Members {
			if member.Status == "pending" && member.BatchNumber > current && (nextBatch < 0 || member.BatchNumber < nextBatch) {
				nextBatch = member.BatchNumber
			}
		}
		if nextBatch < 0 {
			if err := c.store.UpdateNodeUpdateCampaignState(ctx, campaignID, "succeeded", current, ""); err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			return c.store.GetNodeUpdateCampaign(ctx, campaignID)
		}
		if err := c.store.UpdateNodeUpdateCampaignState(ctx, campaignID, "running", nextBatch, ""); err != nil {
			return db.NodeUpdateCampaignDetail{}, err
		}
	}
	return c.store.GetNodeUpdateCampaign(ctx, campaignID)
}

func (c *updateCampaignController) reconcileForOperation(ctx context.Context, operationID string) {
	campaignID, found, err := c.store.GetNodeUpdateCampaignIDByOperation(ctx, operationID)
	if err == nil && found {
		_, _ = c.reconcile(ctx, campaignID)
	}
}

func adminCreateUpdateCampaignHandler(store *db.DB, catalog *updateCatalog, controller *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request adminCreateUpdateCampaignPayload
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
		if request.BatchSize == 0 {
			request.BatchSize = 2
		}
		if request.BatchSize < 1 || request.BatchSize > 2 {
			http.Error(w, "batch_size must be 1 or 2", http.StatusUnprocessableEntity)
			return
		}
		components, err := normalizeRequestedUpdateComponents(request.Components)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		members, err := buildUpdateCampaignMembers(r, store, catalog, request.Nodes, components, request.BatchSize)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		detail, created, err := store.CreateNodeUpdateCampaign(r.Context(), db.CreateNodeUpdateCampaignParams{
			Release: strings.TrimSpace(catalog.options.Version), Components: components,
			IdempotencyKey: request.IdempotencyKey, BatchSize: request.BatchSize,
			RequestedBy: "admin-api", Members: members,
		})
		if err != nil {
			if errors.Is(err, db.ErrOperationIdempotencyConflict) || strings.Contains(err.Error(), "UNIQUE constraint failed") {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			writeAdminError(w, err)
			return
		}
		detail, err = controller.reconcile(r.Context(), detail.Campaign.ID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if created {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
		}
		writeJSON(w, detail)
	}
}

func adminUpdateCampaignHandler(controller *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := controller.reconcile(r.Context(), chi.URLParam(r, "campaign"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, detail)
	}
}

func adminCurrentUpdateCampaignHandler(store *db.DB, controller *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, found, err := store.GetActiveNodeUpdateCampaign(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if !found {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		detail, err = controller.reconcile(r.Context(), detail.Campaign.ID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, detail)
	}
}

func adminCancelUpdateCampaignHandler(store *db.DB, controller *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		detail, err := store.GetNodeUpdateCampaign(r.Context(), chi.URLParam(r, "campaign"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if detail.Campaign.Status == "succeeded" || detail.Campaign.Status == "cancelled" {
			http.Error(w, "campaign is already terminal", http.StatusConflict)
			return
		}
		if err := store.UpdateNodeUpdateCampaignState(r.Context(), detail.Campaign.ID, "cancelled", detail.Campaign.CurrentBatch, "cancelled by admin"); err != nil {
			writeAdminError(w, err)
			return
		}
		for _, member := range detail.Members {
			if member.OperationID != "" && (member.Status == "queued" || member.Status == "running") {
				_, _ = store.RequestNodeOperationCancel(r.Context(), member.OperationID)
			}
		}
		detail, _ = store.GetNodeUpdateCampaign(r.Context(), detail.Campaign.ID)
		writeJSON(w, detail)
	}
}

func adminResumeUpdateCampaignHandler(store *db.DB, controller *updateCampaignController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		detail, err := store.GetNodeUpdateCampaign(r.Context(), chi.URLParam(r, "campaign"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if detail.Campaign.Status != "paused" {
			http.Error(w, "only a paused campaign can be resumed", http.StatusConflict)
			return
		}
		retried := 0
		for _, member := range detail.Members {
			if member.BatchNumber != detail.Campaign.CurrentBatch || (member.Status != "failed" && member.Status != "cancelled") {
				continue
			}
			if member.OperationID == "" {
				http.Error(w, fmt.Sprintf("campaign member %s has no failed operation to retry", member.NodeName), http.StatusConflict)
				return
			}
			operation, _, err := store.CreateNodeOperation(r.Context(), db.CreateNodeOperationParams{
				NodeName: member.NodeName, Kind: member.Kind, Payload: member.Payload,
				IdempotencyKey: "campaign-retry:" + db.SHA256Hex([]byte(detail.Campaign.ID+":"+member.NodeID+":"+member.OperationID)),
				RequestedBy:    "update-campaign:" + detail.Campaign.ID,
				RetryOf:        member.OperationID,
			})
			if err != nil {
				writeAdminError(w, err)
				return
			}
			if err := store.ReplaceNodeUpdateCampaignOperationForRetry(r.Context(), detail.Campaign.ID, member.NodeID, operation.ID, operation.Status); err != nil {
				writeAdminError(w, err)
				return
			}
			controller.notifier.notify(member.NodeName)
			retried++
		}
		if retried == 0 {
			http.Error(w, "paused campaign has no failed member in the current batch", http.StatusConflict)
			return
		}
		if err := store.UpdateNodeUpdateCampaignState(r.Context(), detail.Campaign.ID, "running", detail.Campaign.CurrentBatch, ""); err != nil {
			writeAdminError(w, err)
			return
		}
		detail, err = store.GetNodeUpdateCampaign(r.Context(), detail.Campaign.ID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, detail)
	}
}

func normalizeRequestedUpdateComponents(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"agent", "sing_box"}, nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "agent" && value != "sing_box" {
			return nil, fmt.Errorf("unsupported update component %q", value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func buildUpdateCampaignMembers(
	r *http.Request,
	store *db.DB,
	catalog *updateCatalog,
	requestedNodes, components []string,
	batchSize int64,
) ([]db.CreateNodeUpdateCampaignMemberParams, error) {
	nodes, err := store.ListNodes(r.Context())
	if err != nil {
		return nil, err
	}
	statuses, err := store.ListNodeConfigStatuses(r.Context())
	if err != nil {
		return nil, err
	}
	statusByName := make(map[string]db.NodeConfigStatus, len(statuses))
	for _, status := range statuses {
		statusByName[status.NodeName] = status
	}
	tokenNames, err := store.ListNodeNamesWithActiveTokens(r.Context())
	if err != nil {
		return nil, err
	}
	hasToken := make(map[string]bool, len(tokenNames))
	for _, name := range tokenNames {
		hasToken[name] = true
	}
	requested := make(map[string]bool)
	for _, name := range requestedNodes {
		requested[strings.TrimSpace(name)] = true
	}
	type eligibleMember struct {
		member db.CreateNodeUpdateCampaignMemberParams
		online bool
		active bool
	}
	var eligible []eligibleMember
	for _, node := range nodes {
		if len(requested) > 0 && !requested[node.Name] {
			continue
		}
		status, ok := statusByName[node.Name]
		if !ok || node.Status == "pending" || !hasToken[node.Name] || !containsCapability(status.Capabilities, model.CapabilityOperationsV1) {
			continue
		}
		agentAsset, singBoxAsset, err := catalog.assetsForNode(r.Context(), r, status.AgentGOOS, status.AgentGOARCH)
		if err != nil {
			continue
		}
		selected := make(map[string]bool)
		for _, component := range components {
			selected[component] = true
		}
		if selected["agent"] && versionsEquivalentAPI(status.AgentVersion, agentAsset.Version) {
			delete(selected, "agent")
		}
		if selected["sing_box"] && versionsEquivalentAPI(status.SingBoxVersion, singBoxAsset.Version) {
			delete(selected, "sing_box")
		}
		if len(selected) == 0 || (selected["agent"] && !containsCapability(status.Capabilities, model.CapabilityAgentUpdateV1)) ||
			(selected["sing_box"] && !containsCapability(status.Capabilities, model.CapabilitySingBoxUpdateV1)) {
			continue
		}
		payload := model.NodeUpdatePayload{Release: strings.TrimSpace(catalog.options.Version)}
		kind := ""
		if selected["agent"] {
			payload.Agent, kind = &agentAsset, "update.agent"
		}
		if selected["sing_box"] {
			payload.SingBox, kind = &singBoxAsset, "update.sing_box"
		}
		if payload.Agent != nil && payload.SingBox != nil {
			kind = "update.bundle"
		}
		raw, _ := json.Marshal(payload)
		eligible = append(eligible, eligibleMember{
			member: db.CreateNodeUpdateCampaignMemberParams{NodeName: node.Name, Kind: kind, Payload: raw},
			online: heartbeatIsOnline(status.LatestHeartbeat.String), active: node.Status == "active",
		})
	}
	if len(requested) > 0 {
		for name := range requested {
			found := false
			for _, member := range eligible {
				if member.member.NodeName == name {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("requested node %q is not eligible for this update", name)
			}
		}
	}
	if len(eligible) == 0 {
		return nil, errors.New("no eligible nodes require the selected update")
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].member.NodeName < eligible[j].member.NodeName })
	canary := -1
	for i, member := range eligible {
		if member.online && member.active {
			canary = i
			break
		}
	}
	if canary < 0 {
		for i, member := range eligible {
			if member.online {
				canary = i
				break
			}
		}
	}
	if canary < 0 {
		return nil, errors.New("no online eligible node is available as canary")
	}
	eligible[0], eligible[canary] = eligible[canary], eligible[0]
	members := make([]db.CreateNodeUpdateCampaignMemberParams, 0, len(eligible))
	for i, item := range eligible {
		item.member.Position = int64(i)
		if i == 0 {
			item.member.BatchNumber = 0
		} else {
			item.member.BatchNumber = 1 + int64(i-1)/batchSize
		}
		members = append(members, item.member)
	}
	return members, nil
}

func heartbeatIsOnline(value string) bool {
	reported, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && time.Since(reported) <= 3*time.Minute
}
