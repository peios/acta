package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"acta/internal/accounts"
	"github.com/google/uuid"
)

const MCPIdentityGrant = "identity.read"
const MCPMemoriesRead = "memories.read"
const MCPMemoriesWrite = "memories.write"
const MCPTasksRead = "tasks.read"
const MCPTasksWrite = "tasks.write"

type mcpAuthorityKey struct{}
type mcpAuthority struct {
	account string
	digest  []byte
}

const OAuthScope = "acta"
const OAuthAccessLifetime = 5 * time.Minute
const OAuthCodeLifetime = time.Minute

type OAuthError struct{ Code, Description string }

func (e *OAuthError) Error() string             { return e.Description }
func oauthError(code, description string) error { return &OAuthError{code, description} }
func invalidGrant() error {
	return oauthError("invalid_grant", "This authorization is invalid, expired or revoked. Connect again.")
}

var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func oauthTokenValid(raw, prefix string) bool {
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	s := strings.TrimPrefix(raw, prefix)
	return !strings.HasPrefix(s, "cli_") && validToken(s)
}
func ValidateRedirect(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return oauthError("invalid_redirect_uri", "Use an absolute HTTPS or loopback HTTP callback URL.")
	}
	local := u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return oauthError("invalid_redirect_uri", "Use an absolute HTTPS or loopback HTTP callback URL.")
	}
	return nil
}
func RedirectMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	a, e := url.Parse(registered)
	if e != nil {
		return false
	}
	b, e := url.Parse(requested)
	if e != nil {
		return false
	}
	// RFC 8252 permits the native client to select an ephemeral loopback port.
	if a.Scheme == "http" && b.Scheme == "http" && net.ParseIP(a.Hostname()).IsLoopback() && a.Hostname() == b.Hostname() {
		a.Host = a.Hostname()
		b.Host = b.Hostname()
		return a.String() == b.String()
	}
	return false
}
func (s *Security) RegisterOAuthClient(ctx context.Context, c OAuthClient, address string) (OAuthClient, error) {
	if err := s.auth.throttle(ctx, "oauth-register:"+address, 20, 5*time.Minute); err != nil {
		return c, err
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		c.Name = "MCP client"
	}
	if len(c.Name) > 100 || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 {
		return c, oauthError("invalid_client_metadata", "Use a client name of at most 100 characters.")
	}
	if c.AuthMethod != "" && c.AuthMethod != "none" {
		return c, oauthError("invalid_client_metadata", "Only public clients are supported.")
	}
	if len(c.Redirects) < 1 || len(c.Redirects) > 8 {
		return c, oauthError("invalid_client_metadata", "Register between one and eight redirect URIs.")
	}
	for _, u := range c.Redirects {
		if err := ValidateRedirect(u); err != nil {
			return c, err
		}
	}
	for _, g := range c.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			return c, oauthError("invalid_client_metadata", "Unsupported grant type.")
		}
	}
	for _, r := range c.ResponseTypes {
		if r != "code" {
			return c, oauthError("invalid_client_metadata", "Only code responses are supported.")
		}
	}
	c.ID = uuid.NewString()
	c.AuthMethod = "none"
	if len(c.GrantTypes) == 0 {
		c.GrantTypes = []string{"authorization_code"}
	}
	if len(c.ResponseTypes) == 0 {
		c.ResponseTypes = []string{"code"}
	}
	err := s.store.SaveOAuthClient(ctx, c)
	return c, err
}
func (s *Security) BeginOAuth(ctx context.Context, in url.Values, binding, address, resource string) (string, error) {
	if err := s.auth.throttle(ctx, "oauth-authorize:"+address, 30, 5*time.Minute); err != nil {
		return "", err
	}
	for _, v := range in {
		if len(v) != 1 {
			return "", oauthError("invalid_request", "Duplicate authorization parameters.")
		}
	}
	c, err := s.store.OAuthClient(ctx, in.Get("client_id"))
	if errors.Is(err, ErrNotFound) {
		return "", oauthError("invalid_client", "Unknown OAuth client.")
	}
	if err != nil {
		return "", err
	}
	if in.Get("response_type") != "code" {
		return "", oauthError("unsupported_response_type", "Only authorization codes are supported.")
	}
	if !slices.Contains(c.GrantTypes, "authorization_code") {
		return "", oauthError("unauthorized_client", "Client does not support authorization codes.")
	}
	redirect := in.Get("redirect_uri")
	valid := false
	if ValidateRedirect(redirect) != nil {
		return "", oauthError("invalid_request", "Invalid redirect URI.")
	}
	for _, u := range c.Redirects {
		valid = valid || RedirectMatches(u, redirect)
	}
	if !valid {
		return "", oauthError("invalid_request", "The callback does not match the registered client.")
	}
	if in.Get("resource") != resource {
		return "", oauthError("invalid_target", "Request access to this Acta MCP endpoint.")
	}
	if in.Get("scope") != "" && in.Get("scope") != OAuthScope {
		return "", oauthError("invalid_scope", "Only Acta access is supported.")
	}
	raw, e := base64.RawURLEncoding.DecodeString(in.Get("code_challenge"))
	if e != nil || len(raw) != 32 || in.Get("code_challenge_method") != "S256" {
		return "", oauthError("invalid_request", "S256 PKCE is required.")
	}
	if len(in.Get("state")) > 2048 {
		return "", oauthError("invalid_request", "State is too long.")
	}
	id, err := Token()
	if err != nil {
		return "", err
	}
	req := OAuthRequest{Digest: Digest(id), Binding: Digest(binding), ClientID: c.ID, ClientName: c.Name, RedirectURI: redirect, Resource: resource, State: in.Get("state"), Challenge: in.Get("code_challenge"), Status: "pending", ExpiresAt: s.now().Add(FlowLifetime)}
	err = s.store.CreateOAuthRequest(ctx, req)
	return id, err
}
func (s *Security) OAuthConsent(ctx context.Context, id, binding string) (OAuthRequest, error) {
	if !validToken(id) {
		return OAuthRequest{}, invalidGrant()
	}
	req, err := s.store.OAuthRequest(ctx, Digest(id), false)
	if errors.Is(err, ErrNotFound) {
		return req, invalidGrant()
	}
	if err != nil {
		return req, err
	}
	if req.Status != "pending" || !s.now().Before(req.ExpiresAt) || subtle.ConstantTimeCompare(req.Binding, Digest(binding)) != 1 {
		return req, invalidGrant()
	}
	return req, nil
}
func (s *Security) ApproveOAuth(ctx context.Context, token, id, binding string, approve bool, issuer, subject string) (string, error) {
	return s.ApproveOAuthTools(ctx, token, id, binding, approve, issuer, subject, []string{MCPIdentityGrant})
}
func (s *Security) ApproveOAuthTools(ctx context.Context, token, id, binding string, approve bool, issuer, subject string, grants []string) (string, error) {
	if !validMCPGrants(grants) {
		return "", ErrForbidden
	}
	if strings.HasPrefix(token, "cli_") {
		return "", ErrForbidden
	}
	a, err := s.auth.Current(ctx, token)
	if err != nil {
		return "", err
	}
	req, err := s.OAuthConsent(ctx, id, binding)
	if err != nil {
		return "", err
	}
	redirect := ""
	err = s.store.WithOAuthRequest(ctx, a.ID, req.Digest, func(r *SecurityRecord, tx SecurityTx, req *OAuthRequest) error {
		if req.Status != "pending" || !s.now().Before(req.ExpiresAt) || subtle.ConstantTimeCompare(req.Binding, Digest(binding)) != 1 {
			return invalidGrant()
		}
		if approve && slices.Contains(grants, MCPMigration) && (r.Account.IsAgent() || !accounts.CheckPermission(r.Account, accounts.Superuser)) {
			return ErrForbidden
		}
		selected := subject
		if !approve {
			selected = ""
		}
		identity, parent, targetTx, err := s.connectionIdentity(ctx, r, tx, token, selected)
		if err != nil {
			return err
		}
		u, _ := url.Parse(req.RedirectURI)
		q := u.Query()
		q.Set("state", req.State)
		q.Set("iss", issuer)
		req.AccountID = identity.ID
		if !approve {
			req.Status = "denied"
			q.Set("error", "access_denied")
		} else {
			code, err := Token()
			if err != nil {
				return err
			}
			sessionSecret, err := Token()
			if err != nil {
				return err
			}
			req.CodeDigest = Digest(code)
			req.SessionDigest = Digest("mcp-session:" + sessionSecret)
			req.SessionID = uuid.NewString()
			req.Status = "approved"
			req.ExpiresAt = s.now().Add(OAuthCodeLifetime)
			now := s.now()
			session := SecuritySession{ID: req.SessionID, Digest: req.SessionDigest, AccountID: identity.ID, AuthorizedBy: a.ID, Kind: "mcp", Tools: grants, Description: "MCP · " + req.ClientName, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(SessionLifetime), Proof: parent.Proof}
			if err = targetTx.PutSession(ctx, session); err != nil {
				return err
			}
			if err = targetTx.Event(ctx, "mcp_authorized", now); err != nil {
				return err
			}
			q.Set("code", code)
		}
		u.RawQuery = q.Encode()
		redirect = u.String()
		return nil
	})
	return redirect, err
}

type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

func (s *Security) issueOAuthTokens(ctx context.Context, tx SecurityTx, session SecuritySession, client, resource string, refreshAllowed bool) (OAuthTokens, error) {
	out := OAuthTokens{TokenType: "Bearer", Scope: OAuthScope}
	access, err := Token()
	if err != nil {
		return out, err
	}
	refresh, err := Token()
	if err != nil {
		return out, err
	}
	out.AccessToken = "mcp_" + access
	if refreshAllowed {
		out.RefreshToken = "mcp_refresh_" + refresh
	}
	expiry := s.now().Add(OAuthAccessLifetime)
	if session.ExpiresAt.Before(expiry) {
		expiry = session.ExpiresAt
	}
	out.ExpiresIn = int(expiry.Sub(s.now()).Seconds())
	if out.ExpiresIn < 1 {
		return out, invalidGrant()
	}
	tokens := []OAuthToken{{Digest: Digest(out.AccessToken), SessionID: session.ID, ClientID: client, Resource: resource, Kind: "access", ExpiresAt: expiry}}
	if refreshAllowed {
		tokens = append(tokens, OAuthToken{Digest: Digest(out.RefreshToken), SessionID: session.ID, ClientID: client, Resource: resource, Kind: "refresh", ExpiresAt: session.ExpiresAt})
	}
	for _, t := range tokens {
		if err = tx.PutOAuthToken(ctx, t); err != nil {
			return OAuthTokens{}, err
		}
	}
	return out, nil
}
func (s *Security) ExchangeOAuth(ctx context.Context, in url.Values, resource, address string) (OAuthTokens, error) {
	out := OAuthTokens{}
	if err := s.auth.throttle(ctx, "oauth-token:"+address, 120, 5*time.Minute); err != nil {
		return out, err
	}
	for _, v := range in {
		if len(v) != 1 {
			return out, oauthError("invalid_request", "Duplicate token parameters.")
		}
	}
	if in.Get("resource") != resource {
		return out, oauthError("invalid_target", "Request access to this Acta MCP endpoint.")
	}
	if in.Get("scope") != "" && in.Get("scope") != OAuthScope {
		return out, oauthError("invalid_scope", "Only Acta access is supported.")
	}
	client, err := s.store.OAuthClient(ctx, in.Get("client_id"))
	if errors.Is(err, ErrNotFound) {
		return out, oauthError("invalid_client", "Unknown OAuth client.")
	}
	if err != nil {
		return out, err
	}
	if !slices.Contains(client.GrantTypes, in.Get("grant_type")) {
		return out, oauthError("unauthorized_client", "This grant type is not registered for the client.")
	}
	switch in.Get("grant_type") {
	case "authorization_code":
		if !validToken(in.Get("code")) || !verifierPattern.MatchString(in.Get("code_verifier")) {
			return out, invalidGrant()
		}
		req, err := s.store.OAuthRequest(ctx, Digest(in.Get("code")), true)
		if errors.Is(err, ErrNotFound) {
			return out, invalidGrant()
		}
		if err != nil {
			return out, err
		}
		compromised := false
		err = s.store.WithOAuthRequest(ctx, req.AccountID, req.Digest, func(r *SecurityRecord, tx SecurityTx, req *OAuthRequest) error {
			challenge := base64.RawURLEncoding.EncodeToString(Digest(in.Get("code_verifier")))
			if !s.now().Before(req.ExpiresAt) || req.ClientID != in.Get("client_id") || req.RedirectURI != in.Get("redirect_uri") || req.Resource != resource || subtle.ConstantTimeCompare([]byte(challenge), []byte(req.Challenge)) != 1 {
				return invalidGrant()
			}
			if req.Status == "consumed" {
				compromised = true
				return tx.RevokeSessions(ctx, []byte{}, req.SessionID)
			}
			if req.Status != "approved" {
				return invalidGrant()
			}
			if accounts.RequiresMFASetup(r.Account) {
				return invalidGrant()
			}
			session, e := tx.Session(ctx, req.SessionDigest, s.now())
			if e != nil {
				return oauthSessionError(e)
			}
			out, e = s.issueOAuthTokens(ctx, tx, session, req.ClientID, req.Resource, slices.Contains(client.GrantTypes, "refresh_token"))
			if e != nil {
				return e
			}
			req.Status = "consumed"
			return nil
		})
		if compromised && err == nil {
			return OAuthTokens{}, invalidGrant()
		}
		return out, oauthSessionError(err)
	case "refresh_token":
		return s.refreshOAuth(ctx, in, resource)
	default:
		return out, oauthError("unsupported_grant_type", "Use authorization_code or refresh_token.")
	}
}
func oauthSessionError(err error) error {
	if errors.Is(err, ErrUnauthenticated) || errors.Is(err, ErrNotFound) {
		return invalidGrant()
	}
	return err
}
func (s *Security) refreshOAuth(ctx context.Context, in url.Values, resource string) (OAuthTokens, error) {
	out := OAuthTokens{}
	raw := in.Get("refresh_token")
	if !oauthTokenValid(raw, "mcp_refresh_") {
		return out, invalidGrant()
	}
	old, err := s.store.ReadOAuthToken(ctx, Digest(raw))
	if errors.Is(err, ErrNotFound) {
		return out, invalidGrant()
	}
	if err != nil {
		return out, err
	}
	compromised := false
	err = s.store.WithSecurity(ctx, old.AccountID, func(r *SecurityRecord, tx SecurityTx) error {
		old, e := tx.OAuthToken(ctx, Digest(raw))
		if e != nil {
			return oauthSessionError(e)
		}
		if old.Resource != resource || old.Kind != "refresh" || old.ClientID != in.Get("client_id") || !s.now().Before(old.ExpiresAt) {
			return invalidGrant()
		}
		session, e := tx.Session(ctx, old.SessionDigest, s.now())
		if e != nil {
			return oauthSessionError(e)
		}
		if accounts.RequiresMFASetup(r.Account) {
			return invalidGrant()
		}
		if old.Used {
			compromised = true
			return tx.RevokeSessions(ctx, []byte{}, old.SessionID)
		}
		if e = tx.ConsumeOAuthToken(ctx, old.Digest); e != nil {
			return e
		}
		out, e = s.issueOAuthTokens(ctx, tx, session, old.ClientID, old.Resource, true)
		return e
	})
	if compromised && err == nil {
		return OAuthTokens{}, invalidGrant()
	}
	return out, oauthSessionError(err)
}
func (s *Security) MCPAccount(ctx context.Context, raw, resource string) (accounts.Account, []string, error) {
	_, a, g, e := s.MCPContext(ctx, raw, resource)
	return a, g, e
}
func (s *Security) MCPContext(ctx context.Context, raw, resource string) (context.Context, accounts.Account, []string, error) {
	var account accounts.Account
	var grants []string
	if !oauthTokenValid(raw, "mcp_") {
		return ctx, account, nil, ErrUnauthenticated
	}
	token, err := s.store.ReadOAuthToken(ctx, Digest(raw))
	if errors.Is(err, ErrNotFound) {
		return ctx, account, nil, ErrUnauthenticated
	}
	if err != nil {
		return ctx, account, nil, err
	}
	err = s.store.WithSecurity(ctx, token.AccountID, func(r *SecurityRecord, tx SecurityTx) error {
		t, e := tx.OAuthToken(ctx, Digest(raw))
		if errors.Is(e, ErrNotFound) {
			return ErrUnauthenticated
		}
		if e != nil {
			return e
		}
		if t.Resource != resource || t.Kind != "access" || !s.now().Before(t.ExpiresAt) {
			return ErrUnauthenticated
		}
		session, e := tx.Session(ctx, t.SessionDigest, s.now())
		if e != nil {
			return e
		}
		if accounts.RequiresMFASetup(r.Account) {
			return ErrMFARequired
		}
		if session.Kind != "mcp" {
			return ErrUnauthenticated
		}
		ctx = context.WithValue(ctx, mcpAuthorityKey{}, mcpAuthority{r.Account.ID, t.SessionDigest})
		account = r.Account
		grants = append([]string{}, session.Tools...)
		if !CanDelegateMigration(account) {
			grants = slices.DeleteFunc(grants, func(g string) bool { return g == MCPMigration })
		}
		return tx.TouchSession(ctx, session.ID, s.now())
	})
	return ctx, account, grants, err
}
func (s *Security) RevokeOAuth(ctx context.Context, raw, clientID, address string) error {
	if err := s.auth.throttle(ctx, "oauth-revoke:"+address, 60, 5*time.Minute); err != nil {
		return err
	}
	t, err := s.store.ReadOAuthToken(ctx, Digest(raw))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if t.ClientID != clientID {
		return nil
	}
	err = s.store.WithSecurity(ctx, t.AccountID, func(_ *SecurityRecord, tx SecurityTx) error { return tx.RevokeSessions(ctx, []byte{}, t.SessionID) })
	if errors.Is(err, ErrUnauthenticated) {
		return nil
	}
	return err
}
