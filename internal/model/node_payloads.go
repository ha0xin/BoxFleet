package model

import "encoding/json"

const CanonicalNodeNameHeader = "X-BoxFleet-Node-Name"

const (
	CapabilityOperationsV1         = "operations.v1"
	CapabilityAgentUpdateV1        = "update.agent.v1"
	CapabilitySingBoxUpdateV1      = "update.sing_box.v1"
	CapabilityStreamingDownloadV1  = "download.streaming.v1"
	CapabilityVersionedInstallV1   = "install.versioned.v1"
	CapabilityAgentRestartResumeV1 = "restart_resume.agent.v1"
	CapabilitySingBoxRollbackV1    = "rollback.sing_box.v1"
)

type ApplyResult struct {
	NodeName        string `json:"node_name"`
	ConfigVersionID string `json:"config_version_id"`
	ConfigHash      string `json:"config_hash"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	ReportedAt      string `json:"reported_at"`
}

type Heartbeat struct {
	NodeName             string   `json:"node_name"`
	AgentVersion         string   `json:"agent_version"`
	AgentGOOS            string   `json:"agent_goos,omitempty"`
	AgentGOARCH          string   `json:"agent_goarch,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	SingBoxVersion       string   `json:"sing_box_version"`
	Status               string   `json:"status"`
	MemoryBytes          int64    `json:"memory_bytes"`
	RxBytes              int64    `json:"rx_bytes"`
	TxBytes              int64    `json:"tx_bytes"`
	CurrentConfigVersion string   `json:"current_config_version"`
	CurrentConfigHash    string   `json:"current_config_hash"`
	ReportedAt           string   `json:"reported_at"`
}

// NodeOperationClaimRequest is sent by an authenticated agent. Supplying the
// current operation and lease lets a restarted agent reclaim the same attempt
// instead of creating a second executor.
type NodeOperationClaimRequest struct {
	Capabilities       []string `json:"capabilities"`
	CurrentOperationID string   `json:"current_operation_id,omitempty"`
	LeaseToken         string   `json:"lease_token,omitempty"`
	WaitSeconds        int      `json:"wait_seconds,omitempty"`
}

type NodeOperationAssignment struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Payload         json.RawMessage `json:"payload"`
	Attempt         int64           `json:"attempt"`
	LeaseToken      string          `json:"lease_token"`
	LeaseExpiresAt  string          `json:"lease_expires_at"`
	CancelRequested bool            `json:"cancel_requested"`
}

type NodeOperationLeaseRequest struct {
	LeaseToken string `json:"lease_token"`
	Attempt    int64  `json:"attempt"`
}

type NodeOperationLeaseResponse struct {
	LeaseExpiresAt  string `json:"lease_expires_at"`
	CancelRequested bool   `json:"cancel_requested"`
}

type NodeOperationEventReport struct {
	LeaseToken string          `json:"lease_token"`
	Attempt    int64           `json:"attempt"`
	Sequence   int64           `json:"sequence"`
	Status     string          `json:"status"`
	Phase      string          `json:"phase"`
	Message    string          `json:"message,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	ReportedAt string          `json:"reported_at,omitempty"`
}

// UpdateAsset is selected from the server's fixed release catalog. Node update
// APIs never accept a caller-provided URL; the agent only receives the resolved
// immutable asset plus its expected length and checksum.
type UpdateAsset struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
}

type NodeUpdatePayload struct {
	Release string       `json:"release"`
	Agent   *UpdateAsset `json:"agent,omitempty"`
	SingBox *UpdateAsset `json:"sing_box,omitempty"`
}

type TrafficReport struct {
	NodeName    string         `json:"node_name"`
	Sequence    int64          `json:"sequence"`
	AgentBootID string         `json:"agent_boot_id"`
	ReportedAt  string         `json:"reported_at"`
	Deltas      []TrafficDelta `json:"deltas"`
}

type TrafficDelta struct {
	AuthName      string `json:"auth_name"`
	Direction     string `json:"direction"`
	RawBytesDelta int64  `json:"raw_bytes_delta"`
	CounterValue  int64  `json:"counter_value"`
	CounterEpoch  int64  `json:"counter_epoch"`
	ObservedAt    string `json:"observed_at"`
}

type LogEventReport struct {
	NodeName string          `json:"node_name"`
	Events   []LogEventInput `json:"events"`
}

type LogEventInput struct {
	AuthName    string `json:"auth_name"`
	SourceIP    string `json:"source_ip"`
	TargetHost  string `json:"target_host"`
	TargetPort  int64  `json:"target_port"`
	Action      string `json:"action"`
	RawMessage  string `json:"raw_message"`
	Count       int64  `json:"count"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	ObservedAt  string `json:"observed_at"`
	Cursor      string `json:"cursor"`
}

type SystemLogReport struct {
	NodeName string           `json:"node_name"`
	Entries  []SystemLogInput `json:"entries"`
}

type SystemLogInput struct {
	Service    string `json:"service"`
	Level      string `json:"level"`
	RawMessage string `json:"raw_message"`
	ObservedAt string `json:"observed_at"`
	Cursor     string `json:"cursor"`
}
