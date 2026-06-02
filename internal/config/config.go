// Package config loads runtime configuration from the environment, with
// sane defaults for local development.
package config

import (
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	Env             string // "dev" or "prod"
	SessionIdle     time.Duration
	SessionAbsolute time.Duration

	// WebAuthn relying-party settings. RPID is the effective domain (no
	// scheme/port); RPOrigin is the full origin the browser sends. For local
	// dev these are localhost / http://localhost:8080.
	RPID     string
	RPOrigin string
	RPName   string

	// TrustedProxies are the CIDRs of reverse proxies allowed to set forwarding
	// headers (CF-Connecting-IP / X-Forwarded-For). Empty by default, which is
	// the safe stance for a directly-exposed origin: only the socket address is
	// believed, so a client can't spoof its IP to dodge per-IP limits. Populate
	// it (e.g. with Cloudflare's ranges) only once traffic genuinely arrives via
	// that proxy.
	TrustedProxies []*net.IPNet
}

// CookieSecure reports whether session/CSRF cookies should carry the Secure
// attribute. On in prod (HTTPS assumed), off in dev so localhost http works.
func (c Config) CookieSecure() bool { return c.Env == "prod" }

func Load() Config {
	return Config{
		HTTPAddr:        env("ACTA_HTTP_ADDR", ":8080"),
		DatabaseURL:     env("ACTA_DATABASE_URL", "postgres://acta:acta@localhost:5432/acta?sslmode=disable"),
		Env:             env("ACTA_ENV", "dev"),
		SessionIdle:     envDuration("ACTA_SESSION_IDLE", 24*time.Hour),
		SessionAbsolute: envDuration("ACTA_SESSION_ABSOLUTE", 30*24*time.Hour),
		RPID:            env("ACTA_RP_ID", "localhost"),
		RPOrigin:        env("ACTA_RP_ORIGIN", "http://localhost:8080"),
		RPName:          env("ACTA_RP_NAME", "Acta"),
		TrustedProxies:  parseTrustedProxies(os.Getenv("ACTA_TRUSTED_PROXIES")),
	}
}

// parseTrustedProxies reads a comma/space-separated list of CIDRs (or bare IPs,
// treated as a single host) into networks. Invalid entries are logged and
// skipped rather than failing startup.
func parseTrustedProxies(s string) []*net.IPNet {
	var out []*net.IPNet
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if _, n, err := net.ParseCIDR(field); err == nil {
			out = append(out, n)
			continue
		}
		if ip := net.ParseIP(field); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		slog.Warn("ignoring invalid ACTA_TRUSTED_PROXIES entry", "value", field)
	}
	return out
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
