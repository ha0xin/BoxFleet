package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/haoxin/boxfleet/internal/server/api"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/haoxin/boxfleet/internal/server/install"
)

var version = "dev"

func main() {
	if err := newServerCommand().Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "boxfleet-server",
		Short:         "Run the BoxFleet management server",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context())
		},
	}
	cmd.Flags().String("addr", "127.0.0.1:8080", "HTTP listen address")
	cmd.Flags().String("db", "boxfleet.db", "SQLite database path")
	cmd.Flags().String("artifact-dir", "", "directory served under /artifacts")
	cmd.Flags().String("admin-token", "", "admin API bearer token")
	cmd.Flags().String("admin-path-token", "", "path segment that gates admin UI and admin API routes")
	cmd.Flags().Bool("allow-insecure-admin", false, "allow admin API without a bearer token")

	viper.SetEnvPrefix("BOXFLEET")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	for _, name := range []string{"addr", "db", "artifact-dir", "admin-token", "admin-path-token", "allow-insecure-admin"} {
		_ = viper.BindPFlag(name, cmd.Flags().Lookup(name))
	}
	return cmd
}

func runServer(ctx context.Context) error {
	addr := viper.GetString("addr")
	dbPath := viper.GetString("db")
	artifactDir := viper.GetString("artifact-dir")
	adminToken := strings.TrimSpace(viper.GetString("admin-token"))
	adminPathToken := strings.Trim(strings.TrimSpace(viper.GetString("admin-path-token")), "/")
	allowInsecureAdmin := viper.GetBool("allow-insecure-admin")
	if adminToken == "" && !allowInsecureAdmin {
		return errors.New("admin token is required; set BOXFLEET_ADMIN_TOKEN or pass --allow-insecure-admin for local development")
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
	store, err := db.OpenSQLite(dbPath)
	if err != nil {
		logger.Error().Err(err).Str("db", dbPath).Msg("open database")
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		logger.Error().Err(err).Msg("migrate database")
		return err
	}
	if allowInsecureAdmin {
		logger.Warn().Msg("admin API authentication disabled by --allow-insecure-admin")
	}
	router := api.NewRouter(api.Options{
		DB:                 store,
		ArtifactDir:        artifactDir,
		AdminToken:         adminToken,
		AdminPathToken:     adminPathToken,
		AllowInsecureAdmin: allowInsecureAdmin,
		Version:            version,
		Repo:               install.DefaultRepo,
		SingBoxVersion:     install.DefaultSingBoxVersion,
	})

	logger.Info().Str("addr", addr).Str("db", dbPath).Str("artifact_dir", artifactDir).Str("version", version).Bool("admin_auth", adminToken != "").Bool("admin_path_token", adminPathToken != "").Msg("starting boxfleet-server")
	if err := http.ListenAndServe(addr, router); err != nil {
		logger.Error().Err(err).Msg("server stopped")
		return err
	}
	return nil
}
