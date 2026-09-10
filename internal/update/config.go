package update

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"

	"acta2/internal/localstate"
)

type Config struct {
	RetainRecoveryCopies int      `json:"retain_recovery_copies"`
	Repository           string   `json:"repository"`
	PublicKeyFile        string   `json:"public_key_file"`
	GitHubTokenFile      string   `json:"github_token_file,omitempty"`
	StateDir             string   `json:"state_dir"`
	Socket               string   `json:"socket"`
	TokenFile            string   `json:"token_file"`
	Project              string   `json:"project"`
	Installation         string   `json:"installation"`
	ComposeFiles         []string `json:"compose_files"`
	EnvFile              string   `json:"env_file"`
	BackupSocket         string   `json:"backup_socket"`
	BackupTokenFile      string   `json:"backup_token_file"`
	Prereleases          bool     `json:"prereleases"`
	TimeoutMinutes       int      `json:"timeout_minutes"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	if err := localstate.Read(path, &c); err != nil {
		return c, err
	}
	if !repositoryPattern.MatchString(c.Repository) || !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,60}$`).MatchString(c.Project) || !regexp.MustCompile(`^[a-zA-Z0-9-]{8,64}$`).MatchString(c.Installation) {
		return c, errors.New("invalid installation identity")
	}
	if len(c.ComposeFiles) == 0 || c.TimeoutMinutes < 5 || c.TimeoutMinutes > 1440 {
		return c, errors.New("configure compose files and a 5–1440 minute operation timeout")
	}
	for _, p := range append(c.ComposeFiles, c.StateDir, c.Socket, c.TokenFile, c.PublicKeyFile, c.EnvFile, c.BackupSocket, c.BackupTokenFile) {
		if !filepath.IsAbs(p) {
			return c, errors.New("updater paths must be absolute")
		}
	}
	if c.RetainRecoveryCopies == 0 {
		c.RetainRecoveryCopies = 2
	}
	if c.RetainRecoveryCopies < 2 || c.RetainRecoveryCopies > 100 {
		return c, errors.New("retain 2–100 local recovery copies")
	}
	return c, nil
}
func (c Config) Key() ([]byte, error) {
	raw, e := os.ReadFile(c.PublicKeyFile)
	if e != nil {
		return nil, e
	}
	return base64.StdEncoding.DecodeString(stringTrim(raw))
}
