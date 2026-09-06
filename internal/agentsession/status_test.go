package agentsession

import "testing"

// TestSplitStatus reads markers at either end, in the spellings people
// type, and composes them back at the start.
func TestSplitStatus(t *testing.T) {
	cases := []struct{ in, status, bare string }{
		{"Testing [IN PROGRESS]", StatusInProgress, "Testing"},
		{"[TODO] Fix the build", StatusTodo, "Fix the build"},
		{"[done] shipped", StatusDone, "shipped"},
		{"  [ in-progress ]  Middle  ", StatusInProgress, "Middle"},
		{"Pekit2 [TODO]", StatusTodo, "Pekit2"},
		{"[WIP] thing", StatusInProgress, "thing"},
		{"No marker here", "", "No marker here"},
		{"Brackets [inside] the title", "", "Brackets [inside] the title"},
		{"[TODO]", StatusTodo, ""},
		{"", "", ""},
	}
	for _, c := range cases {
		s, b := SplitStatus(c.in)
		if s != c.status || b != c.bare {
			t.Errorf("SplitStatus(%q) = %q, %q; want %q, %q", c.in, s, b, c.status, c.bare)
		}
	}
	if got := TitleWithStatus(StatusInProgress, "Testing"); got != "[IN PROGRESS] Testing" {
		t.Errorf("compose = %q", got)
	}
	if got := TitleWithStatus("", "Plain"); got != "Plain" {
		t.Errorf("compose none = %q", got)
	}
	if got := TitleWithStatus(StatusDone, ""); got != "[DONE]" {
		t.Errorf("compose bare-less = %q", got)
	}
	// a round trip moves an end marker to the start
	s, b := SplitStatus("Testing [IN PROGRESS]")
	if got := TitleWithStatus(s, b); got != "[IN PROGRESS] Testing" {
		t.Errorf("round trip = %q", got)
	}
}
