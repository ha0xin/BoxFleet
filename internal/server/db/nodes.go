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

// NodeHost is one reachable address for a node (domain, IPv4, or IPv6). Selected
// hosts each produce a client connection profile; the first host is the primary
// and is mirrored into the node's public_host column.
type NodeHost struct {
	Host     string `json:"host"`
	Selected bool   `json:"selected"`
}

type Node struct {
	ID             string
	Name           string
	PublicHost     string
	Hosts          []NodeHost
	APIBaseURL     string
	Status         string
	SingBoxVersion string
	LastSeenAt     sql.NullString
	CreatedAt      string
	UpdatedAt      string
}

type UpdateNodeParams struct {
	Name       string
	PublicHost string
	// Hosts, when non-empty, replaces the node's full host list. Callers that
	// only deal with a single address may leave it nil and set PublicHost.
	Hosts      []NodeHost
	APIBaseURL string
	Status     string
}

// normalizeNodeHosts trims, drops empty/duplicate hosts (keeping first), and
// guarantees a usable list: at least one host, with the first selected when
// nothing else is. It errors only when no non-empty host remains.
func normalizeNodeHosts(hosts []NodeHost) ([]NodeHost, error) {
	seen := make(map[string]bool, len(hosts))
	out := make([]NodeHost, 0, len(hosts))
	for _, h := range hosts {
		host := strings.TrimSpace(h.Host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, NodeHost{Host: host, Selected: h.Selected})
	}
	if len(out) == 0 {
		return nil, errors.New("node public host is required")
	}
	anySelected := false
	for _, h := range out {
		if h.Selected {
			anySelected = true
			break
		}
	}
	if !anySelected {
		out[0].Selected = true
	}
	return out, nil
}

func encodeNodeHosts(hosts []NodeHost) (string, error) {
	raw, err := json.Marshal(hosts)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// parseNodeHosts decodes the stored hosts_json, falling back to the legacy
// single public_host for rows written before multi-host support.
func parseNodeHosts(raw, fallbackHost string) []NodeHost {
	if trimmed := strings.TrimSpace(raw); trimmed != "" && trimmed != "[]" {
		var hosts []NodeHost
		if err := json.Unmarshal([]byte(trimmed), &hosts); err == nil {
			if normalized, err := normalizeNodeHosts(hosts); err == nil {
				return normalized
			}
		}
	}
	if fallbackHost = strings.TrimSpace(fallbackHost); fallbackHost != "" {
		return []NodeHost{{Host: fallbackHost, Selected: true}}
	}
	return nil
}

type NodeFilter struct {
	Search    string
	Status    string
	Sort      string
	Direction string
	Limit     int64
	Offset    int64
}

type NodePage struct {
	Nodes  []Node
	Total  int64
	Limit  int64
	Offset int64
}

func (db *DB) CreateNode(ctx context.Context, name, publicHost, apiBaseURL string) (Node, error) {
	name = normalizeName(name)
	publicHost = strings.TrimSpace(publicHost)
	if name == "" {
		return Node{}, errors.New("node name is required")
	}
	if publicHost == "" {
		return Node{}, errors.New("node public host is required")
	}
	nodeID, err := id.New("node")
	if err != nil {
		return Node{}, err
	}
	// A node is created with a single primary host; additional hosts are added
	// later via UpdateNode. Keep hosts_json in sync with public_host from the start.
	hostsJSON, err := encodeNodeHosts([]NodeHost{{Host: publicHost, Selected: true}})
	if err != nil {
		return Node{}, err
	}
	err = db.q.CreateNode(ctx, store.CreateNodeParams{
		ID:         nodeID,
		Name:       name,
		PublicHost: publicHost,
		HostsJson:  hostsJSON,
		ApiBaseUrl: strings.TrimSpace(apiBaseURL),
	})
	if err != nil {
		return Node{}, err
	}
	return db.GetNode(ctx, name)
}

func (db *DB) UpdateNode(ctx context.Context, params UpdateNodeParams) (Node, error) {
	name := normalizeName(params.Name)
	status := strings.TrimSpace(params.Status)
	if name == "" {
		return Node{}, errors.New("node name is required")
	}
	if !validNodeStatus(status) {
		return Node{}, fmt.Errorf("unsupported node status %q", status)
	}
	// Hosts is the source of truth when provided; single-host callers may leave it
	// nil and set PublicHost instead. A non-nil but empty Hosts is an explicit
	// "replace with no hosts" — it must NOT fall back to PublicHost, so that an
	// empty host-list replacement is rejected by normalizeNodeHosts rather than
	// silently collapsing the node back to its primary host. nil != empty in Go.
	hosts := params.Hosts
	if hosts == nil {
		hosts = []NodeHost{{Host: strings.TrimSpace(params.PublicHost), Selected: true}}
	}
	normalized, err := normalizeNodeHosts(hosts)
	if err != nil {
		return Node{}, err
	}
	hostsJSON, err := encodeNodeHosts(normalized)
	if err != nil {
		return Node{}, err
	}
	affected, err := db.q.UpdateNode(ctx, store.UpdateNodeParams{
		PublicHost: normalized[0].Host,
		HostsJson:  hostsJSON,
		ApiBaseUrl: strings.TrimSpace(params.APIBaseURL),
		Status:     status,
		Name:       name,
	})
	if err != nil {
		return Node{}, err
	}
	if err := requireAffected(affected, "node", name); err != nil {
		return Node{}, err
	}
	return db.GetNode(ctx, name)
}

func (db *DB) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := db.q.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]Node, 0, len(rows))
	for _, row := range rows {
		nodes = append(nodes, mapNode(row))
	}
	return nodes, nil
}

func (db *DB) ListNodesPage(ctx context.Context, filter NodeFilter) (NodePage, error) {
	limit := pageLimit(filter.Limit, 50)
	offset := pageOffset(filter.Offset)
	where, args := nodePageWhere(filter)
	whereSQL := strings.Join(where, " AND ")
	var total int64
	countQuery := `
SELECT COUNT(*)
FROM nodes n
WHERE ` + whereSQL
	if err := db.sql.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return NodePage{}, err
	}
	sortSQL := nodePageSort(filter.Sort, filter.Direction)
	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)
	listQuery := `
SELECT
  n.id,
  n.name,
  n.public_host,
  n.hosts_json,
  n.api_base_url,
  n.status,
  n.sing_box_version,
  n.last_seen_at,
  n.created_at,
  n.updated_at
FROM nodes n
WHERE ` + whereSQL + `
ORDER BY ` + sortSQL + `
LIMIT ?
OFFSET ?`
	rows, err := db.sql.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return NodePage{}, err
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		var hostsJSON string
		if err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.PublicHost,
			&hostsJSON,
			&node.APIBaseURL,
			&node.Status,
			&node.SingBoxVersion,
			&node.LastSeenAt,
			&node.CreatedAt,
			&node.UpdatedAt,
		); err != nil {
			return NodePage{}, err
		}
		node.Hosts = parseNodeHosts(hostsJSON, node.PublicHost)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return NodePage{}, err
	}
	return NodePage{
		Nodes:  nodes,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func nodePageWhere(filter NodeFilter) ([]string, []any) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 2)
	if status := strings.TrimSpace(filter.Status); status != "" {
		where = append(where, "n.status = ?")
		args = append(args, status)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		where = append(where, `(LOWER(n.name) LIKE ? OR LOWER(n.public_host) LIKE ? OR LOWER(n.api_base_url) LIKE ? OR LOWER(n.status) LIKE ? OR LOWER(n.sing_box_version) LIKE ?)`)
		args = append(args, like, like, like, like, like)
	}
	return where, args
}

func nodePageSort(sort, direction string) string {
	dir := "ASC"
	if strings.EqualFold(direction, "desc") {
		dir = "DESC"
	}
	sortColumn := "n.name"
	switch strings.TrimSpace(sort) {
	case "status":
		sortColumn = "n.status"
	case "public_host":
		sortColumn = "n.public_host"
	case "last_seen_at":
		sortColumn = "COALESCE(n.last_seen_at, '')"
	case "sing_box_version":
		sortColumn = "n.sing_box_version"
	case "created_at":
		sortColumn = "n.created_at"
	case "updated_at":
		sortColumn = "n.updated_at"
	}
	return sortColumn + " " + dir + ", n.name ASC"
}

func (db *DB) GetNode(ctx context.Context, name string) (Node, error) {
	node, err := db.q.GetNodeByName(ctx, normalizeName(name))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Node{}, fmt.Errorf("node %q not found", name)
		}
		return Node{}, err
	}
	return mapNode(node), nil
}

func (db *DB) SetNodeStatus(ctx context.Context, name, status string) error {
	if !validNodeStatus(status) {
		return fmt.Errorf("unsupported node status %q", status)
	}
	affected, err := db.q.SetNodeStatus(ctx, store.SetNodeStatusParams{
		Status: status,
		Name:   normalizeName(name),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "node", name)
}

func (db *DB) DisableNode(ctx context.Context, name string) (Node, error) {
	if err := db.SetNodeStatus(ctx, name, "disabled"); err != nil {
		return Node{}, err
	}
	if err := db.RevokeNodeTokens(ctx, name); err != nil {
		return Node{}, err
	}
	return db.GetNode(ctx, name)
}

func validNodeStatus(status string) bool {
	switch status {
	case "pending", "active", "disabled", "degraded":
		return true
	default:
		return false
	}
}

func mapNode(row store.Node) Node {
	return Node{
		ID:             row.ID,
		Name:           row.Name,
		PublicHost:     row.PublicHost,
		Hosts:          parseNodeHosts(row.HostsJson, row.PublicHost),
		APIBaseURL:     row.ApiBaseUrl,
		Status:         row.Status,
		SingBoxVersion: row.SingBoxVersion,
		LastSeenAt:     row.LastSeenAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
