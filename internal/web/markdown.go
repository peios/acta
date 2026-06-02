package web

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

// Markdown is rendered server-side and sanitized. goldmark runs with raw HTML
// disabled (the default), so embedded <script> etc. is escaped; bluemonday then
// strips anything dangerous that survives (e.g. javascript: URLs) as a second
// layer. The result is trusted template.HTML.

var mdRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM), // tables, strikethrough, autolinks, task lists
	goldmark.WithRendererOptions(ghtml.WithHardWraps()),
)

var mdPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// GFM task lists render disabled checkboxes; allow exactly those.
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	// Open external links in a new tab (bluemonday adds rel=noopener).
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

func mdToHTML(src string) template.HTML {
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(mdPolicy.SanitizeBytes(buf.Bytes()))
}

// descView is a description rendered for display: the full HTML, a collapsed
// preview, and whether the two differ (i.e. a "Show more" is warranted).
type descView struct {
	HasText   bool
	HTML      template.HTML // full
	Preview   template.HTML // collapsed (equals HTML when not truncated)
	Truncated bool
}

func renderDescription(raw string) descView {
	if strings.TrimSpace(raw) == "" {
		return descView{}
	}
	full := mdToHTML(raw)
	dv := descView{HasText: true, HTML: full, Preview: full}
	if preview, truncated := previewSource(raw); truncated {
		dv.Truncated = true
		dv.Preview = mdToHTML(preview)
	}
	return dv
}

// previewSource trims a description to its collapsed form: the greater of the
// first paragraph (up to the first blank line) and 10 lines. It returns the
// preview text and whether anything was cut. A dangling code fence is closed so
// the partial markdown still renders cleanly.
func previewSource(src string) (preview string, truncated bool) {
	const minLines = 10
	lines := strings.Split(src, "\n")

	cutoff := len(lines)
	for i, ln := range lines {
		if i > 0 && strings.TrimSpace(ln) == "" {
			cutoff = i
			break
		}
	}
	if cutoff < minLines {
		cutoff = minLines
	}
	if cutoff >= len(lines) {
		return src, false
	}
	// Only collapse if real content remains past the cutoff — a trailing
	// newline or blank lines aren't worth a "Show more".
	if strings.TrimSpace(strings.Join(lines[cutoff:], "\n")) == "" {
		return src, false
	}

	preview = strings.Join(lines[:cutoff], "\n")
	if fenceCount(preview)%2 == 1 {
		preview += "\n```"
	}
	return preview, true
}

func fenceCount(s string) int {
	n := 0
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			n++
		}
	}
	return n
}
