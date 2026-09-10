package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// address follows X-Forwarded-For from the trusted peer towards the client,
// stopping at the first untrusted hop. Never let a caller-selected header or
// malformed chain create new rate-limit identities. RemoteAddr is not mutated.
func (h *Handler) address(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	direct := peer.String()
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range h.config.TrustedProxies {
			if prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return direct
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return direct
	}
	// Bound work independently of the server-wide header size limit.
	size := 0
	for _, value := range values {
		size += len(value)
	}
	if size > 4096 {
		return direct
	}
	parts := strings.Split(strings.Join(values, ","), ",")
	if len(parts) > 32 {
		return direct
	}
	hops := make([]netip.Addr, len(parts))
	for i, part := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || ip.Zone() != "" {
			return direct
		}
		hops[i] = ip.Unmap()
	}
	for i := len(hops) - 1; i >= 0; i-- {
		peer = hops[i]
		if !trusted(peer) {
			return peer.String()
		}
	}
	// No untrusted client was established. Retain the actual peer's bucket.
	return direct
}
