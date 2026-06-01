// Package httpx holds small HTTP helpers shared across the web and auth
// layers, with no dependencies of its own.
package httpx

import "strings"

// SafeReturnTo sanitises a post-login redirect target. It only permits local,
// absolute paths — anything that could redirect off-site (scheme-relative
// "//host", absolute URLs, backslash tricks) collapses to "/". This is what
// keeps the ?return_to= parameter from becoming an open-redirect.
func SafeReturnTo(raw string) string {
	if raw == "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") {
		return "/"
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/"
	}
	if strings.ContainsAny(raw, "\\") || strings.Contains(raw, "://") {
		return "/"
	}
	return raw
}
