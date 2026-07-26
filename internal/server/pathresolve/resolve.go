package pathresolve

import (
	"context"
	"fmt"
	"sort"

	"github.com/haoxin/boxfleet/internal/server/db"
)

type ResolvedPath struct {
	Path       db.Path
	Name       string
	Endpoint   db.Endpoint
	Proxy      db.Proxy
	Host       db.NodeHost
	Credential db.ProxyCredential
	Dialer     *ResolvedPath
}

type Result struct {
	Selectable []*ResolvedPath
	Ordered    []*ResolvedPath
}

func ForUser(ctx context.Context, store *db.DB, userName string) (Result, error) {
	user, err := store.GetProxyUser(ctx, userName)
	if err != nil {
		return Result{}, err
	}
	if user.Status != "active" {
		return Result{}, fmt.Errorf("user %q is %s", user.Name, user.Status)
	}
	accesses, err := store.ListActivePathAccessesByUser(ctx, user.Name)
	if err != nil {
		return Result{}, err
	}
	if len(accesses) == 0 {
		return Result{Selectable: []*ResolvedPath{}, Ordered: []*ResolvedPath{}}, nil
	}

	cache := make(map[string]*ResolvedPath)
	visiting := make(map[string]bool)
	var resolve func(string, int) (*ResolvedPath, error)
	resolve = func(pathID string, depth int) (*ResolvedPath, error) {
		if cached := cache[pathID]; cached != nil {
			return cached, nil
		}
		if visiting[pathID] {
			return nil, fmt.Errorf("dialer path cycle includes %q", pathID)
		}
		if depth > db.MaxDialerPathDepth {
			return nil, fmt.Errorf("dialer path depth exceeds %d", db.MaxDialerPathDepth)
		}
		visiting[pathID] = true
		defer delete(visiting, pathID)

		path, err := store.GetPath(ctx, pathID)
		if err != nil {
			return nil, err
		}
		if !path.Enabled {
			return nil, fmt.Errorf("path %q is disabled", path.Name)
		}
		endpoint, err := store.GetEndpoint(ctx, path.EndpointID)
		if err != nil {
			return nil, err
		}
		if !endpoint.Enabled {
			return nil, fmt.Errorf("endpoint %q is disabled", endpoint.ID)
		}
		proxy, host, err := store.ResolveEndpoint(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		if !proxy.Enabled {
			return nil, fmt.Errorf("proxy %q is disabled", proxy.Name)
		}
		node, err := store.GetNode(ctx, proxy.NodeName)
		if err != nil {
			return nil, err
		}
		if node.Status != "active" {
			return nil, fmt.Errorf("node %q is %s", node.Name, node.Status)
		}
		binding, err := store.GetUserNodeBinding(ctx, user.Name, node.Name)
		if err != nil {
			return nil, err
		}
		if !binding.Enabled {
			return nil, fmt.Errorf("binding for user %q on node %q is disabled", user.Name, node.Name)
		}
		credential, err := store.GetProxyCredentialByIDs(ctx, user.ID, proxy.ID)
		if err != nil {
			return nil, err
		}
		if !credential.Enabled || credential.DeletedAt.Valid {
			return nil, fmt.Errorf("credential for user %q on proxy %q is disabled", user.Name, proxy.Name)
		}
		name := path.DisplayName
		if name == "" {
			name = proxy.Name + " · " + path.Name
		}
		resolved := &ResolvedPath{
			Path: path, Name: name, Endpoint: endpoint, Proxy: proxy,
			Host: host, Credential: credential,
		}
		if path.DialerPathID.Valid {
			resolved.Dialer, err = resolve(path.DialerPathID.String, depth+1)
			if err != nil {
				return nil, fmt.Errorf("resolve dialer for path %q: %w", path.Name, err)
			}
		}
		cache[pathID] = resolved
		return resolved, nil
	}

	selectable := make([]*ResolvedPath, 0, len(accesses))
	for _, access := range accesses {
		path, err := resolve(access.PathID, 1)
		if err != nil {
			// Runtime state can invalidate one granted Path (disabled credential,
			// host, proxy, or dependency). Omit that Path rather than taking down
			// the user's entire subscription; resolution still fails closed because
			// it never degrades a chain to direct.
			continue
		}
		if path.Path.Visibility == db.PathVisibilitySelectable {
			selectable = append(selectable, path)
		}
	}
	sort.SliceStable(selectable, func(i, j int) bool {
		if selectable[i].Path.SortOrder != selectable[j].Path.SortOrder {
			return selectable[i].Path.SortOrder < selectable[j].Path.SortOrder
		}
		return selectable[i].Name < selectable[j].Name
	})

	ordered := make([]*ResolvedPath, 0, len(cache))
	emitted := make(map[string]bool, len(cache))
	var emit func(*ResolvedPath)
	emit = func(path *ResolvedPath) {
		if path == nil || emitted[path.Path.ID] {
			return
		}
		emit(path.Dialer)
		emitted[path.Path.ID] = true
		ordered = append(ordered, path)
	}
	for _, path := range selectable {
		emit(path)
	}
	seenNames := make(map[string]string, len(ordered))
	for _, path := range ordered {
		if previous, exists := seenNames[path.Name]; exists {
			return Result{}, fmt.Errorf("Mihomo profile name %q conflicts between %s and %s", path.Name, previous, path.Path.ID)
		}
		seenNames[path.Name] = path.Path.ID
	}
	return Result{Selectable: selectable, Ordered: ordered}, nil
}
