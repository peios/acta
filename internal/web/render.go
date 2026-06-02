package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// staticHandler serves the embedded /static assets (currently just the passkey
// JS glue). Same-origin, so it's already permitted by our default-src 'self'
// CSP without any inline script.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServerFS(sub))
}

// Each page is parsed in its own set together with the base layout (and the
// shared item-modal partial), so the "content"/"title" blocks don't collide
// across pages.
var pages = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, name := range []string{"login.html", "board.html", "account.html", "workspaces.html", "welcome.html", "archive.html", "agents.html", "agent_detail.html", "cli_authorize.html"} {
		m[name] = template.Must(
			template.New(name).ParseFS(templatesFS,
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
