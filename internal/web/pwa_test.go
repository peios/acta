package web_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestPageEmitsPWAHead checks the installable-app head is present on a rendered
// page: the manifest link, the apple-* meta iOS keys off to drop the browser
// chrome, and the cover viewport that lets the canvas run under the notch.
func TestPageEmitsPWAHead(t *testing.T) {
	base, client := newTestServer(t)
	body := getBody(t, client, base+"/login", http.StatusOK)

	for _, want := range []string{
		`rel="manifest"`,
		`name="apple-mobile-web-app-capable" content="yes"`,
		`name="apple-mobile-web-app-status-bar-style"`,
		`rel="apple-touch-icon"`,
		`name="theme-color" content="#0a0b0d"`,
		`viewport-fit=cover`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("login page head missing %q", want)
		}
	}
}

// TestManifestServed checks the manifest is reachable, typed so browsers accept
// it (not sniffed as text/plain), and declares the standalone shell + root scope
// that actually removes the chrome.
func TestManifestServed(t *testing.T) {
	base, client := newTestServer(t)

	resp, err := client.Get(base + "/static/manifest.webmanifest?v=test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest: want 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/manifest+json") {
		t.Errorf("manifest Content-Type = %q, want application/manifest+json", ct)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Display string `json:"display"`
		Scope   string `json:"scope"`
		Icons   []struct {
			Src string `json:"src"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if m.Display != "standalone" {
		t.Errorf("manifest display = %q, want standalone", m.Display)
	}
	if m.Scope != "/" {
		t.Errorf("manifest scope = %q, want /", m.Scope)
	}
	if len(m.Icons) == 0 {
		t.Error("manifest declares no icons")
	}
}
