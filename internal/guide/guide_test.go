package guide

import (
	"strings"
	"testing"
)

func TestCompositionAndValidation(t *testing.T) {
	base := "# Guide\n"
	if Compose(base, Preferences{}).Markdown != base {
		t.Fatal("empty appendices changed guide")
	}
	doc := Compose(base, Preferences{Site: Preference{Content: "Site policy"}, User: Preference{Content: "User policy"}})
	if !strings.HasPrefix(doc.Markdown, base) || strings.Index(doc.Markdown, "## Site preferences") > strings.Index(doc.Markdown, "## User preferences") || !strings.Contains(doc.Markdown, "do not grant permissions") {
		t.Fatal(doc.Markdown)
	}
	for _, in := range []Save{{Scope: "agent"}, {Scope: "site", Revision: -1}, {Scope: "user", Content: strings.Repeat("a", 8001)}, {Scope: "site", Content: "bad\x00text"}} {
		if Validate(in) == nil {
			t.Fatal(in)
		}
	}
	if err := Validate(Save{Scope: "user", Content: strings.Repeat("🙂", 8000)}); err != nil {
		t.Fatal(err)
	}
}
