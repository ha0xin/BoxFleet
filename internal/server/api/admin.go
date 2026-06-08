package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
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

type adminRelease struct {
	Repo            string `json:"repo"`
	BoxFleetVersion string `json:"boxfleet_version"`
	SingBoxVersion  string `json:"sing_box_version"`
}

type adminNode struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PublicHost      string `json:"public_host"`
	APIBaseURL      string `json:"api_base_url"`
	Status          string `json:"status"`
	SingBoxVersion  string `json:"sing_box_version"`
	LastSeenAt      string `json:"last_seen_at"`
	TargetVersion   string `json:"target_version,omitempty"`
	CurrentVersion  string `json:"current_version,omitempty"`
	ApplyStatus     string `json:"apply_status,omitempty"`
	ApplyError      string `json:"apply_error,omitempty"`
	LatestHeartbeat string `json:"latest_heartbeat,omitempty"`
	AgentVersion    string `json:"agent_version,omitempty"`
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
	SettingsJSON      string  `json:"settings_json"`
	InboundRulesJSON  string  `json:"inbound_rules_json"`
	OutboundRulesJSON string  `json:"outbound_rules_json"`
	RouteRulesJSON    string  `json:"route_rules_json"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type adminUser struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	DisplayName       string  `json:"display_name"`
	Status            string  `json:"status"`
	GlobalQuotaBytes  int64   `json:"global_quota_bytes"`
	TrafficMultiplier float64 `json:"traffic_multiplier"`
	ExpireAt          string  `json:"expire_at"`
	ProxyCount        int     `json:"proxy_count"`
}

type adminProxyAccess struct {
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

type adminNodePayload struct {
	Name       string `json:"name"`
	PublicHost string `json:"public_host"`
	APIBaseURL string `json:"api_base_url"`
	Status     string `json:"status"`
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
}

type adminUserPayload struct {
	Name              string   `json:"name"`
	DisplayName       string   `json:"display_name"`
	GlobalQuotaBytes  *int64   `json:"global_quota_bytes"`
	TrafficMultiplier *float64 `json:"traffic_multiplier"`
	ExpireAt          string   `json:"expire_at"`
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
				SingBoxVersion:  releaseSingBoxVersion(options),
			},
		})
	}
}

func adminNodesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, adminNodeFromNode(node))
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
		issued, err := store.IssueNodeToken(r.Context(), node.Name)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		serverURL := strings.TrimRight(strings.TrimSpace(payload.ServerURL), "/")
		if serverURL == "" {
			serverURL = requestBaseURL(r)
		}
		singBoxURL := strings.TrimSpace(payload.SingBoxURL)
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
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeBootstrapResponse{
			Node:             adminNodeFromNode(node),
			BootstrapString:  bootstrapString,
			InstallScriptURL: serverURL + "/install.sh",
		})
	}
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
		writeJSON(w, adminNodeFromNode(node))
	}
}

func adminUpdateNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		existing, err := store.GetNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		var payload adminNodePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(payload.PublicHost) == "" {
			payload.PublicHost = existing.PublicHost
		}
		if strings.TrimSpace(payload.Status) == "" {
			payload.Status = existing.Status
		}
		node, err := store.UpdateNode(r.Context(), db.UpdateNodeParams{
			Name:       existing.Name,
			PublicHost: payload.PublicHost,
			APIBaseURL: payload.APIBaseURL,
			Status:     payload.Status,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeFromNode(node))
	}
}

func adminDeleteNodeHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		node, err := store.DisableNode(r.Context(), chi.URLParam(r, "node"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminNodeFromNode(node))
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
		if payload.SettingsJSON != nil {
			payload.Protocol = existing.Protocol
			var err error
			settingsJSON, err = proxySettingsJSON(payload)
			if err != nil {
				writeAdminError(w, err)
				return
			}
		}
		proxy, err := store.UpdateProxy(r.Context(), db.UpdateProxyParams{
			NodeName:          existing.NodeName,
			Name:              existing.Name,
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
		writeJSON(w, adminProxyFromDB(proxy))
	}
}

func adminDeleteProxyHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy, err := store.DisableProxy(r.Context(), chi.URLParam(r, "node"), chi.URLParam(r, "proxy"))
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
		multiplier := 1.0
		if payload.TrafficMultiplier != nil {
			multiplier = *payload.TrafficMultiplier
		}
		user, err := store.CreateProxyUser(r.Context(), db.CreateProxyUserParams{
			Name:              payload.Name,
			DisplayName:       payload.DisplayName,
			GlobalQuotaBytes:  quota,
			TrafficMultiplier: multiplier,
			ExpireAt:          payload.ExpireAt,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:                user.ID,
			Name:              user.Name,
			DisplayName:       user.DisplayName,
			Status:            user.Status,
			GlobalQuotaBytes:  user.GlobalQuotaBytes,
			TrafficMultiplier: user.TrafficMultiplier,
			ExpireAt:          nullString(user.ExpireAt),
			ProxyCount:        0,
		})
	}
}

func adminDeleteUserHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := store.DisableProxyUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminUser{
			ID:                user.ID,
			Name:              user.Name,
			DisplayName:       user.DisplayName,
			Status:            user.Status,
			GlobalQuotaBytes:  user.GlobalQuotaBytes,
			TrafficMultiplier: user.TrafficMultiplier,
			ExpireAt:          nullString(user.ExpireAt),
		})
	}
}

func adminUserProxiesHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accesses, err := store.ListProxyAccessesByUser(r.Context(), chi.URLParam(r, "user"))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyAccesses(accesses))
	}
}

func adminIssueUserProxyAccessHandler(store *db.DB) http.HandlerFunc {
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
		access, err := store.IssueVLESSRealityAccess(r.Context(), db.IssueAccessParams{
			UserName:  userName,
			NodeName:  payload.NodeName,
			ProxyName: payload.ProxyName,
		})
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyAccesses([]db.ProxyAccess{access})[0])
	}
}

func adminDeleteUserProxyAccessHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		access, err := store.RevokeProxyAccess(
			r.Context(),
			chi.URLParam(r, "user"),
			chi.URLParam(r, "node"),
			chi.URLParam(r, "proxy"),
		)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, adminProxyAccesses([]db.ProxyAccess{access})[0])
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
		userName := chi.URLParam(r, "user")
		accesses, err := store.ListProxyAccessesByUser(r.Context(), userName)
		if err != nil {
			writeAdminError(w, err)
			return
		}
		seen := make(map[string]bool)
		out := make([]render.NodeInfo, 0)
		for _, access := range accesses {
			if seen[access.NodeName] {
				continue
			}
			seen[access.NodeName] = true
			info, err := render.NodeInfoForUser(r.Context(), store, userName, access.NodeName)
			if err != nil {
				continue
			}
			out = append(out, info)
		}
		writeJSON(w, map[string]any{
			"user":  userName,
			"nodes": out,
		})
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
		nodeName := strings.TrimSpace(r.URL.Query().Get("node"))
		logs, err := store.ListRecentSystemLogs(r.Context(), nodeName, queryLimit(r, 100))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		writeJSON(w, map[string]any{
			"logs": adminSystemLogs(logs),
			"note": "",
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
		if node.Status != "active" {
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
	statuses, err := store.ListNodeConfigStatuses(r.Context())
	if err != nil {
		return nil, err
	}
	statusByNode := make(map[string]db.NodeConfigStatus, len(statuses))
	for _, status := range statuses {
		statusByNode[status.NodeName] = status
	}
	out := make([]adminNode, 0, len(nodes))
	for _, node := range nodes {
		item := adminNode{
			ID:             node.ID,
			Name:           node.Name,
			PublicHost:     node.PublicHost,
			APIBaseURL:     node.APIBaseURL,
			Status:         node.Status,
			SingBoxVersion: node.SingBoxVersion,
			LastSeenAt:     nullString(node.LastSeenAt),
		}
		if status, ok := statusByNode[node.Name]; ok {
			statusItem := adminNodeFromStatus(status)
			item.TargetVersion = statusItem.TargetVersion
			item.CurrentVersion = statusItem.CurrentVersion
			item.ApplyStatus = statusItem.ApplyStatus
			item.ApplyError = statusItem.ApplyError
			item.LatestHeartbeat = statusItem.LatestHeartbeat
			item.AgentVersion = statusItem.AgentVersion
			item.SingBoxVersion = statusItem.SingBoxVersion
		}
		out = append(out, item)
	}
	return out, nil
}

func adminNodeFromNode(node db.Node) adminNode {
	return adminNode{
		ID:             node.ID,
		Name:           node.Name,
		PublicHost:     node.PublicHost,
		APIBaseURL:     node.APIBaseURL,
		Status:         node.Status,
		SingBoxVersion: node.SingBoxVersion,
		LastSeenAt:     nullString(node.LastSeenAt),
	}
}

func listAdminUsers(r *http.Request, store *db.DB) ([]adminUser, error) {
	users, err := store.ListProxyUsersWithProxyCounts(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]adminUser, 0, len(users))
	for _, user := range users {
		out = append(out, adminUser{
			ID:                user.ID,
			Name:              user.Name,
			DisplayName:       user.DisplayName,
			Status:            user.Status,
			GlobalQuotaBytes:  user.GlobalQuotaBytes,
			TrafficMultiplier: user.TrafficMultiplier,
			ExpireAt:          nullString(user.ExpireAt),
			ProxyCount:        int(user.ProxyCount),
		})
	}
	return out, nil
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

func adminProxyAccesses(accesses []db.ProxyAccess) []adminProxyAccess {
	out := make([]adminProxyAccess, 0, len(accesses))
	for _, access := range accesses {
		var multiplier *float64
		if access.TrafficMultiplier.Valid {
			value := access.TrafficMultiplier.Float64
			multiplier = &value
		}
		out = append(out, adminProxyAccess{
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
		SettingsJSON:      proxy.SettingsJSON,
		InboundRulesJSON:  proxy.InboundRulesJSON,
		OutboundRulesJSON: proxy.OutboundRulesJSON,
		RouteRulesJSON:    proxy.RouteRulesJSON,
		CreatedAt:         proxy.CreatedAt,
		UpdatedAt:         proxy.UpdatedAt,
	}
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
	if _, ok := settings["short_id"]; !ok {
		settings["short_id"] = ""
	}
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
	http.Error(w, err.Error(), http.StatusUnprocessableEntity)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
