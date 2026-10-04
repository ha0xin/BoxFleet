package db

import (
	"context"
	"errors"
	"sort"

	store "github.com/haoxin/boxfleet/internal/server/store/sqlc"
)

type DeletionImpactItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Effect string `json:"effect"`
}

type DeletionImpact struct {
	Items   []DeletionImpactItem `json:"items"`
	Blocked string               `json:"blocked,omitempty"`
}

// Resolve public names once, then inspect dependencies by immutable identity.
func (db *DB) ResourceDeletionImpact(ctx context.Context, kind, resourceID string) (DeletionImpact, error) {
	result := DeletionImpact{Items: []DeletionImpactItem{}}
	switch kind {
	case "node":
		node, err := db.GetNode(ctx, resourceID)
		if err != nil {
			return result, err
		}
		resourceID = node.ID
	case "proxy":
		proxy, err := db.GetProxyByID(ctx, resourceID)
		if err != nil {
			return result, err
		}
		resourceID = proxy.ID
	case "user":
		user, err := db.GetProxyUser(ctx, resourceID)
		if err != nil {
			return result, err
		}
		resourceID = user.ID
	case "path":
		path, err := db.GetPath(ctx, resourceID)
		if err != nil {
			return result, err
		}
		resourceID = path.ID
		if path.Managed {
			result.Blocked = "Managed paths must be removed through their proxy."
		}
	default:
		return result, errors.New("unsupported resource kind")
	}
	proxies, err := db.q.ListDeletionProxies(ctx)
	if err != nil {
		return result, err
	}
	endpoints, err := db.q.ListEndpoints(ctx)
	if err != nil {
		return result, err
	}
	paths, err := db.q.ListPaths(ctx)
	if err != nil {
		return result, err
	}
	accesses, err := db.q.ListDeletionAccesses(ctx)
	if err != nil {
		return result, err
	}
	credentials, err := db.q.ListDeletionCredentials(ctx)
	if err != nil {
		return result, err
	}
	proxyNames := map[string]string{}
	retiredProxies := map[string]bool{}
	targets := map[string]bool{}
	for _, p := range proxies {
		proxyNames[p.ID] = p.Name
		retiredProxies[p.ID] = p.DeletedAt.Valid || p.NodeDeletedAt.Valid
		if !p.DeletedAt.Valid && (kind == "node" && p.NodeID == resourceID || kind == "proxy" && p.ID == resourceID) {
			targets[p.ID] = true
		}
	}
	endpointProxy := map[string]string{}
	endpointEnabled := map[string]bool{}
	for _, e := range endpoints {
		endpointProxy[e.ID] = e.ProxyID
		endpointEnabled[e.ID] = e.Enabled == 1
	}
	byID := map[string]store.Path{}
	affected := map[string]bool{}
	labels := map[string]string{}
	for _, p := range paths {
		byID[p.ID] = p
		labels[p.ID] = p.DisplayName
		if labels[p.ID] == "" {
			labels[p.ID] = proxyNames[endpointProxy[p.EndpointID]] + " / " + p.Name
		}
		if targets[endpointProxy[p.EndpointID]] || kind == "path" && p.ID == resourceID {
			affected[p.ID] = true
		}
	}
	for {
		changed := false
		for _, p := range paths {
			if p.DialerPathID.Valid && affected[p.DialerPathID.String] && !affected[p.ID] {
				affected[p.ID] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	add := func(kind, id, name, effect string) {
		result.Items = append(result.Items, DeletionImpactItem{Kind: kind, ID: id, Name: name, Effect: effect})
	}
	hostNames := map[string]string{}
	for _, p := range proxies {
		if targets[p.ID] {
			n, err := db.GetNode(ctx, p.NodeID)
			if err != nil {
				return result, err
			}
			for _, h := range n.Hosts {
				hostNames[h.ID] = h.Host
			}
		}
	}
	bindings, err := db.q.ListUserNodeBindings(ctx)
	if err != nil {
		return result, err
	}
	for _, binding := range bindings {
		if binding.Enabled == 1 && (kind == "node" && binding.NodeID == resourceID || kind == "user" && binding.ProxyUserID == resourceID) {
			effect := "Disabled"
			if kind == "user" {
				effect = "Unavailable"
			}
			add("Node access", binding.ID, binding.ProxyUserName+" → "+binding.NodeName, effect)
		}
	}
	if kind == "node" {
		tokens, err := db.q.ListActiveNodeTokensByNodeName(ctx, resourceID)
		if err != nil {
			return result, err
		}
		for _, token := range tokens {
			add("Agent token", token.ID, token.NodeName+" agent", "Revoked")
		}
	}
	if kind == "node" {
		for _, p := range proxies {
			if targets[p.ID] {
				add("Proxy", p.ID, p.Name, "Archived")
			}
		}
	}
	for _, e := range endpoints {
		if targets[e.ProxyID] && e.Enabled == 1 {
			name := proxyNames[e.ProxyID] + " @ " + hostNames[e.HostID]
			if name == "" {
				name = proxyNames[e.ProxyID]
			}
			add("Endpoint", e.ID, name, "Disabled")
		}
	}
	for _, p := range paths {
		if affected[p.ID] {
			if kind == "path" && p.ID != resourceID {
				add("Path", p.ID, labels[p.ID], "Blocks deletion")
				result.Blocked = "Other paths reference this path. Change their dialer path before deleting it."
			} else if kind != "path" && p.Enabled == 1 {
				add("Path", p.ID, labels[p.ID], "Disabled")
			}
		}
	}
	affectedUsers := map[string]bool{}
	required := map[string]map[string]bool{}
	var visit func(string, string, map[string]bool)
	visit = func(userID, pathID string, seen map[string]bool) {
		if seen[pathID] {
			return
		}
		seen[pathID] = true
		p, ok := byID[pathID]
		if !ok || affected[pathID] || p.Enabled != 1 || !endpointEnabled[p.EndpointID] || retiredProxies[endpointProxy[p.EndpointID]] {
			return
		}
		if required[userID] == nil {
			required[userID] = map[string]bool{}
		}
		required[userID][endpointProxy[p.EndpointID]] = true
		if p.DialerPathID.Valid {
			visit(userID, p.DialerPathID.String, seen)
		}
	}
	for _, a := range accesses {
		if a.DeletedAt.Valid {
			continue
		}
		if affected[a.PathID] || kind == "user" && a.ProxyUserID == resourceID {
			effect := "Revoked"
			if kind == "user" {
				effect = "Unavailable"
			}
			if kind == "path" {
				effect = "Removed"
			}
			add("Access", a.ID, a.UserName+" → "+labels[a.PathID], effect)
			affectedUsers[a.ProxyUserID] = true
		} else if a.Enabled == 1 {
			visit(a.ProxyUserID, a.PathID, map[string]bool{})
		}
	}
	for _, a := range credentials {
		if a.DeletedAt.Valid {
			continue
		}
		effect := "Disabled"
		include := targets[a.ProxyID] || a.Enabled == 1 && affectedUsers[a.ProxyUserID] && !required[a.ProxyUserID][a.ProxyID]
		if targets[a.ProxyID] {
			effect = "Archived"
		}
		if kind == "user" && a.ProxyUserID == resourceID {
			include = true
			effect = "Unavailable"
		}
		if include {
			add("Credential", a.ID, a.UserName+" @ "+proxyNames[a.ProxyID], effect)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		a, b := result.Items[i], result.Items[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
	return result, nil
}

// Retain historical IDs and ledger rows while removing every live route through
// retired resources. All mutations run inside the caller's deletion transaction.
func retireResourceDependenciesTx(ctx context.Context, q *store.Queries) error {
	paths, err := q.ListRetiredResourcePaths(ctx)
	if err != nil {
		return err
	}
	users := map[string]bool{}
	for _, pathID := range paths {
		affected, err := q.ListPathAccessUsersIncludingDeleted(ctx, pathID)
		if err != nil {
			return err
		}
		for _, userID := range affected {
			users[userID] = true
		}
		if _, err := q.SetPathEnabled(ctx, store.SetPathEnabledParams{ID: pathID, Enabled: 0}); err != nil {
			return err
		}
		if err := q.ArchivePathAccesses(ctx, pathID); err != nil {
			return err
		}
	}
	if err := q.DisableRetiredNodeBindings(ctx); err != nil {
		return err
	}
	if err := q.DisableRetiredEndpoints(ctx); err != nil {
		return err
	}
	if err := q.ArchiveRetiredProxyCredentials(ctx); err != nil {
		return err
	}
	for userID := range users {
		user, err := q.GetProxyUserByNameIncludingDeleted(ctx, userID)
		if err != nil {
			return err
		}
		if user.DeletedAt.Valid {
			continue
		}
		if err := disableUnusedProxyCredentialsTx(ctx, q, ProxyUser{ID: user.ID, Name: user.Name}); err != nil {
			return err
		}
	}
	return nil
}
