package bf

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/units"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func bindCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "bind", Short: "Manage user-node bindings"}
	cmd.AddCommand(bindUserCommand())
	cmd.AddCommand(bindListCommand())
	cmd.AddCommand(bindStatusCommand("enable", true))
	cmd.AddCommand(bindStatusCommand("disable", false))
	cmd.AddCommand(bindStatusCommand("delete", false))
	cmd.AddCommand(bindSetQuotaCommand())
	cmd.AddCommand(bindSetMultiplierCommand())
	return cmd
}

func bindUserCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "user <user>",
		Short: "Bind user to node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				binding, err := store.BindUserToNode(ctx, args[0], nodeName)
				if err != nil {
					return err
				}
				printBinding(cmd, binding)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func bindListCommand() *cobra.Command {
	var userName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List user-node bindings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				bindings, err := store.ListUserNodeBindings(ctx, userName)
				if err != nil {
					return err
				}
				rows := make([]table.Row, 0, len(bindings))
				for _, binding := range bindings {
					multiplier := "inherit"
					if binding.TrafficMultiplier.Valid {
						multiplier = fmt.Sprintf("%.3g", binding.TrafficMultiplier.Float64)
					}
					rows = append(rows, table.Row{
						binding.ProxyUserName, binding.NodeName, binding.Enabled,
						units.FormatBytes(binding.NodeQuotaBytes), multiplier, binding.DisabledReason,
					})
				}
				renderTable(cmd, table.Row{"USER", "NODE", "ENABLED", "QUOTA", "MULT", "REASON"}, rows)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&userName, "user", "", "filter by user")
	return cmd
}

func bindStatusCommand(name string, enabled bool) *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   name + " <user>",
		Short: titleASCII(name) + " a user-node binding",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetUserNodeBindingEnabled(ctx, args[0], nodeName, enabled); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "binding %s@%s enabled: %t\n", args[0], nodeName, enabled)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func bindSetQuotaCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "set-quota <user> <bytes|gb>",
		Short: "Set user quota on a node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			quota, err := units.ParseBytes(args[1])
			if err != nil {
				return err
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetUserNodeQuota(ctx, args[0], nodeName, quota); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "binding %s@%s quota: %s\n", args[0], nodeName, units.FormatBytes(quota))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func bindSetMultiplierCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "set-multiplier <user> <ratio|inherit>",
		Short: "Set per-node traffic multiplier for a user binding",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			multiplier := sql.NullFloat64{}
			if args[1] != "inherit" && args[1] != "default" {
				value, err := strconv.ParseFloat(args[1], 64)
				if err != nil {
					return err
				}
				multiplier = sql.NullFloat64{Float64: value, Valid: true}
			}
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetUserNodeMultiplier(ctx, args[0], nodeName, multiplier); err != nil {
					return err
				}
				value := "inherit"
				if multiplier.Valid {
					value = fmt.Sprintf("%.3g", multiplier.Float64)
				}
				okText.Fprintf(cmd.OutOrStdout(), "binding %s@%s multiplier: %s\n", args[0], nodeName, value)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}
