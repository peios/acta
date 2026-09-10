package integration

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	ws "acta2/internal/workspaces"
)

func workspaceOwner(t *testing.T, root securityFixture, name string) (accounts.Account, securityFixture, ws.Workspace) {
	t.Helper()
	a, f := permissionMember(t, root, name)
	a = grantPermissions(t, root, a.ID, []string{accounts.CreateWorkspaces}, false)
	w, e := manager(f).CreateWorkspace(t.Context(), f.token, "Workspace "+name, name, "")
	must(t, e)
	return a, f, w
}
func readWorkspace(t *testing.T, f securityFixture, id string) ws.Workspace {
	t.Helper()
	w, e := manager(f).Workspace(t.Context(), f.token, id, false)
	must(t, e)
	return w
}
func joinWorkspace(t *testing.T, f securityFixture, w ws.Workspace, id string) ws.Workspace {
	t.Helper()
	w = readWorkspace(t, f, w.ID)
	must(t, manager(f).WorkspaceMembership(t.Context(), f.token, w.ID, id, w.Version, true))
	return readWorkspace(t, f, w.ID)
}
func workspaceGrants(t *testing.T, f securityFixture, w ws.Workspace, id string, group bool, grants []string) ws.Workspace {
	t.Helper()
	w = readWorkspace(t, f, w.ID)
	must(t, manager(f).WorkspaceGrants(t.Context(), f.token, w.ID, id, w.Version, group, grants))
	return readWorkspace(t, f, w.ID)
}
func TestWorkspaceIdentityVisibilityAndCreation(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "owner")
	_, other := permissionMember(t, root, "other")
	ctx := t.Context()
	if !ws.Subset(ws.All(), w.Permissions) {
		t.Fatal("creator lacks management")
	}
	if _, e := manager(other).CreateWorkspace(ctx, other.token, "Denied", "denied", ""); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("creation permission", e)
	}
	for _, ref := range []string{w.ID, w.Slug} {
		_, e := manager(other).Workspace(ctx, other.token, ref, ref == w.Slug)
		if !errors.Is(e, auth.ErrNotFound) {
			t.Fatal("private workspace revealed", e)
		}
	}
	list, _, e := manager(other).WorkspaceList(ctx, other.token, "", 0)
	must(t, e)
	if len(list) != 0 {
		t.Fatal("nonmember listing")
	}
	if r := readWorkspace(t, root, w.ID); !ws.Subset(ws.All(), r.Permissions) {
		t.Fatal("Superuser excluded")
	}
	w, e = manager(f).EditWorkspace(ctx, f.token, w.ID, w.Version, "Renamed", "renamed", "Description")
	must(t, e)
	old, e := manager(f).Workspace(ctx, f.token, "owner", true)
	must(t, e)
	if old.ID != w.ID || old.Slug != "renamed" || !slices.Contains(old.PreviousSlugs, "owner") {
		t.Fatal("lost aliases")
	}
	if _, e = manager(other).Workspace(ctx, other.token, "owner", true); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("private alias revealed")
	}
	if _, e = manager(root).CreateWorkspace(ctx, root.token, "Collision", "OWNER", ""); e == nil {
		t.Fatal("reserved slug reused")
	}
	if _, e = manager(f).EditWorkspace(ctx, f.token, w.ID, w.Version-1, "Stale", "stale", ""); !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale profile accepted", e)
	}
	second, e := manager(f).CreateWorkspace(ctx, f.token, "Second", "second", "")
	must(t, e)
	must(t, manager(f).VisitWorkspace(ctx, f.token, w.ID))
	list, _, e = manager(f).WorkspaceList(ctx, f.token, "", 0)
	must(t, e)
	if len(list) != 2 || list[0].ID != w.ID || list[1].ID != second.ID {
		t.Fatal("recent workspace order")
	}
	agent := agentAccount(t, f, "reviewer", nil)
	secret := agentCLI(t, f, agent.ID)
	if _, e = manager(f).CreateWorkspace(ctx, secret, "Agent", "agent", ""); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent creation", e)
	}
}
func TestWorkspaceGroupsMembershipAndAuthority(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "owner")
	user, member := permissionMember(t, root, "member")
	weak, weakSession := permissionMember(t, root, "weak")
	ctx := t.Context()
	g, e := manager(root).CreateGroup(ctx, root.token, "Workspace maintainers", "")
	must(t, e)
	must(t, manager(root).SetGroupMembership(ctx, root.token, g.ID, user.ID, g.Version, user.PermissionsVersion, true))
	w = workspaceGrants(t, f, w, g.ID, true, []string{ws.Edit, ws.Permissions})
	if _, e = manager(member).Workspace(ctx, member.token, w.ID, false); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("group auto-admitted member")
	}
	w = joinWorkspace(t, f, w, weak.ID)
	w = workspaceGrants(t, f, w, weak.ID, false, []string{ws.Members})
	// Membership management intentionally activates authority already granted to a site group.
	must(t, manager(weakSession).WorkspaceMembership(ctx, weakSession.token, w.ID, user.ID, w.Version, true))
	w = readWorkspace(t, f, w.ID)
	access := readWorkspace(t, member, w.ID)
	if !ws.Subset([]string{ws.Edit, ws.Permissions}, access.Permissions) {
		t.Fatal("group grants absent")
	}
	if e = manager(weakSession).WorkspaceMembership(ctx, weakSession.token, w.ID, user.ID, w.Version, false); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("removed stronger member", e)
	}
	if e = manager(member).WorkspaceGrants(ctx, member.token, w.ID, user.ID, w.Version, false, []string{ws.Members}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("unowned workspace grant", e)
	}
	if _, e = manager(weakSession).EditWorkspace(ctx, weakSession.token, w.ID, w.Version, "Changed", "changed", ""); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("members permission edited details", e)
	}
	if _, _, e = manager(member).WorkspaceMembers(ctx, member.token, w.ID, "", 0, true); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("permission manager enumerated candidates")
	}
	// Sources stay additive when a direct grant is removed.
	w = workspaceGrants(t, f, w, user.ID, false, []string{ws.Edit})
	w = workspaceGrants(t, f, w, user.ID, false, nil)
	if !slices.Contains(readWorkspace(t, member, w.ID).Permissions, ws.Edit) {
		t.Fatal("removed group authority with direct grant")
	}
}
func TestWorkspaceAgentInheritanceSelectionAndPruning(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "owner")
	ctx := t.Context()
	a := agentAccount(t, f, "reviewer", nil)
	secret := agentCLI(t, f, a.ID)
	if !ws.Subset(ws.All(), readWorkspace(t, securityFixture{fixture: f.fixture, service: f.service, security: f.security, token: secret, binding: f.binding}, w.ID).Permissions) {
		t.Fatal("default inheritance")
	}
	settings, e := manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	if !settings.All || !settings.Workspaces[0].Inherit {
		t.Fatal("wrong defaults")
	}
	must(t, manager(f).SetAgentWorkspacePolicy(ctx, f.token, a.ID, w.ID, settings.Version, false, []string{ws.Edit}))
	w = workspaceGrants(t, root, w, owner.ID, false, []string{ws.Members, ws.Permissions})
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	if len(settings.Workspaces[0].Grants) != 0 || settings.Workspaces[0].Inherit {
		t.Fatal("explicit grant not pruned")
	}
	w = workspaceGrants(t, root, w, owner.ID, false, ws.All())
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	if len(settings.Workspaces[0].Grants) != 0 {
		t.Fatal("grant resurrected")
	}
	must(t, manager(f).SetAgentWorkspacePolicy(ctx, f.token, a.ID, w.ID, settings.Version, true, nil))
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspaces(ctx, f.token, a.ID, settings.Version, false, []string{w.ID}))
	next, e := manager(f).CreateWorkspace(ctx, f.token, "Next", "next", "")
	must(t, e)
	if _, e = manager(f).Workspace(ctx, secret, next.ID, false); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("selected policy admitted new workspace")
	}
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspaces(ctx, f.token, a.ID, settings.Version, true, nil))
	if v, e := manager(f).Workspace(ctx, secret, next.ID, false); e != nil || !ws.Subset(ws.All(), v.Permissions) {
		t.Fatal("all future workspaces", e)
	}
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspacePolicy(ctx, f.token, a.ID, w.ID, settings.Version, false, ws.All()))
	w = readWorkspace(t, root, w.ID)
	must(t, manager(root).WorkspaceMembership(ctx, root.token, w.ID, owner.ID, w.Version, false))
	if _, e = manager(f).Workspace(ctx, secret, w.ID, false); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("agent survived owner removal")
	}
	w = joinWorkspace(t, root, w, owner.ID)
	w = workspaceGrants(t, root, w, owner.ID, false, ws.All())
	settings, e = manager(f).AgentWorkspaces(ctx, f.token, a.ID)
	must(t, e)
	for _, p := range settings.Workspaces {
		if p.Workspace.ID == w.ID && len(p.Grants) != 0 {
			t.Fatal("remembership resurrected explicit grants")
		}
	}
}
func TestWorkspaceAgentSiteGroupLossPrunes(t *testing.T) {
	root := securityDatabase(t)
	owner, f := permissionMember(t, root, "owner")
	w, e := manager(root).CreateWorkspace(t.Context(), root.token, "Scope", "scope", "")
	must(t, e)
	w = joinWorkspace(t, root, w, owner.ID)
	g, e := manager(root).CreateGroup(t.Context(), root.token, "Editors", "")
	must(t, e)
	must(t, manager(root).SetGroupMembership(t.Context(), root.token, g.ID, owner.ID, g.Version, owner.PermissionsVersion, true))
	w = workspaceGrants(t, root, w, g.ID, true, []string{ws.Edit})
	a := agentAccount(t, f, "reviewer", nil)
	settings, e := manager(f).AgentWorkspaces(t.Context(), f.token, a.ID)
	must(t, e)
	must(t, manager(f).SetAgentWorkspacePolicy(t.Context(), f.token, a.ID, w.ID, settings.Version, false, []string{ws.Edit}))
	owner, e = f.service.Current(t.Context(), f.token)
	must(t, e)
	must(t, manager(root).SetGroupMembership(t.Context(), root.token, g.ID, owner.ID, g.Version, owner.PermissionsVersion, false))
	settings, e = manager(f).AgentWorkspaces(t.Context(), f.token, a.ID)
	must(t, e)
	if len(settings.Workspaces[0].Grants) != 0 {
		t.Fatal("site membership change failed to prune workspace grant")
	}
}
func TestWorkspaceConcurrentEditsAndHTTP(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "owner")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, slug := range []string{"first", "second"} {
		wg.Go(func() {
			_, e := manager(f).EditWorkspace(t.Context(), f.token, w.ID, w.Version, slug, slug, "")
			results <- e
		})
	}
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, auth.ErrPermissionsChanged) {
			stale++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatal("lost update", success, stale)
	}
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	for _, path := range []string{"/api/workspaces", "/api/workspace-slugs/owner", "/api/workspaces/" + w.ID + "/members"} {
		r := httptest.NewRequest("GET", "http://localhost:8081"+path, nil)
		r.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, r)
		if out.Code != 200 {
			t.Fatal(path, out.Code, out.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "http://localhost:8081/api/workspaces", strings.NewReader(`{"name":"Bad origin","slug":"origin"}`))
	r.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
	r.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("missing origin accepted")
	}
}
