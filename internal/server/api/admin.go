package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/agent"
	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/secret"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/install"
	"github.com/haoxin/boxfleet/internal/server/render"
)

type adminOverview struct {
	Nodes         []adminNode        `json:"nodes"`
	Users         []adminUser        `json:"users"`
	Traffic       []adminUserTraffic `json:"traffic"`
	SystemLogs    []adminSystemLog   `json:"system_logs"`
	SystemLogNote string             `json:"system_log_note"`
	Release       adminRelease       `json:"release"`
}

type adminNodesPage struct {
	Nodes  []adminNode `json:"nodes"`
	Total  int64       `json:"total"`
	Limit  int64       `json:"limit"`
	Offset int64       `json:"offset"`
}

type adminProxiesPage struct {
	Proxies []adminProxy `json:"proxies"`
	Total   int64        `json:"total"`
	Limit   int64        `json:"limit"`
	Offset  int64        `json:"offset"`
}

type adminUsersPage struct {
	Users  []adminUserRow `json:"users"`
	Total  int64          `json:"total"`
	Limit  int64          `json:"limit"`
	Offset int64          `json:"offset"`
}

// adminUserRow embeds adminUser so a paged row stays a superset of the unpaged
// list the overview renders, and adds only what a paged table cannot derive on
// its own: the status the server filtered and sorted on, and the traffic totals
// that would otherwise cost a full-inventory request alongside every page.
type adminUserRow struct {
	adminUser
	EffectiveStatus string               `json:"effective_status"`
	Traffic         adminTrafficDirected `json:"traffic"`
}

type adminRelease struct {
	Repo            string `json:"repo"`
	BoxFleetVersion string `json:"boxfleet_version"`
	AgentVersion    string `json:"agent_version"`
	SingBoxVersion  string `json:"sing_box_version"`
	UpdatesEnabled  bool   `json:"updates_enabled"`
	UpdateError     string `json:"update_error,omitempty"`
}

type adminNode struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	PublicHost      string            `json:"public_host"`
	APIBaseURL      string            `json:"api_base_url"`
	Status          string            `json:"status"`
	SingBoxVersion  string            `json:"sing_box_version"`
	LastSeenAt      string            `json:"last_seen_at"`
	DeletedAt       string            `json:"deleted_at"`
	TargetVersion   string            `json:"target_version,omitempty"`
	CurrentVersion  string            `json:"current_version,omitempty"`
	ApplyStatus     string            `json:"apply_status,omitempty"`
	ApplyError      string            `json:"apply_error,omitempty"`
	LatestHeartbeat string            `json:"latest_heartbeat,omitempty"`
	AgentVersion    string            `json:"agent_version,omitempty"`
	AgentGOOS       string            `json:"agent_goos,omitempty"`
	AgentGOARCH     string            `json:"agent_goarch,omitempty"`
	Capabilities    []string          `json:"capabilities,omitempty"`
	ActiveOperation *db.NodeOperation `json:"active_operation,omitempty"`
	// HasActiveToken distinguishes a reversible pause (disabled, token intact)
	// from a decommission (disabled, tokens revoked) so the UI does not offer to
	// re-enable a node whose agent could never authenticate.
	HasActiveToken bool `json:"has_active_token"`
	// Hosts is the full ordered host list; public_host mirrors Hosts[0]. Each
	// selected host produces a client connection profile.
	Hosts []adminNodeHost `json:"hosts,omitempty"`
}

type adminNodeHost struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Tag      string `json:"tag,omitempty"`
	Selected bool   `json:"selected"`
}

type adminProxy struct {
	ID                string  `json:"id"`
	NodeName          string  `json:"node_name"`
	Name              string  `json:"name"`
	Protocol          string  `json:"protocol"`
	Listen            string  `json:"listen"`
	ListenPort        int     `json:"listen_port"`
	Transport         string  `json:"transport"`
	Enabled           bool    `json:"enabled"`
	TrafficMultiplier float64 `json:"traffic_multiplier"`
	ShortID           string  `json:"short_id"`
	SettingsJSON      string  `json:"settings_json"`
	InboundRulesJSON  string  `json:"inbound_rules_json"`
	OutboundRulesJSON string  `json:"outbound_rules_json"`
	RouteRulesJSON    string  `json:"route_rules_json"`
	DeletedAt         string  `json:"deleted_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type adminPath struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	EndpointID   string `json:"endpoint_id"`
	ProxyID      string `json:"proxy_id"`
	ProxyName    string `json:"proxy_name"`
	NodeName     string `json:"node_name"`
	HostID       string `json:"host_id"`
	Host         string `json:"host"`
	HostTag      string `json:"host_tag"`
	DialerPathID string `json:"dialer_path_id"`
	Enabled      bool   `json:"enabled"`
	Visibility   string `json:"visibility"`
	Managed      bool   `json:"managed"`
	SortOrder    int    `json:"sort_order"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type adminPathPayload struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	ProxyID      string `json:"proxy_id"`
	HostID       string `json:"host_id"`
	DialerPathID string `json:"dialer_path_id"`
	Enabled      *bool  `json:"enabled"`
	Visibility   string `json:"visibility"`
	SortOrder    int    `json:"sort_order"`
}

type adminPathPatchPayload struct {
	Name         *string `json:"name"`
	DisplayName  *string `json:"display_name"`
	ProxyID      *string `json:"proxy_id"`
	HostID       *string `json:"host_id"`
	DialerPathID *string `json:"dialer_path_id"`
	Enabled      *bool   `json:"enabled"`
	Visibility   *string `json:"visibility"`
	SortOrder    *int    `json:"sort_order"`
}

type adminPathAccess struct {
	ID          string `json:"id"`
	PathID      string `json:"path_id"`
	ProxyUserID string `json:"proxy_user_id"`
	Enabled     bool   `json:"enabled"`
	DeletedAt   string `json:"deleted_at"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type adminUser struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	Status           string `json:"status"`
	GlobalQuotaBytes int64  `json:"global_quota_bytes"`
	ExpireAt         string `json:"expire_at"`
	ProxyCount       int    `json:"proxy_count"`
	DeletedAt        string `json:"deleted_at"`
}

type adminProxyCredential struct {
	ID                string   `json:"id"`
	UserName          string   `json:"user_name"`
	NodeName          string   `json:"node_name"`
	ProxyName         string   `json:"proxy_name"`
	Protocol          string   `json:"protocol"`
	Listen            string   `json:"listen"`
	ListenPort        int      `json:"listen_port"`
	Transport         string   `json:"transport"`
	AuthName          string   `json:"auth_name"`
	Enabled           bool     `json:"enabled"`
	QuotaBytes        int64    `json:"quota_bytes"`
	TrafficMultiplier *float64 `json:"traffic_multiplier,omitempty"`
	ProxyMultiplier   float64  `json:"proxy_multiplier"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
	DeletedAt         string   `json:"deleted_at"`
}

type adminUserTraffic struct {
	UserName      string `json:"user_name"`
	Direction     string `json:"direction"`
	RawBytes      int64  `json:"raw_bytes"`
	BillableBytes int64  `json:"billable_bytes"`
}

type adminNetworkEvent struct {
	NodeName    string `json:"node_name"`
	UserName    string `json:"user_name"`
	AuthName    string `json:"auth_name"`
	SourceIP    string `json:"source_ip"`
	TargetHost  string `json:"target_host"`
	TargetPort  int64  `json:"target_port"`
	Action      string `json:"action"`
	RawMessage  string `json:"raw_message"`
	Count       int64  `json:"count"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	CreatedAt   string `json:"created_at"`
}

type adminNetworkEventsResponse struct {
	Events []adminNetworkEvent `json:"events"`
	Total  int64               `json:"total"`
	Limit  int64               `json:"limit"`
	Offset int64               `json:"offset"`
}

type adminSettings struct {
	NetworkEventRetentionDays int64 `json:"network_event_retention_days"`
}

type adminSettingsPayload struct {
	NetworkEventRetentionDays *int64 `json:"network_event_retention_days"`
}

type adminRawNetworkLog struct {
	Cursor     string `json:"cursor"`
	Message    string `json:"message"`
	ObservedAt string `json:"observed_at"`
	IngestedAt string `json:"ingested_at"`
}

type adminSystemLog struct {
	Node       string `json:"node"`
	Service    string `json:"service"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	ObservedAt string `json:"observed_at"`
	IngestedAt string `json:"ingested_at"`
}

// adminSystemLogsResponse is a strict superset of the pre-pagination payload:
// `note` stays so existing readers keep working, and the page envelope matches
// adminNetworkEventsResponse.
type adminSystemLogsResponse struct {
	Logs []adminSystemLog `json:"logs"`
	// Services is the unfiltered option list for the page's service filter; a
	// single page can no longer enumerate it.
	Services []string `json:"services"`
	Total    int64    `json:"total"`
	Limit    int64    `json:"limit"`
	Offset   int64    `json:"offset"`
	Note     string   `json:"note"`
}

type adminNodePayload struct {
	Name       string `json:"name"`
	PublicHost string `json:"public_host"`
	APIBaseURL string `json:"api_base_url"`
	Status     string `json:"status"`
}

type adminNodePatchPayload struct {
	// Omitted (null) fields are preserved; an explicit value is written. So a
	// status-only toggle keeps the API URL, and the edit dialog can clear
	// api_base_url with "". public_host is required (UpdateNode rejects "")
	// and the edit form enforces a non-empty value before sending it.
	Name       *string `json:"name"`
	PublicHost *string `json:"public_host"`
	// Hosts, when present, replaces the full host list (the edit dialog sends it
	// and public_host is then derived from the first entry). Single-host clients
	// may still send public_host instead.
	Hosts      *[]adminNodeHost `json:"hosts"`
	APIBaseURL *string          `json:"api_base_url"`
	Status     *string          `json:"status"`
}

type adminNodeBootstrapPayload struct {
	Name       string `json:"name"`
	PublicHost string `json:"public_host"`
	ServerURL  string `json:"server_url"`
	SingBoxURL string `json:"sing_box_url"`
}

type adminNodeBootstrapResponse struct {
	Node             adminNode `json:"node"`
	BootstrapString  string    `json:"bootstrap_string"`
	InstallScriptURL string    `json:"install_script_url"`
}

type adminProxyPayload struct {
	// For PATCH, omitted and null fields both mean "leave the existing value".
	Name              string   `json:"name"`
	ShortID           *string  `json:"short_id"`
	Protocol          string   `json:"protocol"`
	Listen            *string  `json:"listen"`
	ListenPort        *int     `json:"listen_port"`
	Transport         *string  `json:"transport"`
	Enabled           *bool    `json:"enabled"`
	TrafficMultiplier *float64 `json:"traffic_multiplier"`
	SettingsJSON      *string  `json:"settings_json"`
	InboundRulesJSON  *string  `json:"inbound_rules_json"`
	OutboundRulesJSON *string  `json:"outbound_rules_json"`
	RouteRulesJSON    *string  `json:"route_rules_json"`
	DirectPublish     *bool    `json:"direct_publish"`
}

type adminUserPayload struct {
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	GlobalQuotaBytes *int64 `json:"global_quota_bytes"`
	ExpireAt         string `json:"expire_at"`
}

type adminUserPatchPayload struct {
	// Omitted (null) fields are left unchanged.
	DisplayName      *string `json:"display_name"`
	Status           *string `json:"status"`
	GlobalQuotaBytes *int64  `json:"global_quota_bytes"`
	ExpireAt         *string `json:"expire_at"`
}

type adminIssueAccessPayload struct {
	NodeName  string `json:"node_name"`
	ProxyName string `json:"proxy_name"`
}

type adminConfigChange struct {
	Node           string `json:"node"`
	TargetHash     string `json:"target_hash"`
	RenderedHash   string `json:"rendered_hash"`
	TargetVersion  string `json:"target_version"`
	TargetConfig   string `json:"target_config"`
	RenderedConfig string `json:"rendered_config"`
}

type adminConfigPublishResult struct {
	Node    string `json:"node"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Hash    string `json:"hash"`
	Created bool   `json:"created"`
}

func adminAuthMiddleware(token string, allowInsecure bool) func(http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	tokenDigest := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				if !allowInsecure {
					http.Error(w, "admin token is not configured", http.StatusServiceUnavailable)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			presentedDigest := sha256.Sum256([]byte(bearerToken(r.Header.Get("Authorization"))))
			if subtle.ConstantTimeCompare(presentedDigest[:], tokenDigest[:]) != 1 {
				http.Error(w, "invalid admin token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func adminOverviewHandler(store *db.DB, options Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, err := listAdminNodes(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		users, err := listAdminUsers(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		traffic, err := listAdminTraffic(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		systemLogs, err := store.ListRecentSystemLogs(r.Context(), "", 10)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminOverview{
			Nodes:         nodes,
			Users:         users,
			Traffic:       traffic,
			SystemLogs:    adminSystemLogs(systemLogs),
			SystemLogNote: "",
			Release: adminRelease{
				Repo:            releaseRepo(options),
				BoxFleetVersion: releaseVersion(options),
				AgentVersion:    releaseAgentVersion(options),
				SingBoxVersion:  releaseSingBoxVersion(options),
			},
		})
	}
}

func adminNodesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminPageRequested(r) {
			page, err := store.ListNodesPage(r.Context(), db.NodeFilter{
				Search:    strings.TrimSpace(r.URL.Query().Get("search")),
				Status:    strings.TrimSpace(r.URL.Query().Get("status")),
				Deleted:   deletedFilter(r),
				Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
				Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
				Limit:     queryLimit(r, 50),
				Offset:    queryOffset(r),
			})
			if err != nil {
				writeAdminError(w, err)
				return
			}
			nodes, err := adminNodesFromDB(r.Context(), store, page.Nodes)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			writeJSON(w, adminNodesPage{
				Nodes:  nodes,
				Total:  page.Total,
				Limit:  page.Limit,
				Offset: page.Offset,
			})
			return
		}
		nodes, err := listAdminNodes(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, nodes)
	}
}

func adminCreateNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminNodePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		node, err := store.CreateNode(r.Context(), payload.Name, payload.PublicHost, payload.APIBaseURL)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		resp, err := adminNodeResponse(r.Context(), store, node)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

func adminCreateNodeBootstrapHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminNodeBootstrapPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		publicHost := strings.TrimSpace(payload.PublicHost)
		if publicHost == "" {
			publicHost = strings.TrimSpace(payload.Name)
		}
		node, err := store.CreateNode(r.Context(), payload.Name, publicHost, "")
		if err != nil {
			writeAdminError(w, err)
			return
		}
		// Enrolled nodes start pending until their agent's first authenticated
		// heartbeat activates them (RecordHeartbeat), so an un-checked-in node
		// stays out of rendering/publishing and the enroll dialog's promise holds.
		if err := store.SetNodeStatus(r.Context(), node.Name, "pending"); err != nil {
			writeAdminError(w, err)
			return
		}
		node.Status = "pending"
		serverURL := strings.TrimRight(strings.TrimSpace(payload.ServerURL), "/")
		if serverURL == "" {
			serverURL = requestBaseURL(r)
		}
		resp, err := bootstrapResponseForNode(r.Context(), store, node, serverURL, strings.TrimSpace(payload.SingBoxURL))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

// adminReenrollNodeHandler re-issues a bootstrap string for an existing node so
// its install command can be shown again after the one-time enroll dialog was
// closed, or so a rebuilt/decommissioned node can be brought back online. It is
// deliberately restricted: an active node keeps its working token (no need, and
// minting extra tokens for a live agent is undesirable), and a paused node
// (disabled but token intact) should be re-enabled, not re-enrolled. Only a
// pending node (bootstrap lost before first check-in) or a decommissioned node
// (disabled with no valid token) qualifies.
func adminReenrollNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "node")
		node, err := store.GetNode(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		hasToken, err := store.NodeHasActiveToken(r.Context(), node.Name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if !(node.Status == "pending" || (node.Status == "disabled" && !hasToken)) {
			writeAdminError(w, fmt.Errorf("node %q cannot be re-enrolled in status %q", node.Name, node.Status))
			return
		}
		// The raw bootstrap token is never stored (only its hash), so the original
		// install string is unrecoverable. Revoke any existing token and issue a
		// fresh one, keeping exactly one valid token rather than accumulating.
		if err := store.RevokeNodeTokens(r.Context(), node.Name); err != nil {
			writeAdminError(w, err)
			return
		}
		// A decommissioned node is disabled; return it to pending so it re-enters
		// rendering/publishing once its agent checks in. A pending node stays pending.
		if node.Status != "pending" {
			if err := store.SetNodeStatus(r.Context(), node.Name, "pending"); err != nil {
				writeAdminError(w, err)
				return
			}
			node.Status = "pending"
		}
		// Body is optional; honor server_url / sing_box_url overrides when present.
		var payload adminNodeBootstrapPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		serverURL := strings.TrimRight(strings.TrimSpace(payload.ServerURL), "/")
		if serverURL == "" {
			serverURL = requestBaseURL(r)
		}
		resp, err := bootstrapResponseForNode(r.Context(), store, node, serverURL, strings.TrimSpace(payload.SingBoxURL))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

// bootstrapResponseForNode issues a fresh node token and builds the bootstrap
// string + install command response shared by enroll and re-enroll.
func bootstrapResponseForNode(ctx context.Context, store *db.DB, node db.Node, serverURL, singBoxURL string) (adminNodeBootstrapResponse, error) {
	issued, err := store.IssueNodeToken(ctx, node.Name)
	if err != nil {
		return adminNodeBootstrapResponse{}, err
	}
	bootstrapString, err := model.EncodeBootstrap(model.BootstrapConfig{
		NodeName:        issued.NodeName,
		Token:           issued.Token,
		ServerURL:       serverURL,
		SingBoxURL:      singBoxURL,
		InstallDir:      agent.DefaultInstallDir,
		AgentConfigPath: agent.DefaultConfigPath,
		V2RayAPIAddress: agent.DefaultV2RayAPIAddress,
	})
	if err != nil {
		return adminNodeBootstrapResponse{}, err
	}
	nodeResp, err := adminNodeResponse(ctx, store, node)
	if err != nil {
		return adminNodeBootstrapResponse{}, err
	}
	return adminNodeBootstrapResponse{
		Node:             nodeResp,
		BootstrapString:  bootstrapString,
		InstallScriptURL: serverURL + "/install.sh",
	}, nil
}

func releaseRepo(options Options) string {
	repo := strings.TrimSpace(options.Repo)
	if repo == "" {
		return install.DefaultRepo
	}
	return repo
}

func releaseVersion(options Options) string {
	version := strings.TrimSpace(options.Version)
	if version == "" {
		return "dev"
	}
	return version
}

func releaseAgentVersion(options Options) string {
	version := strings.TrimSpace(options.AgentVersion)
	if version == "" {
		return releaseVersion(options)
	}
	return version
}

func releaseSingBoxVersion(options Options) string {
	version := strings.TrimSpace(options.SingBoxVersion)
	if version == "" {
		return install.DefaultSingBoxVersion
	}
	return version
}

func requestBaseURL(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host
}

func adminNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		node, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		resp, err := adminNodeResponse(r.Context(), store, node)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

func adminUpdateNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		existing, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		var payload adminNodePatchPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		params := db.UpdateNodeParams{
			Name:       existing.Name,
			PublicHost: existing.PublicHost,
			Hosts:      existing.Hosts,
			APIBaseURL: existing.APIBaseURL,
			Status:     existing.Status,
		}
		if payload.Name != nil {
			params.Name = *payload.Name
		}
		if payload.Hosts != nil {
			hosts := make([]db.NodeHost, 0, len(*payload.Hosts))
			for _, h := range *payload.Hosts {
				hosts = append(hosts, db.NodeHost{ID: h.ID, Host: h.Host, Tag: h.Tag, Selected: h.Selected})
			}
			params.Hosts = hosts
		} else if payload.PublicHost != nil {
			// Single-host edit: replace the whole list with just this address.
			params.Hosts = nil
			params.PublicHost = *payload.PublicHost
		}
		if payload.APIBaseURL != nil {
			params.APIBaseURL = *payload.APIBaseURL
		}
		if payload.Status != nil {
			params.Status = *payload.Status
		}
		node, err := store.UpdateNodeByName(r.Context(), chi.URLParam(r, "node"), params)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		resp, err := adminNodeResponse(r.Context(), store, node)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

func adminDeleteNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		node, err := store.SoftDeleteNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		resp, err := adminNodeResponse(r.Context(), store, node)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

func adminRestoreNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		node, err := store.RestoreNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		resp, err := adminNodeResponse(r.Context(), store, node)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, resp)
	}
}

func adminNodeStatusHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, err := store.GetNodeConfigStatus(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeFromStatus(status))
	}
}

func adminProxiesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminPageRequested(r) {
			page, err := store.ListProxiesPage(r.Context(), db.ProxyFilter{
				Search:    strings.TrimSpace(r.URL.Query().Get("search")),
				NodeName:  strings.TrimSpace(r.URL.Query().Get("node")),
				Enabled:   strings.TrimSpace(r.URL.Query().Get("enabled")),
				Deleted:   deletedFilter(r),
				Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
				Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
				Limit:     queryLimit(r, 50),
				Offset:    queryOffset(r),
			})
			if err != nil {
				writeAdminError(w, err)
				return
			}
			writeJSON(w, adminProxiesPage{
				Proxies: adminProxies(page.Proxies),
				Total:   page.Total,
				Limit:   page.Limit,
				Offset:  page.Offset,
			})
			return
		}
		proxies, err := store.ListProxies(r.Context(), "")
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxies(proxies))
	}
}

func adminNodeProxiesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminPageRequested(r) {
			page, err := store.ListProxiesPage(r.Context(), db.ProxyFilter{
				Search:    strings.TrimSpace(r.URL.Query().Get("search")),
				NodeName:  chi.URLParam(r, "node"),
				Enabled:   strings.TrimSpace(r.URL.Query().Get("enabled")),
				Deleted:   deletedFilter(r),
				Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
				Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
				Limit:     queryLimit(r, 50),
				Offset:    queryOffset(r),
			})
			if err != nil {
				writeAdminError(w, err)
				return
			}
			writeJSON(w, adminProxiesPage{
				Proxies: adminProxies(page.Proxies),
				Total:   page.Total,
				Limit:   page.Limit,
				Offset:  page.Offset,
			})
			return
		}
		proxies, err := store.ListProxies(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxies(proxies))
	}
}

func adminCreateProxyHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminProxyPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if payload.Protocol == "" {
			payload.Protocol = db.ProtocolVLESSReality
		}
		settingsJSON, err := proxySettingsJSON(payload)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		enabled := true
		if payload.Enabled != nil {
			enabled = *payload.Enabled
		}
		multiplier := 1.0
		if payload.TrafficMultiplier != nil {
			multiplier = *payload.TrafficMultiplier
		}
		proxy, err := store.CreateProxy(r.Context(), db.CreateProxyParams{
			NodeName:          chi.URLParam(r, "node"),
			Name:              payload.Name,
			Protocol:          payload.Protocol,
			Listen:            stringValue(payload.Listen, ""),
			ListenPort:        intValue(payload.ListenPort, 0),
			Transport:         stringValue(payload.Transport, ""),
			Enabled:           enabled,
			TrafficMultiplier: multiplier,
			SettingsJSON:      settingsJSON,
			InboundRulesJSON:  stringValue(payload.InboundRulesJSON, ""),
			OutboundRulesJSON: stringValue(payload.OutboundRulesJSON, ""),
			RouteRulesJSON:    stringValue(payload.RouteRulesJSON, ""),
			DirectPublish:     payload.DirectPublish,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyFromDB(proxy))
	}
}

func adminUpdateProxyHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		existing, err := store.GetProxy(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "proxy"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		var payload adminProxyPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		enabled := existing.Enabled
		if payload.Enabled != nil {
			enabled = *payload.Enabled
		}
		multiplier := existing.TrafficMultiplier
		if payload.TrafficMultiplier != nil {
			multiplier = *payload.TrafficMultiplier
		}
		settingsJSON := existing.SettingsJSON
		if payload.SettingsJSON != nil || payload.ShortID != nil {
			payload.Protocol = existing.Protocol
			if payload.SettingsJSON == nil {
				payload.SettingsJSON = &existing.SettingsJSON
			}
			var err error
			settingsJSON, err = proxySettingsJSON(payload)
			if err != nil {
				writeAdminError(w, err)
				return
			}
		}
		name := existing.Name
		if strings.TrimSpace(payload.Name) != "" {
			name = payload.Name
		}
		proxy, err := store.UpdateProxyByName(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "proxy"), db.UpdateProxyParams{
			NodeName:          existing.NodeName,
			Name:              name,
			Listen:            stringValue(payload.Listen, existing.Listen),
			ListenPort:        intValue(payload.ListenPort, existing.ListenPort),
			Transport:         stringValue(payload.Transport, existing.Transport),
			Enabled:           enabled,
			TrafficMultiplier: multiplier,
			SettingsJSON:      settingsJSON,
			InboundRulesJSON:  stringValue(payload.InboundRulesJSON, existing.InboundRulesJSON),
			OutboundRulesJSON: stringValue(payload.OutboundRulesJSON, existing.OutboundRulesJSON),
			RouteRulesJSON:    stringValue(payload.RouteRulesJSON, existing.RouteRulesJSON),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		if payload.DirectPublish != nil {
			if _, err := store.SetProxyDirectPublication(r.Context(), proxy.ID, *payload.DirectPublish); err != nil {
				writeAdminError(w, err)
				return
			}
		}
		writeJSON(w, adminProxyFromDB(proxy))
	}
}

func adminDeleteProxyHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := store.SoftDeleteProxy(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "proxy"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyFromDB(proxy))
	}
}

func adminRestoreProxyHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := store.RestoreProxy(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "proxy"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyFromDB(proxy))
	}
}

func adminRenderConfigHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := render.RenderNodeConfig(r.Context(), store, chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n"))
	}
}

func adminUsersHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminPageRequested(r) {
			page, err := store.ListUsersPage(r.Context(), db.UserFilter{
				Search:    strings.TrimSpace(r.URL.Query().Get("search")),
				Status:    strings.TrimSpace(r.URL.Query().Get("status")),
				Deleted:   deletedFilter(r),
				Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
				Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
				Limit:     queryLimit(r, 50),
				Offset:    queryOffset(r),
			})
			if err != nil {
				writeAdminError(w, err)
				return
			}
			writeJSON(w, adminUsersPage{
				Users:  adminUserRows(page.Users),
				Total:  page.Total,
				Limit:  page.Limit,
				Offset: page.Offset,
			})
			return
		}
		users, err := listAdminUsers(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, users)
	}
}

func adminCreateUserHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminUserPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		quota := int64(0)
		if payload.GlobalQuotaBytes != nil {
			quota = *payload.GlobalQuotaBytes
		}
		user, err := store.CreateProxyUser(r.Context(), db.CreateProxyUserParams{
			Name:             payload.Name,
			DisplayName:      payload.DisplayName,
			GlobalQuotaBytes: quota,
			ExpireAt:         payload.ExpireAt,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:               user.ID,
			Name:             user.Name,
			DisplayName:      user.DisplayName,
			Status:           user.Status,
			GlobalQuotaBytes: user.GlobalQuotaBytes,
			ExpireAt:         nullString(user.ExpireAt),
			ProxyCount:       0,
		})
	}
}

func adminUpdateUserHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminUserPatchPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		name := chi.URLParam(r, "user")
		user, err := store.UpdateProxyUser(r.Context(), name, db.UpdateProxyUserParams{
			DisplayName:      payload.DisplayName,
			Status:           payload.Status,
			GlobalQuotaBytes: payload.GlobalQuotaBytes,
			ExpireAt:         payload.ExpireAt,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		accesses, err := store.ListProxyCredentialsByUser(r.Context(), name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:               user.ID,
			Name:             user.Name,
			DisplayName:      user.DisplayName,
			Status:           user.Status,
			GlobalQuotaBytes: user.GlobalQuotaBytes,
			ExpireAt:         nullString(user.ExpireAt),
			ProxyCount:       len(accesses),
		})
	}
}

func adminDeleteUserHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := store.SoftDeleteProxyUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:               user.ID,
			Name:             user.Name,
			DisplayName:      user.DisplayName,
			Status:           user.Status,
			GlobalQuotaBytes: user.GlobalQuotaBytes,
			ExpireAt:         nullString(user.ExpireAt),
			DeletedAt:        nullString(user.DeletedAt),
		})
	}
}

func adminRestoreUserHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := store.RestoreProxyUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		accesses, err := store.ListProxyCredentialsByUser(r.Context(), user.Name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:               user.ID,
			Name:             user.Name,
			DisplayName:      user.DisplayName,
			Status:           user.Status,
			GlobalQuotaBytes: user.GlobalQuotaBytes,
			ExpireAt:         nullString(user.ExpireAt),
			ProxyCount:       len(accesses),
		})
	}
}

func adminUserProxiesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accesses, err := store.ListProxyCredentialsByUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyCredentials(accesses))
	}
}

func adminIssueUserProxyCredentialHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminIssueAccessPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		userName := chi.URLParam(r, "user")
		if _, err := store.BindUserToNode(r.Context(), userName, payload.NodeName); err != nil {
			writeAdminError(w, err)
			return
		}
		params := db.IssueCredentialParams{
			UserName:  userName,
			NodeName:  payload.NodeName,
			ProxyName: payload.ProxyName,
		}
		proxy, err := store.GetProxy(r.Context(), payload.NodeName, payload.ProxyName)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		var access db.ProxyCredential
		switch proxy.Protocol {
		case db.ProtocolVLESSReality:
			access, err = store.IssueVLESSRealityAccess(r.Context(), params)
		case db.ProtocolShadowsocks2022:
			access, err = store.IssueShadowsocks2022Access(r.Context(), params)
		default:
			err = fmt.Errorf("proxy protocol %s does not support credentials", proxy.Protocol)
		}
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyCredentials([]db.ProxyCredential{access})[0])
	}
}

func adminPathsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		paths, err := store.ListPaths(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		out := make([]adminPath, 0, len(paths))
		for _, path := range paths {
			item, err := adminPathFromDB(r.Context(), store, path)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			out = append(out, item)
		}
		writeJSON(w, out)
	}
}

func adminCreatePathHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminPathPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		endpoint, err := store.EnsureEndpoint(r.Context(), payload.ProxyID, payload.HostID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		enabled := true
		if payload.Enabled != nil {
			enabled = *payload.Enabled
		}
		path, err := store.CreatePath(r.Context(), db.CreatePathParams{
			Name: payload.Name, DisplayName: payload.DisplayName, EndpointID: endpoint.ID,
			DialerPathID: payload.DialerPathID, Enabled: enabled,
			Visibility: payload.Visibility, SortOrder: payload.SortOrder,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		item, err := adminPathFromDB(r.Context(), store, path)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, item)
	}
}

func adminUpdatePathHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pathID := chi.URLParam(r, "path")
		existing, err := store.GetPath(r.Context(), pathID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		var payload adminPathPatchPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		endpointID := existing.EndpointID
		if (payload.ProxyID == nil) != (payload.HostID == nil) {
			writeAdminError(w, fmt.Errorf("proxy_id and host_id must be updated together"))
			return
		}
		if payload.ProxyID != nil {
			endpoint, err := store.EnsureEndpoint(r.Context(), *payload.ProxyID, *payload.HostID)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			endpointID = endpoint.ID
		}
		path, err := store.UpdatePath(r.Context(), db.UpdatePathParams{
			ID:           pathID,
			Name:         stringPointerValue(payload.Name, existing.Name),
			DisplayName:  stringPointerValue(payload.DisplayName, existing.DisplayName),
			EndpointID:   endpointID,
			DialerPathID: stringPointerValue(payload.DialerPathID, existing.DialerPathID.String),
			Enabled:      boolPointerValue(payload.Enabled, existing.Enabled),
			Visibility:   stringPointerValue(payload.Visibility, existing.Visibility),
			Managed:      existing.Managed,
			SortOrder:    intPointerValue(payload.SortOrder, existing.SortOrder),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		item, err := adminPathFromDB(r.Context(), store, path)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, item)
	}
}

func adminDeletePathHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := store.DeletePath(r.Context(), chi.URLParam(r, "path")); err != nil {
			writeAdminError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func adminUserPathsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accesses, err := store.ListActivePathAccessesByUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		out := make([]adminPathAccess, 0, len(accesses))
		for _, access := range accesses {
			out = append(out, adminPathAccessFromDB(access))
		}
		writeJSON(w, out)
	}
}

func adminGrantUserPathHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			PathID string `json:"path_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		access, err := store.GrantPathToUser(r.Context(), chi.URLParam(r, "user"), payload.PathID)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminPathAccessFromDB(access))
	}
}

func adminRevokeUserPathHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		access, err := store.RevokePathAccess(r.Context(), chi.URLParam(r, "user"), chi.URLParam(r, "path"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminPathAccessFromDB(access))
	}
}

func adminPathFromDB(ctx context.Context, store *db.DB, path db.Path) (adminPath, error) {
	endpoint, err := store.GetEndpoint(ctx, path.EndpointID)
	if err != nil {
		return adminPath{}, err
	}
	proxy, host, err := store.ResolveEndpoint(ctx, endpoint)
	if err != nil {
		return adminPath{}, err
	}
	return adminPath{
		ID: path.ID, Name: path.Name, DisplayName: path.DisplayName,
		EndpointID: endpoint.ID, ProxyID: proxy.ID, ProxyName: proxy.Name,
		NodeName: proxy.NodeName, HostID: host.ID, Host: host.Host, HostTag: host.Tag,
		DialerPathID: path.DialerPathID.String, Enabled: path.Enabled,
		Visibility: path.Visibility, Managed: path.Managed, SortOrder: path.SortOrder,
		CreatedAt: path.CreatedAt, UpdatedAt: path.UpdatedAt,
	}, nil
}

func adminPathAccessFromDB(access db.PathAccess) adminPathAccess {
	return adminPathAccess{
		ID: access.ID, PathID: access.PathID, ProxyUserID: access.ProxyUserID,
		Enabled: access.Enabled, DeletedAt: nullString(access.DeletedAt),
		CreatedAt: access.CreatedAt, UpdatedAt: access.UpdatedAt,
	}
}

func adminDeleteUserProxyCredentialHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		access, err := store.SoftDeleteProxyCredential(
			r.Context(),
			chi.URLParam(r, "user"),
			chi.URLParam(r, "node"),
			chi.URLParam(r, "proxy"),
		)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyCredentials([]db.ProxyCredential{access})[0])
	}
}

func adminUserNodeInfoHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName := strings.TrimSpace(r.URL.Query().Get("node"))
		raw, err := render.RenderNodeInfo(r.Context(), store, chi.URLParam(r, "user"), nodeName)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		_, _ = w.Write([]byte("\n"))
	}
}

func adminUserConnectionInfoHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := render.ConnectionInfoForUser(r.Context(), store, chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, info)
	}
}

func adminUserTrafficHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.SumTrafficByUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminTrafficRows(rows))
	}
}

func adminTrafficUsersHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := listAdminTraffic(r, store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, rows)
	}
}

func adminNetworkEventsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start, ok := queryOptionalTime(w, r, "start")
		if !ok {
			return
		}
		end, ok := queryOptionalTime(w, r, "end")
		if !ok {
			return
		}
		page, err := store.ListLogEventsPage(r.Context(), db.LogEventFilter{
			NodeName: strings.TrimSpace(r.URL.Query().Get("node")),
			UserName: strings.TrimSpace(r.URL.Query().Get("user")),
			Action:   strings.TrimSpace(r.URL.Query().Get("action")),
			Search:   strings.TrimSpace(r.URL.Query().Get("search")),
			Start:    start,
			End:      end,
			Limit:    queryLimit(r, 100),
			Offset:   queryOffset(r),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNetworkEventsResponse{
			Events: adminNetworkEventDetails(page.Events),
			Total:  page.Total,
			Limit:  page.Limit,
			Offset: page.Offset,
		})
	}
}

func adminSettingsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := store.AdminSettings(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSettingsFromDB(settings))
	}
}

func adminUpdateSettingsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload adminSettingsPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if payload.NetworkEventRetentionDays != nil {
			if err := store.SetNetworkEventRetentionDays(r.Context(), *payload.NetworkEventRetentionDays); err != nil {
				writeAdminError(w, err)
				return
			}
		}
		settings, err := store.AdminSettings(r.Context())
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSettingsFromDB(settings))
	}
}

func adminNodeNetworkEventsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListRecentLogEventsByNode(r.Context(), chi.URLParam(r, "node"), queryLimit(r, 100))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNetworkEvents(events))
	}
}

func adminUserNetworkEventsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListRecentLogEventsByUser(r.Context(), chi.URLParam(r, "user"), queryLimit(r, 100))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNetworkEvents(events))
	}
}

func adminNodeRawNetworkLogsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := store.ListRecentRawLogEntriesByNode(r.Context(), chi.URLParam(r, "node"), queryLimit(r, 100))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		out := make([]adminRawNetworkLog, 0, len(entries))
		for _, entry := range entries {
			cursor := ""
			if entry.JournalCursor.Valid {
				cursor = entry.JournalCursor.String
			}
			out = append(out, adminRawNetworkLog{
				Cursor:     cursor,
				Message:    entry.RawMessage,
				ObservedAt: entry.ObservedAt,
				IngestedAt: entry.IngestedAt,
			})
		}
		writeJSON(w, out)
	}
}

func adminSystemLogsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := store.ListSystemLogsPage(r.Context(), db.SystemLogFilter{
			NodeName:  strings.TrimSpace(r.URL.Query().Get("node")),
			Service:   strings.TrimSpace(r.URL.Query().Get("service")),
			Level:     strings.TrimSpace(r.URL.Query().Get("level")),
			Search:    strings.TrimSpace(r.URL.Query().Get("search")),
			Sort:      strings.TrimSpace(r.URL.Query().Get("sort")),
			Direction: strings.TrimSpace(r.URL.Query().Get("direction")),
			Limit:     queryLimit(r, 100),
			Offset:    queryOffset(r),
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminSystemLogsResponse{
			Logs:     adminSystemLogs(page.Logs),
			Services: page.Services,
			Total:    page.Total,
			Limit:    page.Limit,
			Offset:   page.Offset,
			Note:     "",
		})
	}
}

func adminPublishConfigHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName := chi.URLParam(r, "node")
		raw, err := render.RenderNodeConfig(r.Context(), store, nodeName)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		published, err := store.PublishConfig(r.Context(), nodeName, raw)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, map[string]any{
			"id":      published.ConfigVersion.ID,
			"version": published.ConfigVersion.Version,
			"hash":    published.ConfigVersion.ConfigHash,
			"created": published.Created,
		})
	}
}

func adminConfigChangesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		changes, err := configChanges(r.Context(), store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, map[string]any{"changed": changes})
	}
}

func adminPublishChangedConfigsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		changes, err := configChanges(r.Context(), store)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		results := make([]adminConfigPublishResult, 0, len(changes))
		for _, change := range changes {
			raw, err := render.RenderNodeConfig(r.Context(), store, change.Node)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			published, err := store.PublishConfig(r.Context(), change.Node, raw)
			if err != nil {
				writeAdminError(w, err)
				return
			}
			results = append(results, adminConfigPublishResult{
				Node:    change.Node,
				ID:      published.ConfigVersion.ID,
				Version: published.ConfigVersion.Version,
				Hash:    published.ConfigVersion.ConfigHash,
				Created: published.Created,
			})
		}
		writeJSON(w, map[string]any{"published": results})
	}
}

func configChanges(ctx context.Context, store *db.DB) ([]adminConfigChange, error) {
	nodes, err := store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	changes := make([]adminConfigChange, 0)
	for _, node := range nodes {
		// Only disabled nodes are skipped: a disabled node is served the
		// no-inbound config directly. Pending nodes (enrolled, awaiting first
		// heartbeat) and degraded nodes are still rendered/published so their
		// agent pulls the right target as soon as it polls — otherwise Apply
		// would silently skip a freshly enrolled node.
		if node.Status == "disabled" {
			continue
		}
		raw, err := render.RenderNodeConfig(ctx, store, node.Name)
		if err != nil {
			return nil, err
		}
		renderedHash := db.SHA256Hex(raw)
		status, err := store.GetNodeConfigStatus(ctx, node.Name)
		if err != nil {
			return nil, err
		}
		targetHash := ""
		if status.TargetConfigHash.Valid {
			targetHash = status.TargetConfigHash.String
		}
		if targetHash == renderedHash {
			continue
		}
		targetConfig := ""
		if status.TargetConfigVersionID.Valid {
			target, err := store.GetTargetConfig(ctx, node.Name)
			if err != nil {
				return nil, err
			}
			targetConfig = target.ConfigJson
		}
		changes = append(changes, adminConfigChange{
			Node:           node.Name,
			TargetHash:     targetHash,
			RenderedHash:   renderedHash,
			TargetVersion:  nullInt(status.TargetVersion),
			TargetConfig:   targetConfig,
			RenderedConfig: string(raw),
		})
	}
	return changes, nil
}

func listAdminNodes(r *http.Request, store *db.DB) ([]adminNode, error) {
	nodes, err := store.ListNodes(r.Context())
	if err != nil {
		return nil, err
	}
	return adminNodesFromDB(r.Context(), store, nodes)
}

func adminNodesFromDB(ctx context.Context, store *db.DB, nodes []db.Node) ([]adminNode, error) {
	statuses, err := store.ListNodeConfigStatuses(ctx)
	if err != nil {
		return nil, err
	}
	statusByNode := make(map[string]db.NodeConfigStatus, len(statuses))
	for _, status := range statuses {
		statusByNode[status.NodeName] = status
	}
	tokenNames, err := store.ListNodeNamesWithActiveTokens(ctx)
	if err != nil {
		return nil, err
	}
	hasToken := make(map[string]bool, len(tokenNames))
	for _, name := range tokenNames {
		hasToken[name] = true
	}
	activeOperations, err := store.ListActiveNodeOperations(ctx)
	if err != nil {
		return nil, err
	}
	operationByNodeID := make(map[string]db.NodeOperation, len(activeOperations))
	for _, operation := range activeOperations {
		operationByNodeID[operation.NodeID] = operation
	}
	out := make([]adminNode, 0, len(nodes))
	for _, node := range nodes {
		// Base off adminNodeFromNode so list responses carry the same fields as
		// single-node responses (notably Hosts) instead of a divergent literal.
		item := adminNodeFromNode(node)
		item.HasActiveToken = hasToken[node.Name]
		if operation, ok := operationByNodeID[node.ID]; ok {
			operationCopy := operation
			item.ActiveOperation = &operationCopy
		}
		if status, ok := statusByNode[node.Name]; ok {
			statusItem := adminNodeFromStatus(status)
			item.TargetVersion = statusItem.TargetVersion
			item.CurrentVersion = statusItem.CurrentVersion
			item.ApplyStatus = statusItem.ApplyStatus
			item.ApplyError = statusItem.ApplyError
			item.LatestHeartbeat = statusItem.LatestHeartbeat
			item.AgentVersion = statusItem.AgentVersion
			item.AgentGOOS = statusItem.AgentGOOS
			item.AgentGOARCH = statusItem.AgentGOARCH
			item.Capabilities = statusItem.Capabilities
			item.SingBoxVersion = statusItem.SingBoxVersion
		}
		out = append(out, item)
	}
	return out, nil
}

func adminNodeFromNode(node db.Node) adminNode {
	hosts := make([]adminNodeHost, 0, len(node.Hosts))
	for _, h := range node.Hosts {
		hosts = append(hosts, adminNodeHost{ID: h.ID, Host: h.Host, Tag: h.Tag, Selected: h.Selected})
	}
	return adminNode{
		ID:             node.ID,
		Name:           node.Name,
		PublicHost:     node.PublicHost,
		APIBaseURL:     node.APIBaseURL,
		Status:         node.Status,
		SingBoxVersion: node.SingBoxVersion,
		LastSeenAt:     nullString(node.LastSeenAt),
		DeletedAt:      nullString(node.DeletedAt),
		Hosts:          hosts,
	}
}

// adminNodeResponse builds a single-node response with has_active_token filled
// in, so single-node endpoints carry the same paused-vs-decommissioned signal
// the list endpoint does.
func adminNodeResponse(ctx context.Context, store *db.DB, node db.Node) (adminNode, error) {
	item := adminNodeFromNode(node)
	if node.DeletedAt.Valid {
		return item, nil
	}
	has, err := store.NodeHasActiveToken(ctx, node.Name)
	if err != nil {
		return adminNode{}, err
	}
	item.HasActiveToken = has
	return item, nil
}

func listAdminUsers(r *http.Request, store *db.DB) ([]adminUser, error) {
	var users []db.ProxyUserWithProxyCount
	var err error
	if deletedFilter(r) == "only" {
		users, err = store.ListDeletedProxyUsersWithProxyCounts(r.Context())
	} else {
		users, err = store.ListProxyUsersWithProxyCounts(r.Context())
	}
	if err != nil {
		return nil, err
	}
	out := make([]adminUser, 0, len(users))
	for _, user := range users {
		out = append(out, adminUser{
			ID:               user.ID,
			Name:             user.Name,
			DisplayName:      user.DisplayName,
			Status:           user.Status,
			GlobalQuotaBytes: user.GlobalQuotaBytes,
			ExpireAt:         nullString(user.ExpireAt),
			ProxyCount:       int(user.ProxyCount),
			DeletedAt:        nullString(user.DeletedAt),
		})
	}
	return out, nil
}

func adminUserRows(rows []db.ProxyUserPageRow) []adminUserRow {
	out := make([]adminUserRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminUserRow{
			adminUser: adminUser{
				ID:               row.ID,
				Name:             row.Name,
				DisplayName:      row.DisplayName,
				Status:           row.Status,
				GlobalQuotaBytes: row.GlobalQuotaBytes,
				ExpireAt:         nullString(row.ExpireAt),
				ProxyCount:       int(row.ProxyCount),
				DeletedAt:        nullString(row.DeletedAt),
			},
			EffectiveStatus: row.EffectiveStatus,
			Traffic: adminTrafficDirected{
				UplinkRawBytes:        row.UplinkRawBytes,
				UplinkBillableBytes:   row.UplinkBillableBytes,
				DownlinkRawBytes:      row.DownlinkRawBytes,
				DownlinkBillableBytes: row.DownlinkBillableBytes,
			},
		})
	}
	return out
}

func listAdminTraffic(r *http.Request, store *db.DB) ([]adminUserTraffic, error) {
	rows, err := store.SumTrafficByAllUsers(r.Context())
	if err != nil {
		return nil, err
	}
	return adminTrafficRows(rows), nil
}

func adminTrafficRows(rows []db.TrafficSummary) []adminUserTraffic {
	out := make([]adminUserTraffic, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminUserTraffic{
			UserName:      row.UserName,
			Direction:     row.Direction,
			RawBytes:      row.RawBytes,
			BillableBytes: row.BillableBytes,
		})
	}
	return out
}

func adminSettingsFromDB(settings db.AdminSettings) adminSettings {
	return adminSettings{
		NetworkEventRetentionDays: settings.NetworkEventRetentionDays,
	}
}

func adminNetworkEvents(events []db.LogEvent) []adminNetworkEvent {
	out := make([]adminNetworkEvent, 0, len(events))
	for _, event := range events {
		out = append(out, adminNetworkEvent{
			AuthName:    event.AuthName,
			SourceIP:    event.SourceIp,
			TargetHost:  event.TargetHost,
			TargetPort:  event.TargetPort,
			Action:      event.Action,
			RawMessage:  event.RawMessage,
			Count:       event.Count,
			WindowStart: event.WindowStart,
			WindowEnd:   event.WindowEnd,
			CreatedAt:   event.CreatedAt,
		})
	}
	return out
}

func adminNetworkEventDetails(events []db.LogEventDetail) []adminNetworkEvent {
	out := make([]adminNetworkEvent, 0, len(events))
	for _, event := range events {
		out = append(out, adminNetworkEvent{
			NodeName:    event.NodeName,
			UserName:    event.UserName,
			AuthName:    event.AuthName,
			SourceIP:    event.SourceIp,
			TargetHost:  event.TargetHost,
			TargetPort:  event.TargetPort,
			Action:      event.Action,
			RawMessage:  event.RawMessage,
			Count:       event.Count,
			WindowStart: event.WindowStart,
			WindowEnd:   event.WindowEnd,
			CreatedAt:   event.CreatedAt,
		})
	}
	return out
}

func adminProxies(proxies []db.Proxy) []adminProxy {
	out := make([]adminProxy, 0, len(proxies))
	for _, proxy := range proxies {
		out = append(out, adminProxyFromDB(proxy))
	}
	return out
}

func adminProxyCredentials(accesses []db.ProxyCredential) []adminProxyCredential {
	out := make([]adminProxyCredential, 0, len(accesses))
	for _, access := range accesses {
		var multiplier *float64
		if access.TrafficMultiplier.Valid {
			value := access.TrafficMultiplier.Float64
			multiplier = &value
		}
		out = append(out, adminProxyCredential{
			ID:                access.ID,
			UserName:          access.ProxyUserName,
			NodeName:          access.NodeName,
			ProxyName:         access.ProxyName,
			Protocol:          access.Protocol,
			Listen:            access.Listen,
			ListenPort:        access.ListenPort,
			Transport:         access.Transport,
			AuthName:          access.AuthName,
			Enabled:           access.Enabled,
			QuotaBytes:        access.QuotaBytes,
			TrafficMultiplier: multiplier,
			ProxyMultiplier:   access.ProxyTrafficMultiplier,
			CreatedAt:         access.CreatedAt,
			UpdatedAt:         access.UpdatedAt,
			DeletedAt:         nullString(access.DeletedAt),
		})
	}
	return out
}

func adminProxyFromDB(proxy db.Proxy) adminProxy {
	return adminProxy{
		ID:                proxy.ID,
		NodeName:          proxy.NodeName,
		Name:              proxy.Name,
		Protocol:          proxy.Protocol,
		Listen:            proxy.Listen,
		ListenPort:        proxy.ListenPort,
		Transport:         proxy.Transport,
		Enabled:           proxy.Enabled,
		TrafficMultiplier: proxy.TrafficMultiplier,
		ShortID:           proxyShortID(proxy.SettingsJSON),
		SettingsJSON:      proxy.SettingsJSON,
		InboundRulesJSON:  proxy.InboundRulesJSON,
		OutboundRulesJSON: proxy.OutboundRulesJSON,
		RouteRulesJSON:    proxy.RouteRulesJSON,
		DeletedAt:         nullString(proxy.DeletedAt),
		CreatedAt:         proxy.CreatedAt,
		UpdatedAt:         proxy.UpdatedAt,
	}
}

func proxyShortID(settingsJSON string) string {
	var settings struct {
		ShortID string `json:"short_id"`
	}
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return ""
	}
	return settings.ShortID
}

func adminSystemLogs(logs []db.SystemLog) []adminSystemLog {
	out := make([]adminSystemLog, 0, len(logs))
	for _, log := range logs {
		out = append(out, adminSystemLog{
			Node:       log.NodeName,
			Service:    log.Service,
			Level:      log.Level,
			Message:    log.RawMessage,
			ObservedAt: log.ObservedAt,
			IngestedAt: log.IngestedAt,
		})
	}
	return out
}

func adminNodeFromStatus(status db.NodeConfigStatus) adminNode {
	return adminNode{
		ID:              status.NodeID,
		Name:            status.NodeName,
		TargetVersion:   nullInt(status.TargetVersion),
		CurrentVersion:  nullInt(status.CurrentVersion),
		ApplyStatus:     status.LastApplyStatus,
		ApplyError:      status.LastApplyError,
		LatestHeartbeat: nullString(status.LatestHeartbeat),
		AgentVersion:    status.AgentVersion,
		AgentGOOS:       status.AgentGOOS,
		AgentGOARCH:     status.AgentGOARCH,
		Capabilities:    status.Capabilities,
		SingBoxVersion:  status.SingBoxVersion,
	}
}

func proxySettingsJSON(payload adminProxyPayload) (string, error) {
	settingsJSON := ""
	if payload.SettingsJSON != nil {
		settingsJSON = strings.TrimSpace(*payload.SettingsJSON)
	}
	if settingsJSON != "" && payload.Protocol != db.ProtocolVLESSReality {
		if !json.Valid([]byte(settingsJSON)) {
			return "", errInvalidJSON("settings_json")
		}
		return settingsJSON, nil
	}
	if payload.Protocol != db.ProtocolVLESSReality {
		return "{}", nil
	}
	settings := map[string]any{}
	if settingsJSON != "" {
		if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
			return "", errInvalidJSON("settings_json")
		}
	}
	serverName, _ := settings["server_name"].(string)
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		serverName = "www.amazon.com"
	}
	handshakeServer, _ := settings["handshake_server"].(string)
	handshakeServer = strings.TrimSpace(handshakeServer)
	if handshakeServer == "" {
		handshakeServer = serverName
	}
	if _, ok := settings["handshake_port"]; !ok {
		settings["handshake_port"] = 443
	}
	privateKey, _ := settings["reality_private_key"].(string)
	publicKey, _ := settings["reality_public_key"].(string)
	if strings.TrimSpace(privateKey) == "" || strings.TrimSpace(publicKey) == "" {
		keyPair, err := secret.RealityKeyPairX25519()
		if err != nil {
			return "", err
		}
		settings["reality_private_key"] = keyPair.PrivateKey
		settings["reality_public_key"] = keyPair.PublicKey
	}
	shortID := ""
	if existing, ok := settings["short_id"].(string); ok {
		shortID = existing
	}
	if payload.ShortID != nil {
		shortID = *payload.ShortID
	}
	normalizedShortID, err := db.NormalizeRealityShortID(shortID)
	if err != nil {
		return "", err
	}
	settings["short_id"] = normalizedShortID
	settings["server_name"] = serverName
	settings["handshake_server"] = handshakeServer
	raw, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func stringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func stringPointerValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func boolPointerValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func intPointerValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

type errInvalidJSON string

func (e errInvalidJSON) Error() string {
	return string(e) + " must be valid JSON"
}

func queryLimit(r *http.Request, fallback int64) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	if value > 500 {
		return 500
	}
	return value
}

func adminPageRequested(r *http.Request) bool {
	query := r.URL.Query()
	for _, key := range []string{"limit", "offset", "search", "status", "enabled", "deleted", "node", "sort", "direction"} {
		if strings.TrimSpace(query.Get(key)) != "" {
			return true
		}
	}
	return false
}

func deletedFilter(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("deleted"))) {
	case "1", "true", "only":
		return "only"
	default:
		return ""
	}
}

func queryOffset(r *http.Request) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get("offset"))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func queryOptionalTime(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return "", true
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		http.Error(w, name+" must be RFC3339 time", http.StatusUnprocessableEntity)
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339Nano), true
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func nullInt(value sql.NullInt64) string {
	if value.Valid {
		return strconv.FormatInt(value.Int64, 10)
	}
	return ""
}

func writeAdminError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
