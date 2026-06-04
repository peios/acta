// Package config loads runtime configuration from the environment, with
// sane defaults for local development.
package config

import (
	"log/slog"
	"net"
	"os"
	"strconv"
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

	// Login brute-force throttle. Failures are remembered for LoginWindow. An IP
	// is blocked once it accumulates LoginIPMax failures in that window. Each
	// consecutive failure against a given username adds LoginBackoffStep of delay
	// to the response (capped at LoginBackoffMax) — a soft slow-down that never
	// hard-locks an account, so it can't be used to deny a real user access.
	LoginWindow      time.Duration
	LoginIPMax       int
	LoginBackoffStep time.Duration
	LoginBackoffMax  time.Duration

	// Web Push (VAPID). The public key reaches the browser to authenticate the
	// subscription; the private key signs the JWT we send to the push service.
	// Generate a pair with `go run ./cmd/acta-vapid`. Both empty (the default)
	// disables push entirely — the settings toggle hides and the sender no-ops —
	// so dev and an offline Peios box need no keys. VAPIDSubject is the contact
	// the push service can reach about our traffic; it defaults to RPOrigin.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
}

// CookieSecure reports whether session/CSRF cookies should carry the Secure
// attribute. On in prod (HTTPS assumed), off in dev so localhost http works.
func (c Config) CookieSecure() bool { return c.Env == "prod" }

// PushEnabled reports whether Web Push is configured (a VAPID key pair is set).
// When false the push subsystem is inert.
func (c Config) PushEnabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
}

// PushSubject is the VAPID "sub" contact, falling back to the public origin so
// a deployment that sets only the key pair still sends a valid subscriber.
func (c Config) PushSubject() string {
	if c.VAPIDSubject != "" {
		return c.VAPIDSubject
	}
	return c.RPOrigin
}

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

		LoginWindow:      envDuration("ACTA_LOGIN_WINDOW", 15*time.Minute),
		LoginIPMax:       envInt("ACTA_LOGIN_IP_MAX", 20),
		LoginBackoffStep: envDuration("ACTA_LOGIN_BACKOFF_STEP", time.Second),
		LoginBackoffMax:  envDuration("ACTA_LOGIN_BACKOFF_MAX", 10*time.Second),

		VAPIDPublicKey:  os.Getenv("ACTA_VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("ACTA_VAPID_PRIVATE_KEY"),
		VAPIDSubject:    os.Getenv("ACTA_VAPID_SUBJECT"),
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
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
