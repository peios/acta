package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// assetVersion is a short content hash of all embedded static assets, computed
// once at startup. Templates append it as ?v=<hash> (see the "asset" func) so
// each build serves assets under a fresh URL — a CDN/browser that cached an old
// board.js can't keep serving it after a deploy, because the URL changed.
var assetVersion = hashStatic()

func hashStatic() string {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return "dev"
	}
	h := sha256.New()
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	if err != nil {
		return "dev"
	}
	return hex.EncodeToString(h.Sum(nil))[:10]
}

// assetURL fingerprints a static asset path for cache-busting.
func assetURL(path string) string { return path + "?v=" + assetVersion }

var funcMap = template.FuncMap{"asset": assetURL}

// staticHandler serves the embedded /static assets. They're same-origin (so the
// default-src 'self' CSP already permits them) and addressed with a content
// hash in the query, so they're safe to cache hard and long.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.StripPrefix("/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
}

// Each page is parsed in its own set together with the base layout (and the
// shared item-modal partial), so the "content"/"title" blocks don't collide
// across pages.
var pages = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, name := range []string{"login.html", "board.html", "account.html", "workspaces.html", "principals.html", "welcome.html", "archive.html", "agents.html", "agent_detail.html", "cli_authorize.html"} {
		m[name] = template.Must(
			template.New(name).Funcs(funcMap).ParseFS(templatesFS,
				"templates/base.html", "templates/item_modal.html", "templates/tokens.html", "templates/"+name),
		)
	}
	return m
}()

// itemModalTmpl renders just the modal markup, for board.js to inject when
// opening an item without a full page reload.
var itemModalTmpl = template.Must(
	template.New("item_modal.html").ParseFS(templatesFS, "templates/item_modal.html"),
)

// renderDescView writes just the rendered description fragment (the markdown
// view + its show-more control), so the editor can swap it in after a save.
func renderDescView(w http.ResponseWriter, dv descView) {
	var buf bytes.Buffer
	if err := itemModalTmpl.ExecuteTemplate(&buf, "desc-view", dv); err != nil {
		slog.Error("render desc", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func renderItemModal(w http.ResponseWriter, data any) {
	var buf bytes.Buffer
	if err := itemModalTmpl.ExecuteTemplate(&buf, "item-modal", data); err != nil {
		slog.Error("render modal", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := pages[page]
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Render into a buffer first so a template error becomes a clean 500
	// rather than a half-written page.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", data); err != nil {
		slog.Error("render", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
