package threads

import (
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, name := range []string{"A", "Agent: café 🚀", strings.Repeat("界", 120)} {
		if !ValidName(name) {
			t.Fatalf("rejected %q", name)
		}
	}
	for _, name := range []string{"", " ", " leading", "trailing ", "line\nbreak", "tab\tname", "\x00", "\xff", "a\u2028b", strings.Repeat("a", 121)} {
		if ValidName(name) {
			t.Fatalf("accepted %q", name)
		}
	}
}
