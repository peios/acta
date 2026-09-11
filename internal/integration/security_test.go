package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/auth"
	"acta/internal/config"
	"acta/internal/httpapi"
	"github.com/pquerna/otp/totp"
)

const testPassword = "silver meadow orbit lamp"

type securityFixture struct {
	fixture
	service        *auth.Service
	security       *auth.Security
	token, binding string
}

func securityDatabase(t *testing.T) securityFixture {
	f := database(t)
	bootstrap(t, f, time.Now())
	a, _, err := auth.New(t.Context(), f.store)
	must(t, err)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	s := securityForTest(t, f, a, cfg)
	binding := "a-random-test-browser-binding"
	login, err := s.PasswordLogin(t.Context(), "jack", testPassword, binding, "test", "Test browser")
	must(t, err)
	return securityFixture{f, a, s, login.SessionToken, binding}
}
func (f securityFixture) begin(t *testing.T, purpose string) auth.FlowResult {
	t.Helper()
	result, err := f.security.Begin(t.Context(), purpose, "", false, f.token, f.binding, "test")
	must(t, err)
	return result
}
func (f securityFixture) step(t *testing.T, flow auth.FlowResult, in auth.FlowInput) auth.FlowResult {
	t.Helper()
	in.ID = flow.ID
	result, err := f.security.Advance(t.Context(), in, f.token, f.binding, "test", "Test browser")
	must(t, err)
	return result
}
func (f securityFixture) enroll(t *testing.T) (string, []string) {
	t.Helper()
	flow := f.begin(t, "mfa_setup")
	if flow.Step == "verify" {
		flow = f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	}
	if flow.Step != "totp_setup" {
		t.Fatalf("setup step %s", flow.Step)
	}
	secret := flow.Secret
	code, err := totp.GenerateCode(secret, time.Now())
	must(t, err)
	flow = f.step(t, flow, auth.FlowInput{Action: "setup_verify", Code: code})
	codes := flow.Codes
	view, err := f.security.View(t.Context(), f.token)
	must(t, err)
	if view.MFA {
		t.Fatal("MFA enabled before acknowledgment")
	}
	flow = f.step(t, flow, auth.FlowInput{Action: "acknowledge"})
	if flow.Step != "done" || len(codes) != 10 {
		t.Fatal("enrollment did not complete")
	}
	return secret, codes
}
func TestSecurityMFARecoveryAndCancellation(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	old, err := f.security.PasswordLogin(ctx, "jack", testPassword, "another-browser", "test", "Other browser")
	must(t, err)
	_, codes := f.enroll(t)
	if _, err = f.service.Current(ctx, old.SessionToken); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("MFA enrollment left old session alive")
	}
	view, err := f.security.View(ctx, f.token)
	must(t, err)
	if !view.MFA || view.RecoveryRemaining != 10 {
		t.Fatal(view)
	}
	pending, err := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test browser")
	must(t, err)
	if pending.Step != "code" || pending.SessionToken != "" {
		t.Fatal("password bypassed MFA")
	}
	in := auth.FlowInput{ID: pending.ID, Action: "code", Code: codes[0], Recovery: true}
	_, err = f.security.Advance(ctx, in, "", "different-binding", "test", "Test browser")
	if !errors.Is(err, auth.ErrFlow) {
		t.Fatal("flow crossed browser binding", err)
	}
	complete, err := f.security.Advance(ctx, in, "", f.binding, "test", "Test browser")
	must(t, err)
	if complete.SessionToken == "" {
		t.Fatal("MFA did not issue session")
	}
	f.token = complete.SessionToken
	replay, err := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test browser")
	must(t, err)
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: replay.ID, Action: "code", Code: codes[0], Recovery: true}, "", f.binding, "test", "Test browser")
	if err == nil {
		t.Fatal("recovery-code replay accepted")
	}
	flow := f.begin(t, "recovery")
	if flow.Step != "recovery_codes" {
		t.Fatal("fresh MFA proof not reused", flow.Step)
	}
	must(t, f.security.Cancel(ctx, flow.ID, f.binding))
	view, err = f.security.View(ctx, f.token)
	must(t, err)
	if view.RecoveryRemaining != 9 {
		t.Fatal("cancel replaced recovery codes")
	}
	replacement := f.begin(t, "recovery")
	newCodes := replacement.Codes
	f.step(t, replacement, auth.FlowInput{Action: "acknowledge"})
	view, err = f.security.View(ctx, f.token)
	must(t, err)
	if view.RecoveryRemaining != 10 || len(newCodes) != 10 {
		t.Fatal("replacement failed")
	}
	stale, err := f.security.Advance(ctx, auth.FlowInput{ID: replay.ID, Action: "code", Code: codes[1], Recovery: true}, "", f.binding, "test", "Test browser")
	if !errors.Is(err, auth.ErrFlow) || stale.SessionToken != "" {
		t.Fatal("old login survived recovery reset", err)
	}
	pending, err = f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test browser")
	must(t, err)
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Code: codes[1], Recovery: true}, "", f.binding, "test", "Test browser")
	if err == nil {
		t.Fatal("old recovery set survived replacement")
	}
	completed, err := f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Code: newCodes[0], Recovery: true}, "", f.binding, "test", "Test browser")
	must(t, err)
	f.token = completed.SessionToken
	disable := f.begin(t, "mfa_disable")
	f.step(t, disable, auth.FlowInput{Action: "confirm"})
	view, err = f.security.View(ctx, f.token)
	must(t, err)
	if view.MFA || view.ExtraCode || view.RecoveryRemaining != 0 {
		t.Fatal("disable left MFA state", view)
	}
}
func TestRecoveryCodeConcurrentConsumption(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	flows := []auth.FlowResult{}
	for range 2 {
		flow, err := f.security.PasswordLogin(t.Context(), "jack", testPassword, f.binding, "test", "Test browser")
		must(t, err)
		flows = append(flows, flow)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, flow := range flows {
		wg.Go(func() {
			<-start
			_, err := f.security.Advance(t.Context(), auth.FlowInput{ID: flow.ID, Action: "code", Code: codes[0], Recovery: true}, "", f.binding, "test", "Test browser")
			if err == nil {
				wins.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("recovery code had multiple winners", wins.Load())
	}
}
func TestPasswordChangeInvalidatesPendingLoginAndSessions(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	ctx := t.Context()
	pending, err := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test browser")
	must(t, err)
	change := f.begin(t, "password")
	change = f.step(t, change, auth.FlowInput{Action: "password", Password: testPassword, NewPassword: "another quiet mountain lantern"})
	if change.Step != "code" {
		t.Fatal("password change bypassed MFA")
	}
	change = f.step(t, change, auth.FlowInput{Action: "code", Code: codes[0], Recovery: true})
	if change.Step != "done" {
		t.Fatal(change)
	}
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Code: codes[1], Recovery: true}, "", f.binding, "test", "Test browser")
	if !errors.Is(err, auth.ErrFlow) {
		t.Fatal("old login survived password change", err)
	}
	_, err = f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Test browser")
	if !errors.Is(err, auth.ErrCredentials) {
		t.Fatal("old password survived")
	}
	_, err = f.service.Current(ctx, f.token)
	must(t, err)
}
func TestSecurityHTTPRequiresCompleteMFA(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	c := client{t, httpapi.New(f.service, f.security, nil, nil, cfg), map[string]*http.Cookie{}}
	var flow auth.FlowResult
	must(t, json.Unmarshal(c.request("POST", "login", `{"username":"jack","password":"silver meadow orbit lamp"}`, 200).Body.Bytes(), &flow))
	if c.cookies["acta_session"] != nil {
		t.Fatal("partial login set session cookie")
	}
	c.request("GET", "account", "", 401)
	c.request("GET", "security", "", 401)
	c.request("POST", "security/advance", body(auth.FlowInput{ID: flow.ID, Action: "code", Code: codes[0], Recovery: true}), 200)
	c.request("GET", "security", "", 200)
}
func TestSecurityKeyMismatch(t *testing.T) {
	f := securityDatabase(t)
	_, err := auth.NewSecurity(context.Background(), f.service, f.store, "http://localhost:8081", []string{"http://localhost:8081"}, []byte(strings.Repeat("x", 32)))
	if err == nil {
		t.Fatal("wrong encryption key accepted")
	}
}

func TestSecurityChangeRacesWithSessionIssuance(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	ctx := t.Context()
	// Finish fresh authentication on the original browser, then race a fully
	// authorized policy change against another browser's final login factor.
	disable := f.begin(t, "mfa_disable")
	disable = f.step(t, disable, auth.FlowInput{Action: "password", Password: testPassword})
	disable = f.step(t, disable, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	pending, err := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Other browser")
	must(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var login auth.FlowResult
	var loginErr, changeErr error
	wg.Go(func() {
		<-start
		login, loginErr = f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Recovery: true, Code: codes[1]}, "", f.binding, "test", "Other browser")
	})
	wg.Go(func() {
		<-start
		_, changeErr = f.security.Advance(ctx, auth.FlowInput{ID: disable.ID, Action: "confirm"}, f.token, f.binding, "test", "Test browser")
	})
	close(start)
	wg.Wait()
	must(t, changeErr)
	if loginErr != nil && !errors.Is(loginErr, auth.ErrFlow) {
		t.Fatal(loginErr)
	}
	if login.SessionToken != "" {
		if _, err = f.service.Current(ctx, login.SessionToken); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatal("old login survived security change", err)
		}
	}
	view, err := f.security.View(ctx, f.token)
	must(t, err)
	if view.MFA || len(view.Sessions) != 1 {
		t.Fatal(view)
	}
}
func TestAuthenticatorReplacementIsAtomicAndSessionBound(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	ctx := t.Context()
	flow := f.begin(t, "mfa_setup")
	flow = f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	flow = f.step(t, flow, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	if flow.Step != "totp_setup" {
		t.Fatal(flow.Step)
	}
	code, err := totp.GenerateCode(flow.Secret, time.Now())
	must(t, err)
	flow = f.step(t, flow, auth.FlowInput{Action: "setup_verify", Code: code})
	replacementCodes := flow.Codes
	// A second session cannot redeem a flow authorized on the first.
	pending, err := f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Other browser")
	must(t, err)
	other := f.step(t, pending, auth.FlowInput{Action: "code", Recovery: true, Code: codes[1]})
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "acknowledge"}, other.SessionToken, f.binding, "test", "Other browser")
	if !errors.Is(err, auth.ErrFlow) {
		t.Fatal("flow crossed sessions", err)
	}
	view, err := f.security.View(ctx, f.token)
	must(t, err)
	if !view.MFA || view.RecoveryRemaining != 8 {
		t.Fatal("replacement changed active state before acknowledgment", view)
	}
	f.step(t, flow, auth.FlowInput{Action: "acknowledge"})
	if _, err = f.service.Current(ctx, other.SessionToken); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("replacement retained old session", err)
	}
	pending, err = f.security.PasswordLogin(ctx, "jack", testPassword, f.binding, "test", "Other browser")
	must(t, err)
	_, err = f.security.Advance(ctx, auth.FlowInput{ID: pending.ID, Action: "code", Recovery: true, Code: codes[2]}, "", f.binding, "test", "Other browser")
	if err == nil {
		t.Fatal("old recovery set survived replacement")
	}
	f.step(t, pending, auth.FlowInput{Action: "code", Recovery: true, Code: replacementCodes[0]})
}
func TestConcurrentSecurityChangesHaveOneWinner(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	ctx := t.Context()
	first := f.begin(t, "recovery")
	first = f.step(t, first, auth.FlowInput{Action: "password", Password: testPassword})
	first = f.step(t, first, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	second := f.begin(t, "recovery")
	start := make(chan struct{})
	var wg sync.WaitGroup
	var winners atomic.Int32
	for _, flow := range []auth.FlowResult{first, second} {
		wg.Go(func() {
			<-start
			_, err := f.security.Advance(ctx, auth.FlowInput{ID: flow.ID, Action: "acknowledge"}, f.token, f.binding, "test", "Test browser")
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, auth.ErrFlow) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent recovery replacements committed", winners.Load())
	}
}

func TestPasswordChangeReusesFreshSecondFactor(t *testing.T) {
	f := securityDatabase(t)
	_, codes := f.enroll(t)
	pending, err := f.security.PasswordLogin(t.Context(), "jack", testPassword, f.binding, "test", "Test browser")
	must(t, err)
	login := f.step(t, pending, auth.FlowInput{Action: "code", Recovery: true, Code: codes[0]})
	f.token = login.SessionToken
	change := f.begin(t, "password")
	change = f.step(t, change, auth.FlowInput{Action: "password", Password: testPassword, NewPassword: "another quiet mountain lantern"})
	if change.Step != "done" {
		t.Fatal("fresh second factor was discarded", change.Step)
	}
}
