package httpapi

import (
	"acta/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAddressTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, trust, peer string
		headers           []string
		want              string
	}{
		{"default ignores spoof", "", "192.0.2.1:123", []string{"198.51.100.1"}, "192.0.2.1"},
		{"untrusted peer", "10.0.0.2", "192.0.2.1:123", []string{"198.51.100.1"}, "192.0.2.1"},
		{"trusted peer", "10.0.0.2", "10.0.0.2:123", []string{"198.51.100.1"}, "198.51.100.1"},
		{"forged prefix", "10.0.0.2", "10.0.0.2:123", []string{"203.0.113.77,198.51.100.1"}, "198.51.100.1"},
		{"multiple proxies", "10.0.0.0/24", "10.0.0.2:123", []string{"203.0.113.77,198.51.100.1,10.0.0.3"}, "198.51.100.1"},
		{"multiple header lines", "10.0.0.0/24", "10.0.0.2:123", []string{"203.0.113.77,198.51.100.1", "10.0.0.3"}, "198.51.100.1"},
		{"all trusted", "10.0.0.0/24", "10.0.0.2:123", []string{"10.0.0.3"}, "10.0.0.2"},
		{"missing", "10.0.0.2", "10.0.0.2:123", nil, "10.0.0.2"},
		{"empty", "10.0.0.2", "10.0.0.2:123", []string{""}, "10.0.0.2"},
		{"malformed chain", "10.0.0.2", "10.0.0.2:123", []string{"unknown,198.51.100.1"}, "10.0.0.2"},
		{"port not IP", "10.0.0.2", "10.0.0.2:123", []string{"198.51.100.1:9"}, "10.0.0.2"},
		{"zone rejected", "10.0.0.2", "10.0.0.2:123", []string{"fe80::1%eth0"}, "10.0.0.2"},
		{"IPv6 normalized", "::1", "[::1]:123", []string{"2001:0db8:0:0::1"}, "2001:db8::1"},
		{"mapped peer", "10.0.0.2", "[::ffff:10.0.0.2]:123", []string{"::ffff:198.51.100.1"}, "198.51.100.1"},
		{"too many hops", "10.0.0.2", "10.0.0.2:123", []string{strings.Repeat("10.0.0.2,", 32) + "198.51.100.1"}, "10.0.0.2"},
		{"oversized", "10.0.0.2", "10.0.0.2:123", []string{strings.Repeat(" ", 4096) + "198.51.100.1"}, "10.0.0.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefixes, err := config.ParseTrustedProxies(tc.trust)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{config: config.Config{TrustedProxies: prefixes}}
			r := httptest.NewRequest("GET", "http://localhost/", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.headers {
				r.Header.Add("X-Forwarded-For", v)
			}
			r.Header.Set("X-Real-IP", "203.0.113.99")
			r.Header.Set("Forwarded", "for=203.0.113.99")
			if got := h.address(r); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
			if r.RemoteAddr != tc.peer {
				t.Fatal("modified transport peer")
			}
		})
	}
}
