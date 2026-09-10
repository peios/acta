package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func manager(f securityFixture) *auth.Management {
	return auth.NewManagement(f.service, f.security, f.store)
}
func TestManagedInvitationAndProfile(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, link, e := m.Create(ctx, f.token, "new.user", "Invited person")
	must(t, e)
	if a.Status() != "pending" {
		t.Fatal(a)
	}
	_, e = f.security.PasswordLogin(ctx, "new.user", testPassword, f.binding, "test", "Test")
	if !errors.Is(e, auth.ErrCredentials) {
		t.Fatal(e)
	}
	rows, _, e := m.List(ctx, f.token, "pending", "invited", 0)
	must(t, e)
	if len(rows) != 1 || rows[0].ID != a.ID {
		t.Fatal(rows)
	}
	a, e = m.Update(ctx, f.token, a.ID, a.ProfileVersion, "renamed.user", "Admin name")
	must(t, e)
	_, _, e = m.Create(ctx, f.token, "new.user", "")
	var field *accounts.FieldError
	if !errors.As(e, &field) {
		t.Fatal("reserved name was available", e)
	}
	g, view, e := m.InspectLink(ctx, link, "test")
	must(t, e)
	if g.Purpose != "invite" || view.Username != "renamed.user" {
		t.Fatal(g, view)
	}
	must(t, m.Redeem(ctx, link, testPassword, "Chosen name", "test", view.ProfileVersion))
	_, _, e = m.InspectLink(ctx, link, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("link reusable", e)
	}
	a, e = m.Get(ctx, f.token, a.ID)
	must(t, e)
	if a.Status() != "active" || *a.DisplayName != "Chosen name" {
		t.Fatal(a)
	}
	login, e := f.security.PasswordLogin(ctx, a.Username, testPassword, "other-browser", "test", "Other browser")
	must(t, e)
	_, _, e = m.List(ctx, login.SessionToken, "", "", 0)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("ordinary user managed accounts", e)
	}
	_, _, e = m.Create(ctx, login.SessionToken, "intruder", "")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	_, e = m.Update(ctx, login.SessionToken, a.ID, a.ProfileVersion, "intruder", "")
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
}
func TestRecoveryScopesAndSessionRevocation(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, invite, e := m.Create(ctx, f.token, "member", "")
	must(t, e)
	must(t, m.Redeem(ctx, invite, testPassword, "", "test", a.ProfileVersion))
	login, e := f.security.PasswordLogin(ctx, "member", testPassword, "member-binding", "test", "Member browser")
	must(t, e)
	member := f
	member.token = login.SessionToken
	member.binding = "member-binding"
	member.register(t)
	_, codes := member.enroll(t)
	reset, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	must(t, m.Redeem(ctx, reset, "another meadow lamp orchard", "ignored", "test", 0))
	_, e = f.service.Current(ctx, member.token)
	if !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal("reset preserved session", e)
	}
	pending, e := f.security.PasswordLogin(ctx, "member", "another meadow lamp orchard", member.binding, "test", "Member browser")
	must(t, e)
	if pending.Step != "code" {
		t.Fatal("normal reset disabled MFA")
	}
	completed := member.step(t, pending, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	member.token = completed.SessionToken
	view, e := f.security.View(ctx, member.token)
	must(t, e)
	if len(view.Passkeys) != 1 || !view.MFA {
		t.Fatal("normal reset removed factors", view)
	}
	clear, _, e := m.Link(ctx, f.token, a.ID, true, true)
	must(t, e)
	before, e := f.security.View(ctx, member.token)
	must(t, e)
	if !before.MFA || len(before.Passkeys) != 1 {
		t.Fatal("issuing link changed factors")
	}
	must(t, m.Redeem(ctx, clear, testPassword, "", "test", 0))
	fresh, e := f.security.PasswordLogin(ctx, "member", testPassword, member.binding, "test", "Member browser")
	must(t, e)
	after, e := f.security.View(ctx, fresh.SessionToken)
	must(t, e)
	if after.MFA || len(after.Passkeys) != 0 || after.RecoveryRemaining != 0 {
		t.Fatal(after)
	}
}
func TestDisableAndEnableDoNotResurrectSessionsOrLinks(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, invite, e := m.Create(ctx, f.token, "member", "")
	must(t, e)
	must(t, m.Redeem(ctx, invite, testPassword, "", "test", 1))
	login, e := f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member browser")
	must(t, e)
	reset, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	must(t, m.Disable(ctx, f.token, a.ID, true))
	_, e = f.service.Current(ctx, login.SessionToken)
	if !errors.Is(e, auth.ErrAccountDisabled) {
		t.Fatal("revoked browser did not get disabled explanation", e)
	}
	_, e = f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member browser")
	if !errors.Is(e, auth.ErrAccountDisabled) {
		t.Fatal(e)
	}
	_, e = f.security.PasswordLogin(ctx, "member", "incorrect", "member", "test", "Member browser")
	if !errors.Is(e, auth.ErrCredentials) {
		t.Fatal("disabled status leaked without credentials", e)
	}
	_, _, e = m.InspectLink(ctx, reset, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal(e)
	}
	must(t, m.Disable(ctx, f.token, a.ID, false))
	_, e = f.service.Current(ctx, login.SessionToken)
	if !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal("old session resurrected", e)
	}
	_, _, e = m.InspectLink(ctx, reset, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("old link resurrected", e)
	}
	_, e = f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member browser")
	must(t, e)
	admin, e := f.service.Current(ctx, f.token)
	must(t, e)
	e = m.Disable(ctx, f.token, admin.ID, true)
	if e == nil {
		t.Fatal("last administrator disabled")
	}
}
func TestAccountLinksReplaceExpireAndRedeemOnce(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, old, e := m.Create(ctx, f.token, "member", "")
	must(t, e)
	fresh, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	_, _, e = m.InspectLink(ctx, old, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() {
			<-start
			e := m.Redeem(ctx, fresh, testPassword, "Member", "test", 1)
			if e == nil {
				wins.Add(1)
			} else if !errors.Is(e, auth.ErrAccountLink) {
				t.Error(e)
			}
		})
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("link redeemed more than once", wins.Load())
	}
	expiry, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	_, e = f.conn.Exec(ctx, `UPDATE account_links SET expires_at=$1 WHERE account_id=$2`, time.Now().Add(-time.Minute), a.ID)
	must(t, e)
	_, _, e = m.InspectLink(ctx, expiry, "test")
	if !errors.Is(e, auth.ErrAccountLink) {
		t.Fatal("expired link accepted", e)
	}
}

func TestDisabledPasskeyOnlyExplainsAfterValidProof(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, invite, e := m.Create(ctx, f.token, "member", "")
	must(t, e)
	must(t, m.Redeem(ctx, invite, testPassword, "", "test", 1))
	login, e := f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member")
	must(t, e)
	member := f
	member.token = login.SessionToken
	member.binding = "member"
	key := member.register(t)
	must(t, m.Disable(ctx, f.token, a.ID, true))
	flow := member.begin(t, "login")
	_, e = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: key.assertion(t, flow, false, "http://localhost:8081")}, "", member.binding, "test", "Member")
	if e == nil || errors.Is(e, auth.ErrAccountDisabled) {
		t.Fatal("disabled status disclosed before verified proof", e)
	}
	_, e = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: key.assertion(t, flow, true, "http://localhost:8081")}, "", member.binding, "test", "Member")
	if !errors.Is(e, auth.ErrAccountDisabled) {
		t.Fatal("valid disabled passkey not explained", e)
	}
}
func TestAdminResetRacesWithPendingLogin(t *testing.T) {
	f := securityDatabase(t)
	m := manager(f)
	ctx := t.Context()
	a, invite, e := m.Create(ctx, f.token, "member", "")
	must(t, e)
	must(t, m.Redeem(ctx, invite, testPassword, "", "test", 1))
	login, e := f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member")
	must(t, e)
	member := f
	member.token = login.SessionToken
	member.binding = "member"
	_, codes := member.enroll(t)
	pending, e := f.security.PasswordLogin(ctx, "member", testPassword, "member", "test", "Member")
	must(t, e)
	reset, _, e := m.Link(ctx, f.token, a.ID, false, false)
	must(t, e)
	var wg sync.WaitGroup
	start := make(chan struct{})
	var completed auth.FlowResult
	var loginError, resetError error
	wg.Go(func() {
		<-start
		completed, loginError = f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Recovery: true, Code: codes[0]}, "", member.binding, "test", "Member")
	})
	wg.Go(func() { <-start; resetError = m.Redeem(ctx, reset, "replacement quiet meadow lantern", "", "test", 0) })
	close(start)
	wg.Wait()
	must(t, resetError)
	if loginError != nil && !errors.Is(loginError, auth.ErrFlow) {
		t.Fatal(loginError)
	}
	if completed.SessionToken != "" {
		_, e = f.service.Current(ctx, completed.SessionToken)
		if !errors.Is(e, auth.ErrUnauthenticated) {
			t.Fatal("pending old login survived reset", e)
		}
	}
}

func TestManagementHTTPAuthorizationAndDisabledPageSignal(t *testing.T) {
	f := securityDatabase(t)
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	handler := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	admin := client{t, handler, map[string]*http.Cookie{"acta_session": {Name: "acta_session", Value: f.token}}}
	anonymous := client{t, handler, map[string]*http.Cookie{}}
	anonymous.request("GET", "users", "", 401)
	var created struct {
		Account struct {
			ID      string `json:"id"`
			Version int64  `json:"profile_version"`
		}
		URL string `json:"url"`
	}
	response := admin.request("POST", "users", `{"username":"http.member","display_name":"HTTP Member"}`, 201)
	must(t, json.Unmarshal(response.Body.Bytes(), &created))
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invitation response cacheable")
	}
	parsed, e := url.Parse(created.URL)
	must(t, e)
	fragment, e := url.ParseQuery(parsed.Fragment)
	must(t, e)
	grant := fragment.Get("token")
	anonymous.request("POST", "account-link/open", body(map[string]any{"token": grant}), 200)
	anonymous.request("POST", "account-link/redeem", body(map[string]any{"token": grant, "new_password": testPassword, "display_name": "Chosen", "profile_version": created.Account.Version}), 200)
	anonymous.request("POST", "login", `{"username":"http.member","password":"silver meadow orbit lamp"}`, 200)
	anonymous.request("GET", "users", "", 403)
	anonymous.request("POST", "users/"+created.Account.ID+"/link", `{}`, 403)
	admin.request("POST", "users/"+created.Account.ID+"/disabled", `{"disabled":true}`, 200)
	denied := anonymous.request("GET", "account", "", 403)
	var payload struct {
		Error struct {
			Code string `json:"code"`
		}
	}
	must(t, json.Unmarshal(denied.Body.Bytes(), &payload))
	if payload.Error.Code != "account_disabled" {
		t.Fatal("missing disabled-page signal", payload)
	}
	anonymous.request("POST", "login", `{"username":"http.member","password":"wrong"}`, 401)
	anonymous.request("POST", "login", `{"username":"http.member","password":"silver meadow orbit lamp"}`, 403)
	admin.request("POST", "users/"+created.Account.ID+"/disabled", `{"disabled":false}`, 200)
	anonymous.request("GET", "account", "", 401)
	admin.request("GET", "users/00000000-0000-0000-0000-000000000000", "", 404)
}
