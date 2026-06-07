package bf

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestCredentialAndConfigCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "boxfleet.db")

	runBF(t, dbPath, "db", "init")
	runBF(t, dbPath, "user", "create", "alice")
	runBF(t, dbPath, "node", "create", "azus", "--host", "203.0.113.10")
	runBF(t, dbPath, "proxy", "create", "vless-reality",
		"--node", "azus",
		"--port", "39090",
		"--listen", "0.0.0.0",
		"--sni", "www.amazon.com",
		"--handshake-server", "www.amazon.com",
		"--handshake-port", "443",
	)
	runBF(t, dbPath, "bind", "user", "alice", "--node", "azus")
	accessOut := runBF(t, dbPath, "access", "issue", "alice", "--node", "azus", "--proxy", "vless-39090")
	if !strings.Contains(accessOut, "auth_name: vless-39090@alice") {
		t.Fatalf("access output missing auth name:\n%s", accessOut)
	}

	serverConfig := runBF(t, dbPath, "config", "render", "--node", "azus")
	var server map[string]any
	if err := json.Unmarshal([]byte(serverConfig), &server); err != nil {
		t.Fatalf("server config is not json: %v\n%s", err, serverConfig)
	}
	inbound := server["inbounds"].([]any)[0].(map[string]any)
	if inbound["type"] != "vless" {
		t.Fatalf("server inbound = %#v", inbound)
	}

	nodeInfo := runBF(t, dbPath, "user", "node-info", "alice", "--node", "azus", "--format", "json")
	var info map[string]any
	if err := json.Unmarshal([]byte(nodeInfo), &info); err != nil {
		t.Fatalf("node info is not json: %v\n%s", err, nodeInfo)
	}
	if info["user"] != "alice" {
		t.Fatalf("node info = %#v", info)
	}

	clientConfig := runBF(t, dbPath, "config", "render-client", "alice", "--node", "azus", "--proxy", "vless-39090")
	var client map[string]any
	if err := json.Unmarshal([]byte(clientConfig), &client); err != nil {
		t.Fatalf("client config is not json: %v\n%s", err, clientConfig)
	}
	outbound := client["outbounds"].([]any)[0].(map[string]any)
	if outbound["server"] != "203.0.113.10" || outbound["flow"] != "xtls-rprx-vision" {
		t.Fatalf("client outbound = %#v", outbound)
	}

	revokeOut := runBF(t, dbPath, "access", "revoke", "alice", "--node", "azus", "--proxy", "vless-39090")
	if !strings.Contains(revokeOut, "enabled: false") {
		t.Fatalf("access revoke output missing disabled state:\n%s", revokeOut)
	}
	serverConfig = runBF(t, dbPath, "config", "render", "--node", "azus")
	if strings.Contains(serverConfig, "vless-39090@alice") {
		t.Fatalf("revoked access still rendered:\n%s", serverConfig)
	}
	runBF(t, dbPath, "access", "issue", "alice", "--node", "azus", "--proxy", "vless-39090")
	runBF(t, dbPath, "proxy", "delete", "vless-39090", "--node", "azus")
	serverConfig = runBF(t, dbPath, "config", "render", "--node", "azus")
	if strings.Contains(serverConfig, "vless-39090@alice") {
		t.Fatalf("disabled proxy still rendered:\n%s", serverConfig)
	}
	runBF(t, dbPath, "user", "delete", "alice")
	runBF(t, dbPath, "node", "delete", "azus")
}

func runBF(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	viper.Reset()
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--db", dbPath}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bf %s failed: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}
