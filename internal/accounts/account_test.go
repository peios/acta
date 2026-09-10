package accounts

import (
	"strings"
	"testing"
)

func TestUsername(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"JACK.Reviewer-2_ok", "jack.reviewer-2_ok"}, {"a", "a"}, {strings.Repeat("a", 32), strings.Repeat("a", 32)},
		{"", ""}, {" jack", ""}, {"jack ", ""}, {"jack/reviewer", ""}, {".jack", ""}, {"jack-", ""}, {"JäCK", ""}, {"Kate", ""}, {strings.Repeat("a", 33), ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := Username(tc.raw)
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted invalid username")
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}
func TestDisplayName(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		invalid   bool
	}{
		{"  Jack · Peios  ", "Jack · Peios", false}, {" \t ", "", false}, {"Cafe\u0301", "Café", false},
		{strings.Repeat("👩‍💻", 100), strings.Repeat("👩‍💻", 100), false}, {strings.Repeat("👩‍💻", 101), "", true},
		{"two\nlines", "", true}, {"hidden\u202ename", "", true}, {"a\x00b", "", true}, {"a\u2028b", "", true}, {"\xff", "", true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := DisplayName(tc.raw)
			if tc.invalid {
				if err == nil {
					t.Fatal("accepted invalid name")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatal("blank should be absent")
				}
			} else if got == nil || *got != tc.want {
				t.Fatalf("unexpected normalized name %v", got)
			}
		})
	}
}
func TestHandleUsesResolvedParent(t *testing.T) {
	parent := "permanent-parent-id"
	a := Account{ID: "child-id", Username: "reviewer", ParentID: &parent, ParentUsername: "jack"}
	if a.Handle() != "jack/reviewer" {
		t.Fatal(a.Handle())
	}
	a.ParentUsername = "renamed"
	if a.Handle() != "renamed/reviewer" || a.ID != "child-id" || *a.ParentID != parent {
		t.Fatal("name changed identity")
	}
}
