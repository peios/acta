package integration

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"

	"acta/internal/auth"
	"github.com/fxamacker/cbor/v2"
)

// protocolAuthenticator exercises server verification with signed WebAuthn
// messages. It does not model a browser or prove a hardware authenticator works.
type protocolAuthenticator struct {
	key     *ecdsa.PrivateKey
	id      []byte
	handle  string
	counter uint32
}

func b64(v []byte) string                  { return base64.RawURLEncoding.EncodeToString(v) }
func jsonBytes(t *testing.T, v any) []byte { t.Helper(); b, e := json.Marshal(v); must(t, e); return b }
func challenge(t *testing.T, flow auth.FlowResult) string {
	t.Helper()
	var options struct{ PublicKey struct{ Challenge string } }
	must(t, json.Unmarshal(flow.Options, &options))
	if options.PublicKey.Challenge == "" {
		t.Fatal("missing challenge")
	}
	return options.PublicKey.Challenge
}
func (a *protocolAuthenticator) data(flags byte) []byte {
	rp := sha256.Sum256([]byte("localhost"))
	data := append(rp[:], flags)
	return binary.BigEndian.AppendUint32(data, a.counter)
}
func (f securityFixture) register(t *testing.T) *protocolAuthenticator {
	t.Helper()
	a := &protocolAuthenticator{id: make([]byte, 32)}
	var err error
	a.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)
	_, err = rand.Read(a.id)
	must(t, err)
	flow := f.begin(t, "passkey_add")
	if flow.Step == "verify" {
		flow = f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	}
	flow = f.step(t, flow, auth.FlowInput{Action: "registration_begin", Name: "Test authenticator"})
	var options struct {
		PublicKey struct{ User struct{ ID string } }
	}
	must(t, json.Unmarshal(flow.Options, &options))
	a.handle = options.PublicKey.User.ID
	client := jsonBytes(t, map[string]any{"type": "webauthn.create", "challenge": challenge(t, flow), "origin": "http://localhost:8081"})
	publicKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: a.key.X.FillBytes(make([]byte, 32)), -3: a.key.Y.FillBytes(make([]byte, 32))})
	must(t, err)
	data := append(a.data(0x45), make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.id)))
	data = append(data, a.id...)
	data = append(data, publicKey...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	must(t, err)
	response := jsonBytes(t, map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(client), "attestationObject": b64(attestation), "transports": []string{"internal"}}, "clientExtensionResults": map[string]any{}})
	done := f.step(t, flow, auth.FlowInput{Action: "registration_finish", Credential: response})
	if done.Step != "done" {
		t.Fatal(done)
	}
	return a
}
func (a *protocolAuthenticator) assertion(t *testing.T, flow auth.FlowResult, uv bool, origin string) json.RawMessage {
	t.Helper()
	a.counter++
	flags := byte(1)
	if uv {
		flags |= 4
	}
	data := a.data(flags)
	client := jsonBytes(t, map[string]any{"type": "webauthn.get", "challenge": challenge(t, flow), "origin": origin})
	clientHash := sha256.Sum256(client)
	signed := sha256.Sum256(append(append([]byte{}, data...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	must(t, err)
	return jsonBytes(t, map[string]any{"id": b64(a.id), "rawId": b64(a.id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(client), "authenticatorData": b64(data), "signature": b64(signature), "userHandle": a.handle}, "clientExtensionResults": map[string]any{}})
}
func TestPasskeyRegistrationAndVerifiedLogin(t *testing.T) {
	f := securityDatabase(t)
	a := f.register(t)
	ctx := t.Context()
	view, err := f.security.View(ctx, f.token)
	must(t, err)
	if len(view.Passkeys) != 1 || view.Passkeys[0].LastUsedAt != nil {
		t.Fatal(view)
	}
	flow := f.begin(t, "login")
	// User presence without signed user verification must never authenticate.
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: a.assertion(t, flow, false, "http://localhost:8081")}, "", f.binding, "test", "Test passkey browser")
	if err == nil {
		t.Fatal("passkey without UV accepted")
	}
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: a.assertion(t, flow, true, "https://other.example")}, "", f.binding, "test", "Test passkey browser")
	if err == nil {
		t.Fatal("wrong origin accepted")
	}
	assertion := a.assertion(t, flow, true, "http://localhost:8081")
	done, err := f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: assertion}, "", f.binding, "test", "Test passkey browser")
	must(t, err)
	if done.Method != "passkey" || done.SessionToken == "" {
		t.Fatal(done)
	}
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: assertion}, "", f.binding, "test", "Test passkey browser")
	if !errors.Is(err, auth.ErrFlow) {
		t.Fatal("consumed challenge replayed", err)
	}
	another := f.begin(t, "login")
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: another.ID, Action: "passkey_finish", Credential: assertion}, "", f.binding, "test", "Test passkey browser")
	if err == nil {
		t.Fatal("assertion crossed challenges")
	}
	view, err = f.security.View(ctx, f.token)
	must(t, err)
	if view.Passkeys[0].LastUsedAt == nil || len(view.Sessions) != 2 {
		t.Fatal(view)
	}
	// A verified passkey also satisfies the shared sensitive-action policy.
	remove, err := f.security.Begin(ctx, "passkey_remove", view.Passkeys[0].ID, false, done.SessionToken, f.binding, "test")
	must(t, err)
	if remove.Step != "confirm" {
		t.Fatal("fresh passkey proof was not reusable", remove.Step)
	}
	f.token = done.SessionToken
	f.step(t, remove, auth.FlowInput{Action: "confirm"})
	view, err = f.security.View(ctx, f.token)
	must(t, err)
	if len(view.Passkeys) != 0 || len(view.Sessions) != 1 {
		t.Fatal("removal did not revoke other sessions", view)
	}
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: another.ID, Action: "passkey_finish", Credential: a.assertion(t, another, true, "http://localhost:8081")}, "", f.binding, "test", "Test passkey browser")
	if !errors.Is(err, auth.ErrFlow) {
		t.Fatal("pending login survived removal", err)
	}
}
func TestPasskeyMFAPolicy(t *testing.T) {
	f := securityDatabase(t)
	a := f.register(t)
	_, codes := f.enroll(t)
	ctx := t.Context()
	login := func() auth.FlowResult {
		flow := f.begin(t, "login")
		done, err := f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "passkey_finish", Credential: a.assertion(t, flow, true, "http://localhost:8081")}, "", f.binding, "test", "Test browser")
		must(t, err)
		return done
	}
	complete := login()
	if complete.SessionToken == "" {
		t.Fatal("default policy required additional MFA after passkey")
	}
	f.token = complete.SessionToken
	policy, err := f.security.Begin(ctx, "mfa_policy", "", true, f.token, f.binding, "test")
	must(t, err)
	f.step(t, policy, auth.FlowInput{Action: "confirm"})
	pending := login()
	if pending.Step != "code" || pending.SessionToken != "" {
		t.Fatal("passkey bypassed enabled extra-code policy")
	}
	complete = f.step(t, pending, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	f.token = complete.SessionToken
	// Fresh proof from before this setting cannot authorize weakening it.
	policy, err = f.security.Begin(ctx, "mfa_policy", "", false, f.token, f.binding, "test")
	must(t, err)
	if policy.Step != "confirm" {
		t.Fatal("completed MFA proof was not reused")
	}
	f.step(t, policy, auth.FlowInput{Action: "confirm"})
	view, err := f.security.View(ctx, f.token)
	must(t, err)
	if view.ExtraCode || view.RecoveryRemaining != 9 {
		t.Fatal(view)
	}
	if login().SessionToken == "" {
		t.Fatal("disabled extra-code policy still required MFA")
	}
}
