package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
	"github.com/haoxin/boxfleet/internal/token"
)

const legacyNodeTokenVerifyLimit = 3

type IssuedNodeToken struct {
	NodeName string
	Token    string
}

// ListNodeNamesWithActiveTokens returns the names of nodes that currently hold a
// non-revoked token. A disabled node still in this set was paused; one absent
// from it was decommissioned (its tokens were revoked).
func (db *DB) ListNodeNamesWithActiveTokens(ctx context.Context) ([]string, error) {
	return db.q.ListNodeNamesWithActiveTokens(ctx)
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
	tokenDigest, ok := token.Digest(rawToken)
	if !ok {
		return IssuedNodeToken{}, fmt.Errorf("node token is missing digest prefix")
	}
	tokenID, err := id.New("ntok")
	if err != nil {
		return IssuedNodeToken{}, err
	}
	if err := db.q.CreateNodeToken(ctx, store.CreateNodeTokenParams{
		ID:          tokenID,
		NodeID:      node.ID,
		TokenHash:   hashedToken,
		TokenDigest: sql.NullString{String: tokenDigest, Valid: true},
	}); err != nil {
		return IssuedNodeToken{}, err
	}
	return IssuedNodeToken{NodeName: node.Name, Token: rawToken}, nil
}

func (db *DB) VerifyNodeToken(ctx context.Context, nodeName, rawToken string) (bool, error) {
	if tokenDigest, ok := token.Digest(rawToken); ok {
		row, err := db.q.GetActiveNodeTokenByDigest(ctx, store.GetActiveNodeTokenByDigestParams{
			NodeName:    normalizeName(nodeName),
			TokenDigest: sql.NullString{String: tokenDigest, Valid: true},
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		if !token.Verify(row.TokenHash, rawToken) {
			return false, nil
		}
		if err := db.q.MarkNodeTokenUsed(ctx, row.ID); err != nil {
			return false, err
		}
		return true, nil
	}

	rows, err := db.q.ListActiveNodeTokensByNodeName(ctx, normalizeName(nodeName))
	if err != nil {
		return false, err
	}
	checkedLegacy := 0
	for _, row := range rows {
		if row.TokenDigest.Valid {
			continue
		}
		if checkedLegacy >= legacyNodeTokenVerifyLimit {
			break
		}
		checkedLegacy++
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
