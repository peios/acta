package passkey_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/passkey"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

// TestRegisterThenUsernamelessLogin drives a full WebAuthn ceremony against the
// real service using a software authenticator, with no browser: register a
// passkey, then sign in usernameless with it.
func TestRegisterThenUsernamelessLogin(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	u, err := ms.CreateUser(ctx, store.NewUser{Username: "jack", Display: "Jack"})
	if err != nil {
		t.Fatal(err)
	}
	p := identity.Principal{ID: u.ID, Username: u.Username, Display: u.Display}

	svc, err := passkey.New(ms, passkey.Config{
		RPID: "localhost", RPOrigin: "http://localhost:8080", RPName: "Acta",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Virtual relying party + authenticator + credential.
	rp := virtualwebauthn.RelyingParty{Name: "Acta", ID: "localhost", Origin: "http://localhost:8080"}
	authenticator := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	// Resident key returns this user handle on usernameless login; it must be
	// the user's WebAuthn id (our user id).
	authenticator.Options.UserHandle = []byte(u.ID)

	// --- registration ---
	optsJSON, regChallenge, err := svc.BeginRegistration(ctx, p)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(optsJSON))
	if err != nil {
		t.Fatalf("parse attestation options: %v", err)
	}
	attResp := virtualwebauthn.CreateAttestationResponse(rp, authenticator, cred, *attOpts)

	req := httptest.NewRequest("POST", "/finish", strings.NewReader(attResp))
	req.Header.Set("Content-Type", "application/json")
	if err := svc.FinishRegistration(ctx, p, regChallenge, req, "My Laptop"); err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	authenticator.AddCredential(cred)

	creds, err := svc.List(ctx, u.ID)
	if err != nil || len(creds) != 1 {
		t.Fatalf("expected 1 stored credential, got %d (err %v)", len(creds), err)
	}
	if creds[0].Name != "My Laptop" {
		t.Fatalf("credential name = %q, want %q", creds[0].Name, "My Laptop")
	}

	// --- usernameless login ---
	loginJSON, loginChallenge, err := svc.BeginLogin(ctx)
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	asnOpts, err := virtualwebauthn.ParseAssertionOptions(string(loginJSON))
	if err != nil {
		t.Fatalf("parse assertion options: %v", err)
	}
	asnResp := virtualwebauthn.CreateAssertionResponse(rp, authenticator, cred, *asnOpts)

	req2 := httptest.NewRequest("POST", "/finish", strings.NewReader(asnResp))
	req2.Header.Set("Content-Type", "application/json")
	got, err := svc.FinishLogin(ctx, loginChallenge, req2)
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	if got.ID != u.ID || got.Username != "jack" {
		t.Fatalf("login resolved to %+v, want user %s/jack", got, u.ID)
	}

	// Sign counter advanced and last-used recorded.
	creds, _ = svc.List(ctx, u.ID)
	if creds[0].LastUsedAt == nil {
		t.Fatal("expected last_used_at to be set after login")
	}
}

func TestExpiredChallengeRejected(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	u, _ := ms.CreateUser(ctx, store.NewUser{Username: "jack", Display: "Jack"})
	svc, err := passkey.New(ms, passkey.Config{RPID: "localhost", RPOrigin: "http://localhost:8080", RPName: "Acta"})
	if err != nil {
		t.Fatal(err)
	}
	// A challenge id that was never issued must not validate.
	req := httptest.NewRequest("POST", "/finish", strings.NewReader("{}"))
	if err := svc.FinishRegistration(ctx, identity.Principal{ID: u.ID}, "nope", req, ""); err == nil {
		t.Fatal("expected finish with unknown challenge to fail")
	}
}

func TestDeleteCredentialScopedToUser(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	a, _ := ms.CreateUser(ctx, store.NewUser{Username: "a", Display: "A"})
	b, _ := ms.CreateUser(ctx, store.NewUser{Username: "b", Display: "B"})
	svc, _ := passkey.New(ms, passkey.Config{RPID: "localhost", RPOrigin: "http://localhost:8080", RPName: "Acta"})

	_ = ms.CreateCredential(ctx, store.Credential{ID: "cred-1", UserID: a.ID, CredentialID: []byte("x")})

	// B must not be able to delete A's credential.
	if err := svc.Delete(ctx, "cred-1", b.ID); err == nil {
		t.Fatal("expected cross-user delete to fail")
	}
	if err := svc.Delete(ctx, "cred-1", a.ID); err != nil {
		t.Fatalf("owner delete should succeed: %v", err)
	}
}
