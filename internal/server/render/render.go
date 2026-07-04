package render

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/haoxin/boxfleet/internal/server/db"
	"go.yaml.in/yaml/v3"
)

const DefaultMixedListenPort = 2080

type ClientConfigParams struct {
	UserName        string
	NodeName        string
	ProxyName       string
	MixedListen     string
	MixedListenPort int
	UTLSFingerprint string
	OutboundTag     string
}

type NodeInfo struct {
	User    string          `json:"user"`
	Node    string          `json:"node"`
	Proxies []NodeInfoProxy `json:"proxies"`
}

type ConnectionInfo struct {
	User  string     `json:"user"`
	Nodes []NodeInfo `json:"nodes"`
}

type NodeInfoProxy struct {
	Name          string `json:"name"`
	ProxyName     string `json:"proxy_name"`
	HostTag       string `json:"host_tag"`
	Type          string `json:"type"`
	Server        string `json:"server"`
	ServerPort    int    `json:"server_port"`
	UUID          string `json:"uuid"`
	Flow          string `json:"flow"`
	ServerName    string `json:"server_name"`
	PublicKey     string `json:"public_key"`
	ShortID       string `json:"short_id"`
	isPrimaryHost bool
}

type mihomoProxyProvider struct {
	Proxies []mihomoVLESSProxy `yaml:"proxies"`
}

type mihomoVLESSProxy struct {
	Name              string               `yaml:"name"`
	Type              string               `yaml:"type"`
	Server            string               `yaml:"server"`
	Port              int                  `yaml:"port"`
	UUID              string               `yaml:"uuid"`
	UDP               bool                 `yaml:"udp"`
	Flow              string               `yaml:"flow"`
	Network           string               `yaml:"network"`
	TLS               bool                 `yaml:"tls"`
	ServerName        string               `yaml:"servername"`
	ClientFingerprint string               `yaml:"client-fingerprint"`
	PacketEncoding    string               `yaml:"packet-encoding"`
	RealityOpts       mihomoRealityOptions `yaml:"reality-opts"`
	Encryption        string               `yaml:"encryption"`
}

type mihomoRealityOptions struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

type vlessRealitySettings struct {
	ServerName        string `json:"server_name"`
	RealityPrivateKey string `json:"reality_private_key"`
	RealityPublicKey  string `json:"reality_public_key"`
	ShortID           string `json:"short_id"`
	HandshakeServer   string `json:"handshake_server"`
	HandshakePort     int    `json:"handshake_port"`
}

type singBoxConfig struct {
	Log          *logConfig          `json:"log,omitempty"`
	Inbounds     []any               `json:"inbounds,omitempty"`
	Outbounds    []any               `json:"outbounds,omitempty"`
	Route        *routeConfig        `json:"route,omitempty"`
	Experimental *experimentalConfig `json:"experimental,omitempty"`
}

type logConfig struct {
	Level     string `json:"level"`
	Timestamp bool   `json:"timestamp"`
}

type vlessInbound struct {
	Type       string             `json:"type"`
	Tag        string             `json:"tag"`
	Listen     string             `json:"listen"`
	ListenPort int                `json:"listen_port"`
	Users      []vlessInboundUser `json:"users"`
	TLS        inboundTLS         `json:"tls"`
}

type vlessInboundUser struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	Flow string `json:"flow"`
}

type inboundTLS struct {
	Enabled    bool           `json:"enabled"`
	ServerName string         `json:"server_name"`
	Reality    inboundReality `json:"reality"`
}

type inboundReality struct {
	Enabled    bool             `json:"enabled"`
	Handshake  realityHandshake `json:"handshake"`
	PrivateKey string           `json:"private_key"`
	ShortID    string           `json:"short_id"`
}

type realityHandshake struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}

type mixedInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type vlessOutbound struct {
	Type       string      `json:"type"`
	Tag        string      `json:"tag"`
	Server     string      `json:"server"`
	ServerPort int         `json:"server_port"`
	UUID       string      `json:"uuid"`
	Flow       string      `json:"flow"`
	Network    string      `json:"network"`
	TLS        outboundTLS `json:"tls"`
}

type outboundTLS struct {
	Enabled    bool            `json:"enabled"`
	ServerName string          `json:"server_name"`
	UTLS       utlsConfig      `json:"utls"`
	Reality    outboundReality `json:"reality"`
}

type utlsConfig struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}

type outboundReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}

type outbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`
}

type routeConfig struct {
	Final string `json:"final"`
}

type experimentalConfig struct {
	V2RayAPI v2rayAPIConfig `json:"v2ray_api"`
}

type v2rayAPIConfig struct {
	Listen string        `json:"listen"`
	Stats  v2rayAPIStats `json:"stats"`
}

type v2rayAPIStats struct {
	Enabled bool     `json:"enabled"`
	Users   []string `json:"users"`
}

// emptyNodeConfig is a valid running sing-box config with no inbounds: it serves
// nothing. Used both for nodes with no accesses and for disabled nodes.
func emptyNodeConfig() singBoxConfig {
	return singBoxConfig{
		Log: &logConfig{Level: "info", Timestamp: true},
		Outbounds: []any{
			outbound{Type: "direct", Tag: "direct"},
			outbound{Type: "block", Tag: "block"},
		},
		Route: &routeConfig{Final: "direct"},
		Experimental: &experimentalConfig{
			V2RayAPI: v2rayAPIConfig{
				Listen: "127.0.0.1:18082",
				Stats:  v2rayAPIStats{Enabled: true, Users: []string{}},
			},
		},
	}
}

// RenderDisabledConfig returns a valid no-inbound config served to a disabled
// node. New agents act on the X-BoxFleet-Node-State header and stop sing-box;
// legacy agents that ignore the header still stop serving when they apply this
// (it passes `sing-box check` and has no inbounds).
func RenderDisabledConfig() ([]byte, error) {
	return json.MarshalIndent(emptyNodeConfig(), "", "  ")
}

func RenderNodeConfig(ctx context.Context, store *db.DB, nodeName string) ([]byte, error) {
	accesses, err := store.ListProxyAccessesByNode(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	if len(accesses) == 0 {
		return json.MarshalIndent(emptyNodeConfig(), "", "  ")
	}
	inboundByName := make(map[string]*vlessInbound)
	var inboundOrder []string
	var statsUsers []string
	for _, access := range accesses {
		if access.Protocol != db.ProtocolVLESSReality {
			return nil, fmt.Errorf("renderer only supports %s, got %s on %s", db.ProtocolVLESSReality, access.Protocol, access.ProxyName)
		}
		settings, userCredential, err := parseVLESSReality(access)
		if err != nil {
			return nil, err
		}
		inbound := inboundByName[access.ProxyName]
		if inbound == nil {
			inbound = &vlessInbound{
				Type:       "vless",
				Tag:        access.ProxyName,
				Listen:     access.Listen,
				ListenPort: access.ListenPort,
				TLS: inboundTLS{
					Enabled:    true,
					ServerName: settings.ServerName,
					Reality: inboundReality{
						Enabled: true,
						Handshake: realityHandshake{
							Server:     settings.HandshakeServer,
							ServerPort: settings.HandshakePort,
						},
						PrivateKey: settings.RealityPrivateKey,
						ShortID:    settings.ShortID,
					},
				},
			}
			inboundByName[access.ProxyName] = inbound
			inboundOrder = append(inboundOrder, access.ProxyName)
		}
		inbound.Users = append(inbound.Users, vlessInboundUser{
			Name: access.AuthName,
			UUID: userCredential.UUID,
			Flow: userCredential.Flow,
		})
		statsUsers = append(statsUsers, access.AuthName)
	}
	inbounds := make([]any, 0, len(inboundOrder))
	for _, name := range inboundOrder {
		inbounds = append(inbounds, inboundByName[name])
	}
	cfg := singBoxConfig{
		Log:      &logConfig{Level: "info", Timestamp: true},
		Inbounds: inbounds,
		Outbounds: []any{
			outbound{Type: "direct", Tag: "direct"},
			outbound{Type: "block", Tag: "block"},
		},
		Route: &routeConfig{Final: "direct"},
		Experimental: &experimentalConfig{
			V2RayAPI: v2rayAPIConfig{
				Listen: "127.0.0.1:18082",
				Stats:  v2rayAPIStats{Enabled: true, Users: statsUsers},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func RenderClientConfig(ctx context.Context, store *db.DB, params ClientConfigParams) ([]byte, error) {
	info, err := NodeInfoForUser(ctx, store, params.UserName, params.NodeName)
	if err != nil {
		return nil, err
	}
	var selected *NodeInfoProxy
	// Prefer an exact final profile name. This lets callers select tagged and
	// legacy multi-host profiles while keeping the historical --proxy behavior
	// for a canonical proxy name.
	for i := range info.Proxies {
		if info.Proxies[i].Name == params.ProxyName {
			selected = &info.Proxies[i]
			break
		}
	}
	if selected == nil {
		for i := range info.Proxies {
			if info.Proxies[i].ProxyName != params.ProxyName {
				continue
			}
			if selected == nil || info.Proxies[i].isPrimaryHost {
				selected = &info.Proxies[i]
			}
			if info.Proxies[i].isPrimaryHost {
				break
			}
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("access for user %q on %q/%q not found", params.UserName, params.NodeName, params.ProxyName)
	}
	mixedListen := params.MixedListen
	if mixedListen == "" {
		mixedListen = "127.0.0.1"
	}
	mixedPort := params.MixedListenPort
	if mixedPort == 0 {
		mixedPort = DefaultMixedListenPort
	}
	outboundTag := params.OutboundTag
	if outboundTag == "" {
		outboundTag = "proxy"
	}
	fingerprint := params.UTLSFingerprint
	if fingerprint == "" {
		fingerprint = "chrome"
	}
	cfg := singBoxConfig{
		Log: &logConfig{Level: "info", Timestamp: true},
		Inbounds: []any{
			mixedInbound{Type: "mixed", Tag: "mixed-in", Listen: mixedListen, ListenPort: mixedPort},
		},
		Outbounds: []any{
			vlessOutbound{
				Type:       "vless",
				Tag:        outboundTag,
				Server:     selected.Server,
				ServerPort: selected.ServerPort,
				UUID:       selected.UUID,
				Flow:       selected.Flow,
				Network:    "tcp",
				TLS: outboundTLS{
					Enabled:    true,
					ServerName: selected.ServerName,
					UTLS:       utlsConfig{Enabled: true, Fingerprint: fingerprint},
					Reality: outboundReality{
						Enabled:   true,
						PublicKey: selected.PublicKey,
						ShortID:   selected.ShortID,
					},
				},
			},
			outbound{Type: "direct", Tag: "direct"},
		},
		Route: &routeConfig{Final: outboundTag},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func NodeInfoForUser(ctx context.Context, store *db.DB, userName, nodeName string) (NodeInfo, error) {
	accesses, err := store.ListProxyAccessesByUserNode(ctx, userName, nodeName)
	if err != nil {
		return NodeInfo{}, err
	}
	if len(accesses) == 0 {
		return NodeInfo{}, fmt.Errorf("user %q has no active proxy accesses on node %q", userName, nodeName)
	}
	return nodeInfoFromAccesses(ctx, store, userName, nodeName, accesses)
}

func nodeInfoFromAccesses(ctx context.Context, store *db.DB, userName, nodeName string, accesses []db.ProxyAccess) (NodeInfo, error) {
	// Each selected node host yields its own client profile (a node may publish a
	// domain plus several IPv4/IPv6 addresses). Fall back to the view's single
	// public_host if the node row can't be loaded or has no selected host.
	hosts := selectedNodeHosts(ctx, store, nodeName, accesses[0].NodePublicHost)
	info := NodeInfo{User: accesses[0].ProxyUserName, Node: accesses[0].NodeName}
	for _, access := range accesses {
		if access.Protocol != db.ProtocolVLESSReality {
			continue
		}
		settings, userCredential, err := parseVLESSReality(access)
		if err != nil {
			return NodeInfo{}, err
		}
		for _, host := range hosts {
			name := mihomoProfileName(access.ProxyName, host)
			info.Proxies = append(info.Proxies, NodeInfoProxy{
				Name:          name,
				ProxyName:     access.ProxyName,
				HostTag:       host.Tag,
				Type:          access.Protocol,
				Server:        host.Host,
				ServerPort:    access.ListenPort,
				UUID:          userCredential.UUID,
				Flow:          userCredential.Flow,
				ServerName:    settings.ServerName,
				PublicKey:     settings.RealityPublicKey,
				ShortID:       settings.ShortID,
				isPrimaryHost: host.Primary,
			})
		}
	}
	if len(info.Proxies) == 0 {
		return NodeInfo{}, fmt.Errorf("user %q has no supported proxy accesses on node %q", userName, nodeName)
	}
	if err := validateUniqueProfileNames(info.Proxies); err != nil {
		return NodeInfo{}, err
	}
	return info, nil
}

// ConnectionInfoForUser returns all currently active, supported client
// profiles for a user. Disabled users, nodes, bindings, proxies, and accesses
// are filtered by ListProxyAccessesByUserNode.
func ConnectionInfoForUser(ctx context.Context, store *db.DB, userName string) (ConnectionInfo, error) {
	user, err := store.GetProxyUser(ctx, userName)
	if err != nil {
		return ConnectionInfo{}, err
	}
	allAccesses, err := store.ListProxyAccessesByUser(ctx, userName)
	if err != nil {
		return ConnectionInfo{}, err
	}

	info := ConnectionInfo{User: user.Name, Nodes: make([]NodeInfo, 0)}
	seenNodes := make(map[string]struct{})
	for _, access := range allAccesses {
		if _, seen := seenNodes[access.NodeName]; seen {
			continue
		}
		seenNodes[access.NodeName] = struct{}{}

		activeAccesses, err := store.ListProxyAccessesByUserNode(ctx, userName, access.NodeName)
		if err != nil {
			return ConnectionInfo{}, err
		}
		if len(activeAccesses) == 0 {
			continue
		}
		for _, activeAccess := range activeAccesses {
			if activeAccess.Protocol != db.ProtocolVLESSReality {
				return ConnectionInfo{}, fmt.Errorf(
					"connection info renderer only supports %s, got %s on %s/%s",
					db.ProtocolVLESSReality,
					activeAccess.Protocol,
					activeAccess.NodeName,
					activeAccess.ProxyName,
				)
			}
		}

		nodeInfo, err := nodeInfoFromAccesses(ctx, store, userName, access.NodeName, activeAccesses)
		if err != nil {
			return ConnectionInfo{}, err
		}
		info.Nodes = append(info.Nodes, nodeInfo)
	}
	allProfiles := make([]NodeInfoProxy, 0)
	for _, node := range info.Nodes {
		allProfiles = append(allProfiles, node.Proxies...)
	}
	if err := validateUniqueProfileNames(allProfiles); err != nil {
		return ConnectionInfo{}, err
	}
	return info, nil
}

type selectedNodeHost struct {
	Host    string
	Tag     string
	Primary bool
}

// selectedNodeHosts returns the addresses that should each get a client profile:
// the node's hosts marked selected, in order. It degrades gracefully to the
// supplied fallback (the proxy view's public_host) if the node can't be loaded
// or nothing is selected, so rendering never produces an empty server address.
func selectedNodeHosts(ctx context.Context, store *db.DB, nodeName, fallback string) []selectedNodeHost {
	node, err := store.GetNode(ctx, nodeName)
	if err != nil {
		return []selectedNodeHost{{Host: fallback, Primary: true}}
	}
	hosts := make([]selectedNodeHost, 0, len(node.Hosts))
	for i, h := range node.Hosts {
		if h.Selected {
			hosts = append(hosts, selectedNodeHost{
				Host:    h.Host,
				Tag:     h.Tag,
				Primary: i == 0,
			})
		}
	}
	if len(hosts) == 0 {
		return []selectedNodeHost{{Host: fallback, Primary: true}}
	}
	return hosts
}

func mihomoProfileName(proxyName string, host selectedNodeHost) string {
	if host.Tag != "" {
		return proxyName + "-" + host.Tag
	}
	if !host.Primary {
		// Compatibility for hosts written before host tags were introduced.
		return proxyName + "-" + host.Host
	}
	return proxyName
}

func validateUniqueProfileNames(proxies []NodeInfoProxy) error {
	seen := make(map[string]NodeInfoProxy, len(proxies))
	for _, proxy := range proxies {
		if previous, ok := seen[proxy.Name]; ok {
			return fmt.Errorf(
				"Mihomo profile name %q conflicts between %s (%s) and %s (%s)",
				proxy.Name,
				previous.ProxyName,
				previous.Server,
				proxy.ProxyName,
				proxy.Server,
			)
		}
		seen[proxy.Name] = proxy
	}
	return nil
}

func RenderNodeInfo(ctx context.Context, store *db.DB, userName, nodeName string) ([]byte, error) {
	info, err := NodeInfoForUser(ctx, store, userName, nodeName)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(info, "", "  ")
}

// RenderMihomoProxyProvider renders all currently active VLESS-Reality
// accesses for a user as a Mihomo proxy-provider document. It intentionally
// emits only the top-level proxies field, so callers can serve it directly to
// a Mihomo/Clash proxy-provider.
func RenderMihomoProxyProvider(ctx context.Context, store *db.DB, userName string) ([]byte, error) {
	info, err := ConnectionInfoForUser(ctx, store, userName)
	if err != nil {
		return nil, err
	}

	provider := mihomoProxyProvider{Proxies: make([]mihomoVLESSProxy, 0)}
	for _, node := range info.Nodes {
		for _, proxy := range node.Proxies {
			provider.Proxies = append(provider.Proxies, mihomoVLESSProxy{
				Name:              proxy.Name,
				Type:              "vless",
				Server:            proxy.Server,
				Port:              proxy.ServerPort,
				UUID:              proxy.UUID,
				UDP:               true,
				Flow:              proxy.Flow,
				Network:           "tcp",
				TLS:               true,
				ServerName:        proxy.ServerName,
				ClientFingerprint: "chrome",
				PacketEncoding:    "xudp",
				RealityOpts: mihomoRealityOptions{
					PublicKey: proxy.PublicKey,
					ShortID:   proxy.ShortID,
				},
				Encryption: "",
			})
		}
	}

	return yaml.Marshal(provider)
}

func parseVLESSReality(access db.ProxyAccess) (vlessRealitySettings, db.VLESSRealityCredential, error) {
	var settings vlessRealitySettings
	if err := json.Unmarshal([]byte(access.SettingsJSON), &settings); err != nil {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("parse settings for %s: %w", access.ProxyName, err)
	}
	if settings.ServerName == "" {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("settings for %s missing server_name", access.ProxyName)
	}
	if settings.RealityPrivateKey == "" {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("settings for %s missing reality_private_key", access.ProxyName)
	}
	if settings.RealityPublicKey == "" {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("settings for %s missing reality_public_key", access.ProxyName)
	}
	normalizedShortID, err := db.NormalizeRealityShortID(settings.ShortID)
	if err != nil {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("settings for %s invalid short_id: %w", access.ProxyName, err)
	}
	settings.ShortID = normalizedShortID
	if settings.HandshakeServer == "" {
		settings.HandshakeServer = settings.ServerName
	}
	if settings.HandshakePort == 0 {
		settings.HandshakePort = 443
	}
	var userCredential db.VLESSRealityCredential
	if err := json.Unmarshal([]byte(access.CredentialJSON), &userCredential); err != nil {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, fmt.Errorf("parse access for %s: %w", access.AuthName, err)
	}
	if userCredential.UUID == "" {
		return vlessRealitySettings{}, db.VLESSRealityCredential{}, errors.New("access missing uuid")
	}
	if userCredential.Flow == "" {
		userCredential.Flow = db.VLESSRealityFlowVision
	}
	return settings, userCredential, nil
}
