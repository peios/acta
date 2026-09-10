package config

import "testing"

func TestTrustedProxies(t *testing.T) {
	for _, raw := range []string{"", "  ", "127.0.0.1", "::1", "10.2.3.4/24, 2001:db8::/64", "::ffff:192.0.2.1", "::ffff:192.0.2.0/120"} {
		if _, err := ParseTrustedProxies(raw); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
	for _, raw := range []string{"caddy", "*", "private_ranges", "10.0.0.1,", ",", "0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96", "fe80::1%eth0", "10.0.0.1:80", "10.0.0.1/99"} {
		if _, err := ParseTrustedProxies(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	got, err := ParseTrustedProxies("::ffff:192.0.2.1,10.1.2.3/24")
	if err != nil || got[0].String() != "192.0.2.1/32" || got[1].String() != "10.1.2.0/24" {
		t.Fatalf("%v, %v", got, err)
	}
}
