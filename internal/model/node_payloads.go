package model

type ApplyResult struct {
	NodeName        string `json:"node_name"`
	ConfigVersionID string `json:"config_version_id"`
	ConfigHash      string `json:"config_hash"`
	Status          string `json:"status"`
	Error           string `json:"error"`
	ReportedAt      string `json:"reported_at"`
}

type Heartbeat struct {
	NodeName             string `json:"node_name"`
	AgentVersion         string `json:"agent_version"`
	SingBoxVersion       string `json:"sing_box_version"`
	Status               string `json:"status"`
	MemoryBytes          int64  `json:"memory_bytes"`
	RxBytes              int64  `json:"rx_bytes"`
	TxBytes              int64  `json:"tx_bytes"`
	CurrentConfigVersion string `json:"current_config_version"`
	CurrentConfigHash    string `json:"current_config_hash"`
	ReportedAt           string `json:"reported_at"`
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
