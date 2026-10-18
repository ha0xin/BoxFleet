package db

import (
	"context"
	"fmt"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type PathSelectionChange struct {
	PathID  string `json:"path_id"`
	Enabled bool   `json:"enabled"`
}

// ChangeUserPathSelection applies one or many checkbox changes atomically.
// Only the specified Paths change. Groups are selection shortcuts, not sources.
func (db *DB) ChangeUserPathSelection(ctx context.Context, userID string, changes []PathSelectionChange) error {
	if len(changes) == 0 || len(changes) > MaxPathGroupMembers {
		return fmt.Errorf("select between 1 and %d Paths", MaxPathGroupMembers)
	}
	return db.atomic(ctx, func(tx *DB) error {
		user, err := tx.GetProxyUser(ctx, userID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, c := range changes {
			if seen[c.PathID] {
				return fmt.Errorf("duplicate Path %q", c.PathID)
			}
			seen[c.PathID] = true
			p, err := tx.GetPath(ctx, c.PathID)
			if err != nil {
				return err
			}
			if p.Visibility != PathVisibilitySelectable {
				return fmt.Errorf("Path is dependency-only")
			}
			if c.Enabled && !p.Enabled {
				return fmt.Errorf("Path is disabled")
			}
		}
		// Also flatten legacy sources for callers using an older internal integration.
		desired, err := tx.q.ListUserDesiredPathIDs(ctx, user.ID)
		if err != nil {
			return err
		}
		for _, pid := range desired {
			if err = tx.q.SetDirectPathGrant(ctx, store.SetDirectPathGrantParams{ProxyUserID: user.ID, PathID: pid}); err != nil {
				return err
			}
		}
		if err = tx.q.DeleteUserPathGroupGrants(ctx, user.ID); err != nil {
			return err
		}
		for _, c := range changes {
			if c.Enabled {
				err = tx.q.SetDirectPathGrant(ctx, store.SetDirectPathGrantParams{ProxyUserID: user.ID, PathID: c.PathID})
			} else {
				err = tx.q.DeleteDirectPathGrant(ctx, store.DeleteDirectPathGrantParams{ProxyUserID: user.ID, PathID: c.PathID})
			}
			if err != nil {
				return err
			}
		}
		return tx.reconcilePathGrants(ctx, user.ID)
	})
}
