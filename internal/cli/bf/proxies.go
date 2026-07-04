package bf

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haoxin/boxfleet/internal/secret"
	"github.com/haoxin/boxfleet/internal/server/db"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

func proxyCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "proxy", Short: "Manage node proxies"}
	create := &cobra.Command{Use: "create", Short: "Create a proxy"}
	create.AddCommand(createVLESSRealityCommand())
	create.AddCommand(createSS2022Command())
	create.AddCommand(createHysteria2Command())
	cmd.AddCommand(create)
	cmd.AddCommand(proxyListCommand())
	cmd.AddCommand(proxyShowCommand())
	cmd.AddCommand(proxyRenameCommand())
	cmd.AddCommand(proxySetShortIDCommand())
	cmd.AddCommand(proxyStatusCommand("enable", true))
	cmd.AddCommand(proxyStatusCommand("disable", false))
	cmd.AddCommand(proxyStatusCommand("delete", false))
	return cmd
}

func proxyRenameCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "rename <current-name> <new-name>",
		Short: "Rename a proxy while preserving its old name as an alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				proxy, err := store.RenameProxy(ctx, nodeName, args[0], args[1])
				if err != nil {
					return err
				}
				printProxy(cmd, proxy)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func proxySetShortIDCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "set-short-id <proxy> <short-id>",
		Short: "Set a VLESS Reality short ID",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				proxy, err := store.GetProxy(ctx, nodeName, args[0])
				if err != nil {
					return err
				}
				if proxy.Protocol != db.ProtocolVLESSReality {
					return fmt.Errorf("proxy %q is %s, not %s", proxy.Name, proxy.Protocol, db.ProtocolVLESSReality)
				}
				shortID, err := db.NormalizeRealityShortID(args[1])
				if err != nil {
					return err
				}
				var settings map[string]any
				if err := json.Unmarshal([]byte(proxy.SettingsJSON), &settings); err != nil {
					return fmt.Errorf("parse settings for %s: %w", proxy.Name, err)
				}
				settings["short_id"] = shortID
				settingsJSON, err := json.Marshal(settings)
				if err != nil {
					return err
				}
				updated, err := store.UpdateProxy(ctx, db.UpdateProxyParams{
					NodeName:          proxy.NodeName,
					Name:              proxy.Name,
					Listen:            proxy.Listen,
					ListenPort:        proxy.ListenPort,
					Transport:         proxy.Transport,
					Enabled:           proxy.Enabled,
					TrafficMultiplier: proxy.TrafficMultiplier,
					SettingsJSON:      string(settingsJSON),
					InboundRulesJSON:  proxy.InboundRulesJSON,
					OutboundRulesJSON: proxy.OutboundRulesJSON,
					RouteRulesJSON:    proxy.RouteRulesJSON,
				})
				if err != nil {
					return err
				}
				printProxy(cmd, updated)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func createVLESSRealityCommand() *cobra.Command {
	var nodeName, listen, name, sni, handshakeServer, shortID string
	var port, handshakePort int
	cmd := &cobra.Command{
		Use:     "vless-reality",
		Aliases: []string{"vless"},
		Short:   "Create a VLESS Reality proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				name = fmt.Sprintf("vless-%d", port)
			}
			if handshakeServer == "" {
				handshakeServer = sni
			}
			if shortID == "" {
				generated, err := secret.HexBytes(4)
				if err != nil {
					return err
				}
				shortID = generated
			}
			normalizedShortID, err := db.NormalizeRealityShortID(shortID)
			if err != nil {
				return err
			}
			shortID = normalizedShortID
			keyPair, err := secret.RealityKeyPairX25519()
			if err != nil {
				return err
			}
			settings := map[string]any{
				"server_name":         sni,
				"reality_private_key": keyPair.PrivateKey,
				"reality_public_key":  keyPair.PublicKey,
				"short_id":            shortID,
				"handshake_server":    handshakeServer,
				"handshake_port":      handshakePort,
			}
			return createProxy(cmd, db.CreateProxyParams{
				NodeName:     nodeName,
				Name:         name,
				Protocol:     db.ProtocolVLESSReality,
				Listen:       listen,
				ListenPort:   port,
				Transport:    db.TransportTCP,
				Enabled:      true,
				SettingsJSON: mustJSON(settings),
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().IntVar(&port, "port", 443, "listen port")
	cmd.Flags().StringVar(&listen, "listen", "::", "listen address")
	cmd.Flags().StringVar(&name, "name", "", "proxy name")
	cmd.Flags().StringVar(&sni, "sni", "", "Reality TLS server name")
	cmd.Flags().StringVar(&handshakeServer, "handshake-server", "", "Reality handshake server")
	cmd.Flags().IntVar(&handshakePort, "handshake-port", 443, "Reality handshake port")
	cmd.Flags().StringVar(&shortID, "short-id", "", "Reality short ID, 0 to 8 hex characters")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("sni")
	return cmd
}

func createSS2022Command() *cobra.Command {
	var nodeName, listen, name, method, network string
	var port int
	cmd := &cobra.Command{
		Use:     "ss2022",
		Aliases: []string{"shadowsocks-2022", "shadowsocks"},
		Short:   "Create a Shadowsocks 2022 proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				name = fmt.Sprintf("ss-%d", port)
			}
			keyLength, err := ss2022KeyLength(method)
			if err != nil {
				return err
			}
			serverPassword, err := secret.Base64Bytes(keyLength)
			if err != nil {
				return err
			}
			if network != "" && network != "tcp" && network != "udp" {
				return fmt.Errorf("network must be tcp, udp, or empty")
			}
			settings := map[string]any{
				"method":          method,
				"server_password": serverPassword,
				"network":         network,
			}
			return createProxy(cmd, db.CreateProxyParams{
				NodeName:     nodeName,
				Name:         name,
				Protocol:     db.ProtocolShadowsocks2022,
				Listen:       listen,
				ListenPort:   port,
				Transport:    db.TransportTCPUDP,
				Enabled:      true,
				SettingsJSON: mustJSON(settings),
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().IntVar(&port, "port", 8388, "listen port")
	cmd.Flags().StringVar(&listen, "listen", "::", "listen address")
	cmd.Flags().StringVar(&name, "name", "", "proxy name")
	cmd.Flags().StringVar(&method, "method", "2022-blake3-aes-128-gcm", "Shadowsocks method")
	cmd.Flags().StringVar(&network, "network", "", "listen network: tcp, udp, or empty for both")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func createHysteria2Command() *cobra.Command {
	var nodeName, listen, name, sni, certPath, keyPath, obfsPassword string
	var port, upMbps, downMbps int
	cmd := &cobra.Command{
		Use:     "hy2",
		Aliases: []string{"hysteria2"},
		Short:   "Create a Hysteria2 proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				name = fmt.Sprintf("hy2-%d", port)
			}
			tls := map[string]any{
				"enabled":          true,
				"server_name":      sni,
				"certificate_path": certPath,
				"key_path":         keyPath,
			}
			settings := map[string]any{
				"up_mbps":   upMbps,
				"down_mbps": downMbps,
				"tls":       tls,
			}
			if obfsPassword != "" {
				settings["obfs"] = map[string]any{
					"type":     "salamander",
					"password": obfsPassword,
				}
			}
			return createProxy(cmd, db.CreateProxyParams{
				NodeName:     nodeName,
				Name:         name,
				Protocol:     db.ProtocolHysteria2,
				Listen:       listen,
				ListenPort:   port,
				Transport:    db.TransportUDP,
				Enabled:      true,
				SettingsJSON: mustJSON(settings),
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().IntVar(&port, "port", 8443, "listen port")
	cmd.Flags().StringVar(&listen, "listen", "::", "listen address")
	cmd.Flags().StringVar(&name, "name", "", "proxy name")
	cmd.Flags().IntVar(&upMbps, "up-mbps", 0, "Hysteria2 up_mbps")
	cmd.Flags().IntVar(&downMbps, "down-mbps", 0, "Hysteria2 down_mbps")
	cmd.Flags().StringVar(&sni, "sni", "", "TLS server name")
	cmd.Flags().StringVar(&certPath, "cert-path", "", "node-side certificate path")
	cmd.Flags().StringVar(&keyPath, "key-path", "", "node-side private key path")
	cmd.Flags().StringVar(&obfsPassword, "obfs-password", "", "optional Salamander obfs password")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("cert-path")
	_ = cmd.MarkFlagRequired("key-path")
	return cmd
}

func createProxy(cmd *cobra.Command, params db.CreateProxyParams) error {
	return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
		proxy, err := store.CreateProxy(ctx, params)
		if err != nil {
			return err
		}
		printProxy(cmd, proxy)
		return nil
	})
}

func proxyListCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List proxies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				proxies, err := store.ListProxies(ctx, nodeName)
				if err != nil {
					return err
				}
				rows := make([]table.Row, 0, len(proxies))
				for _, proxy := range proxies {
					rows = append(rows, table.Row{
						proxy.NodeName,
						proxy.Name,
						proxy.Protocol,
						proxy.ListenPort,
						proxy.Transport,
						proxy.Enabled,
						proxy.Listen,
					})
				}
				renderTable(cmd, table.Row{"NODE", "NAME", "PROTOCOL", "PORT", "TRANSPORT", "ENABLED", "LISTEN"}, rows)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "filter by node")
	return cmd
}

func proxyShowCommand() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				proxy, err := store.GetProxy(ctx, nodeName, args[0])
				if err != nil {
					return err
				}
				printProxy(cmd, proxy)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func proxyStatusCommand(name string, enabled bool) *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   name + " <name>",
		Short: titleASCII(name) + " a proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigratedStore(cmd.Context(), func(ctx context.Context, store *db.DB) error {
				if err := store.SetProxyEnabled(ctx, nodeName, args[0], enabled); err != nil {
					return err
				}
				okText.Fprintf(cmd.OutOrStdout(), "proxy %s@%s enabled: %t\n", args[0], nodeName, enabled)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}
