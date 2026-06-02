package web

import (
	"strings"
	"testing"
)

func TestMarkdownRendersAndSanitizes(t *testing.T) {
	out := string(mdToHTML("**bold** and a [link](https://example.com)"))
	if !strings.Contains(out, "<strong>bold</strong>") {
		t.Errorf("bold not rendered:\n%s", out)
	}
	if !strings.Contains(out, `href="https://example.com"`) {
		t.Errorf("link not rendered:\n%s", out)
	}

	// Raw HTML is escaped (goldmark) and dangerous URLs are stripped (bluemonday).
	xss := string(mdToHTML("<script>alert(1)</script>\n\n[x](javascript:alert(1))"))
	if strings.Contains(xss, "<script") {
		t.Errorf("script tag survived sanitization:\n%s", xss)
	}
	if strings.Contains(xss, "javascript:") {
		t.Errorf("javascript: URL survived sanitization:\n%s", xss)
	}
}

func TestMarkdownAllowsExternalImages(t *testing.T) {
	out := string(mdToHTML("![a cat](https://example.com/cat.png)"))
	if !strings.Contains(out, "<img") || !strings.Contains(out, `src="https://example.com/cat.png"`) {
		t.Errorf("external image not preserved:\n%s", out)
	}
}

func TestPreviewSourceCutoff(t *testing.T) {
	// Short and unbroken: nothing to collapse.
	if _, trunc := previewSource("one\ntwo\nthree"); trunc {
		t.Error("short description should not be truncated")
	}

	// First blank before line 10 -> the 10-line floor wins.
	early := "a\nb\n\n" + strings.Repeat("x\n", 20)
	prev, trunc := previewSource(early)
	if !trunc {
		t.Fatal("want truncated when content runs past the floor")
	}
	if got := strings.Count(prev, "\n") + 1; got != 10 {
		t.Errorf("preview should be 10 lines, got %d", got)
	}

	// First blank past line 10 -> show the whole first paragraph (up to it).
	para := strings.Repeat("line\n", 15) + "\nmore\nmore"
	prev2, trunc2 := previewSource(para)
	if !trunc2 {
		t.Fatal("want truncated when a paragraph is followed by more")
	}
	if got := strings.Count(prev2, "\n") + 1; got != 15 {
		t.Errorf("preview should be the 15-line paragraph, got %d", got)
	}
}

func TestPreviewClosesDanglingFence(t *testing.T) {
	// A blank line inside an unclosed fence forces the cut mid-fence.
	src := "```\ncode\n\n" + strings.Repeat("code\n", 12)
	prev, trunc := previewSource(src)
	if !trunc {
		t.Fatal("want truncated")
	}
	if fenceCount(prev)%2 != 0 {
		t.Errorf("dangling code fence not closed:\n%q", prev)
	}
}

func TestRenderDescriptionEmptyAndTruncated(t *testing.T) {
	if dv := renderDescription("   \n  "); dv.HasText {
		t.Error("whitespace-only description should have no text")
	}
	dv := renderDescription("a\nb\nc\n\n" + strings.Repeat("more\n", 20))
	if !dv.HasText || !dv.Truncated {
		t.Fatalf("long description should be present and truncated: %+v", dv)
	}
	if dv.Preview == dv.HTML {
		t.Error("a truncated description's preview should differ from the full HTML")
	}
}
