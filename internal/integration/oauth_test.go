package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/learn"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpResource = "http://localhost:8081/mcp"

type oauthFixture struct {
	securityFixture
	client                            auth.OAuthClient
	request, verifier, code, resource string
}

func oauthRequest(t *testing.T, resource string) oauthFixture {
	t.Helper()
	return oauthFromAccount(t, securityDatabase(t), resource)
}
func oauthFromAccount(t *testing.T, f securityFixture, resource string) oauthFixture {
	t.Helper()
	client, e := f.security.RegisterOAuthClient(t.Context(), auth.OAuthClient{Name: "Test client", Redirects: []string{"http://127.0.0.1:1234/callback"}, GrantTypes: []string{"authorization_code", "refresh_token"}}, "test")
	must(t, e)
	verifier, e := auth.Token()
	must(t, e)
	in := url.Values{"client_id": {client.ID}, "redirect_uri": {client.Redirects[0]}, "response_type": {"code"}, "resource": {resource}, "scope": {auth.OAuthScope}, "state": {"test-state"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(auth.Digest(verifier))}}
	request, e := f.security.BeginOAuth(t.Context(), in, f.binding, "test", resource)
	must(t, e)
	return oauthFixture{f, client, request, verifier, "", resource}
}
func (f *oauthFixture) approve(t *testing.T) {
	t.Helper()
	redirect, e := f.security.ApproveOAuth(t.Context(), f.token, f.request, f.binding, true, "http://localhost:8081", "")
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	if u.Query().Get("state") != "test-state" || u.Query().Get("iss") != "http://localhost:8081" {
		t.Fatal("lost redirect binding")
	}
	f.code = u.Query().Get("code")
}
func (f oauthFixture) exchangeValues() url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client.ID}, "code": {f.code}, "code_verifier": {f.verifier}, "redirect_uri": {f.client.Redirects[0]}, "resource": {f.resource}}
}
func (f oauthFixture) exchange(t *testing.T) auth.OAuthTokens {
	t.Helper()
	out, e := f.security.ExchangeOAuth(t.Context(), f.exchangeValues(), f.resource, "test")
	must(t, e)
	return out
}
func (f oauthFixture) refresh(raw string) (auth.OAuthTokens, error) {
	return f.security.ExchangeOAuth(context.Background(), url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client.ID}, "refresh_token": {raw}, "resource": {f.resource}}, f.resource, "test")
}
func invalidOAuth(t *testing.T, e error) {
	t.Helper()
	var oe *auth.OAuthError
	if !errors.As(e, &oe) {
		t.Fatalf("expected OAuth rejection, got %v", e)
	}
}
func TestOAuthBindingAndCodeExchange(t *testing.T) {
	f := oauthRequest(t, mcpResource)
	ctx := t.Context()
	_, e := f.security.OAuthConsent(ctx, f.request, "different-browser")
	invalidOAuth(t, e)
	_, e = f.security.ApproveOAuth(ctx, f.token, f.request, "different-browser", true, "http://localhost:8081", "")
	invalidOAuth(t, e)
	f.approve(t)
	for _, key := range []string{"code_verifier", "redirect_uri", "client_id", "resource"} {
		values := f.exchangeValues()
		values.Set(key, "incorrect")
		_, e = f.security.ExchangeOAuth(ctx, values, f.resource, "test")
		invalidOAuth(t, e)
	}
	tokens := f.exchange(t)
	a, grants, e := f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource)
	must(t, e)
	if a.Username != "jack" || len(grants) != 1 || grants[0] != auth.MCPIdentityGrant || tokens.RefreshToken == "" {
		t.Fatal("wrong identity or grants")
	}
	for _, raw := range []string{f.token, tokens.RefreshToken, "cli_" + f.verifier} {
		if _, _, e = f.security.MCPAccount(ctx, raw, mcpResource); e == nil {
			t.Fatal("accepted other credential type")
		}
	}
	if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource+"-other"); e == nil {
		t.Fatal("accepted wrong audience")
	}
	if _, e = f.service.Current(ctx, tokens.AccessToken); e == nil {
		t.Fatal("MCP token reached unrestricted API")
	}
	_, e = f.security.ExchangeOAuth(ctx, f.exchangeValues(), f.resource, "test")
	invalidOAuth(t, e)
	if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); e == nil {
		t.Fatal("code replay did not revoke connection")
	}
}
func TestOAuthRefreshRotationAndRevocation(t *testing.T) {
	f := oauthRequest(t, mcpResource)
	f.approve(t)
	tokens := f.exchange(t)
	ctx := t.Context()
	old, e := f.store.ReadOAuthToken(ctx, auth.Digest(tokens.RefreshToken))
	must(t, e)
	var before time.Time
	must(t, f.conn.QueryRow(ctx, `SELECT last_seen_at FROM browser_sessions WHERE id=$1`, old.SessionID).Scan(&before))
	next, e := f.refresh(tokens.RefreshToken)
	must(t, e)
	if next.AccessToken == tokens.AccessToken || next.RefreshToken == tokens.RefreshToken {
		t.Fatal("tokens not rotated")
	}
	var after time.Time
	must(t, f.conn.QueryRow(ctx, `SELECT last_seen_at FROM browser_sessions WHERE id=$1`, old.SessionID).Scan(&after))
	if !before.Equal(after) {
		t.Fatal("refresh extended session activity")
	}
	_, e = f.refresh(tokens.RefreshToken)
	invalidOAuth(t, e)
	if _, _, e = f.security.MCPAccount(ctx, next.AccessToken, mcpResource); e == nil {
		t.Fatal("refresh reuse did not revoke access")
	}
	if _, e = f.refresh(next.RefreshToken); e == nil {
		t.Fatal("refresh reuse did not revoke renewal")
	}
	if _, e = f.service.Current(ctx, f.token); e != nil {
		t.Fatal("revocation damaged browser", e)
	}
}
func TestOAuthExpiryAndSessionRevocation(t *testing.T) {
	for _, scenario := range []string{"access-expired", "idle", "absolute", "sign-out", "password-change", "mfa-required", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			f := oauthRequest(t, mcpResource)
			f.approve(t)
			tokens := f.exchange(t)
			ctx := t.Context()
			record, e := f.store.ReadOAuthToken(ctx, auth.Digest(tokens.AccessToken))
			must(t, e)
			switch scenario {
			case "disabled":
				_, e = f.conn.Exec(ctx, `UPDATE accounts SET disabled_at=now() WHERE id=$1`, record.AccountID)
			case "access-expired":
				_, e = f.conn.Exec(ctx, `UPDATE oauth_tokens SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, auth.Digest(tokens.AccessToken))
			case "idle":
				_, e = f.conn.Exec(ctx, `UPDATE browser_sessions SET created_at=now()-interval '10 days',last_seen_at=now()-interval '8 days' WHERE id=$1`, record.SessionID)
			case "absolute":
				_, e = f.conn.Exec(ctx, `UPDATE browser_sessions SET created_at=now()-interval '31 days',expires_at=now()-interval '1 second' WHERE id=$1`, record.SessionID)
			case "sign-out":
				e = f.security.Revoke(ctx, f.token, record.SessionID)
			case "password-change":
				flow := f.begin(t, "password")
				f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword, NewPassword: "another long meadow password"})
			case "mfa-required":
				a, err := f.service.Current(ctx, f.token)
				must(t, err)
				_, e = manager(f.securityFixture).UpdatePermissions(ctx, f.token, a.ID, a.PermissionsVersion, a.DirectPermissions, true)
			}
			must(t, e)
			if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); e == nil {
				t.Fatal("invalid access accepted")
			}
			_, e = f.refresh(tokens.RefreshToken)
			if scenario == "access-expired" {
				must(t, e)
			} else if e == nil {
				t.Fatal("invalid session refreshed")
			}
		})
	}
}
func TestOAuthDenialAndConcurrentApproval(t *testing.T) {
	f := oauthRequest(t, mcpResource)
	var n atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, e := f.security.ApproveOAuth(t.Context(), f.token, f.request, f.binding, true, "http://localhost:8081", ""); e == nil {
				n.Add(1)
			}
		})
	}
	wg.Wait()
	if n.Load() != 1 {
		t.Fatal("approval not single use")
	}
	f = oauthRequest(t, mcpResource)
	redirect, e := f.security.ApproveOAuth(t.Context(), f.token, f.request, f.binding, false, "http://localhost:8081", "")
	must(t, e)
	u, e := url.Parse(redirect)
	must(t, e)
	if u.Query().Get("error") != "access_denied" || u.Query().Get("code") != "" {
		t.Fatal("invalid denial")
	}
	view, e := f.security.View(t.Context(), f.token)
	must(t, e)
	if len(view.Sessions) != 1 {
		t.Fatal("denial created session")
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestMCPHTTPDiscoveryAndTools(t *testing.T) {
	account := securityDatabase(t)
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	f := oauthFromAccount(t, account, server.URL+"/mcp")
	f.approve(t)
	tokens := f.exchange(t)
	cfg, e := config.Parse(server.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f.securityFixture), cfg)
	response, e := http.Get(server.URL + "/mcp")
	must(t, e)
	response.Body.Close()
	if response.StatusCode != 401 || !strings.Contains(response.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Fatal("missing discovery challenge")
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server"} {
		response, e = http.Get(server.URL + path)
		must(t, e)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(path, response.StatusCode)
		}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "Acta integration test", Version: "1"}, nil)
	session, e := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
	must(t, e)
	defer session.Close()
	list, e := session.ListTools(t.Context(), nil)
	must(t, e)
	if len(list.Tools) != 2 || list.Tools[0].Name != "acta_guide" || list.Tools[1].Name != "whoami" {
		t.Fatalf("unexpected tools: %+v", list.Tools)
	}
	result, e := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	must(t, e)
	encoded, e := json.Marshal(result.StructuredContent)
	must(t, e)
	var identity map[string]any
	must(t, json.Unmarshal(encoded, &identity))
	if result.IsError || identity["username"] != "jack" || len(identity) != 3 {
		t.Fatal("unexpected identity", string(encoded))
	}
	if instructions := session.InitializeResult().Instructions; !strings.Contains(instructions, "acta_guide") || !strings.Contains(instructions, "does not grant access") {
		t.Fatal("missing agent orientation", instructions)
	}
	assertGuide := func() {
		t.Helper()
		guide, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "acta_guide", Arguments: map[string]any{}})
		must(t, err)
		if guide.IsError || len(guide.Content) != 1 {
			t.Fatal("invalid guide result", guide)
		}
		text, ok := guide.Content[0].(*mcp.TextContent)
		if !ok || text.Text != learn.AgentGuide || !strings.HasPrefix(text.Text, "# Working with Acta") {
			t.Fatal("guide drifted from documented Markdown")
		}
	}
	assertGuide()
	// An unchanged protocol session must use the latest persisted grants.
	record, e := f.store.ReadOAuthToken(t.Context(), auth.Digest(tokens.AccessToken))
	must(t, e)
	_, e = f.conn.Exec(t.Context(), `UPDATE browser_sessions SET tool_grants='{}' WHERE id=$1`, record.SessionID)
	must(t, e)
	list, e = session.ListTools(t.Context(), nil)
	must(t, e)
	if len(list.Tools) != 1 || list.Tools[0].Name != "acta_guide" {
		t.Fatal("removed grant still listed")
	}
	assertGuide() // A guide-only connection still returns the guide without requiring identity grants.
	for _, name := range []string{"whoami", "create_user"} {
		req, err := http.NewRequest("POST", server.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":123,"method":"tools/call","params":{"name":"`+name+`","arguments":{}}}`))
		must(t, err)
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, err := http.DefaultClient.Do(req)
		must(t, err)
		var result struct {
			Error json.RawMessage `json:"error"`
		}
		if resp.StatusCode == 200 {
			must(t, json.NewDecoder(resp.Body).Decode(&result))
			if len(result.Error) == 0 {
				t.Fatal("ungranted tool callable", name)
			}
		}
		resp.Body.Close()
	}
	must(t, f.security.Revoke(t.Context(), f.token, record.SessionID))
	if result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "acta_guide", Arguments: map[string]any{}}); err == nil && !result.IsError {
		t.Fatal("revoked connection could still read guide")
	}

	if _, e = session.ListTools(t.Context(), nil); e == nil {
		t.Fatal("revoked protocol session usable")
	}
	// Browser cookies cannot authenticate MCP, and MCP tokens cannot use the app API.
	for _, path := range []string{"/mcp", "/api/account"} {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
		if path == "/api/account" {
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		}
		resp, err := http.DefaultClient.Do(req)
		must(t, err)
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatal("credential boundary", path, resp.StatusCode)
		}
	}
}
func TestOAuthHTTPApprovalCSRFAndForms(t *testing.T) {
	f := oauthRequest(t, mcpResource)
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	h := httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f.securityFixture), cfg)
	for _, origin := range []string{"", "http://evil.invalid"} {
		req := httptest.NewRequest("POST", "http://localhost:8081/api/oauth/approve", strings.NewReader(`{"request":"`+f.request+`","approve":true}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
		req.AddCookie(&http.Cookie{Name: "acta_flow_browser", Value: f.binding})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("cross-site approval", w.Code)
		}
	}
	req := httptest.NewRequest("POST", "http://localhost:8081/oauth/register", strings.NewReader(`{"client_name":"Test","redirect_uris":["http://127.0.0.1:8000/callback"],"token_endpoint_auth_method":"none","unknown_metadata":"ignored"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatal("registration", w.Code, w.Body.String())
	}
	f.approve(t)
	req = httptest.NewRequest("POST", "http://localhost:8081/oauth/token", strings.NewReader(f.exchangeValues().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("exchange", w.Code, w.Body.String())
	}
	var tokens auth.OAuthTokens
	must(t, json.NewDecoder(w.Body).Decode(&tokens))
	req = httptest.NewRequest("POST", "http://localhost:8081/oauth/revoke", strings.NewReader(url.Values{"token": {tokens.RefreshToken}, "client_id": {f.client.ID}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("revocation", w.Code)
	}
	if _, _, e = f.security.MCPAccount(t.Context(), tokens.AccessToken, mcpResource); e == nil {
		t.Fatal("revocation endpoint failed")
	}
}

func TestOAuthAuthorizationCannotOutliveAuthority(t *testing.T) {
	for _, scenario := range []string{"request-expired", "code-expired", "revoked-before-exchange", "mfa-before-approval"} {
		t.Run(scenario, func(t *testing.T) {
			f := oauthRequest(t, mcpResource)
			ctx := t.Context()
			if scenario == "mfa-before-approval" {
				a, e := f.service.Current(ctx, f.token)
				must(t, e)
				_, e = manager(f.securityFixture).UpdatePermissions(ctx, f.token, a.ID, a.PermissionsVersion, a.DirectPermissions, true)
				must(t, e)
				_, e = f.security.ApproveOAuth(ctx, f.token, f.request, f.binding, true, "http://localhost:8081", "")
				if !errors.Is(e, auth.ErrMFARequired) {
					t.Fatal("approval bypassed MFA", e)
				}
				return
			}
			if scenario != "request-expired" {
				f.approve(t)
			}
			if scenario == "revoked-before-exchange" {
				must(t, f.security.Revoke(ctx, f.token, ""))
			} else {
				_, e := f.conn.Exec(ctx, `UPDATE oauth_requests SET data=jsonb_set(data,'{ExpiresAt}',to_jsonb($2::text)),expires_at=$2::timestamptz WHERE request_hash=$1`, auth.Digest(f.request), time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano))
				must(t, e)
			}
			if scenario == "request-expired" {
				_, e := f.security.OAuthConsent(ctx, f.request, f.binding)
				invalidOAuth(t, e)
			} else {
				_, e := f.security.ExchangeOAuth(ctx, f.exchangeValues(), f.resource, "test")
				invalidOAuth(t, e)
			}
		})
	}
}
func TestOAuthRegistrationAndRefreshEligibility(t *testing.T) {
	f := oauthRequest(t, mcpResource)
	ctx := t.Context()
	for _, c := range []auth.OAuthClient{
		{Redirects: []string{"http://nonloopback.example/callback"}},
		{Redirects: []string{"https://example.com/callback"}, AuthMethod: "client_secret_basic"},
		{Redirects: []string{"https://example.com/callback"}, GrantTypes: []string{"password"}},
	} {
		_, e := f.security.RegisterOAuthClient(ctx, c, "test")
		invalidOAuth(t, e)
	}
	// A public client that registered only authorization_code receives no refresh token.
	f.client.GrantTypes = []string{"authorization_code"}
	_, e := f.conn.Exec(ctx, `UPDATE oauth_clients SET metadata=$2 WHERE id=$1`, f.client.ID, f.client)
	must(t, e)
	f.approve(t)
	tokens := f.exchange(t)
	if tokens.RefreshToken != "" {
		t.Fatal("issued an unregistered refresh grant")
	}
	if _, _, e = f.security.MCPAccount(ctx, tokens.AccessToken, mcpResource); e != nil {
		t.Fatal("code-only access unusable", e)
	}
}
