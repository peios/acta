package client

import (
	"acta/internal/providers"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/hyperharness"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestHarnessReconnectAndCancellation(t *testing.T) {
	var connections atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cli_test" {
			t.Error("missing credential")
		}
		c, e := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{hyperharness.Protocol}})
		if e != nil {
			return
		}
		defer c.CloseNow()
		var hello hyperharness.Hello
		if e = wsjson.Read(r.Context(), c, &hello); e != nil {
			return
		}
		if hello.Hostname != "desktop" {
			t.Error(hello)
		}
		n := connections.Add(1)
		_ = wsjson.Write(r.Context(), c, map[string]any{"type": "connected", "connection": hyperharness.Connection{ID: "ephemeral", Hostname: hello.Hostname}})
		if n == 1 {
			return
		}
		<-c.CloseRead(r.Context()).Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connected, retries := 0, 0
	err := New(srv.URL, "cli_test").RunHarness(ctx, "desktop", nil, nil, func(s HarnessState) error {
		if s.State == "connected" {
			connected++
			if connected == 2 {
				cancel()
			}
		}
		if s.State == "reconnecting" {
			retries++
		}
		return nil
	})
	if err != nil || connected != 2 || retries != 1 {
		t.Fatalf("%v connected=%d retries=%d", err, connected, retries)
	}
}
func TestHarnessAuthenticationAndRedirectAreTerminal(t *testing.T) {
	for _, status := range []int{401, 403, 426, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.Header().Set("Location", "http://127.0.0.1:1/credential-leak")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"Denied","code":"unauthenticated"}}`))
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			e := New(srv.URL, "cli_secret").RunHarness(ctx, "desktop", nil, nil, func(HarnessState) error { t.Error("must not reconnect"); return nil })
			var api *Error
			if !errors.As(e, &api) || attempts.Load() != 1 {
				t.Fatalf("%v attempts=%d", e, attempts.Load())
			}
		})
	}
}
func TestHarnessRevocationMessageIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{hyperharness.Protocol}})
		if e != nil {
			return
		}
		defer c.CloseNow()
		var hello hyperharness.Hello
		if wsjson.Read(r.Context(), c, &hello) != nil {
			return
		}
		_ = wsjson.Write(r.Context(), c, map[string]string{"type": "error", "code": "unauthenticated", "message": "Session ended"})
		<-c.CloseRead(r.Context()).Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	e := New(srv.URL, "cli_test").RunHarness(ctx, "desktop", nil, nil, func(HarnessState) error { t.Error("unexpected state"); return nil })
	var api *Error
	if !errors.As(e, &api) || api.Status != 401 {
		t.Fatal(e)
	}
}

type harnessTestSource struct {
	mu      sync.Mutex
	states  []providers.Status
	changes chan struct{}
}

func (s *harnessTestSource) Snapshot() []providers.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.states)
}
func (s *harnessTestSource) Subscribe() (<-chan struct{}, func()) { return s.changes, func() {} }
func TestHarnessStreamsProviderChangesAndReplaysAfterReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	source := &harnessTestSource{states: providers.Initial(), changes: make(chan struct{}, 1)}
	received := make(chan hyperharness.ProviderUpdate, 3)
	var connections atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{hyperharness.Protocol}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		var hello hyperharness.Hello
		if wsjson.Read(ctx, c, &hello) != nil {
			return
		}
		n := connections.Add(1)
		if wsjson.Write(ctx, c, map[string]any{"type": "connected", "connection": hyperharness.Connection{ID: "connection", Hostname: hello.Hostname}}) != nil {
			return
		}
		count := 1
		if n == 1 {
			count = 2
		}
		for range count {
			var update hyperharness.ProviderUpdate
			if wsjson.Read(ctx, c, &update) != nil {
				return
			}
			select {
			case received <- update:
			case <-ctx.Done():
				return
			}
		}
		if n > 1 {
			<-c.CloseRead(ctx).Done()
		}
	}))
	defer srv.Close()
	done := make(chan error, 1)
	go func() {
		done <- New(srv.URL, "cli_test").RunHarness(ctx, "desktop", source, nil, func(HarnessState) error { return nil })
	}()
	receive := func() hyperharness.ProviderUpdate {
		t.Helper()
		select {
		case value := <-received:
			return value
		case <-ctx.Done():
			t.Fatal("provider update missing")
			return hyperharness.ProviderUpdate{}
		}
	}
	if update := receive(); update.Type != "providers" || !slices.Equal(update.Providers, providers.Initial()) {
		t.Fatal(update)
	}
	source.mu.Lock()
	source.states[0] = providers.Status{ID: "codex", Installation: "installed", Authentication: "signed_in", Version: "0.153.4"}
	source.mu.Unlock()
	source.changes <- struct{}{}
	if update := receive(); !slices.Equal(update.Providers, source.Snapshot()) {
		t.Fatal(update)
	}
	if update := receive(); !slices.Equal(update.Providers, source.Snapshot()) || connections.Load() != 2 {
		t.Fatal("latest snapshot not replayed", update)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not stop")
	}
}
