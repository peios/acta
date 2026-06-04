package web

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Markdown is rendered server-side and sanitized. goldmark runs with raw HTML
// disabled (the default), so embedded <script> etc. is escaped; bluemonday then
// strips anything dangerous that survives (e.g. javascript: URLs) as a second
// layer. The result is trusted template.HTML.

var mdRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM, mentionExtension{}), // tables, strikethrough, autolinks, task lists, @mentions
	goldmark.WithRendererOptions(ghtml.WithHardWraps()),
)

var mdPolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// GFM task lists render disabled checkboxes; allow exactly those.
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	// Mention chips: a span with exactly class="mention" (our renderer's output).
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^mention$`)).OnElements("span")
	// Open external links in a new tab (bluemonday adds rel=noopener).
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

// --- @mention chips ---
//
// A goldmark inline extension that renders @handles as <span class="mention">
// chips. Working inside goldmark's pipeline (rather than post-processing the
// HTML) means mentions inside code spans/fences are left alone for free, and the
// same @handle tokens that drive notifications (board.parseMentions) render as
// chips. The handle grammar mirrors that regexp.

var mentionInlineRe = regexp.MustCompile(`^@[A-Za-z0-9][A-Za-z0-9._/-]*`)

var kindMention = ast.NewNodeKind("Mention")

type mentionNode struct {
	ast.BaseInline
	handle []byte // the @-stripped handle, e.g. "jack" or "jack/bot"
}

func (n *mentionNode) Kind() ast.NodeKind         { return kindMention }
func (n *mentionNode) Dump(src []byte, level int) { ast.DumpHelper(n, src, level, nil, nil) }

type mentionParser struct{}

func (mentionParser) Trigger() []byte { return []byte{'@'} }

func (mentionParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()
	// Require a non-word char before '@' so emails (a@b.com) aren't chipped —
	// those fall through to GFM's autolinker.
	if seg.Start > 0 {
		if b := block.Source()[seg.Start-1]; isWordByte(b) {
			return nil
		}
	}
	m := mentionInlineRe.Find(line)
	if m == nil {
		return nil
	}
	block.Advance(len(m))
	return &mentionNode{handle: m[1:]} // drop the leading '@'
}

func isWordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

type mentionRenderer struct{}

func (mentionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMention, renderMention)
}

func renderMention(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		w.WriteString(`<span class="mention">@`)
		_, _ = w.Write(util.EscapeHTML(n.(*mentionNode).handle))
		w.WriteString(`</span>`)
	}
	return ast.WalkContinue, nil
}

type mentionExtension struct{}

func (mentionExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(mentionParser{}, 199)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(mentionRenderer{}, 199)))
}

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
