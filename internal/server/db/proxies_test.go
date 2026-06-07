package db

import (
	"context"
	"strings"
	"testing"
)

func TestProxyListenerConflictValidation(t *testing.T) {
	ctx := context.Background()
	store := openTestDB(t)
	if _, err := store.CreateNode(ctx, "azus", "203.0.113.10", ""); err != nil {
		t.Fatal(err)
	}
	settings := `{"server_name":"www.amazon.com","reality_private_key":"private","reality_public_key":"public","short_id":"","handshake_server":"www.amazon.com","handshake_port":443}`
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090-a",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: settings,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "vless-39090-b",
		Protocol:     ProtocolVLESSReality,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCP,
		Enabled:      true,
		SettingsJSON: settings,
	}); err != nil {
		t.Fatalf("same VLESS Reality listener should share multi-user proxy: %v", err)
	}
	_, err := store.CreateProxy(ctx, CreateProxyParams{
		NodeName:     "azus",
		Name:         "hy2-39090",
		Protocol:     ProtocolHysteria2,
		Listen:       "0.0.0.0",
		ListenPort:   39090,
		Transport:    TransportTCPUDP,
		Enabled:      true,
		SettingsJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected listener conflict, got %v", err)
	}
}
