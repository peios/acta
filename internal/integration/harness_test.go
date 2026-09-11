package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/hyperharness"
	"acta/internal/providers"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestHarnessLiveOwnershipLifecycleAndRevocation(t *testing.T) {
	f := securityDatabase(t)
	_, other := permissionMember(t, f, "other")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	cliToken := agentCLI(t, f, "")
	agent := agentAccount(t, f, "worker", nil)
	agentToken := agentCLI(t, f, agent.ID)
	dial := func(path string, headers http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, srv.URL+"/api/harnesses/"+path, &websocket.DialOptions{HTTPHeader: headers, Subprotocols: []string{hyperharness.Protocol}})
	}
	cookie := func(token string) http.Header {
		return http.Header{"Cookie": []string{"acta_session=" + token}, "Origin": []string{srv.URL}}
	}
	bearer := func(token string) http.Header { return http.Header{"Authorization": []string{"Bearer " + token}} }
	for _, tc := range []struct {
		headers http.Header
		path    string
		want    int
	}{
		{cookie(f.token), "connect", 401},
		{bearer(agentToken), "connect", 403},
		{bearer("cli_invalid"), "connect", 401},
		{http.Header{"Cookie": []string{"acta_session=" + f.token}, "Origin": []string{"https://evil.example"}}, "live", 403},
	} {
		c, resp, err := dial(tc.path, tc.headers)
		if c != nil {
			c.CloseNow()
		}
		if err == nil || resp == nil || resp.StatusCode != tc.want {
			t.Fatalf("boundary %s: %v %#v", tc.path, err, resp)
		}
	}
	live, _, e := dial("live", cookie(f.token))
	must(t, e)
	defer live.CloseNow()
	read := func(c *websocket.Conn) hyperharness.Snapshot {
		t.Helper()
		var s hyperharness.Snapshot
		for {
			must(t, wsjson.Read(ctx, c, &s))
			if s.Type == "snapshot" {
				return s
			}
		}
	}
	if len(read(live).Connections) != 0 {
		t.Fatal("not initially empty")
	}
	foreign, _, e := dial("live", cookie(other.token))
	must(t, e)
	defer foreign.CloseNow()
	if len(read(foreign).Connections) != 0 {
		t.Fatal("foreign initial list")
	}
	connect := func() *websocket.Conn {
		c, _, e := dial("connect", bearer(cliToken))
		must(t, e)
		must(t, wsjson.Write(ctx, c, hyperharness.Hello{Hostname: "same-host"}))
		var ack struct{ Type string }
		must(t, wsjson.Read(ctx, c, &ack))
		if ack.Type != "connected" {
			t.Fatal(ack)
		}
		return c
	}
	one := connect()
	defer one.CloseNow()
	one.CloseRead(ctx)
	initial := read(live)
	if len(initial.Connections) != 1 {
		t.Fatal("missing connection")
	}
	states := providers.Initial()
	states[0] = providers.Status{ID: "codex", Installation: "installed", Version: "0.153.4", Authentication: "signed_in"}
	must(t, wsjson.Write(ctx, one, hyperharness.ProviderUpdate{Type: "providers", Providers: states}))
	updated := read(live)
	if len(updated.Connections) != 1 || updated.Connections[0].ID != initial.Connections[0].ID || updated.Connections[0].Providers[0] != states[0] {
		t.Fatal("provider update replaced connection or did not arrive", updated)
	}
	two := connect()
	defer two.CloseNow()
	two.CloseRead(ctx)
	s := read(live)
	if len(s.Connections) != 2 || s.Connections[0].ID == s.Connections[1].ID {
		t.Fatal("merged identical hostnames", s)
	}
	firstID := s.Connections[0].ID
	one.CloseNow()
	s = read(live)
	if len(s.Connections) != 1 || s.Connections[0].ID == firstID {
		t.Fatal("wrong connection removed", s)
	}
	// Even a site superuser may only list their own connections.
	req, e := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/harnesses", nil)
	must(t, e)
	req.AddCookie(&http.Cookie{Name: "acta_session", Value: other.token})
	resp, e := http.DefaultClient.Do(req)
	must(t, e)
	var foreignSnapshot hyperharness.Snapshot
	must(t, json.NewDecoder(resp.Body).Decode(&foreignSnapshot))
	resp.Body.Close()
	if len(foreignSnapshot.Connections) != 0 {
		t.Fatal("cross-owner disclosure")
	}
	// Revoke a credential after the handshake. The next heartbeat must close the
	// existing connection and immediately publish removal to its owner's browser.
	must(t, f.service.Logout(ctx, cliToken))
	s = read(live)
	if len(s.Connections) != 0 {
		t.Fatal("revoked connection retained")
	}
}

func TestHarnessHeartbeatRemovesUnresponsivePeer(t *testing.T) {
	f := securityDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	cliToken := agentCLI(t, f, "")
	dial := func(path string, headers http.Header) *websocket.Conn {
		c, _, e := websocket.Dial(ctx, srv.URL+"/api/harnesses/"+path, &websocket.DialOptions{HTTPHeader: headers, Subprotocols: []string{hyperharness.Protocol}})
		must(t, e)
		return c
	}
	live := dial("live", http.Header{"Cookie": []string{"acta_session=" + f.token}, "Origin": []string{srv.URL}})
	defer live.CloseNow()
	var snapshot hyperharness.Snapshot
	must(t, wsjson.Read(ctx, live, &snapshot))
	peer := dial("connect", http.Header{"Authorization": []string{"Bearer " + cliToken}})
	defer peer.CloseNow()
	must(t, wsjson.Write(ctx, peer, hyperharness.Hello{Hostname: "stalled-peer"}))
	var ack map[string]any
	must(t, wsjson.Read(ctx, peer, &ack))
	// Leave TCP open without servicing reads/pings, as with a suspended client.
	must(t, wsjson.Read(ctx, live, &snapshot))
	if len(snapshot.Connections) != 1 {
		t.Fatal(snapshot)
	}
	for {
		must(t, wsjson.Read(ctx, live, &snapshot))
		if snapshot.Type == "snapshot" && len(snapshot.Connections) == 0 {
			break
		}
	}
}
