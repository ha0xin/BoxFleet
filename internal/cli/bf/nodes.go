package bf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haoxin/boxfleet/internal/agent"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func nodeCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "node", Short: "Manage nodes"}
	var host, apiBaseURL string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				node, err := store.CreateNode(ctx, args[0], host, apiBaseURL)
				if err != nil {
					return err
				}
				printNode(cmd, node)
				return nil
			})
		},
	}
	create.Flags().StringVar(&host, "host", "", "public host")
	create.Flags().StringVar(&apiBaseURL, "api-base-url", "", "node API base URL")
	_ = create.MarkFlagRequired("host")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				nodes, err := store.ListNodes(ctx)
				if err != nil {
					return err
				}
				rows := make([]table.Row, 0, len(nodes))
				for _, node := range nodes {
					lastSeen := "-"
					if node.LastSeenAt.Valid {
						lastSeen = node.LastSeenAt.String
					}
					rows = append(rows, table.Row{node.Name, node.Status, node.PublicHost, lastSeen})
				}
				renderTable(cmd, table.Row{"NAME", "STATUS", "HOST", "LAST_SEEN"}, rows)
				return nil
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				node, err := store.GetNode(ctx, args[0])
				if err != nil {
					return err
				}
				printNode(cmd, node)
				return nil
			})
		},
	})
	cmd.AddCommand(nodeStatusCommand("enable", "active"))
	cmd.AddCommand(nodeStatusCommand("disable", "disabled"))
	cmd.AddCommand(nodeRenameCommand())
	cmd.AddCommand(nodeDeleteCommand())
	cmd.AddCommand(nodeTokenCommand())
	cmd.AddCommand(nodeBootstrapCommand())
	return cmd
}

func nodeRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <current-name> <new-name>",
		Short: "Rename a node while preserving its old name as an alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				node, err := store.RenameNode(ctx, args[0], args[1])
				if err != nil {
					return err
				}
				printNode(cmd, node)
				return nil
			})
		},
	}
}

func nodeTokenCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage node tokens"}
	cmd.AddCommand(&cobra.Command{
		Use:   "issue <node>",
		Short: "Issue a node agent token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				issued, err := store.IssueNodeToken(ctx, args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "node: %s\n", issued.NodeName)
				fmt.Fprintf(cmd.OutOrStdout(), "token: %s\n", issued.Token)
				return nil
			})
		},
	})
	return cmd
}

func nodeBootstrapCommand() *cobra.Command {
	var sshTarget, serverURL, agentBin, singBoxURL, installDir, configPath, remoteTmp, pollInterval string
	cmd := &cobra.Command{
		Use:   "bootstrap <node>",
		Short: "Install and start a BoxFleet-managed node over SSH",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeName := args[0]
			if sshTarget == "" {
				sshTarget = nodeName
			}
			if serverURL == "" {
				return fmt.Errorf("--server-url is required")
			}
			if agentBin == "" {
				return fmt.Errorf("--agent-bin is required")
			}
			if singBoxURL == "" {
				return fmt.Errorf("--sing-box-url is required")
			}
			if _, err := os.Stat(agentBin); err != nil {
				return fmt.Errorf("agent binary: %w", err)
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				issued, err := store.IssueNodeToken(ctx, nodeName)
				if err != nil {
					return err
				}
				cfg := agent.Config{
					NodeName:        issued.NodeName,
					Token:           issued.Token,
					ServerURL:       serverURL,
					SingBoxURL:      singBoxURL,
					InstallDir:      installDir,
					AgentConfigPath: configPath,
					PollInterval:    pollInterval,
				}
				cfg.ApplyDefaults()
				tmpDir, err := os.MkdirTemp("", "boxfleet-bootstrap-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)
				localConfig := filepath.Join(tmpDir, "agent.json")
				if err := agent.WriteConfig(localConfig, cfg); err != nil {
					return err
				}
				if err := runCommand(cmd, "ssh", sshTarget, "mkdir -p "+shellQuote(remoteTmp)); err != nil {
					return err
				}
				if err := runCommand(cmd, "scp", "-q", agentBin, sshTarget+":"+remoteTmp+"/boxfleet-agent"); err != nil {
					return err
				}
				if err := runCommand(cmd, "scp", "-q", localConfig, sshTarget+":"+remoteTmp+"/agent.json"); err != nil {
					return err
				}
				remoteInstall := strings.Join([]string{
					"sudo mkdir -p " + shellQuote(filepath.Dir(cfg.AgentPath)) + " " + shellQuote(filepath.Dir(cfg.AgentConfigPath)),
					"sudo install -m 0755 " + shellQuote(remoteTmp+"/boxfleet-agent") + " " + shellQuote(cfg.AgentPath),
					"sudo install -m 0600 " + shellQuote(remoteTmp+"/agent.json") + " " + shellQuote(cfg.AgentConfigPath),
					"sudo " + shellQuote(cfg.AgentPath) + " install --config " + shellQuote(cfg.AgentConfigPath),
				}, " && ")
				if err := runCommand(cmd, "ssh", sshTarget, remoteInstall); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "bootstrapped node %s via %s\n", issued.NodeName, sshTarget)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&sshTarget, "ssh", "", "SSH target; defaults to node name")
	cmd.Flags().StringVar(&serverURL, "server-url", "", "BoxFleet server URL reachable from the node")
	cmd.Flags().StringVar(&agentBin, "agent-bin", "dist/deploy/boxfleet-agent", "local boxfleet-agent binary to upload")
	cmd.Flags().StringVar(&singBoxURL, "sing-box-url", "", "URL for the node agent to download sing-box")
	cmd.Flags().StringVar(&installDir, "install-dir", agent.DefaultInstallDir, "remote install directory")
	cmd.Flags().StringVar(&configPath, "config-path", agent.DefaultConfigPath, "remote agent config path")
	cmd.Flags().StringVar(&remoteTmp, "remote-tmp", "/tmp/boxfleet-bootstrap", "remote temporary upload directory")
	cmd.Flags().StringVar(&pollInterval, "poll-interval", agent.DefaultPollInterval.String(), "agent config poll interval")
	return cmd
}

func nodeStatusCommand(name, status string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <name>",
		Short: titleASCII(name) + " a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetNodeStatus(ctx, args[0], status); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "node %s: %s\n", args[0], status)
				return nil
			})
		},
	}
}

func nodeDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				node, err := store.DisableNode(ctx, args[0])
				if err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "node %s: %s\n", node.Name, node.Status)
				return nil
			})
		},
	}
}
