package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/haoxin/boxfleet/internal/id"
	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

const MaxPathGroupMembers = 200
const MaxPathGroupUsers = 500
const MaxPathGroupAccesses = 5000

type PathGroup struct {
	SelectedUserCount int64    `json:"selected_user_count"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Enabled           bool     `json:"enabled"`
	Revision          int64    `json:"revision"`
	PathIDs           []string `json:"path_ids"`
	UserIDs           []string `json:"user_ids"`
}

// PathGroupChange is also the canonical input to the impact preview. Preview
// tokens bind the exact operation to all authorization state, not just the group.
type PathGroupChange struct {
	Operation    string   `json:"operation"` // save, delete, grant, revoke
	GroupID      string   `json:"group_id"`
	UserID       string   `json:"user_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Enabled      bool     `json:"enabled"`
	Revision     int64    `json:"revision"`
	PathIDs      []string `json:"path_ids"`
	PreviewToken string   `json:"preview_token"`
}
type PathGroupUserImpact struct {
	UserID   string   `json:"user_id"`
	UserName string   `json:"user_name"`
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Retained []string `json:"retained"`
}
type PathGroupImpact struct {
	PreviewToken string                `json:"preview_token"`
	GroupID      string                `json:"group_id"`
	Users        []PathGroupUserImpact `json:"users"`
	NodeIDs      []string              `json:"node_ids"`
}
type PathAccessSources struct {
	Direct bool           `json:"direct"`
	Groups []PathGroupRef `json:"groups"`
}

type PathGroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PathGroupHistory struct {
	ID        string          `json:"id"`
	CreatedAt string          `json:"created_at"`
	Change    json.RawMessage `json:"change"`
	Impact    json.RawMessage `json:"impact"`
}

func (db *DB) ListPathGroupHistory(ctx context.Context, groupID string) ([]PathGroupHistory, error) {
	rows, err := db.q.ListPathGroupChanges(ctx, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]PathGroupHistory, 0, len(rows))
	for _, r := range rows {
		out = append(out, PathGroupHistory{ID: r.ID, CreatedAt: r.CreatedAt, Change: json.RawMessage(r.ChangeJson), Impact: json.RawMessage(r.ImpactJson)})
	}
	return out, nil
}

func (db *DB) ListPathGroups(ctx context.Context) ([]PathGroup, error) {
	rows, err := db.q.ListPathGroups(ctx)
	if err != nil {
		return nil, err
	}
	members, err := db.q.ListPathGroupMembers(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := db.q.ListPathGroupGrants(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := db.q.ListPathGroupSelectionCounts(ctx)
	if err != nil {
		return nil, err
	}
	selectedCounts := map[string]int64{}
	for _, c := range counts {
		selectedCounts[c.GroupID] = c.SelectedUserCount
	}
	out := make([]PathGroup, 0, len(rows))
	membersByGroup := map[string][]string{}
	usersByGroup := map[string][]string{}
	for _, m := range members {
		membersByGroup[m.GroupID] = append(membersByGroup[m.GroupID], m.PathID)
	}
	for _, g := range grants {
		usersByGroup[g.GroupID] = append(usersByGroup[g.GroupID], g.ProxyUserID)
	}
	for _, r := range rows {
		g := PathGroup{SelectedUserCount: selectedCounts[r.ID], ID: r.ID, Name: r.Name, Description: r.Description, Enabled: r.Enabled == 1, Revision: r.Revision, PathIDs: []string{}, UserIDs: []string{}}
		g.PathIDs = append(g.PathIDs, membersByGroup[g.ID]...)
		g.UserIDs = append(g.UserIDs, usersByGroup[g.ID]...)
		out = append(out, g)
	}
	return out, nil
}

func (db *DB) UserPathSources(ctx context.Context, userID string) (map[string]PathAccessSources, error) {
	user, err := db.GetProxyUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	direct, err := db.q.ListUserDirectPathGrants(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	groups, err := db.ListPathGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]PathAccessSources{}
	for _, d := range direct {
		if d.Enabled == 1 {
			out[d.PathID] = PathAccessSources{Direct: true, Groups: []PathGroupRef{}}
		}
	}
	for _, g := range groups {
		if !g.Enabled || !containsID(g.UserIDs, user.ID) {
			continue
		}
		for _, p := range g.PathIDs {
			s := out[p]
			s.Groups = append(s.Groups, PathGroupRef{ID: g.ID, Name: g.Name})
			out[p] = s
		}
	}
	return out, nil
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

var errPathGroupPreview = errors.New("rollback path group preview")
var ErrPathGroupConflict = errors.New("authorization changed; reload the group and review the changes again")

// ChangePathGroup always preflights and reconciles in one BEGIN IMMEDIATE
// transaction. A preview executes the identical code and deliberately rolls back.
func (db *DB) ChangePathGroup(ctx context.Context, change PathGroupChange, preview bool) (impact PathGroupImpact, err error) {
	err = db.atomic(ctx, func(tx *DB) error {
		state, e := tx.q.PathGroupPreviewState(ctx)
		if e != nil {
			return e
		}
		canonical := change
		canonical.PreviewToken = ""
		raw, e := json.Marshal(canonical)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(append([]byte(state), raw...))
		token := hex.EncodeToString(sum[:])
		if !preview && change.PreviewToken != token {
			return ErrPathGroupConflict
		}
		impact = PathGroupImpact{PreviewToken: token, GroupID: change.GroupID, Users: []PathGroupUserImpact{}, NodeIDs: []string{}}
		users := map[string]bool{}
		grants, e := tx.q.ListPathGroupGrants(ctx)
		if e != nil {
			return e
		}
		for _, g := range grants {
			if g.GroupID == change.GroupID {
				users[g.ProxyUserID] = true
			}
		}
		if change.UserID != "" {
			u, e := tx.GetProxyUser(ctx, change.UserID)
			if e != nil {
				return e
			}
			change.UserID = u.ID
			users[u.ID] = true
		}
		if len(users) > MaxPathGroupUsers {
			return fmt.Errorf("group changes are limited to %d affected users", MaxPathGroupUsers)
		}
		before := map[string][]string{}
		for u := range users {
			ids, e := tx.q.ListUserDesiredPathIDs(ctx, u)
			if e != nil {
				return e
			}
			before[u] = ids
		}
		switch change.Operation {
		case "save":
			name := strings.TrimSpace(change.Name)
			if name == "" || len(name) > 100 || len(change.Description) > 2000 {
				return fmt.Errorf("group name must contain 1–100 bytes; description at most 2000 bytes")
			}
			if len(change.PathIDs) > MaxPathGroupMembers {
				return fmt.Errorf("group may contain at most %d Paths", MaxPathGroupMembers)
			}
			if len(change.PathIDs)*len(users) > MaxPathGroupAccesses {
				return fmt.Errorf("group expansion may contain at most %d user/Path pairs", MaxPathGroupAccesses)
			}
			seen := map[string]bool{}
			for _, pid := range change.PathIDs {
				if seen[pid] {
					return fmt.Errorf("duplicate Path %q", pid)
				}
				seen[pid] = true
				p, e := tx.GetPath(ctx, pid)
				if e != nil {
					return e
				}
				if p.Visibility != PathVisibilitySelectable {
					return fmt.Errorf("Path %q is dependency-only", p.Name)
				}
				ep, e := tx.GetEndpoint(ctx, p.EndpointID)
				if e != nil {
					return e
				}
				if _, _, e = tx.ResolveEndpoint(ctx, ep); e != nil {
					return e
				}
			}
			if change.GroupID == "" {
				gid, e := id.New("pgrp")
				if e != nil {
					return e
				}
				change.GroupID = gid
				impact.GroupID = gid
				e = tx.q.CreatePathGroup(ctx, store.CreatePathGroupParams{ID: gid, Name: name, Description: strings.TrimSpace(change.Description), Enabled: boolToInt64(change.Enabled)})
				if e != nil {
					return e
				}
			} else {
				n, e := tx.q.UpdatePathGroup(ctx, store.UpdatePathGroupParams{ID: change.GroupID, Name: name, Description: strings.TrimSpace(change.Description), Enabled: boolToInt64(change.Enabled), Revision: change.Revision})
				if e != nil {
					return e
				}
				if n != 1 {
					return ErrPathGroupConflict
				}
			}
			if e = tx.q.DeletePathGroupMembers(ctx, change.GroupID); e != nil {
				return e
			}
			for i, p := range change.PathIDs {
				if e = tx.q.AddPathGroupMember(ctx, store.AddPathGroupMemberParams{GroupID: change.GroupID, PathID: p, SortOrder: int64(i)}); e != nil {
					return e
				}
			}
		case "delete":
			g, e := tx.q.GetPathGroup(ctx, change.GroupID)
			if e != nil {
				return e
			}
			if g.Revision != change.Revision {
				return ErrPathGroupConflict
			}
			if _, e = tx.q.ArchivePathGroup(ctx, change.GroupID); e != nil {
				return e
			}
		case "grant", "revoke":
			if change.UserID == "" {
				return fmt.Errorf("user_id is required")
			}
			if _, e = tx.q.GetPathGroup(ctx, change.GroupID); e != nil {
				return e
			}
			if change.Operation == "grant" {
				// Limit fanout before accepting another member of a large group.
				count := 0
				for _, g := range grants {
					if g.GroupID == change.GroupID {
						count++
					}
				}
				if count >= MaxPathGroupUsers && !groupGranted(grants, change.UserID, change.GroupID) {
					return fmt.Errorf("group may have at most %d users", MaxPathGroupUsers)
				}
				if !groupGranted(grants, change.UserID, change.GroupID) {
					count++
				}
				members, e := tx.q.ListPathGroupMembers(ctx)
				if e != nil {
					return e
				}
				memberCount := 0
				for _, m := range members {
					if m.GroupID == change.GroupID {
						memberCount++
					}
				}
				if count*memberCount > MaxPathGroupAccesses {
					return fmt.Errorf("group expansion may contain at most %d user/Path pairs", MaxPathGroupAccesses)
				}
				e = tx.q.GrantUserPathGroup(ctx, store.GrantUserPathGroupParams{ProxyUserID: change.UserID, GroupID: change.GroupID})
			} else {
				e = tx.q.RevokeUserPathGroup(ctx, store.RevokeUserPathGroupParams{ProxyUserID: change.UserID, GroupID: change.GroupID})
			}
			if e != nil {
				return e
			}
			// A user's grant does not change other users' effective permissions.
			users = map[string]bool{change.UserID: true}
		default:
			return fmt.Errorf("unsupported group operation %q", change.Operation)
		}
		ordered := make([]string, 0, len(users))
		for u := range users {
			ordered = append(ordered, u)
		}
		sort.Strings(ordered)
		nodes := map[string]bool{}
		for _, uid := range ordered {
			if e = tx.reconcilePathGrants(ctx, uid); e != nil {
				return e
			}
			user, e := tx.GetProxyUser(ctx, uid)
			if e != nil {
				return e
			}
			after, e := tx.q.ListUserDesiredPathIDs(ctx, uid)
			if e != nil {
				return e
			}
			u := PathGroupUserImpact{UserID: uid, UserName: user.Name, Added: []string{}, Removed: []string{}, Retained: []string{}}
			for _, p := range after {
				if containsID(before[uid], p) {
					u.Retained = append(u.Retained, p)
				} else {
					u.Added = append(u.Added, p)
				}
			}
			for _, p := range before[uid] {
				if !containsID(after, p) {
					u.Removed = append(u.Removed, p)
				}
			}
			for _, pid := range append(append([]string{}, u.Added...), u.Removed...) {
				visited := map[string]bool{}
				for pid != "" && !visited[pid] {
					visited[pid] = true
					p, e := tx.GetPath(ctx, pid)
					if e != nil {
						return e
					}
					ep, e := tx.GetEndpoint(ctx, p.EndpointID)
					if e != nil {
						return e
					}
					proxy, e := tx.GetProxyByID(ctx, ep.ProxyID)
					if e != nil {
						return e
					}
					nodes[proxy.NodeID] = true
					pid = p.DialerPathID.String
				}
			}
			impact.Users = append(impact.Users, u)
		}
		for n := range nodes {
			impact.NodeIDs = append(impact.NodeIDs, n)
		}
		sort.Strings(impact.NodeIDs)
		if preview {
			return errPathGroupPreview
		}
		change.PreviewToken = ""
		changeJSON, e := json.Marshal(change)
		if e != nil {
			return e
		}
		auditImpact := impact
		auditImpact.PreviewToken = ""
		impactJSON, e := json.Marshal(auditImpact)
		if e != nil {
			return e
		}
		eventID, e := id.New("gchange")
		if e != nil {
			return e
		}
		return tx.q.RecordPathGroupChange(ctx, store.RecordPathGroupChangeParams{ID: eventID, GroupID: impact.GroupID, ChangeJson: string(changeJSON), ImpactJson: string(impactJSON)})
	})
	if errors.Is(err, errPathGroupPreview) {
		err = nil
	}
	return
}

func groupGranted(grants []store.UserPathGroupGrant, user, group string) bool {
	for _, g := range grants {
		if g.ProxyUserID == user && g.GroupID == group {
			return true
		}
	}
	return false
}

// Existing effective grants do not re-issue credentials: a membership edit must
// not silently restore a credential or node binding disabled by an operator.
func (db *DB) reconcilePathGrants(ctx context.Context, userID string) error {
	user, err := db.GetProxyUser(ctx, userID)
	if err != nil {
		return err
	}
	desired, err := db.q.ListUserDesiredPathIDs(ctx, user.ID)
	if err != nil {
		return err
	}
	rows, err := db.q.ListUserPathAccessesIncludingInactive(ctx, user.ID)
	if err != nil {
		return err
	}
	active := map[string]bool{}
	previouslyRequired := map[string]bool{}
	var collect func(string, map[string]bool, map[string]bool) error
	collect = func(pid string, proxies, seen map[string]bool) error {
		if seen[pid] {
			return nil
		}
		seen[pid] = true
		p, e := db.GetPath(ctx, pid)
		if e != nil {
			return e
		}
		ep, e := db.GetEndpoint(ctx, p.EndpointID)
		if e != nil {
			return e
		}
		proxies[ep.ProxyID] = true
		if p.DialerPathID.Valid {
			return collect(p.DialerPathID.String, proxies, seen)
		}
		return nil
	}
	for _, r := range rows {
		if r.DeletedAt.Valid || r.Enabled != 1 {
			continue
		}
		if err = collect(r.PathID, previouslyRequired, map[string]bool{}); err != nil {
			return err
		}
		if containsID(desired, r.PathID) {
			active[r.PathID] = true
			continue
		}
		if _, err = db.q.SoftDeletePathAccess(ctx, store.SoftDeletePathAccessParams{PathID: r.PathID, ProxyUserID: user.ID}); err != nil {
			return err
		}
	}
	credentials, err := db.q.ListGroupUserCredentialsIncludingRevoked(ctx, user.ID)
	if err != nil {
		return err
	}
	blocked := map[string]bool{}
	for _, c := range credentials {
		if previouslyRequired[c.ProxyID] && (c.Enabled != 1 || c.DeletedAt.Valid) {
			blocked[c.ProxyID] = true
		}
	}
	for _, pid := range desired {
		if !active[pid] {
			required := map[string]bool{}
			if err = collect(pid, required, map[string]bool{}); err != nil {
				return err
			}
			for proxyID := range required {
				if blocked[proxyID] {
					return fmt.Errorf("credentials for Proxy %s were disabled; restore explicitly before extending group access", proxyID)
				}
			}
			if _, err = db.grantPathToUser(ctx, user.ID, pid); err != nil {
				return err
			}
		}
	}
	if err = db.validatePublishedNamesForUser(ctx, user.ID, "", nil); err != nil {
		return err
	}
	return disableUnusedProxyCredentialsTx(ctx, db.q, user)
}
