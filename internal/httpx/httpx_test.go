package httpx

import "testing"

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                    "/",
		"/":                   "/",
		"/items":              "/items",
		"/items?status=open":  "/items?status=open",
		"//evil.example.com":  "/",
		"https://evil.com":    "/",
		"http://evil.com":     "/",
		"/\\evil.com":         "/",
		"javascript:alert(1)": "/",
		"\\\\evil":            "/",
	}
	for in, want := range cases {
		if got := SafeReturnTo(in); got != want {
			t.Errorf("SafeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
