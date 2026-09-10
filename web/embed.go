package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// Build the frontend before compiling Go. all: includes SvelteKit's _app directory.
//
//go:embed all:build
var assets embed.FS

// Handler serves the compiled frontend, with HTML navigation handled by SvelteKit.
func Handler() (http.Handler, error) {
	files, err := fs.Sub(assets, "build")
	if err != nil {
		return nil, err
	}
	shell, err := fs.ReadFile(files, "200.html")
	if err != nil {
		return nil, err
	}
	static := http.FileServerFS(files)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		name := strings.TrimPrefix(r.URL.Path, "/")
		// API errors must never be disguised as a successful HTML response.
		if name == "api" || strings.HasPrefix(name, "api/") {
			http.NotFound(w, r)
			return
		}
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			if name == "manifest.webmanifest" {
				// Do not depend on the host OS MIME database.
				w.Header().Set("Content-Type", "application/manifest+json")
			}
			static.ServeHTTP(w, r)
			return
		}
		// Only page navigations receive the SPA shell. Missing assets stay 404s.
		if strings.HasPrefix(name, "_app/") || !strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "200.html", time.Time{}, bytes.NewReader(shell))
	})
	return mux, nil
}
