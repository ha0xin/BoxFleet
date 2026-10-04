package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

type updateCampaignController struct {
	store    *db.DB
	notifier *nodeOperationNotifier
	mu       sync.Mutex
	now      func() time.Time
}

const updateQueueTimeout = 10 * time.Minute

type adminCreateUpdateCampaignPayload struct {
	Nodes          []string `json:"nodes,omitempty"`
	Components     []string `json:"components,omitempty"`
	BatchSize      int64    `json:"batch_size,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

func newUpdateCampaignController(store *db.DB, notifier *nodeOperationNotifier) *updateCampaignController {
	return &updateCampaignController{store: store, notifier: notifier, now: time.Now}
}

// Reconcile even when no browser is open and no offline agent can report back.
func (c *updateCampaignController) run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		detail, found, err := c.store.GetActiveNodeUpdateCampaign(ctx)
		if err == nil && found {
			_, err = c.reconcile(ctx, detail.Campaign.ID)
		}
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("reconcile node update campaign")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
			if member.Status == "skipped" || member.Status == "succeeded" {
				continue
			}
			_, nodeErr := c.store.GetNode(ctx, member.NodeID)
			if errors.Is(nodeErr, sql.ErrNoRows) {
				if member.OperationID != "" && (member.Status == "queued" || member.Status == "running") {
					if _, err := c.store.RequestNodeOperationCancel(ctx, member.OperationID); err != nil && !errors.Is(err, sql.ErrNoRows) {
						return db.NodeUpdateCampaignDetail{}, err
					}
				}
				if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, "skipped", "node was deleted"); err != nil {
					return db.NodeUpdateCampaignDetail{}, err
				}
				continue
			}
			if nodeErr != nil {
				return db.NodeUpdateCampaignDetail{}, nodeErr
			}
			if member.OperationID == "" {
				continue
			}
			operation, err := c.store.GetNodeOperation(ctx, member.OperationID)
			if err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			status := operation.Status
			if status == "queued" {
				requested, parseErr := time.Parse(time.RFC3339Nano, operation.RequestedAt)
				if parseErr == nil && c.now().Sub(requested) >= updateQueueTimeout {
					operation, err = c.store.CancelUnclaimedNodeOperation(ctx, operation.ID)
					if err != nil {
						return db.NodeUpdateCampaignDetail{}, err
					}
					// A claim can race with cancellation; never skip an operation
					// that has started executing on the node.
					if operation.Status == "cancelled" {
						if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, "skipped", "node did not claim the update within 10 minutes"); err != nil {
							return db.NodeUpdateCampaignDetail{}, err
						}
						continue
					}
					status = operation.Status
				}
			}
			if status == "expired" {
				if operation.Attempt == 0 {
					if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, "skipped", operation.Error); err != nil {
						return db.NodeUpdateCampaignDetail{}, err
					}
					continue
				}
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
			deadline, _ := time.Parse(time.RFC3339Nano, campaignQueueDeadline(detail.Campaign.UpdatedAt))
			if !c.now().Before(deadline) {
				if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, "skipped", "update queue timed out before dispatch"); err != nil {
					return db.NodeUpdateCampaignDetail{}, err
				}
				createdAny = true
				continue
			}
			node, err := c.store.GetNode(ctx, member.NodeID)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && node.Status == "disabled") {
				if err := c.store.UpdateNodeUpdateCampaignMemberState(ctx, campaignID, member.NodeID, "skipped", "node is deleted or disabled"); err != nil {
					return db.NodeUpdateCampaignDetail{}, err
				}
				createdAny = true
				continue
			}
			if err != nil {
				return db.NodeUpdateCampaignDetail{}, err
			}
			operation, created, err := c.store.CreateNodeOperation(ctx, db.CreateNodeOperationParams{
				NodeName: member.NodeID, Kind: member.Kind, Payload: member.Payload,
				IdempotencyKey: "campaign:" + campaignID + ":" + member.NodeID,
				RequestedBy:    "update-campaign:" + campaignID,
				ExpiresAt:      campaignQueueDeadline(detail.Campaign.UpdatedAt),
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

func campaignQueueDeadline(batchStartedAt string) string {
	started, err := time.Parse(time.RFC3339Nano, batchStartedAt)
	if err != nil {
		started = time.Now().UTC()
	}
	return started.Add(updateQueueTimeout).UTC().Format(time.RFC3339Nano)
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
				NodeName: member.NodeID, Kind: member.Kind, Payload: member.Payload,
				IdempotencyKey: "campaign-retry:" + db.SHA256Hex([]byte(detail.Campaign.ID+":"+member.NodeID+":"+member.OperationID)),
				RequestedBy:    "update-campaign:" + detail.Campaign.ID,
				ExpiresAt:      time.Now().UTC().Add(updateQueueTimeout).Format(time.RFC3339Nano),
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
		statusByName[status.NodeID] = status
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
		status, ok := statusByName[node.ID]
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
