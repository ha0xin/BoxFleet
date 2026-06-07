package bf

import (
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultDBPath = "boxfleet.db"

var (
	cfgFile string
	okText  = color.New(color.FgGreen, color.Bold)
)

func Execute() error {
	return NewRootCommand().Execute()
}

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "bf",
		Short:         "Manage a BoxFleet server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
	root.PersistentFlags().String("db", defaultDBPath, "SQLite database path")
	_ = viper.BindPFlag("db", root.PersistentFlags().Lookup("db"))
	viper.SetDefault("db", defaultDBPath)
	viper.SetEnvPrefix("BOXFLEET")
	viper.AutomaticEnv()

	cobra.OnInitialize(initConfig)

	root.AddCommand(dbCommand())
	root.AddCommand(userCommand())
	root.AddCommand(nodeCommand())
	root.AddCommand(bindCommand())
	root.AddCommand(proxyCommand())
	root.AddCommand(accessCommand())
	root.AddCommand(configCommand())
	root.AddCommand(statsCommand())
	root.AddCommand(logsCommand())
	return root
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
		_ = viper.ReadInConfig()
		return
	}
	viper.SetConfigName("boxfleet")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.config/boxfleet")
	_ = viper.ReadInConfig()
}
