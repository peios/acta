package postgres

import "testing"

// TestJSONBSafe checks the NUL escape becomes the replacement character while
// every other escape, including an escaped backslash before u0000, survives.
func TestJSONBSafe(t *testing.T) {
	nul := string(nulEscape)
	cases := []struct{ in, want string }{
		{`{"a":"plain"}`, `{"a":"plain"}`},
		{`{"a":"x` + nul + `y"}`, `{"a":"x` + "�" + `y"}`},
		{`{"a":"x\\` + nul[1:] + `"}`, `{"a":"x\\` + nul[1:] + `"}`}, // backslash then the letters u0000: not the escape
		{`{"a":"\n\"` + nul + nul + `"}`, `{"a":"\n\"` + "��" + `"}`},
		{`"` + nul, `"` + "�"},
	}
	for _, c := range cases {
		if got := string(jsonbSafe([]byte(c.in))); got != c.want {
			t.Errorf("jsonbSafe(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
