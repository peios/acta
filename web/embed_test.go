package web

import (
	"acta2/internal/hyperharness"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendRouting(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, accept string
		status                     int
		html                       bool
	}{
		{"home", "GET", "/", "text/html", 200, true},
		{"direct navigation", "GET", "/workspaces/example", "text/html", 200, true},
		{"head navigation", "HEAD", "/", "text/html", 200, true},
		{"unknown API", "GET", "/api/tasks", "text/html", 404, false},
		{"API root", "GET", "/api", "text/html", 404, false},
		{"missing script", "GET", "/_app/immutable/missing.js", "text/html", 404, false},
		{"missing image", "GET", "/missing.png", "image/*", 404, false},
		{"asset directory", "GET", "/_app/immutable/", "text/html", 404, false},
		{"unsupported method", "POST", "/", "text/html", 405, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if html := strings.HasPrefix(w.Header().Get("Content-Type"), "text/html"); html != tc.html {
				t.Fatalf("Content-Type = %q, want HTML %v", w.Header().Get("Content-Type"), tc.html)
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if tc.html && w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("application shell must revalidate")
			}
		})
	}

	// Exercise a real generated asset: an incomplete embed can serve HTML while
	// leaving the browser unable to start SvelteKit.
	paths, err := fs.Glob(assets, "build/_app/immutable/entry/start.*.js")
	if err != nil || len(paths) != 1 {
		t.Fatalf("expected one compiled entry script, got %v (%v)", paths, err)
	}
	r := httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(paths[0], "build/"), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.Len() == 0 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("compiled script not served correctly: status %d, type %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatal("hashed assets should be cached as immutable")
	}
}

func TestEmbeddedHarnessProtocolMatchesServer(t *testing.T) {
	found := false
	err := fs.WalkDir(assets, "build", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		raw, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "acta-harness-v") {
			if !strings.Contains(string(raw), hyperharness.Protocol) {
				t.Errorf("embedded harness client disagrees with server protocol: %s", path)
			}
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("embedded frontend has no harness protocol")
	}
}

func TestPWAAssets(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	for path, kind := range map[string]string{"/manifest.webmanifest": "application/manifest+json", "/service-worker.js": "javascript", "/icons/acta-192.png": "image/png", "/icons/acta-512.png": "image/png", "/icons/acta-maskable.png": "image/png", "/icons/apple-touch-icon.png": "image/png"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), kind) || w.Body.Len() == 0 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Header().Get("Content-Type"))
		}
		if w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s must revalidate", path)
		}
	}
}
