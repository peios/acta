package config

import "testing"

func TestOrigins(t *testing.T) {
	for _, tc := range []struct {
		url, dev      string
		valid, secure bool
	}{
		{"http://localhost:8081", "", true, false}, {"http://127.0.0.1:8081", "http://localhost:5173", true, false}, {"https://acta.peios.org/", "", true, true},
		{"http://acta.peios.org", "", false, false}, {"https://acta.peios.org/path", "", false, false}, {"https://user:pass@example.com", "", false, false}, {"https://acta.peios.org", "http://localhost:5173", false, false}, {"http://localhost:8081", "http://evil.test", false, false}, {"", "", false, false},
	} {
		t.Run(tc.url+tc.dev, func(t *testing.T) {
			got, err := Parse(tc.url, tc.dev)
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v", err)
			}
			if tc.valid && got.SecureCookies != tc.secure {
				t.Fatal("wrong cookie security")
			}
		})
	}
}
