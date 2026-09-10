package threads

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Names are single-line display metadata, never paths or provider input.
func ValidName(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 120 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}
