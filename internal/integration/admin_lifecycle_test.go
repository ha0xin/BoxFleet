package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haoxin/boxfleet/internal/server/api"
	"github.com/haoxin/boxfleet/internal/server/db"
)

func TestAdminAndNodeLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openIntegrationDB(t)
	server := httptest.NewServer(api.NewRouter(api.Options{
		DB:                 store,
		AllowInsecureAdmin: true,
	}))
	t.Cleanup(server.Close)

	node := postJSON[adminNode](t, server.URL+"/api/admin/nodes", map[string]any{
		"name":        "edge-a",
		"public_host": "203.0.113.10",
	})
	if node.Name != "edge-a" || node.Status != "active" {
		t.Fatalf("node = %#v", node)
	}
	user := postJSON[adminUser](t, server.URL+"/api/admin/users", map[string]any{
		"name": "alice",
	})
	if user.Name != "alice" || user.Status != "active" {
		t.Fatalf("user = %#v", user)
	}
	proxy := postJSON[adminProxy](t, server.URL+"/api/admin/nodes/edge-a/proxies", map[string]any{
		"name":        "vless-39090",
		"protocol":    "vless_reality",
		"listen":      "0.0.0.0",
		"listen_port": 39090,
		"settings_json": `{
			"server_name": "www.amazon.com",
			"reality_private_key": "private-key",
			"reality_public_key": "public-key",
			"short_id": "01234567",
			"handshake_server": "www.amazon.com",
			"handshake_port": 443
		}`,
	})
	if proxy.Name != "vless-39090" || !proxy.Enabled {
		t.Fatalf("proxy = %#v", proxy)
	}
	access := postJSON[adminAccess](t, server.URL+"/api/admin/users/alice/proxies", map[string]any{
		"node_name":  "edge-a",
		"proxy_name": "vless-39090",
	})
	if access.AuthName != "vless-39090@alice" || !access.Enabled {
		t.Fatalf("access = %#v", access)
	}

	rendered := getText(t, server.URL+"/api/admin/nodes/edge-a/config/render", nil)
	if !strings.Contains(rendered, `"name": "vless-39090@alice"`) {
		t.Fatalf("rendered config missing auth user:\n%s", rendered)
	}
	published := postJSON[publishResult](t, server.URL+"/api/admin/nodes/edge-a/config/publish", nil)
	if published.Version != 1 || published.Hash == "" {
		t.Fatalf("published = %#v", published)
	}
	issued, err := store.IssueNodeToken(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	nodeConfig := getText(t, server.URL+"/api/node/config", map[string]string{
		"X-BoxFleet-Node": "edge-a",
		"Authorization":   "Bearer " + issued.Token,
	})
	if !strings.Contains(nodeConfig, `"tag": "vless-39090"`) {
		t.Fatalf("node config missing proxy:\n%s", nodeConfig)
	}
	postNodeJSON(t, server.URL+"/api/node/apply-result", issued.Token, db.ApplyResult{
		ConfigVersionID: published.ID,
		ConfigHash:      published.Hash,
		Status:          "applied",
	})
	postNodeJSON(t, server.URL+"/api/node/heartbeat", issued.Token, db.Heartbeat{
		AgentVersion:   "it-agent",
		SingBoxVersion: "it-sing-box",
	})
	postNodeJSON(t, server.URL+"/api/node/traffic", issued.Token, db.TrafficReport{
		Sequence:    1,
		AgentBootID: "boot-a",
		Deltas: []db.TrafficDelta{{
			AuthName:      "vless-39090@alice",
			Direction:     "uplink",
			RawBytesDelta: 1234,
			CounterValue:  1234,
		}},
	})

	overview := getJSON[overviewResponse](t, server.URL+"/api/admin/overview", nil)
	if len(overview.Nodes) != 1 || overview.Nodes[0].ApplyStatus != "applied" || overview.Nodes[0].AgentVersion != "it-agent" {
		t.Fatalf("overview nodes = %#v", overview.Nodes)
	}
	if len(overview.Traffic) != 1 || overview.Traffic[0].RawBytes != 1234 {
		t.Fatalf("overview traffic = %#v", overview.Traffic)
	}

	revoked := deleteJSON[adminAccess](t, server.URL+"/api/admin/users/alice/proxies/edge-a/vless-39090")
	if revoked.Enabled {
		t.Fatalf("revoked access still enabled: %#v", revoked)
	}
	rendered = getText(t, server.URL+"/api/admin/nodes/edge-a/config/render", nil)
	if strings.Contains(rendered, "vless-39090@alice") {
		t.Fatalf("revoked access still rendered:\n%s", rendered)
	}
	reissued := postJSON[adminAccess](t, server.URL+"/api/admin/users/alice/proxies", map[string]any{
		"node_name":  "edge-a",
		"proxy_name": "vless-39090",
	})
	if reissued.ID != access.ID || !reissued.Enabled {
		t.Fatalf("reissued access = %#v, original = %#v", reissued, access)
	}

	disabledProxy := deleteJSON[adminProxy](t, server.URL+"/api/admin/nodes/edge-a/proxies/vless-39090")
	if disabledProxy.Enabled {
		t.Fatalf("disabled proxy still enabled: %#v", disabledProxy)
	}
	rendered = getText(t, server.URL+"/api/admin/nodes/edge-a/config/render", nil)
	if strings.Contains(rendered, "vless-39090@alice") {
		t.Fatalf("disabled proxy still rendered:\n%s", rendered)
	}
	disabledUser := deleteJSON[adminUser](t, server.URL+"/api/admin/users/alice")
	if disabledUser.Status != "disabled" {
		t.Fatalf("disabled user = %#v", disabledUser)
	}
	disabledNode := deleteJSON[adminNode](t, server.URL+"/api/admin/nodes/edge-a")
	if disabledNode.Status != "disabled" {
		t.Fatalf("disabled node = %#v", disabledNode)
	}
	if _, err := store.IssueNodeToken(ctx, "edge-a"); err == nil {
		t.Fatal("disabled node accepted a new token")
	}
	resp := rawRequest(t, http.MethodGet, server.URL+"/api/node/config", nil, map[string]string{
		"X-BoxFleet-Node": "edge-a",
		"Authorization":   "Bearer " + issued.Token,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("disabled node token status = %d, body = %s", resp.StatusCode, body)
	}
}

type adminNode struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	ApplyStatus  string `json:"apply_status"`
	AgentVersion string `json:"agent_version"`
}

type adminUser struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type adminProxy struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type adminAccess struct {
	ID       string `json:"id"`
	AuthName string `json:"auth_name"`
	Enabled  bool   `json:"enabled"`
}

type publishResult struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Hash    string `json:"hash"`
}

type overviewResponse struct {
	Nodes   []adminNode `json:"nodes"`
	Traffic []struct {
		UserName string `json:"user_name"`
		RawBytes int64  `json:"raw_bytes"`
	} `json:"traffic"`
}

func openIntegrationDB(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.OpenSQLite(filepath.Join(t.TempDir(), "boxfleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func getJSON[T any](t *testing.T, url string, headers map[string]string) T {
	t.Helper()
	return decodeJSON[T](t, rawRequest(t, http.MethodGet, url, nil, headers))
}

func postJSON[T any](t *testing.T, url string, body any) T {
	t.Helper()
	return decodeJSON[T](t, rawRequest(t, http.MethodPost, url, body, nil))
}

func deleteJSON[T any](t *testing.T, url string) T {
	t.Helper()
	return decodeJSON[T](t, rawRequest(t, http.MethodDelete, url, nil, nil))
}

func getText(t *testing.T, url string, headers map[string]string) string {
	t.Helper()
	resp := rawRequest(t, http.MethodGet, url, nil, headers)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, body = %s", url, resp.StatusCode, raw)
	}
	return string(raw)
}

func postNodeJSON(t *testing.T, url, token string, body any) {
	t.Helper()
	resp := rawRequest(t, http.MethodPost, url, body, map[string]string{
		"X-BoxFleet-Node": "edge-a",
		"Authorization":   "Bearer " + token,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s status = %d, body = %s", url, resp.StatusCode, raw)
	}
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", resp.Request.Method, resp.Request.URL, resp.StatusCode, raw)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func rawRequest(t *testing.T, method, url string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		requestBody = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
