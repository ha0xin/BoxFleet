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

const (
	PathVisibilitySelectable = "selectable"
	PathVisibilityDependency = "dependency"
	MaxDialerPathDepth       = 3
)

type Endpoint struct {
	ID        string
	ProxyID   string
	HostID    string
	Enabled   bool
	CreatedAt string
	UpdatedAt string
}

type Path struct {
	ID           string
	Name         string
	DisplayName  string
	EndpointID   string
	DialerPathID sql.NullString
	Enabled      bool
	Visibility   string
	Managed      bool
	SortOrder    int
	CreatedAt    string
	UpdatedAt    string
}

type PathAccess struct {
	ID          string
	PathID      string
	ProxyUserID string
	Enabled     bool
	DeletedAt   sql.NullString
	CreatedAt   string
	UpdatedAt   string
}

type CreatePathParams struct {
	Name         string
	DisplayName  string
	EndpointID   string
	DialerPathID string
	Enabled      bool
	Visibility   string
	Managed      bool
	SortOrder    int
}

type UpdatePathParams struct {
	ID           string
	Name         string
	DisplayName  string
	EndpointID   string
	DialerPathID string
	Enabled      bool
	Visibility   string
	Managed      bool
	SortOrder    int
	AllowManaged bool
}

func (db *DB) EnsureEndpoint(ctx context.Context, proxyID, hostID string) (Endpoint, error) {
	proxy, host, err := db.validateEndpointPair(ctx, proxyID, hostID)
	if err != nil {
		return Endpoint{}, err
	}
	row, err := db.q.GetEndpointByProxyHost(ctx, store.GetEndpointByProxyHostParams{
		ProxyID: proxy.ID,
		HostID:  host.ID,
	})
	if err == nil {
		return endpointFromRow(row), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Endpoint{}, err
	}
	endpointID, err := id.New("ep")
	if err != nil {
		return Endpoint{}, err
	}
	if err := db.q.CreateEndpoint(ctx, store.CreateEndpointParams{
		ID: endpointID, ProxyID: proxy.ID, HostID: host.ID, Enabled: 1,
	}); err != nil {
		return Endpoint{}, err
	}
	return db.GetEndpoint(ctx, endpointID)
}

func (db *DB) GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error) {
	row, err := db.q.GetEndpointByID(ctx, strings.TrimSpace(endpointID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Endpoint{}, fmt.Errorf("endpoint %q not found", endpointID)
		}
		return Endpoint{}, err
	}
	return endpointFromRow(row), nil
}

func (db *DB) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := db.q.ListEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Endpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, endpointFromRow(row))
	}
	return out, nil
}

func (db *DB) SetEndpointEnabled(ctx context.Context, endpointID string, enabled bool) (Endpoint, error) {
	affected, err := db.q.SetEndpointEnabled(ctx, store.SetEndpointEnabledParams{
		Enabled: boolToInt64(enabled), ID: strings.TrimSpace(endpointID),
	})
	if err != nil {
		return Endpoint{}, err
	}
	if err := requireAffected(affected, "endpoint", endpointID); err != nil {
		return Endpoint{}, err
	}
	return db.GetEndpoint(ctx, endpointID)
}

func (db *DB) DeleteEndpoint(ctx context.Context, endpointID string) error {
	affected, err := db.q.DeleteEndpoint(ctx, strings.TrimSpace(endpointID))
	if err != nil {
		return fmt.Errorf("delete endpoint %q: %w", endpointID, err)
	}
	return requireAffected(affected, "endpoint", endpointID)
}

func (db *DB) validateEndpointPair(ctx context.Context, proxyID, hostID string) (Proxy, NodeHost, error) {
	proxy, err := db.GetProxyByID(ctx, proxyID)
	if err != nil {
		return Proxy{}, NodeHost{}, err
	}
	node, err := db.GetNode(ctx, proxy.NodeName)
	if err != nil {
		return Proxy{}, NodeHost{}, err
	}
	for _, host := range node.Hosts {
		if host.ID != strings.TrimSpace(hostID) {
			continue
		}
		if !host.Selected {
			return Proxy{}, NodeHost{}, fmt.Errorf("host %q on node %q is not selected", host.ID, node.Name)
		}
		return proxy, host, nil
	}
	return Proxy{}, NodeHost{}, fmt.Errorf("host %q does not belong to proxy %q's node %q", hostID, proxy.Name, node.Name)
}

func (db *DB) ResolveEndpoint(ctx context.Context, endpoint Endpoint) (Proxy, NodeHost, error) {
	return db.validateEndpointPair(ctx, endpoint.ProxyID, endpoint.HostID)
}

func (db *DB) CreatePath(ctx context.Context, params CreatePathParams) (Path, error) {
	normalized, err := db.normalizePath(ctx, Path{
		Name:         params.Name,
		DisplayName:  params.DisplayName,
		EndpointID:   params.EndpointID,
		DialerPathID: nullablePathString(params.DialerPathID),
		Enabled:      params.Enabled,
		Visibility:   params.Visibility,
		Managed:      params.Managed,
		SortOrder:    params.SortOrder,
	})
	if err != nil {
		return Path{}, err
	}
	pathID, err := id.New("path")
	if err != nil {
		return Path{}, err
	}
	normalized.ID = pathID
	if err := db.validatePathGraph(ctx, normalized); err != nil {
		return Path{}, err
	}
	if err := db.q.CreatePath(ctx, store.CreatePathParams{
		ID: pathID, Name: normalized.Name, DisplayName: normalized.DisplayName,
		EndpointID: normalized.EndpointID, DialerPathID: normalized.DialerPathID,
		Enabled: boolToInt64(normalized.Enabled), Visibility: normalized.Visibility,
		Managed:   boolToInt64(normalized.Managed),
		SortOrder: int64(normalized.SortOrder),
	}); err != nil {
		return Path{}, err
	}
	return db.GetPath(ctx, pathID)
}

// EnsureDirectPathsForProxy materializes the legacy direct publication for each
// selected host. It is used by the compatibility proxy-grant API; new product
// flows should create and grant Paths explicitly.
func (db *DB) EnsureDirectPathsForProxy(ctx context.Context, proxyID string) ([]Path, error) {
	proxy, err := db.GetProxyByID(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	node, err := db.GetNode(ctx, proxy.NodeName)
	if err != nil {
		return nil, err
	}
	selectedHostIDs := make(map[string]bool, len(node.Hosts))
	for _, host := range node.Hosts {
		if host.Selected {
			selectedHostIDs[host.ID] = true
		}
	}
	endpoints, err := db.q.ListEndpointsByProxyID(ctx, proxy.ID)
	if err != nil {
		return nil, err
	}
	for _, endpointRow := range endpoints {
		if selectedHostIDs[endpointRow.HostID] {
			continue
		}
		rows, err := db.q.ListPathsByEndpointID(ctx, endpointRow.ID)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			path := pathFromRow(row)
			if path.Managed && path.Enabled {
				if err := db.setPathEnabledUnchecked(ctx, path.ID, false); err != nil {
					return nil, err
				}
			}
		}
	}
	paths := make([]Path, 0, len(node.Hosts))
	for index, host := range node.Hosts {
		if !host.Selected {
			continue
		}
		endpoint, err := db.EnsureEndpoint(ctx, proxy.ID, host.ID)
		if err != nil {
			return nil, err
		}
		existing, err := db.q.ListPathsByEndpointID(ctx, endpoint.ID)
		if err != nil {
			return nil, err
		}
		var direct *Path
		for _, row := range existing {
			candidate := pathFromRow(row)
			if candidate.Managed && !candidate.DialerPathID.Valid {
				direct = &candidate
				break
			}
		}
		name := "direct"
		displayName := proxy.Name
		if host.Tag != "" {
			name = host.Tag
			displayName += "-" + host.Tag
		} else if index > 0 {
			displayName += "-" + host.Host
		}
		if direct != nil {
			if direct.Managed && (direct.Name != name || direct.DisplayName != displayName || direct.SortOrder != index || !direct.Enabled) {
				updated, err := db.UpdatePath(ctx, UpdatePathParams{
					ID: direct.ID, Name: name, DisplayName: displayName,
					EndpointID: endpoint.ID, Enabled: true,
					Visibility: PathVisibilitySelectable, Managed: true, SortOrder: index, AllowManaged: true,
				})
				if err != nil {
					return nil, err
				}
				direct = &updated
			}
			paths = append(paths, *direct)
			continue
		}
		created, err := db.CreatePath(ctx, CreatePathParams{
			Name: name, DisplayName: displayName, EndpointID: endpoint.ID,
			Enabled: true, Visibility: PathVisibilitySelectable, Managed: true, SortOrder: index,
		})
		if err != nil {
			return nil, err
		}
		paths = append(paths, created)
	}
	return paths, nil
}

func (db *DB) GrantDirectPathsForCredential(ctx context.Context, credential ProxyCredential) error {
	enabled, err := db.GetProxyDirectPublication(ctx, credential.ProxyID)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	paths, err := db.EnsureDirectPathsForProxy(ctx, credential.ProxyID)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := db.GrantPathAccess(ctx, credential.ProxyUserName, path.ID); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) GetProxyDirectPublication(ctx context.Context, proxyID string) (bool, error) {
	enabled, err := db.q.GetProxyDirectPublication(ctx, strings.TrimSpace(proxyID))
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return int64ToBool(enabled), err
}

func (db *DB) SetProxyDirectPublication(ctx context.Context, proxyID string, enabled bool) ([]Path, error) {
	if _, err := db.GetProxyByID(ctx, proxyID); err != nil {
		return nil, err
	}
	if err := db.q.SetProxyDirectPublication(ctx, store.SetProxyDirectPublicationParams{
		ProxyID: strings.TrimSpace(proxyID), DirectEnabled: boolToInt64(enabled),
	}); err != nil {
		return nil, err
	}
	if enabled {
		return db.EnsureDirectPathsForProxy(ctx, proxyID)
	}
	endpoints, err := db.q.ListEndpointsByProxyID(ctx, strings.TrimSpace(proxyID))
	if err != nil {
		return nil, err
	}
	paths, err := db.ListPaths(ctx)
	if err != nil {
		return nil, err
	}
	endpointIDs := make(map[string]bool)
	for _, endpoint := range endpoints {
		endpointIDs[endpoint.ID] = true
	}
	updated := make([]Path, 0)
	for _, path := range paths {
		if !path.Managed || !endpointIDs[path.EndpointID] || !path.Enabled {
			continue
		}
		if err := db.setPathEnabledUnchecked(ctx, path.ID, false); err != nil {
			return nil, err
		}
		path.Enabled = false
		updated = append(updated, path)
	}
	return updated, nil
}

func (db *DB) SyncLegacyDirectPathsForNode(ctx context.Context, nodeName string) error {
	proxies, err := db.ListProxies(ctx, nodeName)
	if err != nil {
		return err
	}
	for _, proxy := range proxies {
		enabled, err := db.GetProxyDirectPublication(ctx, proxy.ID)
		if err != nil {
			return err
		}
		if enabled {
			if _, err := db.EnsureDirectPathsForProxy(ctx, proxy.ID); err != nil {
				return err
			}
		}
	}
	credentials, err := db.ListProxyCredentialsByNode(ctx, nodeName)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		if err := db.GrantDirectPathsForCredential(ctx, credential); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) UpdatePath(ctx context.Context, params UpdatePathParams) (Path, error) {
	existing, err := db.GetPath(ctx, params.ID)
	if err != nil {
		return Path{}, err
	}
	if existing.Managed && !params.AllowManaged {
		return Path{}, fmt.Errorf("managed path %q cannot be edited directly", existing.Name)
	}
	normalized, err := db.normalizePath(ctx, Path{
		ID: params.ID, Name: params.Name, DisplayName: params.DisplayName,
		EndpointID: params.EndpointID, DialerPathID: nullablePathString(params.DialerPathID),
		Enabled: params.Enabled, Visibility: params.Visibility, Managed: params.Managed, SortOrder: params.SortOrder,
	})
	if err != nil {
		return Path{}, err
	}
	if err := db.validatePathGraph(ctx, normalized); err != nil {
		return Path{}, err
	}
	if err := db.validatePublishedNamesAfterPathChange(ctx, normalized); err != nil {
		return Path{}, err
	}
	affected, err := db.q.UpdatePath(ctx, store.UpdatePathParams{
		Name: normalized.Name, DisplayName: normalized.DisplayName,
		EndpointID: normalized.EndpointID, DialerPathID: normalized.DialerPathID,
		Enabled: boolToInt64(normalized.Enabled), Visibility: normalized.Visibility,
		Managed:   boolToInt64(normalized.Managed),
		SortOrder: int64(normalized.SortOrder), ID: normalized.ID,
	})
	if err != nil {
		return Path{}, err
	}
	if err := requireAffected(affected, "path", params.ID); err != nil {
		return Path{}, err
	}
	return db.GetPath(ctx, params.ID)
}

func (db *DB) setPathEnabledUnchecked(ctx context.Context, pathID string, enabled bool) error {
	affected, err := db.q.SetPathEnabled(ctx, store.SetPathEnabledParams{
		Enabled: boolToInt64(enabled), ID: strings.TrimSpace(pathID),
	})
	if err != nil {
		return err
	}
	return requireAffected(affected, "path", pathID)
}

func (db *DB) validatePublishedNamesAfterPathChange(ctx context.Context, candidate Path) error {
	users, err := db.q.ListUserNamesWithActivePathAccess(ctx)
	if err != nil {
		return err
	}
	for _, userName := range users {
		if err := db.validatePublishedNamesForUser(ctx, userName, "", &candidate); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) validatePublishedNamesForUser(ctx context.Context, userName, extraRootID string, candidate *Path) error {
	accesses, err := db.ListActivePathAccessesByUser(ctx, userName)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(accesses)+1)
	for _, access := range accesses {
		roots = append(roots, access.PathID)
	}
	if extraRootID != "" {
		roots = append(roots, extraRootID)
	}
	emitted := make(map[string]bool)
	names := make(map[string]string)
	var visit func(string) error
	visit = func(pathID string) error {
		if emitted[pathID] {
			return nil
		}
		path, err := db.GetPath(ctx, pathID)
		if err != nil {
			return err
		}
		if candidate != nil && candidate.ID == pathID {
			path = *candidate
		}
		if !path.Enabled {
			return nil
		}
		if path.DialerPathID.Valid {
			if err := visit(path.DialerPathID.String); err != nil {
				return err
			}
		}
		endpoint, err := db.GetEndpoint(ctx, path.EndpointID)
		if err != nil {
			return err
		}
		proxy, err := db.GetProxyByID(ctx, endpoint.ProxyID)
		if err != nil {
			return err
		}
		name := path.DisplayName
		if name == "" {
			name = proxy.Name + " · " + path.Name
		}
		if previous, exists := names[name]; exists && previous != path.ID {
			if candidate == nil || previous == candidate.ID || path.ID == candidate.ID {
				return fmt.Errorf("published Path name %q conflicts between %s and %s for user %q", name, previous, path.ID, userName)
			}
			return nil
		}
		names[name] = path.ID
		emitted[pathID] = true
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) normalizePath(ctx context.Context, path Path) (Path, error) {
	path.ID = strings.TrimSpace(path.ID)
	path.Name = strings.TrimSpace(path.Name)
	path.DisplayName = strings.TrimSpace(path.DisplayName)
	path.EndpointID = strings.TrimSpace(path.EndpointID)
	path.Visibility = strings.TrimSpace(path.Visibility)
	if path.Name == "" {
		return Path{}, errors.New("path name is required")
	}
	if path.EndpointID == "" {
		return Path{}, errors.New("path endpoint is required")
	}
	if path.Visibility == "" {
		path.Visibility = PathVisibilitySelectable
	}
	if path.Visibility != PathVisibilitySelectable && path.Visibility != PathVisibilityDependency {
		return Path{}, fmt.Errorf("unsupported path visibility %q", path.Visibility)
	}
	endpoint, err := db.GetEndpoint(ctx, path.EndpointID)
	if err != nil {
		return Path{}, err
	}
	if !endpoint.Enabled && path.Enabled {
		return Path{}, fmt.Errorf("endpoint %q is disabled", endpoint.ID)
	}
	if path.Enabled {
		if _, _, err := db.ResolveEndpoint(ctx, endpoint); err != nil {
			return Path{}, err
		}
	}
	return path, nil
}

func (db *DB) GetPath(ctx context.Context, pathID string) (Path, error) {
	row, err := db.q.GetPathByID(ctx, strings.TrimSpace(pathID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Path{}, fmt.Errorf("path %q not found", pathID)
		}
		return Path{}, err
	}
	return pathFromRow(row), nil
}

func (db *DB) ListPaths(ctx context.Context) ([]Path, error) {
	rows, err := db.q.ListPaths(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Path, 0, len(rows))
	for _, row := range rows {
		out = append(out, pathFromRow(row))
	}
	return out, nil
}

func (db *DB) DeletePath(ctx context.Context, pathID string) error {
	path, err := db.GetPath(ctx, pathID)
	if err != nil {
		return err
	}
	if path.Managed {
		return fmt.Errorf("managed path %q cannot be deleted directly", path.Name)
	}
	userNames, err := db.q.ListUserNamesByPathID(ctx, path.ID)
	if err != nil {
		return err
	}
	affected, err := db.q.DeletePath(ctx, strings.TrimSpace(pathID))
	if err != nil {
		return fmt.Errorf("delete path %q: %w", pathID, err)
	}
	if err := requireAffected(affected, "path", pathID); err != nil {
		return err
	}
	count, err := db.q.CountPathsByEndpointID(ctx, path.EndpointID)
	if err != nil {
		return err
	}
	if count == 0 {
		if err := db.DeleteEndpoint(ctx, path.EndpointID); err != nil {
			return err
		}
	}
	for _, userName := range userNames {
		user, err := db.GetProxyUser(ctx, userName)
		if err != nil {
			return err
		}
		if err := db.disableUnusedProxyCredentials(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) validatePathGraph(ctx context.Context, candidate Path) error {
	paths, err := db.ListPaths(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]Path, len(paths)+1)
	for _, path := range paths {
		byID[path.ID] = path
	}
	byID[candidate.ID] = candidate
	if candidate.DialerPathID.Valid {
		if _, ok := byID[candidate.DialerPathID.String]; !ok {
			return fmt.Errorf("dialer path %q not found", candidate.DialerPathID.String)
		}
	}
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string, int) error
	visit = func(pathID string, depth int) error {
		if visiting[pathID] {
			return fmt.Errorf("dialer path cycle includes %q", pathID)
		}
		if depth > MaxDialerPathDepth {
			return fmt.Errorf("dialer path depth exceeds %d", MaxDialerPathDepth)
		}
		if visited[pathID] {
			return nil
		}
		path, ok := byID[pathID]
		if !ok {
			return fmt.Errorf("path %q not found", pathID)
		}
		visiting[pathID] = true
		if path.DialerPathID.Valid {
			if err := visit(path.DialerPathID.String, depth+1); err != nil {
				return err
			}
		}
		delete(visiting, pathID)
		visited[pathID] = true
		return nil
	}
	return visit(candidate.ID, 1)
}

func (db *DB) GrantPathAccess(ctx context.Context, userName, pathID string) (PathAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return PathAccess{}, err
	}
	if _, err := db.GetPath(ctx, pathID); err != nil {
		return PathAccess{}, err
	}
	params := store.GetPathAccessByIDsParams{PathID: pathID, ProxyUserID: user.ID}
	row, err := db.q.GetPathAccessByIDs(ctx, params)
	if err == nil {
		if int64ToBool(row.Enabled) && !row.DeletedAt.Valid {
			return pathAccessFromRow(row), nil
		}
		if _, err := db.q.RestorePathAccess(ctx, store.RestorePathAccessParams(params)); err != nil {
			return PathAccess{}, err
		}
		row, err = db.q.GetPathAccessByIDs(ctx, params)
		return pathAccessFromRow(row), err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PathAccess{}, err
	}
	accessID, err := id.New("pacc")
	if err != nil {
		return PathAccess{}, err
	}
	if err := db.q.CreatePathAccess(ctx, store.CreatePathAccessParams{
		ID: accessID, PathID: pathID, ProxyUserID: user.ID, Enabled: 1,
	}); err != nil {
		return PathAccess{}, err
	}
	row, err = db.q.GetPathAccessByIDs(ctx, params)
	return pathAccessFromRow(row), err
}

// GrantPathToUser is the product-level grant operation. It ensures the
// technical node binding and ProxyCredential for every hop in the chain, then
// grants only the selectable root Path to the user.
func (db *DB) GrantPathToUser(ctx context.Context, userName, pathID string) (PathAccess, error) {
	path, err := db.GetPath(ctx, pathID)
	if err != nil {
		return PathAccess{}, err
	}
	if path.Visibility != PathVisibilitySelectable {
		return PathAccess{}, fmt.Errorf("path %q is dependency-only and cannot be granted directly", path.Name)
	}
	if err := db.validatePublishedNamesForUser(ctx, userName, path.ID, nil); err != nil {
		return PathAccess{}, err
	}
	seen := make(map[string]bool)
	var ensure func(Path) error
	ensure = func(current Path) error {
		if seen[current.ID] {
			return nil
		}
		seen[current.ID] = true
		endpoint, err := db.GetEndpoint(ctx, current.EndpointID)
		if err != nil {
			return err
		}
		proxy, _, err := db.ResolveEndpoint(ctx, endpoint)
		if err != nil {
			return err
		}
		if _, err := db.BindUserToNode(ctx, userName, proxy.NodeName); err != nil {
			return err
		}
		params := IssueCredentialParams{UserName: userName, NodeName: proxy.NodeName, ProxyName: proxy.Name}
		switch proxy.Protocol {
		case ProtocolVLESSReality:
			_, err = db.IssueVLESSRealityCredential(ctx, params)
		case ProtocolShadowsocks2022:
			_, err = db.IssueShadowsocks2022Credential(ctx, params)
		default:
			err = fmt.Errorf("proxy protocol %s does not support credentials", proxy.Protocol)
		}
		if err != nil {
			return err
		}
		if current.DialerPathID.Valid {
			dialer, err := db.GetPath(ctx, current.DialerPathID.String)
			if err != nil {
				return err
			}
			return ensure(dialer)
		}
		return nil
	}
	if err := ensure(path); err != nil {
		return PathAccess{}, err
	}
	return db.GrantPathAccess(ctx, userName, path.ID)
}

func (db *DB) RevokePathAccess(ctx context.Context, userName, pathID string) (PathAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return PathAccess{}, err
	}
	params := store.SoftDeletePathAccessParams{PathID: pathID, ProxyUserID: user.ID}
	affected, err := db.q.SoftDeletePathAccess(ctx, params)
	if err != nil {
		return PathAccess{}, err
	}
	if err := requireAffected(affected, "path access", userName+"@"+pathID); err != nil {
		return PathAccess{}, err
	}
	row, err := db.q.GetPathAccessByIDs(ctx, store.GetPathAccessByIDsParams(params))
	if err != nil {
		return PathAccess{}, err
	}
	if err := db.disableUnusedProxyCredentials(ctx, user); err != nil {
		return PathAccess{}, err
	}
	return pathAccessFromRow(row), nil
}

func (db *DB) disableUnusedProxyCredentials(ctx context.Context, user ProxyUser) error {
	accesses, err := db.ListActivePathAccessesByUser(ctx, user.Name)
	if err != nil {
		return err
	}
	requiredProxyIDs := make(map[string]bool)
	visitedPaths := make(map[string]bool)
	var visit func(string) error
	visit = func(pathID string) error {
		if visitedPaths[pathID] {
			return nil
		}
		visitedPaths[pathID] = true
		path, err := db.GetPath(ctx, pathID)
		if err != nil {
			return err
		}
		endpoint, err := db.GetEndpoint(ctx, path.EndpointID)
		if err != nil {
			return err
		}
		requiredProxyIDs[endpoint.ProxyID] = true
		if path.DialerPathID.Valid {
			return visit(path.DialerPathID.String)
		}
		return nil
	}
	for _, access := range accesses {
		if err := visit(access.PathID); err != nil {
			return err
		}
	}
	credentials, err := db.ListProxyCredentialsByUser(ctx, user.Name)
	if err != nil {
		return err
	}
	for _, credential := range credentials {
		if !credential.Enabled || credential.DeletedAt.Valid || requiredProxyIDs[credential.ProxyID] {
			continue
		}
		if err := db.setProxyCredentialEnabledByIDs(ctx, user.ID, credential.ProxyID, false); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) ListActivePathAccessesByUser(ctx context.Context, userName string) ([]PathAccess, error) {
	user, err := db.GetProxyUser(ctx, userName)
	if err != nil {
		return nil, err
	}
	rows, err := db.q.ListActivePathAccessesByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := make([]PathAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, pathAccessFromRow(row))
	}
	return out, nil
}

func nullablePathString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func endpointFromRow(row store.Endpoint) Endpoint {
	return Endpoint{
		ID: row.ID, ProxyID: row.ProxyID, HostID: row.HostID,
		Enabled: int64ToBool(row.Enabled), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func pathFromRow(row store.Path) Path {
	return Path{
		ID: row.ID, Name: row.Name, DisplayName: row.DisplayName,
		EndpointID: row.EndpointID, DialerPathID: row.DialerPathID,
		Enabled: int64ToBool(row.Enabled), Visibility: row.Visibility,
		Managed:   int64ToBool(row.Managed),
		SortOrder: int(row.SortOrder), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func pathAccessFromRow(row store.PathAccess) PathAccess {
	return PathAccess{
		ID: row.ID, PathID: row.PathID, ProxyUserID: row.ProxyUserID,
		Enabled: int64ToBool(row.Enabled), DeletedAt: row.DeletedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
