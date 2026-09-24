package integration

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/auth"
	actaclient "acta/internal/client"
	"acta/internal/codehosts"
	"acta/internal/config"
	"acta/internal/httpapi"
	"github.com/google/uuid"
)

func codeHostLogin(t *testing.T, f securityFixture, owner string) string {
	t.Helper()
	secret, err := auth.Token()
	must(t, err)
	secret = "cli_" + secret
	now := time.Now()
	must(t, f.store.CreateSession(t.Context(), auth.Session{Digest: auth.Digest(secret), AccountID: owner, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}))
	return secret
}
func codeHostInput() codehosts.Heartbeat {
	return codehosts.Heartbeat{ID: uuid.NewString(), InstanceID: uuid.NewString(), Name: "Desktop", OS: "linux", Arch: "amd64"}
}

func TestCodeHostsPrivateToHumanEvenForSuperuser(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	root, err := f.service.Current(ctx, f.token)
	must(t, err)
	other := uuid.NewString()
	_, err = f.conn.Exec(ctx, `INSERT INTO accounts(id,username) VALUES($1,'other')`, other)
	must(t, err)
	mine, peer := codeHostLogin(t, f, root.ID), codeHostLogin(t, f, other)
	in := codeHostInput()
	_, err = f.service.RenewCodeHost(ctx, peer, in)
	must(t, err)
	hosts, err := f.service.CodeHosts(ctx, mine)
	must(t, err)
	if len(hosts) != 0 {
		t.Fatal("superuser saw another user's hosts")
	}
	must(t, f.service.ReleaseCodeHost(ctx, mine, in.ID, in.InstanceID))
	hosts, err = f.service.CodeHosts(ctx, peer)
	must(t, err)
	if len(hosts) != 1 || !hosts[0].Online {
		t.Fatal("another user released a private host")
	}
	// Even a deliberately reused ID creates a separate owner-scoped record.
	copy := in
	copy.Name = "Mine"
	_, err = f.service.RenewCodeHost(ctx, mine, copy)
	must(t, err)
	hosts, err = f.service.CodeHosts(ctx, peer)
	must(t, err)
	if len(hosts) != 1 || hosts[0].Name != "Desktop" {
		t.Fatal("another user modified a private host")
	}
	agent, err := manager(f).CreateAgent(ctx, f.token, "code-agent", "")
	must(t, err)
	device := startDevice(t, f)
	must(t, f.security.ApproveDevice(ctx, f.token, device.UserCode, "test", true, agent.ID))
	login, err := f.security.PollDevice(ctx, device.DeviceCode)
	must(t, err)
	agentToken := login.Token
	if _, err = f.service.CodeHosts(ctx, agentToken); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("agent listed hosts", err)
	}
	if _, err = f.service.RenewCodeHost(ctx, agentToken, codeHostInput()); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("agent registered host", err)
	}
}

func TestCodeHostLeaseRetryExpiryFencingAndRevocation(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	secret := codeHostLogin(t, f, owner.ID)
	in := codeHostInput()
	for range 2 {
		_, err = f.service.RenewCodeHost(ctx, secret, in)
		must(t, err)
	}
	hosts, err := f.service.CodeHosts(ctx, f.token)
	must(t, err)
	if len(hosts) != 1 || !hosts[0].Online {
		t.Fatal("retry duplicated host")
	}
	next := in
	next.InstanceID = uuid.NewString()
	if _, err = f.service.RenewCodeHost(ctx, secret, next); !errors.Is(err, codehosts.ErrInUse) {
		t.Fatal("live instance stolen", err)
	}
	_, err = f.conn.Exec(ctx, `UPDATE code_hosts SET lease_until=now()-interval '1 second' WHERE owner_id=$1`, owner.ID)
	must(t, err)
	hosts, err = f.service.CodeHosts(ctx, f.token)
	must(t, err)
	if hosts[0].Online {
		t.Fatal("expired lease online")
	}
	_, err = f.service.RenewCodeHost(ctx, secret, next)
	must(t, err)
	must(t, f.service.ReleaseCodeHost(ctx, secret, in.ID, in.InstanceID))
	hosts, err = f.service.CodeHosts(ctx, f.token)
	must(t, err)
	if !hosts[0].Online {
		t.Fatal("old process released newer one")
	}
	must(t, f.service.Logout(ctx, secret))
	hosts, err = f.service.CodeHosts(ctx, f.token)
	must(t, err)
	if hosts[0].Online {
		t.Fatal("revoked session still online")
	}
	if _, err = f.service.RenewCodeHost(ctx, secret, next); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatal("revoked session renewed", err)
	}
	fresh := codeHostLogin(t, f, owner.ID)
	_, err = f.service.RenewCodeHost(ctx, fresh, in)
	must(t, err)
	must(t, f.service.ReleaseCodeHost(ctx, fresh, in.ID, in.InstanceID))
	must(t, f.service.ReleaseCodeHost(ctx, fresh, in.ID, in.InstanceID))
	hosts, err = f.service.CodeHosts(ctx, f.token)
	must(t, err)
	if hosts[0].Online {
		t.Fatal("released host online")
	}
	// Two independent processes cannot both acquire one inactive identity.
	var wg sync.WaitGroup
	var wins atomic.Int32
	for range 2 {
		wg.Go(func() {
			contender := in
			contender.InstanceID = uuid.NewString()
			if _, err := f.service.RenewCodeHost(ctx, fresh, contender); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, codehosts.ErrInUse) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("concurrent registration winners", wins.Load())
	}
}

func TestCodeHostHTTPAuthenticationAndOwnerInput(t *testing.T) {
	f := securityDatabase(t)
	var h http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	defer server.Close()
	cfg, err := config.Parse(server.URL, "")
	must(t, err)
	h = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	owner, err := f.service.Current(t.Context(), f.token)
	must(t, err)
	c := actaclient.New(server.URL, codeHostLogin(t, f, owner.ID))
	assertStatus := func(err error, want int) {
		t.Helper()
		var problem *actaclient.Error
		if !errors.As(err, &problem) || problem.Status != want {
			t.Fatalf("want HTTP %d, got %v", want, err)
		}
	}
	_, err = actaclient.New(server.URL, "").CodeHosts(t.Context())
	assertStatus(err, 401)
	in := codeHostInput()
	conn, err := c.ConnectCodeHost(t.Context(), in)
	must(t, err)
	defer conn.CloseNow()
	conn.CloseRead(t.Context())
	hosts, err := c.CodeHosts(t.Context())
	must(t, err)
	if len(hosts) != 1 || !hosts[0].Online || !hosts[0].Connected {
		t.Fatal("HTTP registration missing")
	}
	err = c.Call(t.Context(), "POST", "code/hosts/"+in.ID+"/request", map[string]any{"method": "codebases.list", "params": map[string]any{}, "owner_id": owner.ID}, nil)
	assertStatus(err, 400)
	in.Name = "bad\nlabel"
	_, err = c.ConnectCodeHost(t.Context(), in)
	var invalid *codehosts.Problem
	if !errors.As(err, &invalid) || invalid.Code != "validation" {
		t.Fatal("invalid host name accepted", err)
	}
	if _, err = f.service.RenewCodeHost(t.Context(), f.token, codeHostInput()); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("browser registered host", err)
	}
	cfg.RecoveryMode = true
	conn.CloseNow()
	h = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	_, err = c.CodeHosts(t.Context())
	assertStatus(err, 503)
}
