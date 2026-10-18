package model

const (
	CapabilityNetworkInventoryV1 = "network.inventory.v1"
	MaxNodeAddresses             = 1024
	MaxNetworkSnapshotBytes      = 256 * 1024
)

// NodeAddress describes a configured local address, not an advertised endpoint
// or a guarantee of public reachability. FlagsKnown is false on portable scans.
type NodeAddress struct {
	Address        string `json:"address"`
	Family         int    `json:"family"`
	PrefixLength   int    `json:"prefix_length"`
	Interface      string `json:"interface"`
	InterfaceIndex int    `json:"interface_index"`
	InterfaceUp    bool   `json:"interface_up"`
	Scope          string `json:"scope"`
	FlagsKnown     bool   `json:"flags_known"`
	Temporary      bool   `json:"temporary"`
	Deprecated     bool   `json:"deprecated"`
	Tentative      bool   `json:"tentative"`
	DADFailed      bool   `json:"dad_failed"`
}

type NetworkSnapshot struct {
	Revision    int64         `json:"revision"`
	CollectedAt string        `json:"collected_at"`
	Complete    bool          `json:"complete"`
	Error       string        `json:"error,omitempty"`
	Addresses   []NodeAddress `json:"addresses"`
}

type NetworkSnapshotAck struct {
	Revision int64 `json:"revision"`
}
