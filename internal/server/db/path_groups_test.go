package db

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/haoxin/boxfleet/migrations"
	"github.com/pressly/goose/v3"
)

func groupFixture(t *testing.T, d *DB) (ProxyUser, []Path, []Proxy) {
	t.Helper()
	ctx := context.Background()
	u, err := d.CreateProxyUser(ctx, CreateProxyUserParams{Name: "group-alice"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.CreateNode(ctx, "group-node", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	publish := false
	proxies := []Proxy{}
	paths := []Path{}
	for i, name := range []string{"group-a", "group-b", "group-c"} {
		proxyIndex := 0
		if i != 1 {
			protocol := ProtocolVLESSReality
			if i == 2 {
				protocol = ProtocolShadowsocks2022
			}
			p, e := d.CreateProxy(ctx, CreateProxyParams{NodeName: n.ID, Name: name, Protocol: protocol, ListenPort: 42000 + i, Enabled: true, SettingsJSON: `{}`, DirectPublish: &publish})
			if e != nil {
				t.Fatal(e)
			}
			proxies = append(proxies, p)
			proxyIndex = len(proxies) - 1
		}
		ep, e := d.EnsureEndpoint(ctx, proxies[proxyIndex].ID, n.Hosts[0].ID)
		if e != nil {
			t.Fatal(e)
		}
		p, e := d.CreatePath(ctx, CreatePathParams{Name: name, EndpointID: ep.ID, Enabled: true, Visibility: PathVisibilitySelectable})
		if e != nil {
			t.Fatal(e)
		}
		paths = append(paths, p)
	}
	return u, paths, proxies
}

func applyGroup(t *testing.T, d *DB, c PathGroupChange) PathGroupImpact {
	t.Helper()
	ctx := context.Background()
	preview, e := d.ChangePathGroup(ctx, c, true)
	if e != nil {
		t.Fatal(e)
	}
	c.PreviewToken = preview.PreviewToken
	result, e := d.ChangePathGroup(ctx, c, false)
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func saveGroup(t *testing.T, d *DB, name string, paths ...Path) PathGroup {
	t.Helper()
	ids := []string{}
	for _, p := range paths {
		ids = append(ids, p.ID)
	}
	r := applyGroup(t, d, PathGroupChange{Operation: "save", Name: name, Enabled: true, PathIDs: ids})
	groups, e := d.ListPathGroups(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range groups {
		if g.ID == r.GroupID {
			return g
		}
	}
	t.Fatal("missing saved group")
	return PathGroup{}
}
func assertGroupPaths(t *testing.T, d *DB, user string, want ...Path) {
	t.Helper()
	got, e := d.ListActivePathAccessesByUser(context.Background(), user)
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]bool{}
	for _, p := range got {
		ids[p.PathID] = true
	}
	if len(ids) != len(want) {
		t.Fatalf("got Paths %v; want %v", ids, want)
	}
	for _, p := range want {
		if !ids[p.ID] {
			t.Fatalf("missing Path %s", p.Name)
		}
	}
}

func TestPathGroupsTrackMembershipAndUnionSources(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, proxies := groupFixture(t, d)
	first := saveGroup(t, d, "first", p[0], p[1])
	second := saveGroup(t, d, "second", p[1])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: first.ID, UserID: u.ID})
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: second.ID, UserID: u.ID})
	if _, e := d.GrantPathToUser(ctx, u.ID, p[0].ID); e != nil {
		t.Fatal(e)
	}
	assertGroupPaths(t, d, u.ID, p[0], p[1])
	sources, e := d.UserPathSources(ctx, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !sources[p[0].ID].Direct || len(sources[p[0].ID].Groups) != 1 || len(sources[p[1].ID].Groups) != 2 {
		t.Fatalf("sources %+v", sources)
	}
	credential, e := d.getProxyCredentialByIDs(ctx, u.ID, proxies[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: first.ID, Name: first.Name, Enabled: true, Revision: first.Revision, PathIDs: []string{p[2].ID}})
	assertGroupPaths(t, d, u.ID, p[0], p[1], p[2])
	current, e := d.getProxyCredentialByIDs(ctx, u.ID, proxies[0].ID)
	if e != nil || current.CredentialJSON != credential.CredentialJSON {
		t.Fatalf("shared credential changed: %v", e)
	}
	if _, e = d.RevokePathAccess(ctx, u.ID, p[1].ID); e != nil {
		t.Fatal(e)
	} // no direct source: group remains
	assertGroupPaths(t, d, u.ID, p[0], p[1], p[2])
	applyGroup(t, d, PathGroupChange{Operation: "revoke", GroupID: second.ID, UserID: u.ID})
	assertGroupPaths(t, d, u.ID, p[0], p[2])
	if _, e = d.RevokePathAccess(ctx, u.ID, p[0].ID); e != nil {
		t.Fatal(e)
	}
	current, e = d.getProxyCredentialByIDs(ctx, u.ID, proxies[0].ID)
	if e != nil || current.Enabled {
		t.Fatalf("unused credential still enabled: %+v %v", current, e)
	}
	assertGroupPaths(t, d, u.ID, p[2])
}

func TestPathGroupPreviewRollsBackAndRejectsStaleState(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "test", p[0])
	change := PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID}
	preview, e := d.ChangePathGroup(ctx, change, true)
	if e != nil {
		t.Fatal(e)
	}
	assertGroupPaths(t, d, u.ID)
	credentials, e := d.q.ListProxyAccessesByUserName(ctx, u.Name)
	if e != nil || len(credentials) != 0 {
		t.Fatalf("preview leaked credential: %v %v", credentials, e)
	}
	if _, e = d.GrantPathToUser(ctx, u.ID, p[1].ID); e != nil {
		t.Fatal(e)
	}
	change.PreviewToken = preview.PreviewToken
	if _, e = d.ChangePathGroup(ctx, change, false); !errors.Is(e, ErrPathGroupConflict) {
		t.Fatalf("stale token accepted: %v", e)
	}
	assertGroupPaths(t, d, u.ID, p[1])
}

func TestPathGroupMembershipFailureIsAtomic(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "atomic", p[0])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	if _, e := d.sql.Exec(`UPDATE paths SET display_name='collision' WHERE id IN (?,?)`, p[0].ID, p[2].ID); e != nil {
		t.Fatal(e)
	}
	before, e := d.q.PathGroupPreviewState(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = d.ChangePathGroup(ctx, PathGroupChange{Operation: "save", GroupID: g.ID, Name: g.Name, Enabled: true, Revision: g.Revision, PathIDs: []string{p[0].ID, p[2].ID}}, true)
	if e == nil {
		t.Fatal("accepted published name conflict")
	}
	after, e := d.q.PathGroupPreviewState(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if before != after {
		t.Fatal("failed transaction changed authorization or technical credentials")
	}
}

func TestPathGroupDisableArchiveAndDeletedResources(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, proxies := groupFixture(t, d)
	g := saveGroup(t, d, "lifecycle", p[0], p[2])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: g.ID, Name: g.Name, Enabled: false, Revision: g.Revision, PathIDs: g.PathIDs})
	assertGroupPaths(t, d, u.ID)
	g.Revision++
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: g.ID, Name: g.Name, Enabled: true, Revision: g.Revision, PathIDs: g.PathIDs})
	assertGroupPaths(t, d, u.ID, p[0], p[2])
	if _, e := d.SoftDeleteProxy(ctx, proxies[0].NodeID, proxies[0].ID); e != nil {
		t.Fatal(e)
	}
	groups, e := d.ListPathGroups(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if containsID(groups[0].PathIDs, p[0].ID) {
		t.Fatal("retired Path remained a group member")
	}
	// Editing another member must not resurrect the retired authorization.
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: g.ID, Name: g.Name, Enabled: true, Revision: groups[0].Revision, PathIDs: []string{p[2].ID}})
	assertGroupPaths(t, d, u.ID, p[2])
	if _, e = d.SoftDeleteProxyUser(ctx, u.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = d.RestoreProxyUser(ctx, u.ID); e != nil {
		t.Fatal(e)
	}
	assertGroupPaths(t, d, u.ID)
	groups, e = d.ListPathGroups(ctx)
	if e != nil || len(groups[0].UserIDs) != 0 {
		t.Fatalf("deleted user source survived: %v %v", groups, e)
	}
}

func TestPathGroupUnrelatedEditDoesNotRestoreDisabledCredential(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, proxies := groupFixture(t, d)
	g := saveGroup(t, d, "disabled-credential", p[0])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	if _, e := d.SoftDeleteProxyCredential(ctx, u.ID, proxies[0].NodeID, proxies[0].ID); e != nil {
		t.Fatal(e)
	}
	before, e := d.getProxyCredentialByIDs(ctx, u.ID, proxies[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	applyGroup(t, d, PathGroupChange{Operation: "save", GroupID: g.ID, Name: "renamed", Enabled: true, Revision: g.Revision, PathIDs: g.PathIDs})
	after, e := d.getProxyCredentialByIDs(ctx, u.ID, proxies[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("unrelated group edit restored a revoked credential")
	}
	// A new Path on the same Proxy is not permission to undo an operator's
	// credential revocation on an existing, still-granted Path.
	_, err := d.ChangePathGroup(ctx, PathGroupChange{Operation: "save", GroupID: g.ID, Name: "renamed", Enabled: true, Revision: g.Revision + 1, PathIDs: []string{p[0].ID, p[1].ID}}, true)
	if err == nil {
		t.Fatal("membership extension would restore a revoked credential")
	}
}

func TestPathGroupConcurrentUpdatesRequireNewReview(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	_, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "concurrent", p[0])
	changes := []PathGroupChange{{Operation: "save", GroupID: g.ID, Name: "first", Enabled: true, Revision: g.Revision, PathIDs: []string{p[0].ID}}, {Operation: "save", GroupID: g.ID, Name: "second", Enabled: true, Revision: g.Revision, PathIDs: []string{p[2].ID}}}
	for i := range changes {
		r, e := d.ChangePathGroup(ctx, changes[i], true)
		if e != nil {
			t.Fatal(e)
		}
		changes[i].PreviewToken = r.PreviewToken
	}
	results := make(chan error, 2)
	for _, c := range changes {
		go func(c PathGroupChange) { _, e := d.ChangePathGroup(ctx, c, false); results <- e }(c)
	}
	succeeded, conflicted := 0, 0
	for range changes {
		e := <-results
		if e == nil {
			succeeded++
		} else if errors.Is(e, ErrPathGroupConflict) {
			conflicted++
		} else {
			t.Fatal(e)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("success=%d conflict=%d", succeeded, conflicted)
	}
}

func TestPathGroupsMigrationPreservesDirectGrants(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	goose.SetBaseFS(migrations.FS)
	if e := goose.DownToContext(ctx, d.sql, ".", 33); e != nil {
		t.Fatal(e)
	}
	u, p, _ := groupFixture(t, d)
	if _, e := d.grantPathToUser(ctx, u.ID, p[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := d.grantPathToUser(ctx, u.ID, p[1].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := d.sql.Exec(`UPDATE path_accesses SET enabled=0,deleted_at='2026-10-01' WHERE path_id=?`, p[1].ID); e != nil {
		t.Fatal(e)
	}
	if e := d.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	sources, e := d.UserPathSources(ctx, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(sources) != 1 || !sources[p[0].ID].Direct {
		t.Fatalf("incorrect migration sources: %+v", sources)
	}
	assertGroupPaths(t, d, u.ID, p[0])
}

func TestPathGroupHostSyncCannotCreateDirectGrants(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, _, proxies := groupFixture(t, d)
	managed, err := d.SetProxyDirectPublication(ctx, proxies[0].ID, true)
	if err != nil || len(managed) == 0 {
		t.Fatalf("managed Paths: %v %v", managed, err)
	}
	g := saveGroup(t, d, "managed-group", managed[0])
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	if err = d.SyncLegacyDirectPathsForNode(ctx, proxies[0].NodeName); err != nil {
		t.Fatal(err)
	}
	sources, err := d.UserPathSources(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sources[managed[0].ID].Direct {
		t.Fatal("host sync converted group access into a permanent direct grant")
	}
	applyGroup(t, d, PathGroupChange{Operation: "revoke", GroupID: g.ID, UserID: u.ID})
	assertGroupPaths(t, d, u.ID)
}

func TestPathGroupMultiHopAndSharedCredentialCleanup(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, proxies := groupFixture(t, d)
	chained, err := d.UpdatePath(ctx, UpdatePathParams{ID: p[2].ID, Name: p[2].Name, EndpointID: p[2].EndpointID, DialerPathID: p[0].ID, Enabled: true, Visibility: PathVisibilitySelectable})
	if err != nil {
		t.Fatal(err)
	}
	g := saveGroup(t, d, "chain", chained)
	applyGroup(t, d, PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID})
	assertGroupPaths(t, d, u.ID, chained) // the dependency is not independently granted
	for _, proxy := range proxies {
		c, e := d.getProxyCredentialByIDs(ctx, u.ID, proxy.ID)
		if e != nil || !c.Enabled {
			t.Fatalf("chain credential missing: %+v %v", c, e)
		}
	}
	if _, err = d.GrantPathToUser(ctx, u.ID, p[1].ID); err != nil {
		t.Fatal(err)
	}
	applyGroup(t, d, PathGroupChange{Operation: "revoke", GroupID: g.ID, UserID: u.ID})
	for i, proxy := range proxies {
		c, e := d.getProxyCredentialByIDs(ctx, u.ID, proxy.ID)
		if e != nil || c.Enabled != (i == 0) {
			t.Fatalf("wrong shared credential cleanup: %+v %v", c, e)
		}
	}
}

func TestPathGroupCommitFailureRollsBackSourcesAndCredentials(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	u, p, _ := groupFixture(t, d)
	g := saveGroup(t, d, "failed-commit", p[0], p[2])
	c := PathGroupChange{Operation: "grant", GroupID: g.ID, UserID: u.ID}
	preview, err := d.ChangePathGroup(ctx, c, true)
	if err != nil {
		t.Fatal(err)
	}
	c.PreviewToken = preview.PreviewToken
	before, err := d.q.PathGroupPreviewState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.sql.Exec(`CREATE TRIGGER group_test_failure BEFORE INSERT ON path_accesses BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.ChangePathGroup(ctx, c, false); err == nil {
		t.Fatal("accepted partial mutation")
	}
	after, err := d.q.PathGroupPreviewState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("commit failure leaked source, binding or credential changes")
	}
}
