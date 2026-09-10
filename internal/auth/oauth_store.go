package auth

import (
	"context"
	"time"
)

// OAuthStore preserves authorization requests across restarts. All request,
// token and session changes serialize on the account before the OAuth record.
type OAuthStore interface {
	SaveOAuthClient(context.Context, OAuthClient) error
	OAuthClient(context.Context, string) (OAuthClient, error)
	CreateOAuthRequest(context.Context, OAuthRequest) error
	OAuthRequest(context.Context, []byte, bool) (OAuthRequest, error)
	WithOAuthRequest(context.Context, string, []byte, func(*SecurityRecord, SecurityTx, *OAuthRequest) error) error
	ReadOAuthToken(context.Context, []byte) (OAuthToken, error)
}
type OAuthClient struct {
	ID            string   `json:"client_id"`
	Name          string   `json:"client_name"`
	Redirects     []string `json:"redirect_uris"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
}
type OAuthRequest struct {
	Digest, Binding, CodeDigest, SessionDigest                                                  []byte
	ClientID, ClientName, RedirectURI, Resource, State, Challenge, AccountID, SessionID, Status string
	ExpiresAt                                                                                   time.Time
}
type OAuthToken struct {
	Digest, SessionDigest                          []byte
	AccountID, SessionID, ClientID, Resource, Kind string
	ExpiresAt                                      time.Time
	Used                                           bool
}
type OAuthTx interface {
	PutOAuthToken(context.Context, OAuthToken) error
	OAuthToken(context.Context, []byte) (OAuthToken, error)
	ConsumeOAuthToken(context.Context, []byte) error
	TouchSession(context.Context, string, time.Time) error
}
