package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// profile names the login in use. Empty is the default login in
// $XDG_CONFIG_HOME/acta; a name keeps its own config and harness state under
// $XDG_CONFIG_HOME/acta/profiles/<name>, so one machine can stay logged in to
// several servers (a hosted Acta and a local one) and run a harness for each.
// Set with --profile on any command, or ACTA_PROFILE.
var profile string

var profileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// setProfile validates and records the profile name; a bad name is refused
// rather than turned into a path.
func setProfile(name string) error {
	if name != "" && !profileRe.MatchString(name) {
		return fmt.Errorf("bad profile name %q: letters, digits, . _ - only", name)
	}
	profile = name
	return nil
}

// takeProfile pulls --profile <name> / --profile=<name> out of args, wherever
// it appears, so subcommands never see it; ACTA_PROFILE applies when the flag
// is absent. It returns the remaining args.
func takeProfile(args []string) ([]string, error) {
	name := os.Getenv("ACTA_PROFILE")
	out := args[:0:0]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--profile" || a == "-profile":
			if i+1 >= len(args) {
				return nil, errors.New("--profile needs a name")
			}
			name = args[i+1]
			i++
		case strings.HasPrefix(a, "--profile=") || strings.HasPrefix(a, "-profile="):
			name = a[strings.Index(a, "=")+1:]
		default:
			out = append(out, a)
		}
	}
	if err := setProfile(name); err != nil {
		return nil, err
	}
	return out, nil
}

// configDir is where this profile's files live: $XDG_CONFIG_HOME/acta (default
// ~/.config/acta), or its profiles/<name> subdirectory under a profile.
func configDir() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	dir = filepath.Join(dir, "acta")
	if profile != "" {
		dir = filepath.Join(dir, "profiles", profile)
	}
	return dir, nil
}

// config is what `acta login` persists so later commands need no env vars. It
// lives at $XDG_CONFIG_HOME/acta/config.json (default ~/.config/acta), 0600;
// under a profile, in that profile's directory (see configDir).
type config struct {
	URL   string               `json:"url"`
	Token string               `json:"token"`
	MCP   map[string]mcpConfig `json:"mcp,omitempty"`
}

type mcpConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadConfig() config {
	path, err := configPath()
	if err != nil {
		return config{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}
	}
	var c config
	_ = json.Unmarshal(data, &c)
	return c
}

func saveConfig(c config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (c *config) MCPProfile(name string, m mcpConfig) {
	if c.MCP == nil {
		c.MCP = map[string]mcpConfig{}
	}
	c.MCP[name] = m
}

func clearConfig() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
