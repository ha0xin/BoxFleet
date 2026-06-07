package model

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

const BootstrapPrefix = "boxfleet-bootstrap:"

type BootstrapConfig struct {
	NodeName        string `json:"node_name"`
	Token           string `json:"token"`
	ServerURL       string `json:"server_url"`
	SingBoxURL      string `json:"sing_box_url,omitempty"`
	InstallDir      string `json:"install_dir,omitempty"`
	SingBoxPath     string `json:"sing_box_path,omitempty"`
	SingBoxConfig   string `json:"sing_box_config,omitempty"`
	SingBoxService  string `json:"sing_box_service,omitempty"`
	AgentPath       string `json:"agent_path,omitempty"`
	AgentConfigPath string `json:"agent_config_path,omitempty"`
	AgentService    string `json:"agent_service,omitempty"`
	PollInterval    string `json:"poll_interval,omitempty"`
	V2RayAPIAddress string `json:"v2ray_api_address,omitempty"`
}

func EncodeBootstrap(config BootstrapConfig) (string, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return BootstrapPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeBootstrap(value string) (BootstrapConfig, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, BootstrapPrefix)
	if value == "" {
		return BootstrapConfig{}, errors.New("bootstrap string is empty")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return BootstrapConfig{}, err
	}
	var config BootstrapConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return BootstrapConfig{}, err
	}
	return config, nil
}
