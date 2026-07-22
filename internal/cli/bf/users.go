package bf

import (
	"context"
	"fmt"

	"github.com/haoxin/boxfleet/internal/server/db"
	configrender "github.com/haoxin/boxfleet/internal/server/render"
	"github.com/haoxin/boxfleet/internal/units"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func userCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage proxy users"}

	var displayName, quota, expire string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a proxy user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			quotaBytes, err := units.ParseBytes(quota)
			if err != nil {
				return err
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				user, err := store.CreateProxyUser(ctx, db.CreateProxyUserParams{
					Name:             args[0],
					DisplayName:      displayName,
					GlobalQuotaBytes: quotaBytes,
					ExpireAt:         expire,
				})
				if err != nil {
					return err
				}
				printProxyUser(cmd, user)
				return nil
			})
		},
	}
	create.Flags().StringVar(&displayName, "display-name", "", "display name")
	create.Flags().StringVar(&quota, "quota", "0", "global quota, e.g. 50GiB or 0")
	create.Flags().StringVar(&expire, "expire", "", "expiration date, e.g. 2026-12-31")
	cmd.AddCommand(create)

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List proxy users",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				users, err := store.ListProxyUsers(ctx)
				if err != nil {
					return err
				}
				rows := make([]table.Row, 0, len(users))
				for _, user := range users {
					expire := "-"
					if user.ExpireAt.Valid {
						expire = user.ExpireAt.String
					}
					rows = append(rows, table.Row{
						user.Name,
						user.Status,
						units.FormatBytes(user.GlobalQuotaBytes),
						expire,
					})
				}
				renderTable(cmd, table.Row{"NAME", "STATUS", "QUOTA", "EXPIRE"}, rows)
				return nil
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show a proxy user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				user, err := store.GetProxyUser(ctx, args[0])
				if err != nil {
					return err
				}
				printProxyUser(cmd, user)
				return nil
			})
		},
	})
	cmd.AddCommand(userStatusCommand("enable", "active"))
	cmd.AddCommand(userStatusCommand("disable", "disabled"))
	cmd.AddCommand(userDeleteCommand())
	cmd.AddCommand(userSetQuotaCommand())
	cmd.AddCommand(userSetExpireCommand())
	cmd.AddCommand(userNodeInfoCommand())
	return cmd
}

func userNodeInfoCommand() *cobra.Command {
	var nodeName, format string
	cmd := &cobra.Command{
		Use:   "node-info <name>",
		Short: "Render user node connection information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "json" {
				return fmt.Errorf("unsupported format %q", format)
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				raw, err := configrender.RenderNodeInfo(ctx, store, args[0], nodeName)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func userStatusCommand(name, status string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <name>",
		Short: titleASCII(name) + " a proxy user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetProxyUserStatus(ctx, args[0], status); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "user %s: %s\n", args[0], status)
				return nil
			})
		},
	}
}

func userDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a proxy user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				user, err := store.SoftDeleteProxyUser(ctx, args[0])
				if err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "user %s deleted\n", user.Name)
				return nil
			})
		},
	}
}

func userSetQuotaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-quota <name> <bytes|gb>",
		Short: "Set global user quota",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			quota, err := units.ParseBytes(args[1])
			if err != nil {
				return err
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetProxyUserQuota(ctx, args[0], quota); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "user %s quota: %s\n", args[0], units.FormatBytes(quota))
				return nil
			})
		},
	}
}

func userSetExpireCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set-expire <name> <date|none>",
		Short: "Set user expiration",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			expire := args[1]
			if expire == "none" || expire == "0" {
				expire = ""
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetProxyUserExpire(ctx, args[0], expire); err != nil {
					return err
				}
				if expire == "" {
					expire = "none"
				}
				okText.Fprintf(cmd.OutOrStdout(), "user %s expire: %s\n", args[0], expire)
				return nil
			})
		},
	}
}
