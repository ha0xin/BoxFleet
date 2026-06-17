package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type Node struct {
	ID             string
	Name           string
	PublicHost     string
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
	APIBaseURL string
	Status     string
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
	err = db.q.CreateNode(ctx, store.CreateNodeParams{
		ID:         nodeID,
		Name:       name,
		PublicHost: publicHost,
		ApiBaseUrl: strings.TrimSpace(apiBaseURL),
	})
	if err != nil {
		return Node{}, err
	}
	return db.GetNode(ctx, name)
}

func (db *DB) UpdateNode(ctx context.Context, params UpdateNodeParams) (Node, error) {
	name := normalizeName(params.Name)
	publicHost := strings.TrimSpace(params.PublicHost)
	status := strings.TrimSpace(params.Status)
	if name == "" {
		return Node{}, errors.New("node name is required")
	}
	if publicHost == "" {
		return Node{}, errors.New("node public host is required")
	}
	if !validNodeStatus(status) {
		return Node{}, fmt.Errorf("unsupported node status %q", status)
	}
	affected, err := db.q.UpdateNode(ctx, store.UpdateNodeParams{
		PublicHost: publicHost,
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
		if err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.PublicHost,
			&node.APIBaseURL,
			&node.Status,
			&node.SingBoxVersion,
			&node.LastSeenAt,
			&node.CreatedAt,
			&node.UpdatedAt,
		); err != nil {
			return NodePage{}, err
		}
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
		APIBaseURL:     row.ApiBaseUrl,
		Status:         row.Status,
		SingBoxVersion: row.SingBoxVersion,
		LastSeenAt:     row.LastSeenAt,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
