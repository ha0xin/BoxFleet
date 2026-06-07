package bf

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/units"
	"github.com/haoxin/boxfleet/internal/v2raystats"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func statsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "stats", Short: "Query traffic statistics"}
	cmd.AddCommand(statsUserCommand())
	cmd.AddCommand(statsV2RayCommand())
	return cmd
}

func statsUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user <name>",
		Short: "Show stored user traffic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				rows, err := store.SumTrafficByUser(ctx, args[0])
				if err != nil {
					return err
				}
				tableRows := make([]table.Row, 0, len(rows))
				for _, row := range rows {
					tableRows = append(tableRows, table.Row{
						row.UserName,
						row.Direction,
						units.FormatBytes(row.RawBytes),
						units.FormatBytes(row.BillableBytes),
					})
				}
				renderTable(cmd, table.Row{"USER", "DIRECTION", "RAW", "BILLABLE"}, tableRows)
				return nil
			})
		},
	}
	return cmd
}

func statsV2RayCommand() *cobra.Command {
	var addr, pattern, format string
	var reset bool
	cmd := &cobra.Command{
		Use:   "v2ray",
		Short: "Query sing-box V2Ray API stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "json" {
				return fmt.Errorf("unsupported format %q", format)
			}
			var patterns []string
			if pattern != "" {
				patterns = []string{pattern}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			stats, err := v2raystats.Query(ctx, addr, patterns, reset)
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(stats, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:18082", "V2Ray API gRPC address")
	cmd.Flags().StringVar(&pattern, "pattern", "", "substring pattern to query")
	cmd.Flags().BoolVar(&reset, "reset", false, "reset counters after query")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json")
	return cmd
}
