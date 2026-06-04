package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestServiceWorkerServed checks /sw.js is reachable at the origin root (so its
// control scope is "/") and typed as JavaScript, with the push/click handlers.
func TestServiceWorkerServed(t *testing.T) {
	base, client := newTestServer(t)

	resp, err := client.Get(base + "/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sw.js: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("sw.js Content-Type = %q, want text/javascript", ct)
	}
	if got := resp.Header.Get("Service-Worker-Allowed"); got != "/" {
		t.Errorf("Service-Worker-Allowed = %q, want /", got)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `addEventListener("push"`) ||
		!strings.Contains(body, `addEventListener("notificationclick"`) {
		t.Error("sw.js missing push / notificationclick handlers")
	}
}

// TestAccountShowsPushToggle confirms the Notifications section renders with the
// server's VAPID public key when push is enabled (the test harness enables it).
func TestAccountShowsPushToggle(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/account/security", http.StatusOK)
	if !strings.Contains(body, "data-push") {
		t.Error("account page missing the push toggle section")
	}
	if !strings.Contains(body, `data-vapid-key="`+testVAPIDPublic+`"`) {
		t.Error("account page missing/incorrect VAPID public key")
	}
	if !strings.Contains(body, "Enable on this device") {
		t.Error("account page missing the enable button")
	}
}

// TestPushSubscribeRoundTrip exercises the subscribe/unsubscribe endpoints: a
// well-formed subscription is accepted, a malformed one is rejected, and the
// endpoint can be forgotten again.
func TestPushSubscribeRoundTrip(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	sub := map[string]any{
		"endpoint": "https://push.example.com/abc123",
		"keys":     map[string]string{"p256dh": "BJxx_validish_key", "auth": "c2VjcmV0"},
	}
	resp := postJSON(t, client, base+"/account/push/subscribe", token, sub)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("subscribe: want 204, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Missing keys is a 400.
	bad := postJSON(t, client, base+"/account/push/subscribe", token,
		map[string]any{"endpoint": "https://push.example.com/x"})
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed subscribe: want 400, got %d", bad.StatusCode)
	}
	bad.Body.Close()

	// Forget it again.
	un := postJSON(t, client, base+"/account/push/unsubscribe", token,
		map[string]string{"endpoint": "https://push.example.com/abc123"})
	if un.StatusCode != http.StatusNoContent {
		t.Errorf("unsubscribe: want 204, got %d", un.StatusCode)
	}
	un.Body.Close()
}
