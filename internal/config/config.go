// Package config loads runtime configuration from the environment, with
// sane defaults for local development.
package config

import (
	"os"
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
	}
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
