package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const (
	ProtocolVLESSReality    = "vless_reality"
	ProtocolShadowsocks2022 = "shadowsocks_2022"
	ProtocolHysteria2       = "hysteria2"

	TransportTCP    = "tcp"
	TransportUDP    = "udp"
	TransportTCPUDP = "tcp_udp"
)

type Proxy struct {
	ID                string
	NodeID            string
	NodeName          string
	NodePublicHost    string
	Name              string
	Protocol          string
	Listen            string
	ListenPort        int
	Transport         string
	Enabled           bool
	TrafficMultiplier float64
	SettingsJSON      string
	InboundRulesJSON  string
	OutboundRulesJSON string
	RouteRulesJSON    string
	CreatedAt         string
	UpdatedAt         string
}

type CreateProxyParams struct {
	NodeName          string
	Name              string
	Protocol          string
	Listen            string
	ListenPort        int
	Transport         string
	Enabled           bool
	TrafficMultiplier float64
	SettingsJSON      string
	InboundRulesJSON  string
	OutboundRulesJSON string
	RouteRulesJSON    string
}

type UpdateProxyParams struct {
	NodeName          string
	Name              string
	Listen            string
	ListenPort        int
	Transport         string
	Enabled           bool
	TrafficMultiplier float64
	SettingsJSON      string
	InboundRulesJSON  string
	OutboundRulesJSON string
	RouteRulesJSON    string
}

func (db *DB) CreateProxy(ctx context.Context, params CreateProxyParams) (Proxy, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return Proxy{}, err
	}
	proxy, err := normalizeProxyParams(CreateProxyParams{
		NodeName:          node.Name,
		Name:              params.Name,
		Protocol:          params.Protocol,
		Listen:            params.Listen,
		ListenPort:        params.ListenPort,
		Transport:         params.Transport,
		Enabled:           params.Enabled,
		TrafficMultiplier: params.TrafficMultiplier,
		SettingsJSON:      params.SettingsJSON,
		InboundRulesJSON:  params.InboundRulesJSON,
		OutboundRulesJSON: params.OutboundRulesJSON,
		RouteRulesJSON:    params.RouteRulesJSON,
	})
	if err != nil {
		return Proxy{}, err
	}
	if err := db.validateProxyListener(ctx, node.Name, proxy, ""); err != nil {
		return Proxy{}, err
	}
	proxyID, err := id.New("prx")
	if err != nil {
		return Proxy{}, err
	}
	if err := db.q.CreateProxy(ctx, store.CreateProxyParams{
		ID:                proxyID,
		NodeID:            node.ID,
		Name:              proxy.Name,
		Protocol:          proxy.Protocol,
		Listen:            proxy.Listen,
		ListenPort:        int64(proxy.ListenPort),
		Transport:         proxy.Transport,
		Enabled:           boolToInt64(proxy.Enabled),
		TrafficMultiplier: proxy.TrafficMultiplier,
		SettingsJson:      proxy.SettingsJSON,
		InboundRulesJson:  proxy.InboundRulesJSON,
		OutboundRulesJson: proxy.OutboundRulesJSON,
		RouteRulesJson:    proxy.RouteRulesJSON,
	}); err != nil {
		return Proxy{}, err
	}
	return db.GetProxy(ctx, node.Name, proxy.Name)
}

func (db *DB) UpdateProxy(ctx context.Context, params UpdateProxyParams) (Proxy, error) {
	node, err := db.GetNode(ctx, params.NodeName)
	if err != nil {
		return Proxy{}, err
	}
	existing, err := db.GetProxy(ctx, node.Name, params.Name)
	if err != nil {
		return Proxy{}, err
	}
	proxy, err := normalizeProxyParams(CreateProxyParams{
		NodeName:          node.Name,
		Name:              params.Name,
		Protocol:          existing.Protocol,
		Listen:            params.Listen,
		ListenPort:        params.ListenPort,
		Transport:         params.Transport,
		Enabled:           params.Enabled,
		TrafficMultiplier: params.TrafficMultiplier,
		SettingsJSON:      params.SettingsJSON,
		InboundRulesJSON:  params.InboundRulesJSON,
		OutboundRulesJSON: params.OutboundRulesJSON,
		RouteRulesJSON:    params.RouteRulesJSON,
	})
	if err != nil {
		return Proxy{}, err
	}
	if err := db.validateProxyListener(ctx, node.Name, proxy, existing.ID); err != nil {
		return Proxy{}, err
	}
	affected, err := db.q.UpdateProxy(ctx, store.UpdateProxyParams{
		Listen:            proxy.Listen,
		ListenPort:        int64(proxy.ListenPort),
		Transport:         proxy.Transport,
		Enabled:           boolToInt64(proxy.Enabled),
		TrafficMultiplier: proxy.TrafficMultiplier,
		SettingsJson:      proxy.SettingsJSON,
		InboundRulesJson:  proxy.InboundRulesJSON,
		OutboundRulesJson: proxy.OutboundRulesJSON,
		RouteRulesJson:    proxy.RouteRulesJSON,
		NodeID:            node.ID,
		Name:              proxy.Name,
	})
	if err != nil {
		return Proxy{}, err
	}
	if err := requireAffected(affected, "proxy", proxy.Name+"@"+node.Name); err != nil {
		return Proxy{}, err
	}
	return db.GetProxy(ctx, node.Name, proxy.Name)
}

func (db *DB) validateProxyListener(ctx context.Context, nodeName string, next Proxy, ignoreID string) error {
	proxies, err := db.ListProxies(ctx, nodeName)
	if err != nil {
		return err
	}
	for _, existing := range proxies {
		if existing.ID == ignoreID {
			continue
		}
		if existing.Listen != next.Listen || existing.ListenPort != next.ListenPort {
			continue
		}
		if !transportsOverlap(existing.Transport, next.Transport) {
			continue
		}
		if compatibleSharedListener(existing, next) {
			continue
		}
		return fmt.Errorf("proxy %q conflicts with %q on %s:%d/%s", next.Name, existing.Name, next.Listen, next.ListenPort, next.Transport)
	}
	return nil
}

func compatibleSharedListener(existing, next Proxy) bool {
	return proxySupportsMultiUser(existing.Protocol) &&
		existing.Protocol == next.Protocol &&
		existing.Transport == next.Transport &&
		existing.SettingsJSON == next.SettingsJSON
}

func proxySupportsMultiUser(protocol string) bool {
	switch protocol {
	case ProtocolVLESSReality, ProtocolHysteria2:
		return true
	default:
		return false
	}
}

func transportsOverlap(left, right string) bool {
	if left == TransportTCPUDP || right == TransportTCPUDP {
		return true
	}
	return left == right
}

func (db *DB) ListProxies(ctx context.Context, nodeName string) ([]Proxy, error) {
	if nodeName != "" {
		rows, err := db.q.ListProxiesByNodeName(ctx, normalizeName(nodeName))
		if err != nil {
			return nil, err
		}
		out := make([]Proxy, 0, len(rows))
		for _, row := range rows {
			out = append(out, proxyFromDetail(row))
		}
		return out, nil
	}
	rows, err := db.q.ListProxies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Proxy, 0, len(rows))
	for _, row := range rows {
		out = append(out, proxyFromDetail(row))
	}
	return out, nil
}

func (db *DB) GetProxy(ctx context.Context, nodeName, name string) (Proxy, error) {
	row, err := db.q.GetProxyByNodeAndName(ctx, store.GetProxyByNodeAndNameParams{
		NodeName: normalizeName(nodeName),
		Name:     normalizeName(name),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Proxy{}, fmt.Errorf("proxy %q on node %q not found", name, nodeName)
		}
		return Proxy{}, err
	}
	return proxyFromDetail(row), nil
}

func (db *DB) SetProxyEnabled(ctx context.Context, nodeName, name string, enabled bool) error {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	affected, err := db.q.SetProxyEnabled(ctx, store.SetProxyEnabledParams{
		Enabled: boolToInt64(enabled),
		NodeID:  node.ID,
		Name:    normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy", name+"@"+nodeName)
}

func (db *DB) DisableProxy(ctx context.Context, nodeName, name string) (Proxy, error) {
	if err := db.SetProxyEnabled(ctx, nodeName, name, false); err != nil {
		return Proxy{}, err
	}
	return db.GetProxy(ctx, nodeName, name)
}

func normalizeProxyParams(params CreateProxyParams) (Proxy, error) {
	name := normalizeName(params.Name)
	if name == "" {
		return Proxy{}, errors.New("proxy name is required")
	}
	if err := validateNameForAuth(name, "proxy"); err != nil {
		return Proxy{}, err
	}
	protocol := strings.TrimSpace(params.Protocol)
	if !validProxyProtocol(protocol) {
		return Proxy{}, fmt.Errorf("unsupported proxy protocol %q", protocol)
	}
	if params.ListenPort <= 0 || params.ListenPort > 65535 {
		return Proxy{}, errors.New("listen port must be between 1 and 65535")
	}
	listen := strings.TrimSpace(params.Listen)
	if listen == "" {
		listen = "::"
	}
	transport := strings.TrimSpace(params.Transport)
	if transport == "" {
		transport = defaultTransport(protocol)
	}
	if !validProxyTransport(transport) {
		return Proxy{}, fmt.Errorf("unsupported proxy transport %q", transport)
	}
	multiplier := params.TrafficMultiplier
	if multiplier == 0 {
		multiplier = 1.0
	}
	if multiplier < 0 {
		return Proxy{}, errors.New("traffic multiplier must be non-negative")
	}
	settingsJSON, err := validJSONOrDefault(params.SettingsJSON, "{}", "settings_json")
	if err != nil {
		return Proxy{}, err
	}
	inboundRulesJSON, err := validJSONOrDefault(params.InboundRulesJSON, "[]", "inbound_rules_json")
	if err != nil {
		return Proxy{}, err
	}
	outboundRulesJSON, err := validJSONOrDefault(params.OutboundRulesJSON, "[]", "outbound_rules_json")
	if err != nil {
		return Proxy{}, err
	}
	routeRulesJSON, err := validJSONOrDefault(params.RouteRulesJSON, "[]", "route_rules_json")
	if err != nil {
		return Proxy{}, err
	}
	return Proxy{
		Name:              name,
		Protocol:          protocol,
		Listen:            listen,
		ListenPort:        params.ListenPort,
		Transport:         transport,
		Enabled:           params.Enabled,
		TrafficMultiplier: multiplier,
		SettingsJSON:      settingsJSON,
		InboundRulesJSON:  inboundRulesJSON,
		OutboundRulesJSON: outboundRulesJSON,
		RouteRulesJSON:    routeRulesJSON,
	}, nil
}

func validJSONOrDefault(raw, fallback, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = fallback
	}
	if !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("%s must be valid JSON", field)
	}
	return raw, nil
}

func validProxyProtocol(protocol string) bool {
	switch protocol {
	case ProtocolVLESSReality, ProtocolShadowsocks2022, ProtocolHysteria2:
		return true
	default:
		return false
	}
}

func validProxyTransport(transport string) bool {
	switch transport {
	case TransportTCP, TransportUDP, TransportTCPUDP:
		return true
	default:
		return false
	}
}

func defaultTransport(protocol string) string {
	switch protocol {
	case ProtocolHysteria2:
		return TransportUDP
	case ProtocolShadowsocks2022:
		return TransportTCPUDP
	default:
		return TransportTCP
	}
}

func proxyFromDetail(row store.ProxyDetail) Proxy {
	return Proxy{
		ID:                row.ID,
		NodeID:            row.NodeID,
		NodeName:          row.NodeName,
		NodePublicHost:    row.NodePublicHost,
		Name:              row.Name,
		Protocol:          row.Protocol,
		Listen:            row.Listen,
		ListenPort:        int(row.ListenPort),
		Transport:         row.Transport,
		Enabled:           int64ToBool(row.Enabled),
		TrafficMultiplier: row.TrafficMultiplier,
		SettingsJSON:      row.SettingsJson,
		InboundRulesJSON:  row.InboundRulesJson,
		OutboundRulesJSON: row.OutboundRulesJson,
		RouteRulesJSON:    row.RouteRulesJson,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
