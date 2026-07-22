package bf

import (
	"context"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/spf13/cobra"
)

func accessCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "access", Short: "Manage per-proxy access"}
	cmd.AddCommand(accessIssueCommand())
	cmd.AddCommand(accessShowCommand())
	cmd.AddCommand(accessRevokeCommand())
	cmd.AddCommand(accessRevokeCommandWithName("delete"))
	return cmd
}

func accessIssueCommand() *cobra.Command {
	var nodeName, proxyName string
	cmd := &cobra.Command{
		Use:   "issue <user>",
		Short: "Issue VLESS Reality access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				access, err := store.IssueVLESSRealityAccess(ctx, db.IssueAccessParams{
					UserName:  args[0],
					NodeName:  nodeName,
					ProxyName: proxyName,
				})
				if err != nil {
					return err
				}
				printAccess(cmd, access)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&proxyName, "proxy", "", "proxy name")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("proxy")
	return cmd
}

func accessRevokeCommand() *cobra.Command {
	return accessRevokeCommandWithName("revoke")
}

func accessRevokeCommandWithName(name string) *cobra.Command {
	var nodeName, proxyName string
	cmd := &cobra.Command{
		Use:   name + " <user>",
		Short: titleASCII(name) + " per-proxy access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				var access db.ProxyAccess
				var err error
				if name == "delete" {
					access, err = store.SoftDeleteProxyAccess(ctx, args[0], nodeName, proxyName)
				} else {
					access, err = store.RevokeProxyAccess(ctx, args[0], nodeName, proxyName)
				}
				if err != nil {
					return err
				}
				printAccess(cmd, access)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&proxyName, "proxy", "", "proxy name")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("proxy")
	return cmd
}

func accessShowCommand() *cobra.Command {
	var nodeName, proxyName string
	cmd := &cobra.Command{
		Use:   "show <user>",
		Short: "Show per-proxy access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				access, err := store.GetProxyAccess(ctx, args[0], nodeName, proxyName)
				if err != nil {
					return err
				}
				printAccess(cmd, access)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&proxyName, "proxy", "", "proxy name")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("proxy")
	return cmd
}
