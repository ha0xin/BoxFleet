package bf

import (
	"encoding/json"
	"fmt"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/units"
	"github.com/spf13/cobra"
)

func printProxyUser(cmd *cobra.Command, user db.ProxyUser) {
	expire := "none"
	if user.ExpireAt.Valid {
		expire = user.ExpireAt.String
	}
	fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", user.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", user.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "display_name: %s\n", user.DisplayName)
	fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", user.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "global_quota: %s\n", units.FormatBytes(user.GlobalQuotaBytes))
	fmt.Fprintf(cmd.OutOrStdout(), "traffic_multiplier: %.3g\n", user.TrafficMultiplier)
	fmt.Fprintf(cmd.OutOrStdout(), "expire_at: %s\n", expire)
}

func printNode(cmd *cobra.Command, node db.Node) {
	lastSeen := "never"
	if node.LastSeenAt.Valid {
		lastSeen = node.LastSeenAt.String
	}
	fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", node.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", node.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "public_host: %s\n", node.PublicHost)
	fmt.Fprintf(cmd.OutOrStdout(), "api_base_url: %s\n", node.APIBaseURL)
	fmt.Fprintf(cmd.OutOrStdout(), "status: %s\n", node.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "sing_box_version: %s\n", node.SingBoxVersion)
	fmt.Fprintf(cmd.OutOrStdout(), "last_seen_at: %s\n", lastSeen)
}

func printBinding(cmd *cobra.Command, binding db.UserNodeBinding) {
	multiplier := "inherit"
	if binding.TrafficMultiplier.Valid {
		multiplier = fmt.Sprintf("%.3g", binding.TrafficMultiplier.Float64)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", binding.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "user: %s\n", binding.ProxyUserName)
	fmt.Fprintf(cmd.OutOrStdout(), "node: %s\n", binding.NodeName)
	fmt.Fprintf(cmd.OutOrStdout(), "enabled: %t\n", binding.Enabled)
	fmt.Fprintf(cmd.OutOrStdout(), "node_quota: %s\n", units.FormatBytes(binding.NodeQuotaBytes))
	fmt.Fprintf(cmd.OutOrStdout(), "traffic_multiplier: %s\n", multiplier)
	fmt.Fprintf(cmd.OutOrStdout(), "disabled_reason: %s\n", binding.DisabledReason)
}

func printProxy(cmd *cobra.Command, proxy db.Proxy) {
	fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", proxy.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "node: %s\n", proxy.NodeName)
	fmt.Fprintf(cmd.OutOrStdout(), "name: %s\n", proxy.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "protocol: %s\n", proxy.Protocol)
	fmt.Fprintf(cmd.OutOrStdout(), "listen: %s\n", proxy.Listen)
	fmt.Fprintf(cmd.OutOrStdout(), "listen_port: %d\n", proxy.ListenPort)
	fmt.Fprintf(cmd.OutOrStdout(), "transport: %s\n", proxy.Transport)
	fmt.Fprintf(cmd.OutOrStdout(), "enabled: %t\n", proxy.Enabled)
	fmt.Fprintf(cmd.OutOrStdout(), "traffic_multiplier: %.3g\n", proxy.TrafficMultiplier)
	fmt.Fprintln(cmd.OutOrStdout(), "settings_json:")
	fmt.Fprintln(cmd.OutOrStdout(), indentJSON(proxy.SettingsJSON))
}

func printAccess(cmd *cobra.Command, access db.ProxyAccess) {
	fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", access.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "user: %s\n", access.ProxyUserName)
	fmt.Fprintf(cmd.OutOrStdout(), "node: %s\n", access.NodeName)
	fmt.Fprintf(cmd.OutOrStdout(), "proxy: %s\n", access.ProxyName)
	fmt.Fprintf(cmd.OutOrStdout(), "auth_name: %s\n", access.AuthName)
	fmt.Fprintf(cmd.OutOrStdout(), "enabled: %t\n", access.Enabled)
	fmt.Fprintln(cmd.OutOrStdout(), "credential_json:")
	fmt.Fprintln(cmd.OutOrStdout(), indentJSON(access.CredentialJSON))
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func indentJSON(raw string) string {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return raw
	}
	return string(encoded)
}
