package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pquerna/otp/totp"
)

func TestAgentMCPWhoamiOverStreamableHTTP(t *testing.T) {
	f := securityDatabase(t)
	a := agentAccount(t, f, "reviewer", nil)
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	o := oauthFromAccount(t, f, server.URL+"/mcp")
	redirect, e := f.security.ApproveOAuth(t.Context(), f.token, o.request, f.binding, true, server.URL, a.ID)
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	cfg, e := config.Parse(server.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	client := mcp.NewClient(&mcp.Implementation{Name: "Agent identity test", Version: "1"}, nil)
	session, e := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer session.Close()
	result, e := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	must(t, e)
	encoded, e := json.Marshal(result.StructuredContent)
	must(t, e)
	var identity map[string]any
	must(t, json.Unmarshal(encoded, &identity))
	if result.IsError || identity["id"] != a.ID || identity["username"] != "jack/reviewer" || len(identity) != 3 {
		t.Fatal("MCP did not expose only the selected agent identity", string(encoded))
	}
	must(t, manager(f).DisableAgent(t.Context(), f.token, a.ID, true))
	if _, e = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}}); e == nil {
		t.Fatal("existing MCP transport survived agent disable")
	}
}

func agentAccount(t *testing.T, f securityFixture, name string, grants []string) accounts.Account {
	t.Helper()
	m := manager(f)
	a, e := m.CreateAgent(t.Context(), f.token, name, "")
	must(t, e)
	if len(grants) > 0 {
		a, e = m.AgentPermissions(t.Context(), f.token, a.ID, a.PermissionsVersion, grants)
		must(t, e)
	}
	return a
}
func agentCLI(t *testing.T, f securityFixture, id string) string {
	t.Helper()
	d := startDevice(t, f)
	must(t, f.security.ApproveDevice(t.Context(), f.token, d.UserCode, "test", true, id))
	r, e := f.security.PollDevice(t.Context(), d.DeviceCode)
	must(t, e)
	return r.Token
}
func agentOAuth(t *testing.T, f securityFixture, id string) (oauthFixture, auth.OAuthTokens) {
	t.Helper()
	o := oauthFromAccount(t, f, mcpResource)
	raw, e := f.security.ApproveOAuth(t.Context(), f.token, o.request, f.binding, true, "http://localhost:8081", id)
	must(t, e)
	u, e := url.Parse(raw)
	must(t, e)
	o.code = u.Query().Get("code")
	return o, o.exchange(t)
}
func TestAgentOwnershipAndCredentialBoundaries(t *testing.T) {
	root := securityDatabase(t)
	_, f := permissionMember(t, root, "owner")
	_, other := permissionMember(t, root, "other")
	a := agentAccount(t, f, "Reviewer", nil)
	ctx := t.Context()
	m := manager(f)
	owner, e := f.service.Current(ctx, f.token)
	must(t, e)
	if a.Handle() != "owner/reviewer" || a.ParentID == nil || *a.ParentID != owner.ID || len(a.Groups) != 0 || len(accounts.ResolvePermissions(a)) != 0 {
		t.Fatal("wrong initial agent identity", a)
	}
	for _, id := range []string{a.ID, owner.ID} {
		if _, e = m.Agent(ctx, other.token, id); !errors.Is(e, auth.ErrNotFound) {
			t.Fatal("foreign identity exposed", e)
		}
	}
	_, e = m.UpdateAgent(ctx, other.token, a.ID, a.ProfileVersion, "stolen", "")
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("foreign update", e)
	}
	_, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.Superuser})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent superuser", e)
	}
	_, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.ManagePermissions})
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("unowned grant", e)
	}
	// Owners do not need either site administration or own-profile grants to manage agents.
	grantPermissions(t, root, owner.ID, nil, false)
	a, e = m.UpdateAgent(ctx, f.token, a.ID, a.ProfileVersion, "reviewer.two", "Reviewer")
	must(t, e)
	secret := agentCLI(t, f, a.ID)
	current, e := f.service.Current(ctx, secret)
	must(t, e)
	if current.ID != a.ID || current.Handle() != "owner/reviewer.two" {
		t.Fatal("CLI acted as owner")
	}
	if _, e = m.CreateAgent(ctx, secret, "nested", ""); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("nested ownership", e)
	}
	for _, purpose := range []string{"passkey_add", "mfa_setup", "password", "recovery"} {
		if _, e = f.security.Begin(ctx, purpose, "", false, secret, "binding", "test"); !errors.Is(e, auth.ErrForbidden) {
			t.Fatal("agent credential flow", purpose, e)
		}
	}
	d := startDevice(t, f)
	if e = f.security.ApproveDevice(ctx, secret, d.UserCode, "test", true, ""); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent authorized another session", e)
	}
	if _, e = f.security.PasswordLogin(ctx, current.Handle(), testPassword, "binding", "test", "test"); !errors.Is(e, auth.ErrCredentials) {
		t.Fatal("agent password login", e)
	}
	// Store invariants reject unsupported agent credentials and groups, even if a new caller forgets a guard.
	for _, query := range []string{`INSERT INTO password_credentials(account_id,password_hash) VALUES($1,'invalid')`, `INSERT INTO group_memberships(group_id,account_id) SELECT id,$1 FROM permission_groups WHERE is_default`, `UPDATE accounts SET require_mfa=true WHERE id=$1`, `UPDATE accounts SET direct_permissions=ARRAY['site.superuser'] WHERE id=$1`} {
		if _, e = f.conn.Exec(ctx, query, a.ID); e == nil {
			t.Fatal("agent database boundary", query)
		}
	}
}
func TestAgentGrantLossIsPermanentAndSourceAware(t *testing.T) {
	root := securityDatabase(t)
	owner, f := permissionMember(t, root, "owner")
	ctx := t.Context()
	m := manager(root)
	owner = grantPermissions(t, root, owner.ID, []string{accounts.ViewUsers}, false)
	g := newGroup(t, root, "Readers", []string{accounts.ViewUsers}, false)
	owner = membership(t, root, g, owner.ID, true)
	a := agentAccount(t, f, "reader", []string{accounts.ViewUsers})
	secret := agentCLI(t, f, a.ID)
	// Remove one source: direct grant removal must preserve delegation supplied by a group.
	_, e := m.UpdatePermissions(ctx, root.token, owner.ID, owner.PermissionsVersion, nil, false)
	must(t, e)
	current, e := f.service.Current(ctx, secret)
	must(t, e)
	if !accounts.CheckPermission(current, accounts.ViewUsers) {
		t.Fatal("remaining source lost")
	}
	g, e = m.UpdateGroupPermissions(ctx, root.token, g.ID, g.Version, nil, false)
	must(t, e)
	current, e = f.service.Current(ctx, secret)
	must(t, e)
	if accounts.CheckPermission(current, accounts.ViewUsers) || len(current.DirectPermissions) != 0 {
		t.Fatal("group loss not pruned")
	}
	g, e = m.UpdateGroupPermissions(ctx, root.token, g.ID, g.Version, []string{accounts.ViewUsers}, false)
	must(t, e)
	current, e = f.service.Current(ctx, secret)
	must(t, e)
	if accounts.CheckPermission(current, accounts.ViewUsers) {
		t.Fatal("old delegation reactivated")
	}
	a, e = m.Agent(ctx, f.token, a.ID)
	must(t, e)
	a, e = m.AgentPermissions(ctx, f.token, a.ID, a.PermissionsVersion, []string{accounts.ViewUsers})
	must(t, e)
	membership(t, root, g, owner.ID, false)
	current, e = f.service.Current(ctx, secret)
	must(t, e)
	if len(current.DirectPermissions) != 0 {
		t.Fatal("membership loss not pruned")
	}
	// The live ceiling is also fail-closed when stored grants are stale.
	_, e = f.conn.Exec(ctx, `UPDATE accounts SET direct_permissions=ARRAY['site.users.view'] WHERE id=$1`, a.ID)
	must(t, e)
	current, e = f.service.Current(ctx, secret)
	must(t, e)
	if accounts.CheckPermission(current, accounts.ViewUsers) {
		t.Fatal("stale grant bypassed owner ceiling")
	}
}
func TestAgentOwnerAndAgentDisableRevokeBothTransports(t *testing.T) {
	for _, disableOwner := range []bool{false, true} {
		t.Run(map[bool]string{true: "owner", false: "agent"}[disableOwner], func(t *testing.T) {
			root := securityDatabase(t)
			owner, f := permissionMember(t, root, "owner")
			a := agentAccount(t, f, "reviewer", nil)
			ctx := t.Context()
			cli := agentCLI(t, f, a.ID)
			o, tokens := agentOAuth(t, f, a.ID)
			current, grants, e := f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource)
			must(t, e)
			if current.ID != a.ID || !slices.Equal(grants, []string{auth.MCPIdentityGrant}) {
				t.Fatal("MCP subject or grants")
			}
			sessions, e := manager(f).AgentSessions(ctx, f.token, a.ID)
			must(t, e)
			if len(sessions) != 2 {
				t.Fatal("missing agent sessions")
			}
			for _, s := range sessions {
				if s.AuthorizedBy != owner.ID {
					t.Fatal("lost authorizer")
				}
			}
			if disableOwner {
				must(t, manager(root).Disable(ctx, root.token, owner.ID, true))
			} else {
				must(t, manager(f).DisableAgent(ctx, f.token, a.ID, true))
			}
			if _, e = f.service.Current(ctx, cli); e == nil {
				t.Fatal("disabled CLI usable")
			}
			if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); e == nil {
				t.Fatal("disabled MCP usable")
			}
			if _, e = o.refresh(tokens.RefreshToken); e == nil {
				t.Fatal("disabled session renewed")
			}
			d := startDevice(t, f)
			if e = f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, a.ID); e == nil {
				t.Fatal("disabled identity authorized")
			}
			if disableOwner {
				must(t, manager(root).Disable(ctx, root.token, owner.ID, false))
				login, e := f.security.PasswordLogin(ctx, "owner", testPassword, f.binding, "test", "Owner")
				must(t, e)
				f.token = login.SessionToken
			} else {
				must(t, manager(f).DisableAgent(ctx, f.token, a.ID, false))
			}
			if _, e = f.service.Current(ctx, cli); e == nil {
				t.Fatal("old CLI revived")
			}
			if _, e = o.refresh(tokens.RefreshToken); e == nil {
				t.Fatal("old refresh revived")
			}
			fresh := agentCLI(t, f, a.ID)
			if _, e = f.service.Current(ctx, fresh); e != nil {
				t.Fatal("fresh authorization failed", e)
			}
		})
	}
}
func TestAgentRenamesReserveFullHandlesAndPreserveIdentity(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	a := agentAccount(t, f, "reviewer", nil)
	cli := agentCLI(t, f, a.ID)
	a, e := m.UpdateAgent(ctx, f.token, a.ID, a.ProfileVersion, "writer", "")
	must(t, e)
	owner, e := f.service.Current(ctx, f.token)
	must(t, e)
	_, e = accounts.NewProfileService(f.store).Update(ctx, owner.ID, owner.ProfileVersion, "john", "")
	must(t, e)
	updated, e := m.Agent(ctx, f.token, a.ID)
	must(t, e)
	if updated.Handle() != "john/writer" || !slices.Contains(updated.PreviousUsernames, "jack/reviewer") || !slices.Contains(updated.PreviousUsernames, "jack/writer") {
		t.Fatal("lost historical handles", updated.PreviousUsernames)
	}
	_, e = m.UpdateAgent(ctx, f.token, a.ID, a.ProfileVersion, "stale", "")
	if !errors.Is(e, accounts.ErrProfileChanged) {
		t.Fatal("owner rename did not stale form", e)
	}
	current, e := f.service.Current(ctx, cli)
	must(t, e)
	if current.ID != a.ID || current.Handle() != "john/writer" {
		t.Fatal("rename changed session identity")
	}
	_, e = m.CreateAgent(ctx, f.token, "reviewer", "")
	var field *accounts.FieldError
	if !errors.As(e, &field) {
		t.Fatal("reserved segment claimed", e)
	}
	other := agentAccount(t, f, "other", nil)
	_, e = f.conn.Exec(ctx, `SELECT reserve_account_handle('jack/writer',$1)`, other.ID)
	if e == nil {
		t.Fatal("historical handle stolen")
	}
	a, e = m.UpdateAgent(ctx, f.token, a.ID, updated.ProfileVersion, "reviewer", "")
	must(t, e)
	if a.Handle() != "john/reviewer" {
		t.Fatal(a.Handle())
	}
}
func TestAgentAuthorizationRejectsForeignAndEnforcesOwnerMFA(t *testing.T) {
	root := securityDatabase(t)
	owner, f := permissionMember(t, root, "owner")
	a := agentAccount(t, f, "reviewer", nil)
	ctx := t.Context()
	d := startDevice(t, root)
	if e := root.security.ApproveDevice(ctx, root.token, d.UserCode, "test", true, a.ID); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("foreign CLI subject", e)
	}
	o := oauthFromAccount(t, root, mcpResource)
	_, e := root.security.ApproveOAuth(ctx, root.token, o.request, root.binding, true, "http://localhost:8081", a.ID)
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("foreign OAuth subject", e)
	}
	cli := agentCLI(t, f, a.ID)
	_, tokens := agentOAuth(t, f, a.ID)
	grantPermissions(t, root, owner.ID, nil, true)
	if _, e = f.service.Current(ctx, cli); !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("CLI ignored owner's MFA policy", e)
	}
	if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("MCP ignored owner's MFA policy", e)
	}
	d = startDevice(t, f)
	if e = f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, a.ID); !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("approved with unmet owner MFA", e)
	}
	flow := f.begin(t, "mfa_setup")
	if flow.Step == "verify" {
		flow = f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	}
	code, e := totp.GenerateCode(flow.Secret, time.Now())
	must(t, e)
	flow = f.step(t, flow, auth.FlowInput{Action: "setup_verify", Code: code})
	f.step(t, flow, auth.FlowInput{Action: "acknowledge"})
	fresh := agentCLI(t, f, a.ID)
	if _, e = f.service.Current(ctx, fresh); e != nil {
		t.Fatal("owner MFA did not authorize agent", e)
	}
}
func TestAgentConcurrentGrantLossAndApproval(t *testing.T) {
	root := securityDatabase(t)
	owner, f := permissionMember(t, root, "owner")
	owner = grantPermissions(t, root, owner.ID, []string{accounts.ViewUsers}, false)
	a := agentAccount(t, f, "reviewer", []string{accounts.ViewUsers})
	d := startDevice(t, f)
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() {
		_, e := manager(root).UpdatePermissions(ctx, root.token, owner.ID, owner.PermissionsVersion, nil, false)
		errs <- e
	})
	wg.Go(func() { errs <- f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, a.ID) })
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	result, e := f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	current, e := f.service.Current(ctx, result.Token)
	must(t, e)
	if accounts.CheckPermission(current, accounts.ViewUsers) || len(current.DirectPermissions) != 0 {
		t.Fatal("race retained delegation")
	}
}
func TestAgentHTTPManagementAndAuthorization(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.PublicURL+path, strings.NewReader(body))
		r.Header.Set("Origin", cfg.PublicURL)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
		r.AddCookie(&http.Cookie{Name: "acta_flow_browser", Value: f.binding})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/agents", `{"username":"reviewer","display_name":"Review agent"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var agent struct{ ID, Username string }
	must(t, json.Unmarshal(w.Body.Bytes(), &agent))
	if agent.Username != "jack/reviewer" {
		t.Fatal("HTTP full handle")
	}
	w = request("GET", "/api/agents/permissions", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"id":"site.superuser"`) {
		t.Fatal("invalid catalogue", w.Body.String())
	}
	d := startDevice(t, f)
	w = request("POST", "/api/device/approve", `{"code":"`+d.UserCode+`","approve":true,"account_id":"`+agent.ID+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	result, e := f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	a, e := f.service.Current(ctx, result.Token)
	must(t, e)
	if a.ID != agent.ID {
		t.Fatal("HTTP selection lost")
	}
	o := oauthFromAccount(t, f, mcpResource)
	w = request("POST", "/api/oauth/approve", `{"request":"`+o.request+`","approve":true,"account_id":"`+agent.ID+`"}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var redirect struct{ Redirect string }
	must(t, json.Unmarshal(w.Body.Bytes(), &redirect))
	u, e := url.Parse(redirect.Redirect)
	must(t, e)
	o.code = u.Query().Get("code")
	tokens := o.exchange(t)
	a, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource)
	must(t, e)
	if a.ID != agent.ID {
		t.Fatal("OAuth selected owner")
	}
	w = request("GET", "/api/agents/"+agent.ID+"/sessions", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("POST", "/api/agents/"+agent.ID+"/revoke", `{"id":""}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); e == nil {
		t.Fatal("HTTP session revocation")
	}
}
