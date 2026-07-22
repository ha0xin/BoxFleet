package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestAdminNodeUpdateUsesOnlyFixedCatalogAssets(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	if err := store.RecordHeartbeat(ctx, db.Heartbeat{
		NodeName: "azus", AgentVersion: "v0.1.0", SingBoxVersion: "sing-box version 1.12.0",
		AgentGOOS: "linux", AgentGOARCH: "amd64",
		Capabilities: []string{
			model.CapabilityOperationsV1, model.CapabilityAgentUpdateV1, model.CapabilitySingBoxUpdateV1,
			model.CapabilityStreamingDownloadV1, model.CapabilityVersionedInstallV1,
			model.CapabilityAgentRestartResumeV1, model.CapabilitySingBoxRollbackV1,
		},
	}); err != nil {
		t.Fatal(err)
	}
	artifactDir := t.TempDir()
	assetDir := filepath.Join(artifactDir, "artifacts")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentName, singBoxName := "boxfleet-agent-v0.2.0-linux-amd64", "sing-box-v1.13.13-linux-amd64"
	agentData, singBoxData := []byte("agent release bytes"), []byte("sing-box release bytes")
	if err := os.WriteFile(filepath.Join(assetDir, agentName), agentData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, singBoxName), singBoxData, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := updateManifest{
		Release: "v0.2.0",
		Platforms: map[string]updateManifestPlatform{"linux/amd64": {
			Agent:   updateManifestAsset{Version: "v0.2.0", Name: agentName, SHA256: testSHA256(agentData), Size: int64(len(agentData))},
			SingBox: updateManifestAsset{Version: "v1.13.13", Name: singBoxName, SHA256: testSHA256(singBoxData), Size: int64(len(singBoxData))},
		}},
	}
	rawManifest, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(artifactDir, updateManifestName), rawManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(Options{
		DB: store, ArtifactDir: artifactDir, AllowInsecureAdmin: true,
		Version: "v0.2.0", Repo: "ha0xin/BoxFleet", SingBoxVersion: "v1.13.13",
	}))
	t.Cleanup(server.Close)

	response, err := operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/admin/nodes/azus/updates", "", adminCreateNodeUpdatePayload{
		IdempotencyKey: "update-fixed-catalog-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, body = %s", response.StatusCode, raw)
	}
	var operation db.NodeOperation
	if err := json.NewDecoder(response.Body).Decode(&operation); err != nil {
		t.Fatal(err)
	}
	if operation.Kind != "update.bundle" {
		t.Fatalf("kind = %q", operation.Kind)
	}
	var payload model.NodeUpdatePayload
	if err := json.Unmarshal(operation.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Agent == nil || payload.SingBox == nil {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Agent.URL != server.URL+"/artifacts/artifacts/"+agentName || payload.Agent.SHA256 != testSHA256(agentData) {
		t.Fatalf("agent asset = %+v", payload.Agent)
	}
	if payload.SingBox.URL != server.URL+"/artifacts/artifacts/"+singBoxName || payload.SingBox.SHA256 != testSHA256(singBoxData) {
		t.Fatalf("sing-box asset = %+v", payload.SingBox)
	}
}

func TestGenericOperationEndpointRejectsUpdateURLs(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	router := NewRouter(Options{DB: store, AllowInsecureAdmin: true})
	raw, _ := json.Marshal(adminCreateNodeOperationPayload{
		Kind: "update.agent", IdempotencyKey: "arbitrary-url",
		Payload: json.RawMessage(`{"agent":{"url":"https://example.invalid/binary"}}`),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/nodes/azus/operations", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateAllCanaryBatchesAndPausesOnFailure(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	nodeNames := []string{"edge-a", "edge-b", "edge-c", "edge-d"}
	capabilities := allUpdateCapabilities()
	for i, name := range nodeNames {
		if _, err := store.CreateNode(ctx, name, fmt.Sprintf("192.0.2.%d", i+1), ""); err != nil {
			t.Fatal(err)
		}
		if _, err := store.IssueNodeToken(ctx, name); err != nil {
			t.Fatal(err)
		}
		if err := store.RecordHeartbeat(ctx, db.Heartbeat{
			NodeName: name, AgentVersion: "v0.1.0", SingBoxVersion: "sing-box version 1.12.0",
			AgentGOOS: "linux", AgentGOARCH: "amd64", Capabilities: capabilities,
			ReportedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatal(err)
		}
	}
	artifactDir, agentData, singBoxData := writeTestUpdateCatalog(t)
	server := httptest.NewServer(NewRouter(Options{
		DB: store, ArtifactDir: artifactDir, AllowInsecureAdmin: true,
		Version: "v0.2.0", Repo: "ha0xin/BoxFleet", SingBoxVersion: "v1.13.13",
	}))
	t.Cleanup(server.Close)
	_ = agentData
	_ = singBoxData

	response, err := operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/admin/node-updates/bulk", "", adminCreateUpdateCampaignPayload{
		Nodes: nodeNames, BatchSize: 2, IdempotencyKey: "campaign-canary-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var detail db.NodeUpdateCampaignDetail
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || detail.Campaign.CurrentBatch != 0 {
		t.Fatalf("create campaign = status %d %+v", response.StatusCode, detail.Campaign)
	}
	if detail.Members[0].NodeName != "edge-a" || detail.Members[0].OperationID == "" {
		t.Fatalf("canary member = %+v", detail.Members[0])
	}
	for _, member := range detail.Members[1:] {
		if member.OperationID != "" || member.Status != "pending" {
			t.Fatalf("non-canary released early: %+v", member)
		}
	}

	canaryClaim, ok, err := store.ClaimNodeOperation(ctx, db.ClaimNodeOperationParams{NodeName: "edge-a", Capabilities: capabilities})
	if err != nil || !ok {
		t.Fatalf("claim canary: %v %v", ok, err)
	}
	if _, err := store.RecordNodeOperationEvent(ctx, db.RecordNodeOperationEventParams{
		NodeName: "edge-a", OperationID: canaryClaim.Operation.ID, LeaseToken: canaryClaim.LeaseToken,
		Attempt: 1, Sequence: 1, Status: "succeeded", Phase: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	response, err = server.Client().Get(server.URL + "/api/admin/node-update-campaigns/" + detail.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if detail.Campaign.CurrentBatch != 1 || detail.Campaign.Status != "running" {
		t.Fatalf("after canary = %+v", detail.Campaign)
	}
	var batchOne []db.NodeUpdateCampaignMember
	for _, member := range detail.Members {
		if member.BatchNumber == 1 {
			batchOne = append(batchOne, member)
		}
		if member.BatchNumber == 2 && (member.OperationID != "" || member.Status != "pending") {
			t.Fatalf("batch 2 released before batch 1 success: %+v", member)
		}
	}
	if len(batchOne) != 2 || batchOne[0].OperationID == "" || batchOne[1].OperationID == "" {
		t.Fatalf("batch one = %+v", batchOne)
	}

	failed := batchOne[0]
	claim, ok, err := store.ClaimNodeOperation(ctx, db.ClaimNodeOperationParams{NodeName: failed.NodeName, Capabilities: capabilities})
	if err != nil || !ok {
		t.Fatalf("claim batch member: %v %v", ok, err)
	}
	if _, err := store.RecordNodeOperationEvent(ctx, db.RecordNodeOperationEventParams{
		NodeName: failed.NodeName, OperationID: claim.Operation.ID, LeaseToken: claim.LeaseToken,
		Attempt: 1, Sequence: 1, Status: "failed", Phase: "failed", Error: "candidate failed health check",
	}); err != nil {
		t.Fatal(err)
	}
	response, err = server.Client().Get(server.URL + "/api/admin/node-update-campaigns/" + detail.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if detail.Campaign.Status != "paused" || !strings.Contains(detail.Campaign.Error, failed.NodeName) {
		t.Fatalf("failed campaign = %+v", detail.Campaign)
	}
	for _, member := range detail.Members {
		if member.BatchNumber == 2 && member.OperationID != "" {
			t.Fatalf("failure spread to later batch: %+v", member)
		}
	}

	response, err = operationJSONRequest(server.Client(), http.MethodPost,
		server.URL+"/api/admin/node-update-campaigns/"+detail.Campaign.ID+"/resume", "", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	rawResume, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resume campaign = status %d body %s", response.StatusCode, rawResume)
	}
	if err := json.Unmarshal(rawResume, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Campaign.Status != "running" {
		t.Fatalf("resumed campaign = %+v", detail.Campaign)
	}
	var retried db.NodeUpdateCampaignMember
	for _, member := range detail.Members {
		if member.NodeID == failed.NodeID {
			retried = member
		}
	}
	if retried.OperationID == "" || retried.OperationID == failed.OperationID || retried.Status != "queued" {
		t.Fatalf("retried member = %+v", retried)
	}
	retryOperation, err := store.GetNodeOperation(ctx, retried.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if retryOperation.RetryOf != failed.OperationID {
		t.Fatalf("retry_of = %q, want %q", retryOperation.RetryOf, failed.OperationID)
	}
}

func allUpdateCapabilities() []string {
	return []string{
		model.CapabilityOperationsV1, model.CapabilityAgentUpdateV1, model.CapabilitySingBoxUpdateV1,
		model.CapabilityStreamingDownloadV1, model.CapabilityVersionedInstallV1,
		model.CapabilityAgentRestartResumeV1, model.CapabilitySingBoxRollbackV1,
	}
}

func writeTestUpdateCatalog(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	artifactDir := t.TempDir()
	assetDir := filepath.Join(artifactDir, "artifacts")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentName, singBoxName := "boxfleet-agent-v0.2.0-linux-amd64", "sing-box-v1.13.13-linux-amd64"
	agentData, singBoxData := []byte("agent release bytes"), []byte("sing-box release bytes")
	if err := os.WriteFile(filepath.Join(assetDir, agentName), agentData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, singBoxName), singBoxData, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := updateManifest{
		Release: "v0.2.0",
		Platforms: map[string]updateManifestPlatform{"linux/amd64": {
			Agent:   updateManifestAsset{Version: "v0.2.0", Name: agentName, SHA256: testSHA256(agentData), Size: int64(len(agentData))},
			SingBox: updateManifestAsset{Version: "v1.13.13", Name: singBoxName, SHA256: testSHA256(singBoxData), Size: int64(len(singBoxData))},
		}},
	}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(artifactDir, updateManifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return artifactDir, agentData, singBoxData
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestNodeOperationLongPollLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewRouter(Options{DB: store, AllowInsecureAdmin: true}))
	t.Cleanup(server.Close)

	claimResult := make(chan struct {
		assignment model.NodeOperationAssignment
		err        error
	}, 1)
	go func() {
		var assignment model.NodeOperationAssignment
		response, err := operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/node/operations/claim", issued.Token, model.NodeOperationClaimRequest{
			Capabilities: []string{model.CapabilityOperationsV1}, WaitSeconds: 2,
		})
		if err == nil {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				raw, _ := io.ReadAll(response.Body)
				err = &operationHTTPError{status: response.StatusCode, body: string(raw)}
			} else {
				err = json.NewDecoder(response.Body).Decode(&assignment)
			}
		}
		claimResult <- struct {
			assignment model.NodeOperationAssignment
			err        error
		}{assignment: assignment, err: err}
	}()

	// It is safe if this races ahead of the long poll's subscription: the claim
	// handler brackets subscription with database reads.
	response, err := operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/admin/nodes/azus/operations", "", adminCreateNodeOperationPayload{
		Kind: "logs.collect", Payload: json.RawMessage(`{"limit":100}`), IdempotencyKey: "api-lifecycle-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}

	select {
	case result := <-claimResult:
		if result.err != nil {
			t.Fatal(result.err)
		}
		assignment := result.assignment
		if assignment.ID == "" || assignment.LeaseToken == "" || assignment.Attempt != 1 {
			t.Fatalf("invalid assignment: %+v", assignment)
		}

		response, err = operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/node/operations/"+assignment.ID+"/events", issued.Token, model.NodeOperationEventReport{
			LeaseToken: assignment.LeaseToken, Attempt: assignment.Attempt, Sequence: 1,
			Status: "running", Phase: "collecting", Message: "collecting logs",
		})
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("event status = %d", response.StatusCode)
		}

		response, err = operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/admin/nodes/azus/operations/"+assignment.ID+"/cancel", "", struct{}{})
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("cancel status = %d", response.StatusCode)
		}

		response, err = operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/node/operations/"+assignment.ID+"/lease", issued.Token, model.NodeOperationLeaseRequest{
			LeaseToken: assignment.LeaseToken, Attempt: assignment.Attempt,
		})
		if err != nil {
			t.Fatal(err)
		}
		var lease model.NodeOperationLeaseResponse
		if err := json.NewDecoder(response.Body).Decode(&lease); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || !lease.CancelRequested || lease.LeaseExpiresAt == "" {
			t.Fatalf("lease response = status %d %+v", response.StatusCode, lease)
		}

		response, err = operationJSONRequest(server.Client(), http.MethodPost, server.URL+"/api/node/operations/"+assignment.ID+"/events", issued.Token, model.NodeOperationEventReport{
			LeaseToken: assignment.LeaseToken, Attempt: assignment.Attempt, Sequence: 2,
			Status: "cancelled", Phase: "cancelled", Message: "cancelled at a safe boundary",
		})
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("terminal event status = %d", response.StatusCode)
		}

		detailResponse, err := server.Client().Get(server.URL + "/api/admin/nodes/azus/operations/" + assignment.ID)
		if err != nil {
			t.Fatal(err)
		}
		defer detailResponse.Body.Close()
		var detail adminNodeOperationDetail
		if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil {
			t.Fatal(err)
		}
		if detail.Operation.Status != "cancelled" || len(detail.Events) != 2 {
			t.Fatalf("detail = %+v", detail)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("long poll was not woken by operation creation")
	}
}

func TestNodeOperationClaimRequiresCapabilities(t *testing.T) {
	ctx := context.Background()
	store := openAPITestDB(t)
	seedAPITestNode(t, ctx, store)
	issued, err := store.IssueNodeToken(ctx, "azus")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.CreateNodeOperation(ctx, db.CreateNodeOperationParams{
		NodeName: "azus", Kind: "update.agent", Payload: json.RawMessage(`{}`),
		IdempotencyKey: "capabilities-1", RequiredCapabilities: []string{model.CapabilityAgentUpdateV1},
	})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Options{DB: store})
	requestBody, _ := json.Marshal(model.NodeOperationClaimRequest{
		Capabilities: []string{model.CapabilityOperationsV1}, WaitSeconds: 0,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/node/operations/claim", bytes.NewReader(requestBody))
	request.Header.Set("X-BoxFleet-Node", "azus")
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

type operationHTTPError struct {
	status int
	body   string
}

func (e *operationHTTPError) Error() string { return http.StatusText(e.status) + ": " + e.body }

func operationJSONRequest(client *http.Client, method, url, nodeToken string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if nodeToken != "" {
		request.Header.Set("X-BoxFleet-Node", "azus")
		request.Header.Set("Authorization", "Bearer "+nodeToken)
	}
	return client.Do(request)
}
