package web

import (
	"net"
	"net/http"
	"strings"
)

// clientIP returns the best estimate of the originating client's address.
//
// If the immediate peer (RemoteAddr) is one of the configured trusted proxies,
// the proxy's forwarding headers are believed: Cloudflare's CF-Connecting-IP
// first, else the right-most non-trusted hop in X-Forwarded-For. Otherwise the
// socket peer address is used verbatim — so a client connecting directly to the
// origin cannot forge a header to dodge per-IP rate limits. With no trusted
// proxies configured (the default), this is always just the peer address.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !ipInAny(peer, trusted) {
		return host
	}

	// The peer is a trusted proxy; honour its forwarding headers.
	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
		if ip := net.ParseIP(cf); ip != nil {
			return ip.String()
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			if !ipInAny(ip, trusted) {
				return ip.String()
			}
		}
	}
	return host
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
