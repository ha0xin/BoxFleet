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
