package bf

import (
	"context"
	"fmt"
	"time"

	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func dbCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "db", Short: "Manage the SQLite database"}
	cmd.AddCommand(&cobra.Command{
		Use:     "init",
		Aliases: []string{"migrate"},
		Short:   "Initialize or migrate the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(cmd.Context(), true, func(ctx context.Context, store *db.DB) error {
				migrateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
				defer cancel()
				if err := store.Migrate(migrateCtx); err != nil {
					return err
				}
				status, err := store.Status(ctx)
				if err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "database: %s\n", viper.GetString("db"))
				fmt.Fprintf(cmd.OutOrStdout(), "schema version: %d\n", status.CurrentVersion)
				return nil
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(cmd.Context(), false, func(ctx context.Context, store *db.DB) error {
				status, err := store.Status(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "database: %s\n", viper.GetString("db"))
				fmt.Fprintf(cmd.OutOrStdout(), "current version: %d\n", status.CurrentVersion)
				fmt.Fprintf(cmd.OutOrStdout(), "latest version: %d\n", status.LatestVersion)
				if len(status.Pending) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "pending: none")
					return nil
				}
				fmt.Fprintln(cmd.OutOrStdout(), "pending:")
				for _, migration := range status.Pending {
					fmt.Fprintf(cmd.OutOrStdout(), "  %03d_%s\n", migration.Version, migration.Name)
				}
				return nil
			})
		},
	})
	return cmd
}
