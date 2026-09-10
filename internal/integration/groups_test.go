package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func newGroup(t *testing.T, f securityFixture, name string, grants []string, required bool) accounts.Group {
	t.Helper()
	m := manager(f)
	g, e := m.CreateGroup(t.Context(), f.token, name, "Shared access")
	must(t, e)
	g, e = m.UpdateGroupPermissions(t.Context(), f.token, g.ID, g.Version, grants, required)
	must(t, e)
	return g
}
func membership(t *testing.T, f securityFixture, g accounts.Group, id string, member bool) accounts.Account {
	t.Helper()
	m := manager(f)
	a, e := m.Get(t.Context(), f.token, id)
	must(t, e)
	g, e = m.GetGroup(t.Context(), f.token, g.ID)
	must(t, e)
	must(t, m.SetGroupMembership(t.Context(), f.token, g.ID, id, g.Version, a.PermissionsVersion, member))
	a, e = m.Get(t.Context(), f.token, id)
	must(t, e)
	return a
}
func TestGroupsDefaultAndAdditiveSources(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	if len(root.Groups) != 1 || !root.Groups[0].Default {
		t.Fatal("fresh installation bootstrap account missing Default")
	}
	a, member := permissionMember(t, f, "member")
	if len(a.Groups) != 1 || !a.Groups[0].Default || len(a.DirectPermissions) != 0 || !accounts.CheckPermission(a, accounts.ChangeUsername) {
		t.Fatal("new account defaults", a)
	}
	defaults := a.Groups[0]
	readers := newGroup(t, f, "Readers", []string{accounts.ViewUsers}, false)
	reviewers := newGroup(t, f, "Reviewers", []string{accounts.ViewUsers}, false)
	a = membership(t, f, readers, a.ID, true)
	a = membership(t, f, reviewers, a.ID, true)
	_, _, e = m.List(ctx, member.token, "", "", 0)
	must(t, e)
	a, e = m.UpdatePermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.ViewUsers}, false)
	must(t, e)
	a = membership(t, f, readers, a.ID, false)
	a = membership(t, f, reviewers, a.ID, false)
	if !accounts.CheckPermission(a, accounts.ViewUsers) {
		t.Fatal("membership removal revoked direct grant")
	}
	a, e = m.UpdatePermissions(ctx, f.token, a.ID, a.PermissionsVersion, nil, false)
	must(t, e)
	_, _, e = m.List(ctx, member.token, "", "", 0)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("existing session retained removed grants", e)
	}
	a = membership(t, f, defaults, a.ID, false)
	if accounts.CheckPermission(a, accounts.ChangeUsername) {
		t.Fatal("removed Default still applies")
	}
	_, e = m.UpdateGroup(ctx, f.token, readers.ID, readers.Version, "rEvIeWeRs", "")
	var field *accounts.FieldError
	if !errors.As(e, &field) {
		t.Fatal("case-insensitive group uniqueness", e)
	}
}
func TestGroupDelegationAndElevatedDefault(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	delegate, session := permissionMember(t, f, "delegate")
	delegate = grantPermissions(t, f, delegate.ID, []string{accounts.ManagePermissions, accounts.ViewUsers, accounts.CreateUsers}, false)
	privileged := newGroup(t, f, "Privileged", []string{accounts.Superuser}, false)
	_, e := m.UpdateGroupPermissions(ctx, session.token, privileged.ID, privileged.Version, nil, false)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("removed unowned Superuser", e)
	}
	e = m.SetGroupMembership(ctx, session.token, privileged.ID, delegate.ID, privileged.Version, delegate.PermissionsVersion, true)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("joined Superuser group", e)
	}
	ordinary := newGroup(t, f, "Read access", []string{accounts.ViewUsers}, false)
	e = m.SetGroupMembership(ctx, session.token, ordinary.ID, delegate.ID, ordinary.Version, delegate.PermissionsVersion, true)
	must(t, e)
	_, e = m.UpdateGroupPermissions(ctx, session.token, ordinary.ID, ordinary.Version, []string{accounts.ViewUsers, accounts.EditUsers}, false)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("group edit escalated", e)
	}
	delegated, e := m.CreateGroup(ctx, session.token, "Delegated group", "")
	must(t, e)
	_, e = m.UpdateGroupPermissions(ctx, session.token, delegated.ID, delegated.Version, []string{accounts.ViewUsers}, false)
	must(t, e)
	groups, _, e := m.ListGroups(ctx, f.token, "", 0)
	must(t, e)
	var defaults accounts.Group
	for _, g := range groups {
		if g.Default {
			defaults = g
		}
	}
	_, e = m.UpdateGroupPermissions(ctx, f.token, defaults.ID, defaults.Version, []string{accounts.Superuser}, false)
	must(t, e)
	current, e := f.service.Current(ctx, session.token)
	must(t, e)
	if accounts.CanCreateUser(current) {
		t.Fatal("elevated Default bypasses delegation")
	}
	_, _, e = m.Create(ctx, session.token, "unauthorized", " ")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("created elevated invite via Default", e)
	}
	a, _, e := m.Create(ctx, f.token, "allowed", "")
	must(t, e)
	if !accounts.CheckPermission(a, accounts.Superuser) {
		t.Fatal("Default not assigned")
	}
}
func TestGroupMFAAndCredentialLinks(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a, member := permissionMember(t, f, "unenrolled")
	b, enrolled := permissionMember(t, f, "enrolled")
	enrolled.enroll(t)
	group := newGroup(t, f, "Protected", nil, false)
	membership(t, f, group, a.ID, true)
	membership(t, f, group, b.ID, true)
	link, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	before := member.begin(t, "passkey_add")
	group, e = m.UpdateGroupPermissions(ctx, f.token, group.ID, group.Version, []string{accounts.ManagePermissions}, true)
	must(t, e)
	_, e = f.service.Current(ctx, member.token)
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("group did not require enrolment", e)
	}
	_, e = f.service.Current(ctx, enrolled.token)
	must(t, e)
	_, _, e = m.InspectLink(ctx, link, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("pre-promotion reset survived group change", e)
	}
	_, e = f.security.Advance(ctx, auth.FlowInput{ID: before.ID, Action: "registration_begin", Name: "bypass"}, member.token, member.binding, "test", "Test")
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("old flow bypassed group MFA", e)
	}
	_, e = f.security.Begin(ctx, "mfa_disable", "", false, enrolled.token, enrolled.binding, "test")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("inherited MFA disabled", e)
	}
	// A direct false setting is not a deny and cannot override group policy.
	a, e = m.Get(ctx, f.token, a.ID)
	must(t, e)
	a, e = m.UpdatePermissions(ctx, f.token, a.ID, a.PermissionsVersion, nil, false)
	must(t, e)
	if !accounts.RequiresMFA(a) {
		t.Fatal("direct false overrode group MFA")
	}
	membership(t, f, group, a.ID, false)
	_, e = f.service.Current(ctx, member.token)
	must(t, e)
	group, e = m.UpdateGroupPermissions(ctx, f.token, group.ID, group.Version, nil, false)
	must(t, e)
	_, e = f.security.Begin(ctx, "mfa_disable", "", false, enrolled.token, enrolled.binding, "test")
	must(t, e)
}
func TestGroupSuperuserInvariantAndConcurrentRemoval(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	group := newGroup(t, f, "Site owners", []string{accounts.Superuser}, false)
	root = membership(t, f, group, root.ID, true)
	root, e = m.UpdatePermissions(ctx, f.token, root.ID, root.PermissionsVersion, nil, false)
	must(t, e)
	current, e := f.service.Current(ctx, f.token)
	must(t, e)
	if !accounts.CheckPermission(current, accounts.Superuser) || !current.LastActiveSuperuser {
		t.Fatal(current)
	}
	_, e = m.UpdateGroupPermissions(ctx, f.token, group.ID, group.Version, nil, false)
	if e == nil {
		t.Fatal("last Superuser removed through group edit")
	}
	e = m.SetGroupMembership(ctx, f.token, group.ID, root.ID, group.Version, root.PermissionsVersion, false)
	if e == nil {
		t.Fatal("last Superuser membership removed")
	}
	e = m.Disable(ctx, f.token, root.ID, true)
	if e == nil {
		t.Fatal("last group Superuser disabled")
	}
	second, session := permissionMember(t, f, "second.owner")
	second = membership(t, f, group, second.ID, true)
	root, e = m.Get(ctx, f.token, root.ID)
	must(t, e)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, v := range []struct {
		a     accounts.Account
		token string
	}{{root, f.token}, {second, session.token}} {
		wg.Go(func() {
			if m.SetGroupMembership(ctx, v.token, group.ID, v.a.ID, group.Version, v.a.PermissionsVersion, false) == nil {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("concurrent Superuser membership removals", wins.Load())
	}
	var count int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM superuser_accounts su JOIN accounts a ON a.id=su.id WHERE a.disabled_at IS NULL AND NOT a.pending`).Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
}
func TestGroupVersionsAndHTTPAuthorization(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a, session := permissionMember(t, f, "member")
	group := newGroup(t, f, "Original", nil, false)
	updated, e := m.UpdateGroup(ctx, f.token, group.ID, group.Version, "Renamed", "Useful description")
	must(t, e)
	e = m.SetGroupMembership(ctx, f.token, group.ID, a.ID, group.Version, a.PermissionsVersion, true)
	if !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale group accepted", e)
	}
	membership(t, f, updated, a.ID, true)
	e = m.SetGroupMembership(ctx, f.token, group.ID, a.ID, updated.Version, a.PermissionsVersion, false)
	if !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale account accepted", e)
	}
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	c := client{t, httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), m, cfg), map[string]*http.Cookie{"acta_session": {Name: "acta_session", Value: session.token}}}
	c.request("GET", "groups", "", 403)
	c.request("POST", "groups", `{"name":"denied"}`, 403)
	c.request("GET", "groups/"+group.ID+"/members", "", 403)
	c.request("POST", "groups/"+group.ID+"/permissions", `{"permissions":["site.superuser"],"permissions_version":2}`, 403)
	c.cookies["acta_session"].Value = f.token
	c.request("GET", "groups", "", 200)
	c.request("GET", "groups/"+group.ID+"/members", "", 200)
	c.request("POST", "groups/"+group.ID+"/profile", `{"name":"renamed","description":"Changed","permissions_version":1}`, 409)
}
