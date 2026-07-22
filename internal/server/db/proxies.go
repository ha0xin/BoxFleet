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
	DeletedAt         sql.NullString
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

type ProxyFilter struct {
	Search    string
	NodeName  string
	Enabled   string
	Deleted   string
	Sort      string
	Direction string
	Limit     int64
	Offset    int64
}

type ProxyPage struct {
	Proxies []Proxy
	Total   int64
	Limit   int64
	Offset  int64
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
	if _, err := db.q.GetProxyIDByNameOrAlias(ctx, proxy.Name); err == nil {
		return Proxy{}, fmt.Errorf("proxy name %q is already in use", proxy.Name)
	} else if !errors.Is(err, sql.ErrNoRows) {
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
	currentName := params.Name
	params.Name = existing.Name
	return db.UpdateProxyByName(ctx, node.Name, currentName, params)
}

// UpdateProxyByName atomically updates a proxy selected by currentName.
// Params.Name is the desired globally unique canonical name and may rename the
// proxy in the same transaction.
func (db *DB) UpdateProxyByName(ctx context.Context, nodeName, currentName string, params UpdateProxyParams) (Proxy, error) {
	lookupNodeName := normalizeName(nodeName)
	if lookupNodeName == "" {
		lookupNodeName = normalizeName(params.NodeName)
	}
	if lookupNodeName == "" {
		return Proxy{}, errors.New("node name is required")
	}
	node, err := db.GetNode(ctx, lookupNodeName)
	if err != nil {
		return Proxy{}, err
	}
	currentName = normalizeName(currentName)
	if currentName == "" {
		return Proxy{}, errors.New("current proxy name is required")
	}
	existing, err := db.GetProxy(ctx, node.Name, currentName)
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
	err = db.withTx(ctx, func(qtx *store.Queries) error {
		current, err := qtx.GetProxyByNodeAndName(ctx, store.GetProxyByNodeAndNameParams{
			NodeName: node.Name,
			Name:     currentName,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("proxy %q on node %q not found", currentName, node.Name)
			}
			return err
		}
		if current.ID != existing.ID {
			return fmt.Errorf("proxy %q changed while it was being updated", currentName)
		}
		if err := renameProxyTx(ctx, qtx, existing.ID, existing.Name, proxy.Name); err != nil {
			return err
		}
		affected, err := qtx.UpdateProxy(ctx, store.UpdateProxyParams{
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
			return err
		}
		return requireAffected(affected, "proxy", currentName+"@"+node.Name)
	})
	if err != nil {
		return Proxy{}, err
	}
	return db.GetProxy(ctx, node.Name, proxy.Name)
}

func renameProxyTx(ctx context.Context, qtx *store.Queries, proxyID, currentName, newName string) error {
	if currentName == newName {
		return nil
	}
	ownerID, err := qtx.GetProxyIDByNameOrAlias(ctx, newName)
	switch {
	case err == nil && ownerID != proxyID:
		return fmt.Errorf("proxy name %q is already in use", newName)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	case err == nil:
		if err := qtx.DeleteProxyNameAlias(ctx, store.DeleteProxyNameAliasParams{
			Alias:   newName,
			ProxyID: proxyID,
		}); err != nil {
			return err
		}
	}
	if err := qtx.CreateProxyNameAlias(ctx, store.CreateProxyNameAliasParams{
		Alias:   currentName,
		ProxyID: proxyID,
	}); err != nil {
		return err
	}
	affected, err := qtx.RenameProxyByID(ctx, store.RenameProxyByIDParams{
		Name: newName,
		ID:   proxyID,
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "proxy", currentName)
}

// RenameProxy changes a proxy's globally unique canonical name while preserving
// its prior name as an alias. Access auth names and credentials are ID-based and
// are deliberately left untouched.
func (db *DB) RenameProxy(ctx context.Context, nodeName, oldName, newName string) (Proxy, error) {
	nodeName = normalizeName(nodeName)
	oldName = normalizeName(oldName)
	newName = normalizeName(newName)
	if oldName == "" {
		return Proxy{}, errors.New("proxy name is required")
	}
	if newName == "" {
		return Proxy{}, errors.New("new proxy name is required")
	}
	if err := validateNameForAuth(newName, "proxy"); err != nil {
		return Proxy{}, err
	}
	var proxyID string
	err := db.withTx(ctx, func(qtx *store.Queries) error {
		existing, err := qtx.GetProxyByNodeAndName(ctx, store.GetProxyByNodeAndNameParams{
			NodeName: nodeName,
			Name:     oldName,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("proxy %q on node %q not found", oldName, nodeName)
			}
			return err
		}
		proxyID = existing.ID
		if existing.Name == newName {
			return nil
		}
		return renameProxyTx(ctx, qtx, existing.ID, existing.Name, newName)
	})
	if err != nil {
		return Proxy{}, err
	}
	proxy, err := db.GetProxy(ctx, nodeName, newName)
	if err != nil {
		return Proxy{}, err
	}
	if proxy.ID != proxyID {
		return Proxy{}, fmt.Errorf("renamed proxy %q resolved to an unexpected record", newName)
	}
	return proxy, nil
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
		node, err := db.GetNode(ctx, nodeName)
		if err != nil {
			return nil, err
		}
		rows, err := db.q.ListProxiesByNodeName(ctx, node.Name)
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

func (db *DB) ListProxiesPage(ctx context.Context, filter ProxyFilter) (ProxyPage, error) {
	if strings.TrimSpace(filter.NodeName) != "" {
		node, err := db.GetNode(ctx, filter.NodeName)
		if err != nil {
			return ProxyPage{}, err
		}
		filter.NodeName = node.Name
	}
	limit := pageLimit(filter.Limit, 50)
	offset := pageOffset(filter.Offset)
	where, args := proxyPageWhere(filter)
	whereSQL := strings.Join(where, " AND ")
	var total int64
	countQuery := `
SELECT COUNT(*)
FROM proxy_details p
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return ProxyPage{}, err
	}
	sortSQL := proxyPageSort(filter.Sort, filter.Direction)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)
	listQuery := `
SELECT
  p.id,
  p.node_id,
  p.node_name,
  p.node_public_host,
  p.name,
  p.protocol,
  p.listen,
  p.listen_port,
  p.transport,
  p.enabled,
  p.traffic_multiplier,
  p.settings_json,
  p.inbound_rules_json,
  p.outbound_rules_json,
  p.route_rules_json,
  p.deleted_at,
  p.created_at,
  p.updated_at
FROM proxy_details p
WHERE ` + whereSQL + `
ORDER BY ` + sortSQL + `
LIMIT ?
OFFSET ?`
	rows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return ProxyPage{}, err
	}
	defer rows.Close()
	proxies := make([]Proxy, 0)
	for rows.Next() {
		var proxy Proxy
		var enabled int64
		if err := rows.Scan(
			&proxy.ID,
			&proxy.NodeID,
			&proxy.NodeName,
			&proxy.NodePublicHost,
			&proxy.Name,
			&proxy.Protocol,
			&proxy.Listen,
			&proxy.ListenPort,
			&proxy.Transport,
			&enabled,
			&proxy.TrafficMultiplier,
			&proxy.SettingsJSON,
			&proxy.InboundRulesJSON,
			&proxy.OutboundRulesJSON,
			&proxy.RouteRulesJSON,
			&proxy.DeletedAt,
			&proxy.CreatedAt,
			&proxy.UpdatedAt,
		); err != nil {
			return ProxyPage{}, err
		}
		proxy.Enabled = int64ToBool(enabled)
		proxies = append(proxies, proxy)
	}
	if err := rows.Err(); err != nil {
		return ProxyPage{}, err
	}
	return ProxyPage{
		Proxies: proxies,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
	}, nil
}

func proxyPageWhere(filter ProxyFilter) ([]string, []any) {
	where := []string{"p.deleted_at IS NULL", "p.node_deleted_at IS NULL"}
	args := make([]any, 0, 3)
	if strings.EqualFold(strings.TrimSpace(filter.Deleted), "only") {
		where[0] = "p.deleted_at IS NOT NULL"
	}
	if nodeName := normalizeName(filter.NodeName); nodeName != "" {
		where = append(where, "p.node_name = ?")
		args = append(args, nodeName)
	}
	switch strings.TrimSpace(filter.Enabled) {
	case "true", "1", "enabled":
		where = append(where, "p.enabled = 1")
	case "false", "0", "disabled":
		where = append(where, "p.enabled = 0")
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, `(LOWER(p.name) LIKE ? OR LOWER(p.node_name) LIKE ? OR LOWER(p.protocol) LIKE ? OR LOWER(p.listen) LIKE ? OR CAST(p.listen_port AS TEXT) LIKE ? OR LOWER(p.transport) LIKE ?)`)
		args = append(args, like, like, like, like, like, like)
	}
	return where, args
}

func proxyPageSort(sort, direction string) string {
	dir := "ASC"
	if strings.EqualFold(direction, "desc") {
		dir = "DESC"
	}
	sortColumn := "p.node_name"
	switch strings.TrimSpace(sort) {
	case "name":
		sortColumn = "p.name"
	case "protocol":
		sortColumn = "p.protocol"
	case "listen_port":
		sortColumn = "p.listen_port"
	case "enabled":
		sortColumn = "p.enabled"
	case "traffic_multiplier":
		sortColumn = "p.traffic_multiplier"
	case "created_at":
		sortColumn = "p.created_at"
	case "updated_at":
		sortColumn = "p.updated_at"
	}
	return sortColumn + " " + dir + ", p.node_name ASC, p.listen_port ASC, p.name ASC"
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
	proxy, err := db.GetProxy(ctx, node.Name, name)
	if err != nil {
		return err
	}
	affected, err := db.q.SetProxyEnabled(ctx, store.SetProxyEnabledParams{
		Enabled: boolToInt64(enabled),
		NodeID:  node.ID,
		Name:    proxy.Name,
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

func (db *DB) SoftDeleteProxy(ctx context.Context, nodeName, name string) (Proxy, error) {
	proxy, err := db.GetProxy(ctx, nodeName, name)
	if err != nil {
		return Proxy{}, err
	}
	affected, err := db.q.SoftDeleteProxy(ctx, proxy.ID)
	if err != nil {
		return Proxy{}, err
	}
	if err := requireAffected(affected, "proxy", name+"@"+nodeName); err != nil {
		return Proxy{}, err
	}
	return db.getProxyIncludingDeleted(ctx, nodeName, proxy.Name)
}

func (db *DB) RestoreProxy(ctx context.Context, nodeName, name string) (Proxy, error) {
	proxy, err := db.getProxyIncludingDeleted(ctx, nodeName, name)
	if err != nil {
		return Proxy{}, err
	}
	affected, err := db.q.RestoreProxy(ctx, proxy.ID)
	if err != nil {
		return Proxy{}, err
	}
	if err := requireAffected(affected, "deleted proxy", name+"@"+nodeName); err != nil {
		return Proxy{}, err
	}
	return db.GetProxy(ctx, nodeName, proxy.Name)
}

func (db *DB) getProxyIncludingDeleted(ctx context.Context, nodeName, name string) (Proxy, error) {
	row, err := db.q.GetProxyByNodeAndNameIncludingDeleted(ctx, store.GetProxyByNodeAndNameIncludingDeletedParams{
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
	if protocol == ProtocolVLESSReality {
		settingsJSON, err = normalizeVLESSRealitySettingsJSON(settingsJSON)
		if err != nil {
			return Proxy{}, err
		}
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
		DeletedAt:         row.DeletedAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
