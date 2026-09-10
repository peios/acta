package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// ParseTrustedProxies accepts only explicit IPs and networks, never hostnames or
// implicit private-address trust. Empty configuration trusts no proxy.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(raw, ",") {
		value := strings.TrimSpace(entry)
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			ip, ipErr := netip.ParseAddr(value)
			if ipErr != nil || ip.Zone() != "" {
				return nil, fmt.Errorf("ACTA_TRUSTED_PROXIES: invalid IP or CIDR %q", value)
			}
			ip = ip.Unmap()
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, fmt.Errorf("ACTA_TRUSTED_PROXIES: invalid mapped IPv4 network %q", value)
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		if prefix.Bits() == 0 {
			return nil, fmt.Errorf("ACTA_TRUSTED_PROXIES: refusing to trust every address")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}
