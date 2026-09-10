package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"errors"
	"github.com/pquerna/otp/totp"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func permissionMember(t *testing.T, f securityFixture, name string) (accounts.Account, securityFixture) {
	t.Helper()
	ctx := t.Context()
	m := manager(f)
	a, link, e := m.Create(ctx, f.token, name, "Operator label")
	must(t, e)
	must(t, m.Redeem(ctx, link, testPassword, "Operator label", "test", a.ProfileVersion))
	login, e := f.security.PasswordLogin(ctx, name, testPassword, name+"-binding", "test", "Member")
	must(t, e)
	member := f
	member.token = login.SessionToken
	member.binding = name + "-binding"
	a, e = m.Get(ctx, f.token, a.ID)
	must(t, e)
	return a, member
}
func grantPermissions(t *testing.T, f securityFixture, id string, grants []string, required bool) accounts.Account {
	t.Helper()
	m := manager(f)
	a, e := m.Get(t.Context(), f.token, id)
	must(t, e)
	// These earlier tests exercise direct grants in isolation. New accounts now
	// inherit defaults, so explicitly remove those memberships first.
	for _, g := range a.Groups {
		must(t, m.SetGroupMembership(t.Context(), f.token, g.ID, a.ID, g.Version, a.PermissionsVersion, false))
		a, e = m.Get(t.Context(), f.token, id)
		must(t, e)
	}
	a, e = m.UpdatePermissions(t.Context(), f.token, id, a.PermissionsVersion, grants, required)
	must(t, e)
	return a
}
func TestDirectPermissionBoundaries(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a, member := permissionMember(t, f, "reader")
	if len(a.DirectPermissions) != 0 || !slices.Equal(accounts.ResolvePermissions(a), accounts.DefaultPermissions()) {
		t.Fatal("incorrect initial grants", a)
	}
	grantPermissions(t, f, a.ID, []string{accounts.ViewUsers}, false)
	_, _, e := m.List(ctx, member.token, "", "", 0)
	must(t, e)
	_, _, e = m.Create(ctx, member.token, "forbidden", "")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("reader created user", e)
	}
	_, e = m.Update(ctx, member.token, a.ID, a.ProfileVersion, a.Username, "changed")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("reader edited user", e)
	}
	_, _, e = m.Link(ctx, member.token, a.ID, false, false)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("reader reset user", e)
	}
	e = m.Disable(ctx, member.token, a.ID, true)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("reader disabled user", e)
	}
	_, e = m.UpdatePermissions(ctx, member.token, a.ID, 2, []string{accounts.Superuser}, false)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("reader escalated", e)
	}
	profiles := accounts.NewProfileService(f.store)
	_, e = profiles.Update(ctx, a.ID, a.ProfileVersion, a.Username, "changed")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("own display restriction bypassed", e)
	}
	grantPermissions(t, f, a.ID, []string{accounts.ChangeDisplayName}, false)
	_, _, e = m.List(ctx, member.token, "", "", 0)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("existing session retained view grant", e)
	}
	_, e = profiles.Update(ctx, a.ID, a.ProfileVersion, "renamed", "changed")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("display grant allowed rename", e)
	}
	a, e = profiles.Update(ctx, a.ID, a.ProfileVersion, a.Username, "Changed label")
	must(t, e)
	// A user editor cannot route their own profile through the admin endpoint to
	// bypass the more restrictive own-account grants.
	grantPermissions(t, f, a.ID, []string{accounts.EditUsers}, false)
	_, e = m.Update(ctx, member.token, a.ID, a.ProfileVersion, a.Username, "Bypassed")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("self-edit bypass", e)
	}
}
func TestPermissionDelegationAndCredentialAuthority(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	admin, delegate := permissionMember(t, f, "delegate")
	target, _ := permissionMember(t, f, "target")
	login, e := f.security.PasswordLogin(ctx, admin.Username, testPassword, "delegate-session", "test", "Delegate")
	must(t, e)
	delegate.token = login.SessionToken
	grants := []string{accounts.ViewUsers, accounts.ManagePermissions, accounts.ResetCredentials, accounts.CreateUsers}
	grantPermissions(t, f, admin.ID, grants, false)
	target = grantPermissions(t, f, target.ID, []string{}, false)
	for _, grant := range []string{accounts.Superuser, accounts.DisableUsers, accounts.EditUsers} {
		_, e = m.UpdatePermissions(ctx, delegate.token, target.ID, target.PermissionsVersion, []string{grant}, false)
		if !errors.Is(e, auth.ErrForbidden) {
			t.Fatalf("delegated unowned %s: %v", grant, e)
		}
	}
	target, e = m.UpdatePermissions(ctx, delegate.token, target.ID, target.PermissionsVersion, []string{accounts.ViewUsers}, false)
	must(t, e)
	_, _, e = m.Link(ctx, delegate.token, target.ID, false, false)
	must(t, e)
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	for _, id := range []string{root.ID, admin.ID} {
		_, _, e = m.Link(ctx, delegate.token, id, false, false)
		if !errors.Is(e, auth.ErrForbidden) {
			t.Fatal("delegated recovery of privileged account", e)
		}
	}
	// A link issued before a promotion cannot be redeemed after promotion.
	reset, _, e := m.Link(ctx, delegate.token, target.ID, false, false)
	must(t, e)
	target = grantPermissions(t, f, target.ID, []string{accounts.ManagePermissions}, false)
	_, _, e = m.InspectLink(ctx, reset, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("pre-promotion reset survived", e)
	}
	_, _, e = m.Link(ctx, delegate.token, target.ID, true, true)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("access manager credential takeover", e)
	}
	// Removing an unowned existing grant is forbidden, even if not adding one.
	target = grantPermissions(t, f, target.ID, []string{accounts.DisableUsers}, false)
	_, e = m.UpdatePermissions(ctx, delegate.token, target.ID, target.PermissionsVersion, nil, false)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("unowned grant revoked", e)
	}
}
func TestPermissionsRevisionAndLastSuperuser(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	if !root.LastActiveSuperuser {
		t.Fatal("missing last-superuser marker")
	}
	_, e = m.UpdatePermissions(ctx, f.token, root.ID, root.PermissionsVersion, accounts.DefaultPermissions(), false)
	if e == nil {
		t.Fatal("last superuser demoted")
	}
	other, member := permissionMember(t, f, "second")
	other = grantPermissions(t, f, other.ID, []string{accounts.Superuser}, false)
	root, e = m.Get(ctx, f.token, root.ID)
	must(t, e)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, v := range []struct {
		a     accounts.Account
		token string
	}{{root, f.token}, {other, member.token}} {
		wg.Go(func() {
			_, err := m.UpdatePermissions(ctx, v.token, v.a.ID, v.a.PermissionsVersion, accounts.DefaultPermissions(), false)
			if err == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("concurrent demotions", successes.Load())
	}
	var count int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE 'site.superuser'=ANY(direct_permissions) AND disabled_at IS NULL AND NOT pending`).Scan(&count))
	if count != 1 {
		t.Fatal(count)
	}
	// Use the remaining superuser to exercise optimistic concurrency.
	actor := f.token
	current, e := f.service.Current(ctx, actor)
	must(t, e)
	if !accounts.CheckPermission(current, accounts.Superuser) {
		actor = member.token
	}
	target, e := m.Get(ctx, actor, root.ID)
	must(t, e)
	_, e = m.UpdatePermissions(ctx, actor, target.ID, target.PermissionsVersion, target.DirectPermissions, false)
	must(t, e)
	_, e = m.UpdatePermissions(ctx, actor, target.ID, target.PermissionsVersion, target.DirectPermissions, true)
	if !errors.Is(e, auth.ErrPermissionsChanged) {
		t.Fatal("stale permissions overwrote current", e)
	}
}
func TestInvitationRespectsDisplayNamePermission(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a, old, e := m.Create(ctx, f.token, "fixed.name", "Assigned name")
	must(t, e)
	grantPermissions(t, f, a.ID, nil, false)
	_, _, e = m.InspectLink(ctx, old, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("old invitation remained valid", e)
	}
	invite, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	must(t, m.Redeem(ctx, invite, testPassword, "Attempted replacement", "test", a.ProfileVersion))
	a, e = m.Get(ctx, f.token, a.ID)
	must(t, e)
	if a.DisplayName == nil || *a.DisplayName != "Assigned name" {
		t.Fatal("activation changed protected name", a)
	}
}
func TestRequiredMFAEnrollmentAndRecovery(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a, member := permissionMember(t, f, "required")
	// An already-started unrelated flow is also blocked after policy changes.
	old := member.begin(t, "passkey_add")
	grantPermissions(t, f, a.ID, accounts.DefaultPermissions(), true)
	_, e := f.service.Current(ctx, member.token)
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("existing session bypassed enrolment", e)
	}
	identity, e := f.service.Authenticated(ctx, member.token)
	must(t, e)
	if !accounts.RequiresMFASetup(identity) {
		t.Fatal(identity)
	}
	_, e = f.security.View(ctx, member.token)
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("restricted security view", e)
	}
	_, e = f.security.Begin(ctx, "password", "", false, member.token, member.binding, "test")
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("unrelated action permitted", e)
	}
	_, e = f.security.Advance(ctx, auth.FlowInput{ID: old.ID, Action: "registration_begin", Name: "bypass"}, member.token, member.binding, "test", "Test")
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("in-flight action bypass", e)
	}
	flow := member.begin(t, "mfa_setup")
	if flow.Step == "verify" {
		flow = member.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	}
	code, e := totp.GenerateCode(flow.Secret, time.Now())
	must(t, e)
	flow = member.step(t, flow, auth.FlowInput{Action: "setup_verify", Code: code})
	_, e = f.service.Current(ctx, member.token)
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("access before recovery acknowledgement", e)
	}
	codes := flow.Codes
	member.step(t, flow, auth.FlowInput{Action: "acknowledge"})
	_, e = f.service.Current(ctx, member.token)
	must(t, e)
	_, e = f.security.Begin(ctx, "mfa_disable", "", false, member.token, member.binding, "test")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("required MFA disabled", e)
	}
	// Password authentication still requires the second factor.
	login, e := f.security.PasswordLogin(ctx, a.Username, testPassword, member.binding, "test", "Test")
	must(t, e)
	if login.Step != "code" {
		t.Fatal(login)
	}
	login = member.step(t, login, auth.FlowInput{Action: "code", Code: codes[0], Recovery: true})
	member.token = login.SessionToken
	// An administrator-authorized recovery clears the factor, not the policy.
	link, _, e := m.Link(ctx, f.token, a.ID, true, false)
	must(t, e)
	must(t, m.Redeem(ctx, link, testPassword, "", "test", 0))
	login, e = f.security.PasswordLogin(ctx, a.Username, testPassword, member.binding, "test", "Test")
	must(t, e)
	_, e = f.service.Current(ctx, login.SessionToken)
	if !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("recovery bypassed required re-enrolment", e)
	}
}
func TestRequiredMFAPasskeyAndExistingDisableFlow(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	key := f.register(t)
	_, codes := f.enroll(t)
	login, e := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test")
	must(t, e)
	login = f.step(t, login, auth.FlowInput{Action: "code", Code: codes[0], Recovery: true})
	f.token = login.SessionToken
	disabled := f.begin(t, "mfa_disable")
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	grantPermissions(t, f, root.ID, root.DirectPermissions, true)
	_, e = f.security.Advance(ctx, auth.FlowInput{ID: disabled.ID, Action: "confirm"}, f.token, f.binding, "test", "Test")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("pending disable bypassed required MFA", e)
	}
	// Requiring MFA on an enrolled Superuser must not force a redundant setup.
	_, e = f.service.Current(ctx, f.token)
	must(t, e)
	passkey := f.begin(t, "login")
	signed := f.step(t, passkey, auth.FlowInput{Action: "passkey_finish", Credential: key.assertion(t, passkey, true, "http://localhost:8081")})
	if signed.Step != "done" {
		t.Fatal("verified passkey did not satisfy MFA", signed)
	}
	f.token = signed.SessionToken
	policy, e := f.security.Begin(ctx, "mfa_policy", "", true, f.token, f.binding, "test")
	must(t, e)
	f.step(t, policy, auth.FlowInput{Action: "confirm"})
	passkey = f.begin(t, "login")
	signed = f.step(t, passkey, auth.FlowInput{Action: "passkey_finish", Credential: key.assertion(t, passkey, true, "http://localhost:8081")})
	if signed.Step != "code" {
		t.Fatal("extra-code preference ignored", signed)
	}
}
func TestPermissionsHTTPEnforcement(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	a, member := permissionMember(t, f, "http.member")
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	c := client{t, h, map[string]*http.Cookie{}}
	c.cookies["acta_session"] = &http.Cookie{Name: "acta_session", Value: member.token}
	c.request("GET", "permissions", "", 403)
	c.request("POST", "users/"+a.ID+"/permissions", `{"permissions":["site.superuser"],"permissions_version":1}`, 403)
	grantPermissions(t, f, a.ID, accounts.DefaultPermissions(), true)
	c.request("GET", "account", "", 200)
	c.request("GET", "security", "", 403)
	c.request("POST", "account/profile", `{"username":"changed","profile_version":2}`, 403)
	c.request("POST", "security/flow", `{"purpose":"password"}`, 403)
	c.request("POST", "security/flow", `{"purpose":"mfa_setup"}`, 200)
	c.request("POST", "logout", `{}`, 200)
	_, e = f.service.Authenticated(ctx, member.token)
	if !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal(e)
	}
}
