package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/haoxin/boxfleet/internal/model"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/install"
	"github.com/haoxin/boxfleet/internal/server/render"
	"github.com/haoxin/boxfleet/internal/server/webui"
)

type Options struct {
	DB                 *db.DB
	ArtifactDir        string
	AdminToken         string
	AdminPathToken     string
	AllowInsecureAdmin bool
	Version            string
	Repo               string
	SingBoxVersion     string
}

func NewRouter(options Options) http.Handler {
	router := chi.NewRouter()
	operationNotifier := newNodeOperationNotifier()
	updateCatalog := newUpdateCatalog(options)
	updateCampaigns := newUpdateCampaignController(options.DB, operationNotifier)
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})
	router.Get("/install.sh", installScriptHandler(options))
	router.Get("/sub/{token}/mihomo.yaml", subscriptionMihomoProfileHandler(options.DB))
	router.Get("/sub/{token}", subscriptionProviderHandler(options.DB))
	router.Get("/api/node/config", nodeConfigHandler(options.DB))
	router.Post("/api/node/apply-result", nodeApplyResultHandler(options.DB))
	router.Post("/api/node/heartbeat", nodeHeartbeatHandler(options.DB))
	router.Post("/api/node/traffic", nodeTrafficHandler(options.DB))
	router.Post("/api/node/logs", nodeLogsHandler(options.DB))
	router.Post("/api/node/system-logs", nodeSystemLogsHandler(options.DB))
	router.Post("/api/node/operations/claim", nodeOperationClaimHandler(options.DB, operationNotifier))
	router.Post("/api/node/operations/{operation}/lease", nodeOperationLeaseHandler(options.DB))
	router.Post("/api/node/operations/{operation}/events", nodeOperationEventHandler(options.DB, updateCampaigns))
	adminPrefix := adminRoutePrefix(options.AdminPathToken)
	router.Route(adminPrefix+"/api/admin", func(r chi.Router) {
		r.Use(adminAuthMiddleware(options.AdminToken, options.AllowInsecureAdmin))
		r.Get("/overview", adminOverviewHandler(options.DB, options))
		r.Get("/release", adminReleaseHandler(options, updateCatalog))
		r.Get("/config/changes", adminConfigChangesHandler(options.DB))
		r.Post("/config/publish", adminPublishChangedConfigsHandler(options.DB))
		r.Get("/proxies", adminProxiesHandler(options.DB))
		r.Get("/nodes", adminNodesHandler(options.DB))
		r.Post("/nodes", adminCreateNodeHandler(options.DB))
		r.Post("/nodes/bootstrap", adminCreateNodeBootstrapHandler(options.DB))
		r.Get("/nodes/{node}", adminNodeHandler(options.DB))
		r.Patch("/nodes/{node}", adminUpdateNodeHandler(options.DB))
		r.Delete("/nodes/{node}", adminDeleteNodeHandler(options.DB))
		r.Post("/nodes/{node}/restore", adminRestoreNodeHandler(options.DB))
		r.Post("/nodes/{node}/reenroll", adminReenrollNodeHandler(options.DB))
		r.Get("/nodes/{node}/status", adminNodeStatusHandler(options.DB))
		r.Post("/nodes/{node}/operations", adminCreateNodeOperationHandler(options.DB, operationNotifier))
		r.Post("/nodes/{node}/updates", adminCreateNodeUpdateHandler(options.DB, operationNotifier, updateCatalog))
		r.Post("/node-updates/bulk", adminCreateUpdateCampaignHandler(options.DB, updateCatalog, updateCampaigns))
		r.Get("/node-update-campaigns/current", adminCurrentUpdateCampaignHandler(options.DB, updateCampaigns))
		r.Get("/node-update-campaigns/{campaign}", adminUpdateCampaignHandler(updateCampaigns))
		r.Post("/node-update-campaigns/{campaign}/resume", adminResumeUpdateCampaignHandler(options.DB, updateCampaigns))
		r.Post("/node-update-campaigns/{campaign}/cancel", adminCancelUpdateCampaignHandler(options.DB, updateCampaigns))
		r.Get("/nodes/{node}/operations", adminListNodeOperationsHandler(options.DB))
		r.Get("/nodes/{node}/operations/current", adminCurrentNodeOperationHandler(options.DB))
		r.Get("/nodes/{node}/operations/{operation}", adminNodeOperationDetailHandler(options.DB))
		r.Post("/nodes/{node}/operations/{operation}/cancel", adminCancelNodeOperationHandler(options.DB, operationNotifier))
		r.Get("/nodes/{node}/proxies", adminNodeProxiesHandler(options.DB))
		r.Post("/nodes/{node}/proxies", adminCreateProxyHandler(options.DB))
		r.Patch("/nodes/{node}/proxies/{proxy}", adminUpdateProxyHandler(options.DB))
		r.Delete("/nodes/{node}/proxies/{proxy}", adminDeleteProxyHandler(options.DB))
		r.Post("/nodes/{node}/proxies/{proxy}/restore", adminRestoreProxyHandler(options.DB))
		r.Get("/nodes/{node}/config/render", adminRenderConfigHandler(options.DB))
		r.Get("/users", adminUsersHandler(options.DB))
		r.Post("/users", adminCreateUserHandler(options.DB))
		r.Patch("/users/{user}", adminUpdateUserHandler(options.DB))
		r.Delete("/users/{user}", adminDeleteUserHandler(options.DB))
		r.Post("/users/{user}/restore", adminRestoreUserHandler(options.DB))
		r.Get("/users/{user}/proxies", adminUserProxiesHandler(options.DB))
		r.Post("/users/{user}/proxies", adminIssueUserProxyAccessHandler(options.DB))
		r.Delete("/users/{user}/proxies/{node}/{proxy}", adminDeleteUserProxyAccessHandler(options.DB))
		r.Get("/users/{user}/connection-info", adminUserConnectionInfoHandler(options.DB))
		r.Get("/users/{user}/node-info", adminUserNodeInfoHandler(options.DB))
		r.Get("/users/{user}/proxy-provider", adminUserProxyProviderHandler(options.DB))
		r.Put("/users/{user}/mihomo-profile", adminAssignUserMihomoProfileHandler(options.DB))
		r.Get("/users/{user}/subscription", adminUserSubscriptionHandler(options.DB))
		r.Post("/users/{user}/subscription", adminIssueUserSubscriptionHandler(options.DB))
		r.Post("/users/{user}/subscription/rotate", adminRotateUserSubscriptionHandler(options.DB))
		r.Delete("/users/{user}/subscription", adminRevokeUserSubscriptionHandler(options.DB))
		r.Get("/mihomo/profiles", adminListMihomoProfilesHandler(options.DB))
		r.Post("/mihomo/profiles", adminCreateMihomoProfileHandler(options.DB))
		r.Get("/mihomo/profiles/{profile}", adminMihomoProfileHandler(options.DB))
		r.Patch("/mihomo/profiles/{profile}", adminUpdateMihomoProfileHandler(options.DB))
		r.Post("/mihomo/profiles/{profile}/preview", adminPreviewMihomoProfileHandler(options.DB))
		r.Post("/mihomo/profiles/{profile}/publish", adminPublishMihomoProfileHandler(options.DB))
		r.Get("/mihomo/profiles/{profile}/revisions", adminListMihomoProfileRevisionsHandler(options.DB))
		r.Post("/mihomo/profiles/{profile}/rollback", adminRollbackMihomoProfileHandler(options.DB))
		r.Get("/mihomo/profiles/{profile}/subscription", adminMihomoProfileSubscriptionHandler(options.DB))
		r.Post("/mihomo/profiles/{profile}/subscription", adminIssueMihomoProfileSubscriptionHandler(options.DB))
		r.Post("/mihomo/profiles/{profile}/subscription/rotate", adminRotateMihomoProfileSubscriptionHandler(options.DB))
		r.Delete("/mihomo/profiles/{profile}/subscription", adminRevokeMihomoProfileSubscriptionHandler(options.DB))
		r.Get("/mihomo/rewrite-templates", adminListMihomoRewriteTemplatesHandler(options.DB))
		r.Post("/mihomo/rewrite-templates", adminCreateMihomoRewriteTemplateHandler(options.DB))
		r.Patch("/mihomo/rewrite-templates/{template}", adminUpdateMihomoRewriteTemplateHandler(options.DB))
		r.Get("/users/{user}/traffic", adminUserTrafficHandler(options.DB))
		r.Get("/traffic/users", adminTrafficUsersHandler(options.DB))
		r.Get("/settings", adminSettingsHandler(options.DB))
		r.Patch("/settings", adminUpdateSettingsHandler(options.DB))
		r.Get("/network-events", adminNetworkEventsHandler(options.DB))
		r.Get("/nodes/{node}/network-events", adminNodeNetworkEventsHandler(options.DB))
		r.Get("/users/{user}/network-events", adminUserNetworkEventsHandler(options.DB))
		r.Get("/nodes/{node}/raw-network-logs", adminNodeRawNetworkLogsHandler(options.DB))
		r.Get("/system-logs", adminSystemLogsHandler(options.DB))
		r.Post("/nodes/{node}/config/publish", adminPublishConfigHandler(options.DB))
	})
	if options.ArtifactDir != "" {
		router.Handle("/artifacts/*", http.StripPrefix("/artifacts/", http.FileServer(http.Dir(options.ArtifactDir))))
	}
	adminMount := adminPrefix + "/admin"
	router.Handle(adminMount, webui.Handler(adminMount))
	router.Handle(adminMount+"/*", webui.Handler(adminMount))
	return router
}

func adminRoutePrefix(pathToken string) string {
	pathToken = strings.Trim(strings.TrimSpace(pathToken), "/")
	if pathToken == "" {
		return ""
	}
	return "/" + pathToken
}

func installScriptHandler(options Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		script, err := install.Script(install.ScriptData{
			Repo:            options.Repo,
			BoxFleetVersion: options.Version,
			SingBoxVersion:  options.SingBoxVersion,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(script)
	}
}

func nodeConfigHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		// An administratively disabled node keeps a valid token (status set via
		// PATCH, not the token-revoking decommission path), so its agent daemon
		// still polls here. Signal it to stop serving (systemctl stop sing-box)
		// instead of handing back config; re-enabling resumes normal config.
		if node, err := store.GetNode(r.Context(), nodeName); err == nil && node.Status == "disabled" {
			// Body is a valid no-inbound config so legacy agents that ignore the
			// header still stop serving on apply; new agents act on the header.
			body, err := render.RenderDisabledConfig()
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("X-BoxFleet-Node-State", "disabled")
			w.Header().Set("X-BoxFleet-Config-Mode", "disabled")
			w.Header().Set("X-BoxFleet-Config-SHA256", db.SHA256Hex(body))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			_, _ = w.Write([]byte("\n"))
			return
		}
		version, err := store.GetTargetConfig(r.Context(), nodeName)
		var config []byte
		if err != nil {
			config, err = render.RenderNodeConfig(r.Context(), store, nodeName)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnprocessableEntity)
				return
			}
			w.Header().Set("X-BoxFleet-Config-Mode", "rendered")
			w.Header().Set("X-BoxFleet-Config-SHA256", db.SHA256Hex(config))
		} else {
			config = []byte(version.ConfigJson)
			w.Header().Set("X-BoxFleet-Config-Mode", "published")
			w.Header().Set("X-BoxFleet-Config-Version-ID", version.ID)
			w.Header().Set("X-BoxFleet-Config-Version", fmt.Sprintf("%d", version.Version))
			w.Header().Set("X-BoxFleet-Config-SHA256", version.ConfigHash)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(config)
		_, _ = w.Write([]byte("\n"))
	}
}

func nodeApplyResultHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var result db.ApplyResult
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		result.NodeName = nodeName
		if err := store.RecordApplyResult(r.Context(), result); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}

func nodeHeartbeatHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var heartbeat db.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		heartbeat.NodeName = nodeName
		if err := store.RecordHeartbeat(r.Context(), heartbeat); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}

func nodeTrafficHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var report db.TrafficReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		report.NodeName = nodeName
		if err := store.RecordTrafficReport(r.Context(), report); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}

func nodeLogsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var report db.LogEventReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		report.NodeName = nodeName
		if err := store.RecordLogEvents(r.Context(), report); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}

func nodeSystemLogsHandler(store *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeName, ok := authenticateNode(w, r, store)
		if !ok {
			return
		}
		var report db.SystemLogReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		report.NodeName = nodeName
		if err := store.RecordSystemLogs(r.Context(), report); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	}
}

func authenticateNode(w http.ResponseWriter, r *http.Request, store *db.DB) (string, bool) {
	nodeName := strings.TrimSpace(r.Header.Get("X-BoxFleet-Node"))
	if nodeName == "" {
		nodeName = strings.TrimSpace(r.URL.Query().Get("node"))
	}
	if nodeName == "" {
		http.Error(w, "missing node name", http.StatusBadRequest)
		return "", false
	}
	rawToken := bearerToken(r.Header.Get("Authorization"))
	if rawToken == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return "", false
	}
	canonicalName, ok, err := store.AuthenticateNodeToken(r.Context(), nodeName, rawToken)
	if err != nil {
		http.Error(w, "token verification failed", http.StatusInternalServerError)
		return "", false
	}
	if !ok {
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		return "", false
	}
	w.Header().Set(model.CanonicalNodeNameHeader, canonicalName)
	return canonicalName, true
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
