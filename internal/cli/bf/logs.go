package bf

import (
	"context"
	"fmt"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func logsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "logs", Short: "Query stored network logs"}
	cmd.AddCommand(logsNodeCommand())
	cmd.AddCommand(logsUserCommand())
	return cmd
}

func logsNodeCommand() *cobra.Command {
	var limit int64
	var raw bool
	cmd := &cobra.Command{
		Use:   "node <node>",
		Short: "Show recent logs for a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if raw {
					entries, err := store.ListRecentRawLogEntriesByNode(ctx, args[0], limit)
					if err != nil {
						return err
					}
					renderRawLogEntries(cmd, entries)
					return nil
				}
				events, err := store.ListRecentLogEventsByNode(ctx, args[0], limit)
				if err != nil {
					return err
				}
				renderLogEvents(cmd, events, raw)
				return nil
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "maximum events to show")
	cmd.Flags().BoolVar(&raw, "raw", false, "show raw stored log entries instead of structured events")
	return cmd
}

func logsUserCommand() *cobra.Command {
	var limit int64
	var raw bool
	cmd := &cobra.Command{
		Use:   "user <user>",
		Short: "Show recent logs for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				events, err := store.ListRecentLogEventsByUser(ctx, args[0], limit)
				if err != nil {
					return err
				}
				renderLogEvents(cmd, events, raw)
				return nil
			})
		},
	}
	cmd.Flags().Int64Var(&limit, "limit", 50, "maximum events to show")
	cmd.Flags().BoolVar(&raw, "raw", false, "include raw sing-box log messages")
	return cmd
}

func renderRawLogEntries(cmd *cobra.Command, entries []db.RawLogEntry) {
	rows := make([]table.Row, 0, len(entries))
	for _, entry := range entries {
		cursor := "-"
		if entry.JournalCursor.Valid && entry.JournalCursor.String != "" {
			cursor = entry.JournalCursor.String
		}
		rows = append(rows, table.Row{
			entry.ObservedAt,
			cursor,
			entry.RawMessage,
		})
	}
	renderTable(cmd, table.Row{"OBSERVED", "CURSOR", "RAW"}, rows)
}

func renderLogEvents(cmd *cobra.Command, events []db.LogEvent, raw bool) {
	header := table.Row{"TIME", "ACTION", "AUTH", "SOURCE", "TARGET", "COUNT"}
	if raw {
		header = append(header, "RAW")
	}
	rows := make([]table.Row, 0, len(events))
	for _, event := range events {
		target := "-"
		if event.TargetHost != "" {
			target = event.TargetHost
			if event.TargetPort > 0 {
				target = fmt.Sprintf("%s:%d", target, event.TargetPort)
			}
		}
		row := table.Row{
			event.WindowStart,
			emptyDash(event.Action),
			emptyDash(event.AuthName),
			emptyDash(event.SourceIp),
			target,
			event.Count,
		}
		if raw {
			row = append(row, event.RawMessage)
		}
		rows = append(rows, row)
	}
	renderTable(cmd, header, rows)
}
