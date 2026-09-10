package tasks

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestSearchCursorAndValidation(t *testing.T) {
	for _, text := range []string{"", "x", strings.Repeat("a", 201), "a\x00b", string([]byte{255, 254})} {
		if _, _, e := NormalizeSearch(SearchQuery{Query: text}); e == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	q, p, e := NormalizeSearch(SearchQuery{Query: "  words  ", Workspace: " WORK "})
	if e != nil || q.Query != "words" || q.Workspace != "work" || p.ID != "" {
		t.Fatal(q, p, e)
	}
	id := uuid.NewString()
	q.Cursor = q.Next(500123, id)
	_, p, e = NormalizeSearch(q)
	if e != nil || p.ID != id || p.Score != 500123 {
		t.Fatal(p, e)
	}
	q.IncludeArchived = true
	if _, _, e = NormalizeSearch(q); e == nil {
		t.Fatal("archive scope change accepted")
	}
	q.IncludeArchived = false
	q.Workspace = "another"
	if _, _, e = NormalizeSearch(q); e == nil {
		t.Fatal("scope change accepted")
	}
}
