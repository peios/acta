package web

import (
	"net"
	"net/http"
	"testing"
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestClientIP(t *testing.T) {
	// 10/8 stands in for a private proxy network; 173.245.48.0/20 is a real
	// Cloudflare range.
	trusted := []*net.IPNet{mustCIDR("10.0.0.0/8"), mustCIDR("173.245.48.0/20")}

	cases := []struct {
		name   string
		remote string
		cf     string
		xff    string
		want   string
	}{
		{"direct, no headers", "203.0.113.7:5555", "", "", "203.0.113.7"},
		{"direct ignores spoofed xff", "203.0.113.7:5555", "", "1.2.3.4", "203.0.113.7"},
		{"direct ignores spoofed cf-connecting-ip", "203.0.113.7:5555", "9.9.9.9", "", "203.0.113.7"},
		{"trusted proxy honours cf-connecting-ip", "173.245.48.1:443", "198.51.100.23", "198.51.100.23", "198.51.100.23"},
		{"trusted proxy falls back to xff", "10.1.2.3:443", "", "198.51.100.5, 10.1.2.3", "198.51.100.5"},
		{"trusted proxy skips trusted xff hops", "10.1.2.3:443", "", "198.51.100.9, 10.9.9.9", "198.51.100.9"},
		{"unparseable remote returned verbatim", "garbage", "", "", "garbage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{RemoteAddr: tc.remote, Header: http.Header{}}
			if tc.cf != "" {
				r.Header.Set("CF-Connecting-IP", tc.cf)
			}
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(r, trusted); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPNoTrustedProxies(t *testing.T) {
	// With nothing trusted, forwarding headers are always ignored.
	r := &http.Request{RemoteAddr: "203.0.113.7:5555", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("CF-Connecting-IP", "9.9.9.9")
	if got := clientIP(r, nil); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7", got)
	}
}
