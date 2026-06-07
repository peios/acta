package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// config is what `acta login` persists so later commands need no env vars. It
// lives at $XDG_CONFIG_HOME/acta/config.json (default ~/.config/acta), 0600.
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
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "acta", "config.json"), nil
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
