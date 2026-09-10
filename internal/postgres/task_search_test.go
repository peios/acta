package postgres

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSearchExcerptIsText(t *testing.T) {
	p := searchExcerpt("<script>alert(1)</script> \ue000matching\ue001 plain")
	if len(p) != 3 || p[0].Text != "<script>alert(1)</script> " || p[0].Match || !p[1].Match || p[1].Text != "matching" || p[2].Match {
		t.Fatal(p)
	}
}

func TestSearchExcerptBounded(t *testing.T) {
	p := searchExcerpt(strings.Repeat("界", 1000))
	if len(p) != 1 || utf8.RuneCountInString(p[0].Text) > 601 {
		t.Fatal("unbounded excerpt")
	}
}
