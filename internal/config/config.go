package config

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

type Config struct {
	UpdateSocket      string
	UpdateTokenFile   string
	TrustedProxies    []netip.Prefix
	BackupSocket      string
	BackupTokenFile   string
	RecoveryMode      bool
	PublicURL         string
	DevelopmentOrigin string
	SecureCookies     bool
	Origins           map[string]bool
	Hosts             map[string]bool
}

func Parse(publicURL, developmentOrigin string) (Config, error) {
	c := Config{PublicURL: publicURL, DevelopmentOrigin: developmentOrigin, Origins: map[string]bool{}, Hosts: map[string]bool{}}
	for i, raw := range []string{publicURL, developmentOrigin} {
		if i == 1 && raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return c, errors.New("origins must be absolute HTTP(S) URLs without paths, credentials, queries or fragments")
		}
		local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
		if u.Scheme != "https" && !(u.Scheme == "http" && local) {
			return c, errors.New("HTTPS is required except on loopback development origins")
		}
		if i == 1 && (!local || c.SecureCookies) {
			return c, errors.New("ACTA_DEV_ORIGIN is only supported with a local HTTP development installation")
		}
		origin := strings.TrimSuffix(raw, "/")
		c.Origins[origin] = true
		c.Hosts[u.Host] = true
		if i == 0 {
			c.PublicURL = origin
			c.SecureCookies = u.Scheme == "https"
		}
	}
	return c, nil
}
