package integration

import (
	"acta/internal/accounts"
	"acta/internal/auth"
	actaclient "acta/internal/client"
	"acta/internal/config"
	"acta/internal/httpapi"
	"errors"
	"github.com/pquerna/otp/totp"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func startDevice(t *testing.T, f securityFixture) auth.DeviceStart {
	t.Helper()
	d, e := f.security.StartDevice(t.Context(), "device-test", "test machine")
	must(t, e)
	return d
}
func clearPoll(t *testing.T, f securityFixture, d auth.DeviceStart) {
	t.Helper()
	_, e := f.conn.Exec(t.Context(), `DELETE FROM authentication_attempts WHERE bucket_hash=$1`, auth.Digest("device-poll:"+d.DeviceCode))
	must(t, e)
}
func TestDeviceApprovalLifecycle(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	d := startDevice(t, f)
	if len(strings.ReplaceAll(d.UserCode, "-", "")) != 8 || d.DeviceCode == d.UserCode {
		t.Fatal("invalid codes")
	}
	r, e := f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	if r.Status != "pending" || r.Token != "" {
		t.Fatal(r)
	}
	if _, e = f.security.PollDevice(ctx, d.DeviceCode); !errors.Is(e, auth.ErrRateLimited) {
		t.Fatal("poll not throttled", e)
	}
	must(t, f.security.ApproveDevice(ctx, f.token, strings.ToLower(strings.ReplaceAll(d.UserCode, "-", "")), "test", true, ""))
	clearPoll(t, f, d)
	r, e = f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	a, e := f.service.Current(ctx, r.Token)
	must(t, e)
	if a.Username != "jack" || !strings.HasPrefix(r.Token, "cli_") {
		t.Fatal("wrong session")
	}
	view, e := f.security.View(ctx, f.token)
	must(t, e)
	var cliID string
	for _, s := range view.Sessions {
		if strings.HasPrefix(s.Description, "CLI ·") {
			cliID = s.ID
			if s.ExpiresAt.Sub(s.CreatedAt) != auth.SessionLifetime {
				t.Fatal("wrong lifetime")
			}
		}
	}
	if cliID == "" {
		t.Fatal("CLI missing from session list")
	}
	clearPoll(t, f, d)
	if _, e = f.security.PollDevice(ctx, d.DeviceCode); !errors.Is(e, auth.ErrDevice) {
		t.Fatal("replayed device code", e)
	}
	must(t, f.security.Revoke(ctx, f.token, cliID))
	if _, e = f.service.Current(ctx, r.Token); !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal("revoked CLI still usable", e)
	}
	if _, e = f.service.Current(ctx, f.token); e != nil {
		t.Fatal("CLI revocation damaged browser", e)
	}
}
func TestDeviceDenialExpiryAndRevocationBeforeCollection(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	d := startDevice(t, f)
	must(t, f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", false, ""))
	r, e := f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	if r.Status != "denied" || r.Token != "" {
		t.Fatal(r)
	}
	d = startDevice(t, f)
	_, e = f.conn.Exec(ctx, `UPDATE device_requests SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, auth.Digest(d.DeviceCode))
	must(t, e)
	if e = f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, ""); !errors.Is(e, auth.ErrDevice) {
		t.Fatal("expired code accepted", e)
	}
	d = startDevice(t, f)
	must(t, f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, ""))
	must(t, f.security.Revoke(ctx, f.token, ""))
	if _, e = f.security.PollDevice(ctx, d.DeviceCode); !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal("collected revoked credential", e)
	}
}
func TestDeviceConcurrentApprovalAndMFA(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	d := startDevice(t, f)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, "") == nil {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("approval was not single-use")
	}
	d = startDevice(t, f)
	root, e := f.service.Current(ctx, f.token)
	must(t, e)
	_, e = manager(f).UpdatePermissions(ctx, f.token, root.ID, root.PermissionsVersion, root.DirectPermissions, true)
	must(t, e)
	if e = f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, ""); !errors.Is(e, auth.ErrMFARequired) {
		t.Fatal("MFA requirement bypassed", e)
	}
	flow := f.begin(t, "mfa_setup")
	if flow.Step == "verify" {
		flow = f.step(t, flow, auth.FlowInput{Action: "password", Password: testPassword})
	}
	code, e := totp.GenerateCode(flow.Secret, time.Now())
	must(t, e)
	flow = f.step(t, flow, auth.FlowInput{Action: "setup_verify", Code: code})
	f.step(t, flow, auth.FlowInput{Action: "acknowledge"})
	must(t, f.security.ApproveDevice(ctx, f.token, d.UserCode, "test", true, ""))
	r, e := f.security.PollDevice(ctx, d.DeviceCode)
	must(t, e)
	if _, e = f.service.Current(ctx, r.Token); e != nil {
		t.Fatal("MFA-approved CLI unusable", e)
	}
}
func TestDeviceHTTPAndClient(t *testing.T) {
	f := securityDatabase(t)
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	var h http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	defer server.Close()
	cfg, e = config.Parse(server.URL, "")
	must(t, e)
	h = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	c := actaclient.New(server.URL, "")
	d, e := c.Start(t.Context(), "integration")
	must(t, e)
	req := httptest.NewRequest("POST", server.URL+"/api/device/approve", strings.NewReader(`{"code":"`+d.UserCode+`","approve":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 403 {
		t.Fatal("browser approval missing Origin accepted", recorder.Code)
	}
	req.Header.Set("Origin", server.URL)
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	d.Interval = 1
	secret, e := c.Wait(t.Context(), d)
	must(t, e)
	c.Token = secret
	a, e := c.Account(t.Context())
	must(t, e)
	if a.Username != "jack" {
		t.Fatal(a)
	}
	// Browser credentials cannot be smuggled through the bearer transport.
	c.Token = f.token
	if _, e = c.Account(t.Context()); e == nil {
		t.Fatal("browser token accepted as bearer")
	}
	c.Token = secret
	next, e := actaclient.New(server.URL, "").Start(t.Context(), "second")
	must(t, e)
	e = c.Call(t.Context(), "POST", "device/approve", map[string]any{"code": next.UserCode, "approve": true}, nil)
	if e == nil {
		t.Fatal("CLI authorized another CLI")
	}
	must(t, c.Logout(t.Context()))
	if _, e = c.Account(t.Context()); e == nil {
		t.Fatal("logout failed")
	}
}
func TestDeviceIdleExpiry(t *testing.T) {
	f := securityDatabase(t)
	d := startDevice(t, f)
	must(t, f.security.ApproveDevice(t.Context(), f.token, d.UserCode, "test", true, ""))
	r, e := f.security.PollDevice(t.Context(), d.DeviceCode)
	must(t, e)
	now := time.Now()
	_, e = f.conn.Exec(t.Context(), `UPDATE browser_sessions SET created_at=$2,last_seen_at=$3 WHERE token_hash=$1`, auth.Digest(r.Token), now.Add(-10*24*time.Hour), now.Add(-8*24*time.Hour))
	must(t, e)
	if _, e = f.service.Current(t.Context(), r.Token); !errors.Is(e, auth.ErrUnauthenticated) {
		t.Fatal("idle CLI accepted", e)
	}
}
