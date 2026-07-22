package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sethvargo/go-retry"

	"github.com/haoxin/boxfleet/internal/model"
)

const (
	operationLongPollSeconds = 45
	operationLeaseInterval   = 25 * time.Second
	maxOperationResponse     = 2 * 1024 * 1024
)

var (
	errOperationCancelled = errors.New("node operation cancelled")
	errOperationLeaseLost = errors.New("node operation lease lost")
)

// OperationState is a local outbox and restart checkpoint. It contains a
// bearer-like lease token, so it is always written with mode 0600.
type OperationState struct {
	Assignment   model.NodeOperationAssignment   `json:"assignment"`
	LastSequence int64                           `json:"last_sequence"`
	Phase        string                          `json:"phase"`
	PendingEvent *model.NodeOperationEventReport `json:"pending_event,omitempty"`
	Checkpoint   json.RawMessage                 `json:"checkpoint,omitempty"`
}

type operationAPIError struct {
	StatusCode int
	Body       string
}

func (e *operationAPIError) Error() string {
	return fmt.Sprintf("operation API returned %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

func agentCapabilities() []string {
	return []string{
		model.CapabilityOperationsV1,
		model.CapabilityAgentUpdateV1,
		model.CapabilitySingBoxUpdateV1,
		model.CapabilityStreamingDownloadV1,
		model.CapabilityVersionedInstallV1,
		model.CapabilityAgentRestartResumeV1,
		model.CapabilitySingBoxRollbackV1,
	}
}

func (a *Agent) RunOperations(ctx context.Context) error {
	for {
		state, err := a.LoadOperationState()
		if err != nil {
			return err
		}
		assignment, err := a.claimNodeOperationWithRetry(ctx, state)
		if err != nil {
			var apiErr *operationAPIError
			if state != nil && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
				if err := a.ClearOperationState(); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if assignment == nil {
			continue
		}
		if state == nil || state.Assignment.ID != assignment.ID || state.Assignment.Attempt != assignment.Attempt {
			if state != nil && state.Assignment.ID == assignment.ID {
				// A lease may expire while systemd is restarting the agent. A new
				// server attempt resets the event sequence but keeps the durable
				// execution checkpoint so already-committed filesystem steps are
				// reconciled instead of blindly repeated.
				state.Assignment = *assignment
				state.LastSequence = 0
				state.PendingEvent = nil
			} else {
				state = &OperationState{Assignment: *assignment, Phase: "claimed"}
			}
		} else {
			// Persist the renewed expiry returned by a successful resume while
			// preserving the event outbox and execution checkpoint.
			state.Assignment = *assignment
		}
		if err := a.SaveOperationState(*state); err != nil {
			return err
		}
		if err := a.runClaimedOperation(ctx, state); err != nil {
			return err
		}
	}
}

func (a *Agent) runClaimedOperation(ctx context.Context, state *OperationState) error {
	if err := a.flushPendingOperationEventWithRetry(ctx, state); err != nil {
		return err
	}
	if state.LastSequence == 0 {
		if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
			Status: "running", Phase: "starting", Message: "operation executor started",
		}); err != nil {
			return err
		}
	}

	opCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var cancelRequested atomic.Bool
	leaseErrors := make(chan error, 1)
	go a.monitorOperationLease(opCtx, state.Assignment, &cancelRequested, cancel, leaseErrors)

	a.maintenanceMu.Lock()
	result, executeErr := a.executeNodeOperation(opCtx, state, &cancelRequested)
	a.maintenanceMu.Unlock()
	// Capture a real parent/lease cancellation before stopping the lease
	// monitor. Calling cancel(nil) records context.Canceled as the cause, which
	// must not replace a concrete executor error such as a version mismatch.
	executeErr = operationExecutionError(opCtx, executeErr)
	cancel(nil)
	if errors.Is(executeErr, ErrAgentRestartRequired) {
		return ErrAgentRestartRequired
	}
	select {
	case leaseErr := <-leaseErrors:
		if leaseErr != nil && executeErr == nil {
			executeErr = leaseErr
		}
	default:
	}

	// A final renewal observes cancellation that arrived after the last monitor
	// tick and proves that this executor still owns the lease before it commits a
	// terminal event.
	if executeErr == nil {
		cancelled, err := a.renewOperationLeaseWithRetry(ctx, state.Assignment)
		if err != nil {
			executeErr = err
		} else if cancelled && !operationResultCommitted(result) {
			cancelRequested.Store(true)
			executeErr = errOperationCancelled
		}
	}

	status, phase, message := "succeeded", "completed", "operation completed"
	if executeErr != nil {
		status, phase, message = "failed", "failed", "operation failed"
		if errors.Is(executeErr, errOperationCancelled) {
			status, phase, message = "cancelled", "cancelled", "operation cancelled at a safe boundary"
		}
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	event := model.NodeOperationEventReport{
		Status: status, Phase: phase, Message: message, Result: resultJSON,
	}
	if executeErr != nil {
		event.Error = executeErr.Error()
	}
	if err := a.reportOperationEventWithRetry(ctx, state, event); err != nil {
		return err
	}
	return a.ClearOperationState()
}

func operationExecutionError(ctx context.Context, executeErr error) error {
	if executeErr == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return executeErr
}

func operationResultCommitted(result map[string]any) bool {
	committed, _ := result["committed"].(bool)
	return committed
}

func (a *Agent) executeNodeOperation(ctx context.Context, state *OperationState, cancelRequested *atomic.Bool) (map[string]any, error) {
	if err := operationBoundary(ctx, cancelRequested); err != nil {
		return nil, err
	}
	switch state.Assignment.Kind {
	case "config.reconcile":
		if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
			Status: "running", Phase: "reconciling", Message: "reconciling node configuration",
		}); err != nil {
			return nil, err
		}
		if err := a.once(ctx); err != nil {
			return nil, err
		}
		return map[string]any{"reconciled": true}, operationBoundary(ctx, cancelRequested)
	case "logs.collect":
		if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
			Status: "running", Phase: "collecting", Message: "collecting node logs",
		}); err != nil {
			return nil, err
		}
		if err := a.ReportLogs(ctx); err != nil {
			return nil, err
		}
		if err := a.ReportSystemLogs(ctx); err != nil {
			return nil, err
		}
		return map[string]any{"collected": true}, operationBoundary(ctx, cancelRequested)
	case "diagnostics.collect":
		if err := a.reportOperationEventWithRetry(ctx, state, model.NodeOperationEventReport{
			Status: "running", Phase: "collecting", Message: "collecting node diagnostics",
		}); err != nil {
			return nil, err
		}
		return a.collectDiagnostics(ctx), operationBoundary(ctx, cancelRequested)
	case "update.agent", "update.sing_box", "update.bundle":
		return a.executeUpdateOperation(ctx, state, cancelRequested)
	default:
		return nil, fmt.Errorf("unsupported node operation kind %q", state.Assignment.Kind)
	}
}

func (a *Agent) collectDiagnostics(ctx context.Context) map[string]any {
	result := map[string]any{"agent_version": Version}
	if output, err := a.Runner.Output(ctx, a.Config.SingBoxPath, "version"); err == nil {
		result["sing_box_version"] = firstLine(string(output))
	} else {
		result["sing_box_version_error"] = err.Error()
	}
	for key, service := range map[string]string{"agent_state": a.Config.AgentService, "sing_box_state": a.Config.SingBoxService} {
		if output, err := a.Runner.Output(ctx, "systemctl", "show", "-p", "ActiveState", "--value", service); err == nil {
			result[key] = strings.TrimSpace(string(output))
		} else {
			result[key+"_error"] = err.Error()
		}
	}
	return result
}

func operationBoundary(ctx context.Context, cancelRequested *atomic.Bool) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if cancelRequested.Load() {
		return errOperationCancelled
	}
	return nil
}

func (a *Agent) monitorOperationLease(
	ctx context.Context,
	assignment model.NodeOperationAssignment,
	cancelRequested *atomic.Bool,
	cancel context.CancelCauseFunc,
	errorsOut chan<- error,
) {
	ticker := time.NewTicker(operationLeaseInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			requested, err := a.renewOperationLeaseWithRetry(ctx, assignment)
			if err != nil {
				wrapped := fmt.Errorf("%w: %v", errOperationLeaseLost, err)
				select {
				case errorsOut <- wrapped:
				default:
				}
				cancel(wrapped)
				return
			}
			if requested {
				cancelRequested.Store(true)
				cancel(errOperationCancelled)
				return
			}
		}
	}
}

func (a *Agent) claimNodeOperationWithRetry(ctx context.Context, state *OperationState) (*model.NodeOperationAssignment, error) {
	backoff := retry.WithFullJitter(retry.WithCappedDuration(30*time.Second, retry.NewExponential(time.Second)))
	return retry.DoValue(ctx, backoff, func(ctx context.Context) (*model.NodeOperationAssignment, error) {
		assignment, err := a.claimNodeOperation(ctx, state)
		if isRetryableOperationError(err) {
			return nil, retry.RetryableError(err)
		}
		return assignment, err
	})
}

func (a *Agent) claimNodeOperation(ctx context.Context, state *OperationState) (*model.NodeOperationAssignment, error) {
	request := model.NodeOperationClaimRequest{
		Capabilities: agentCapabilities(), WaitSeconds: operationLongPollSeconds,
	}
	if state != nil {
		request.CurrentOperationID = state.Assignment.ID
		request.LeaseToken = state.Assignment.LeaseToken
	}
	response, err := a.operationJSONRequest(ctx, "/api/node/operations/claim", request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, operationResponseError(response)
	}
	var assignment model.NodeOperationAssignment
	if err := json.NewDecoder(io.LimitReader(response.Body, maxOperationResponse)).Decode(&assignment); err != nil {
		return nil, fmt.Errorf("decode operation assignment: %w", err)
	}
	if assignment.ID == "" || assignment.LeaseToken == "" || assignment.Attempt <= 0 {
		return nil, errors.New("server returned an invalid operation assignment")
	}
	return &assignment, nil
}

func (a *Agent) renewOperationLeaseWithRetry(ctx context.Context, assignment model.NodeOperationAssignment) (bool, error) {
	backoff := retry.WithMaxDuration(45*time.Second,
		retry.WithFullJitter(retry.WithCappedDuration(10*time.Second, retry.NewExponential(time.Second))))
	return retry.DoValue(ctx, backoff, func(ctx context.Context) (bool, error) {
		response, err := a.operationJSONRequest(ctx, "/api/node/operations/"+assignment.ID+"/lease", model.NodeOperationLeaseRequest{
			LeaseToken: assignment.LeaseToken, Attempt: assignment.Attempt,
		})
		if err != nil {
			return false, retry.RetryableError(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err := operationResponseError(response)
			if isRetryableOperationError(err) {
				return false, retry.RetryableError(err)
			}
			return false, err
		}
		var lease model.NodeOperationLeaseResponse
		if err := json.NewDecoder(io.LimitReader(response.Body, maxOperationResponse)).Decode(&lease); err != nil {
			return false, retry.RetryableError(fmt.Errorf("decode operation lease: %w", err))
		}
		return lease.CancelRequested, nil
	})
}

func (a *Agent) reportOperationEventWithRetry(ctx context.Context, state *OperationState, event model.NodeOperationEventReport) error {
	if state.PendingEvent == nil {
		event.LeaseToken = state.Assignment.LeaseToken
		event.Attempt = state.Assignment.Attempt
		event.Sequence = state.LastSequence + 1
		if event.ReportedAt == "" {
			event.ReportedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		state.PendingEvent = &event
		if err := a.SaveOperationState(*state); err != nil {
			return err
		}
	}
	return a.flushPendingOperationEventWithRetry(ctx, state)
}

func (a *Agent) flushPendingOperationEventWithRetry(ctx context.Context, state *OperationState) error {
	if state.PendingEvent == nil {
		return nil
	}
	backoff := retry.WithFullJitter(retry.WithCappedDuration(30*time.Second, retry.NewExponential(time.Second)))
	err := retry.Do(ctx, backoff, func(ctx context.Context) error {
		response, err := a.operationJSONRequest(ctx, "/api/node/operations/"+state.Assignment.ID+"/events", state.PendingEvent)
		if err != nil {
			return retry.RetryableError(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err := operationResponseError(response)
			if isRetryableOperationError(err) {
				return retry.RetryableError(err)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	state.LastSequence = state.PendingEvent.Sequence
	state.Phase = state.PendingEvent.Phase
	state.PendingEvent = nil
	return a.SaveOperationState(*state)
}

func (a *Agent) operationJSONRequest(ctx context.Context, path string, payload any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(a.Config.ServerURL, "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+a.Config.Token)
	request.Header.Set("X-BoxFleet-Node", a.Config.NodeName)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client().Do(request)
	if err != nil {
		return nil, err
	}
	if err := a.adoptCanonicalNodeName(response.Header.Get(model.CanonicalNodeNameHeader)); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response, nil
}

func operationResponseError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
	return &operationAPIError{StatusCode: response.StatusCode, Body: string(raw)}
}

func isRetryableOperationError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *operationAPIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
}

func (a *Agent) LoadOperationState() (*OperationState, error) {
	raw, err := os.ReadFile(a.Config.OperationStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state OperationState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode operation state: %w", err)
	}
	if state.Assignment.ID == "" || state.Assignment.LeaseToken == "" || state.Assignment.Attempt <= 0 {
		return nil, errors.New("operation state is incomplete")
	}
	return &state, nil
}

func (a *Agent) SaveOperationState(state OperationState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(a.Config.OperationStatePath, append(raw, '\n'), defaultConfigFilePerm)
}

func (a *Agent) ClearOperationState() error {
	err := os.Remove(a.Config.OperationStatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
