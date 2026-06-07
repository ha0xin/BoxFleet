package bf

import (
	"context"
	"fmt"

	"github.com/haoxin/boxfleet/internal/server/db"
	configrender "github.com/haoxin/boxfleet/internal/server/render"
	"github.com/spf13/cobra"
)

func configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Render sing-box configs"}
	cmd.AddCommand(configRenderCommand())
	cmd.AddCommand(configRenderClientCommand())
	cmd.AddCommand(configPublishCommand())
	cmd.AddCommand(configStatusCommand())
	return cmd
}

func configRenderCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render a node sing-box server config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				raw, err := configrender.RenderNodeConfig(ctx, store, nodeName)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func configRenderClientCommand() *cobra.Command {
	var nodeName, proxyName, listen, fingerprint string
	var port int
	cmd := &cobra.Command{
		Use:   "render-client <user>",
		Short: "Render a local sing-box client config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				raw, err := configrender.RenderClientConfig(ctx, store, configrender.ClientConfigParams{
					UserName:        args[0],
					NodeName:        nodeName,
					ProxyName:       proxyName,
					MixedListen:     listen,
					MixedListenPort: port,
					UTLSFingerprint: fingerprint,
				})
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&proxyName, "proxy", "", "proxy name")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1", "local mixed inbound listen address")
	cmd.Flags().IntVar(&port, "port", configrender.DefaultMixedListenPort, "local mixed inbound listen port")
	cmd.Flags().StringVar(&fingerprint, "utls", "chrome", "uTLS fingerprint")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("proxy")
	return cmd
}

func configPublishCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish the current rendered config for a node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				raw, err := configrender.RenderNodeConfig(ctx, store, nodeName)
				if err != nil {
					return err
				}
				published, err := store.PublishConfig(ctx, nodeName, raw)
				if err != nil {
					return err
				}
				action := "reused"
				if published.Created {
					action = "created"
				}
				okText.Fprintf(cmd.OutOrStdout(), "published config %d for node %s (%s)\n", published.ConfigVersion.Version, nodeName, action)
				fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", published.ConfigVersion.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "hash: %s\n", published.ConfigVersion.ConfigHash)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func configStatusCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show node config publish/apply status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				status, err := store.GetNodeConfigStatus(ctx, nodeName)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "node: %s\n", status.NodeName)
				fmt.Fprintf(cmd.OutOrStdout(), "target_version: %s\n", formatNullInt(status.TargetVersion))
				fmt.Fprintf(cmd.OutOrStdout(), "target_hash: %s\n", formatNullString(status.TargetConfigHash))
				fmt.Fprintf(cmd.OutOrStdout(), "current_version: %s\n", formatNullInt(status.CurrentVersion))
				fmt.Fprintf(cmd.OutOrStdout(), "current_hash: %s\n", formatNullString(status.CurrentConfigHash))
				fmt.Fprintf(cmd.OutOrStdout(), "apply_status: %s\n", status.LastApplyStatus)
				fmt.Fprintf(cmd.OutOrStdout(), "apply_error: %s\n", status.LastApplyError)
				fmt.Fprintf(cmd.OutOrStdout(), "updated_at: %s\n", formatNullString(status.UpdatedAt))
				fmt.Fprintf(cmd.OutOrStdout(), "latest_heartbeat: %s\n", formatNullString(status.LatestHeartbeat))
				fmt.Fprintf(cmd.OutOrStdout(), "agent_version: %s\n", status.AgentVersion)
				fmt.Fprintf(cmd.OutOrStdout(), "sing_box_version: %s\n", status.SingBoxVersion)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}
