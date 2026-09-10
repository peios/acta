package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Config is operator-owned. None of its paths, commands or credentials can be
// replaced through the web API. Relative paths and shell command strings are
// deliberately unsupported.
type Config struct {
	ReleaseDir           string        `json:"release_dir"`
	StateDir             string        `json:"state_dir"`
	Socket               string        `json:"socket"`
	TokenFile            string        `json:"token_file"`
	DatabaseURLFile      string        `json:"database_url_file"`
	DatabaseName         string        `json:"database_name"`
	DatabaseUser         string        `json:"database_user"`
	Stanza               string        `json:"stanza"`
	PGBackRest           string        `json:"pgbackrest"`
	Age                  string        `json:"age"`
	PostgresBin          string        `json:"postgres_bin"`
	SecurityKeyFile      string        `json:"security_key_file"`
	ReleaseFile          string        `json:"release_file"`
	RecoveryRecipient    string        `json:"recovery_recipient"`
	RecoveryIdentityFile string        `json:"recovery_identity_file"`
	RestoreRoot          string        `json:"restore_root"`
	AlertCommand         string        `json:"alert_command"`
	JobTimeoutMinutes    int           `json:"job_timeout_minutes"`
	MinRetainFull        int           `json:"min_retain_full"`
	MaxRetainFull        int           `json:"max_retain_full"`
	Destinations         []Destination `json:"destinations"`
}

type Release struct {
	ID        string `json:"id"`
	SHA256    string `json:"sha256"`
	PublicURL string `json:"public_url"`
	// A stable, non-secret identifier prevents attaching one site's recovery
	// data to another. The database's system identifier is also captured.
	Installation string `json:"installation"`
}

func ReadJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

func LoadConfig(path string) (Config, error) {
	var c Config
	if err := ReadJSON(path, &c); err != nil {
		return c, err
	}
	for name, p := range map[string]string{"state_dir": c.StateDir, "release_dir": c.ReleaseDir, "socket": c.Socket, "token_file": c.TokenFile, "database_url_file": c.DatabaseURLFile, "pgbackrest": c.PGBackRest, "age": c.Age, "postgres_bin": c.PostgresBin, "security_key_file": c.SecurityKeyFile, "release_file": c.ReleaseFile, "restore_root": c.RestoreRoot} {
		if !filepath.IsAbs(p) {
			return c, fmt.Errorf("%s must be an absolute path", name)
		}
	}
	for _, p := range []string{c.AlertCommand, c.RecoveryIdentityFile} {
		if p != "" && !filepath.IsAbs(p) {
			return c, errors.New("optional command and identity paths must be absolute")
		}
	}
	if c.Stanza == "" || c.DatabaseName == "" || c.DatabaseUser == "" || c.RecoveryRecipient == "" {
		return c, errors.New("stanza, database identity and recovery recipient are required")
	}
	if c.MinRetainFull < 2 || c.MaxRetainFull < c.MinRetainFull || c.MaxRetainFull > 10000 {
		return c, errors.New("retention bounds must allow at least two full backups")
	}
	if c.JobTimeoutMinutes < 1 || c.JobTimeoutMinutes > 10080 {
		return c, errors.New("job timeout must be 1 minute to 7 days")
	}
	seen := map[string]bool{}
	for _, d := range c.Destinations {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(d.ID) || seen[d.ID] || !filepath.IsAbs(d.ConfigFile) || d.Repo < 1 || d.Repo > 256 || d.Name == "" {
			return c, errors.New("invalid or duplicate destination")
		}
		seen[d.ID] = true
	}
	if len(seen) == 0 {
		return c, errors.New("configure at least one destination")
	}
	return c, nil
}

func (c Config) Destination(id string) (Destination, bool) {
	for _, d := range c.Destinations {
		if d.ID == id {
			return d, true
		}
	}
	return Destination{}, false
}

func Secret(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 1<<20 {
		return nil, errors.New("secret must be a private regular file, at most 1 MiB")
	}
	return os.ReadFile(path)
}
