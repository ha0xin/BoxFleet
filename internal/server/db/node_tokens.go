package db

import (
	"context"
	"fmt"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
	"github.com/haoxin/boxfleet/internal/token"
)

type IssuedNodeToken struct {
	NodeName string
	Token    string
}

func (db *DB) IssueNodeToken(ctx context.Context, nodeName string) (IssuedNodeToken, error) {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return IssuedNodeToken{}, err
	}
	if node.Status == "disabled" {
		return IssuedNodeToken{}, fmt.Errorf("node %q is disabled", node.Name)
	}
	rawToken, err := token.New()
	if err != nil {
		return IssuedNodeToken{}, err
	}
	hashedToken, err := token.Hash(rawToken)
	if err != nil {
		return IssuedNodeToken{}, err
	}
	tokenID, err := id.New("ntok")
	if err != nil {
		return IssuedNodeToken{}, err
	}
	if err := db.q.CreateNodeToken(ctx, store.CreateNodeTokenParams{
		ID:        tokenID,
		NodeID:    node.ID,
		TokenHash: hashedToken,
	}); err != nil {
		return IssuedNodeToken{}, err
	}
	return IssuedNodeToken{NodeName: node.Name, Token: rawToken}, nil
}

func (db *DB) VerifyNodeToken(ctx context.Context, nodeName, rawToken string) (bool, error) {
	rows, err := db.q.ListActiveNodeTokensByNodeName(ctx, normalizeName(nodeName))
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if token.Verify(row.TokenHash, rawToken) {
			if err := db.q.MarkNodeTokenUsed(ctx, row.ID); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func (db *DB) RevokeNodeTokens(ctx context.Context, nodeName string) error {
	node, err := db.GetNode(ctx, nodeName)
	if err != nil {
		return err
	}
	return db.q.RevokeNodeTokensByNodeID(ctx, node.ID)
}
