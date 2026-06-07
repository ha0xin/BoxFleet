package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/haoxin/boxfleet/internal/model"
)

func ConfigFromBootstrap(config model.BootstrapConfig) Config {
	return Config{
		NodeName:        config.NodeName,
		Token:           config.Token,
		ServerURL:       config.ServerURL,
		SingBoxURL:      config.SingBoxURL,
		InstallDir:      config.InstallDir,
		SingBoxPath:     config.SingBoxPath,
		SingBoxConfig:   config.SingBoxConfig,
		SingBoxService:  config.SingBoxService,
		AgentPath:       config.AgentPath,
		AgentConfigPath: config.AgentConfigPath,
		AgentService:    config.AgentService,
		PollInterval:    config.PollInterval,
		V2RayAPIAddress: config.V2RayAPIAddress,
	}
}

func Bootstrap(ctx context.Context, value string) error {
	bootstrapConfig, err := model.DecodeBootstrap(value)
	if err != nil {
		return fmt.Errorf("decode bootstrap string: %w", err)
	}
	config := ConfigFromBootstrap(bootstrapConfig)
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return err
	}
	if err := installCurrentBinary(config.AgentPath); err != nil {
		return err
	}
	if err := WriteConfig(config.AgentConfigPath, config); err != nil {
		return err
	}
	return New(config).Install(ctx)
}

func installCurrentBinary(target string) error {
	current, err := os.Executable()
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	return atomicWrite(target, raw, defaultBinaryFilePerm)
}
