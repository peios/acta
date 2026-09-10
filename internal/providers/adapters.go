package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
)

// Adapter is deliberately discovery-only until execution is designed. It is
// independent of the server connection and the eventual process/pipe boundary.
type Adapter interface {
	ID() string
	Discover(context.Context) Status
}
type cliAdapter struct {
	id     string
	runner Runner
}

func DefaultAdapters() []Adapter {
	return []Adapter{cliAdapter{"codex", localRunner{}}, cliAdapter{"claude", localRunner{}}}
}
func (a cliAdapter) ID() string { return a.id }
func (a cliAdapter) Discover(ctx context.Context) Status {
	s := Status{ID: a.id, Installation: "unknown", Authentication: "unknown"}
	path, err := a.runner.LookPath(a.id)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			s.Installation = "not_installed"
		} else {
			s.Issue = "probe_failed"
		}
		return s
	}
	s.Installation = "installed"
	version := a.runner.Run(ctx, path, "--version")
	if version.Err != nil || version.Code != 0 {
		s.Issue = probeIssue(ctx)
		return s
	}
	raw := strings.TrimSpace(version.Stdout)
	if a.id == "codex" {
		raw = strings.TrimPrefix(raw, "codex-cli ")
	} else {
		raw = strings.TrimSuffix(raw, " (Claude Code)")
	}
	if !validVersion(raw) {
		s.Issue = "version_unknown"
		return s
	}
	s.Version = raw
	args := []string{"login", "status"}
	usage := "Usage: codex login status"
	if a.id == "claude" {
		args = []string{"auth", "status"}
		usage = "Usage: claude auth status"
	}
	// Check the command surface before invoking it: older Claude versions may
	// interpret an unknown subcommand as a conversational prompt.
	help := a.runner.Run(ctx, path, append(append([]string{}, args...), "--help")...)
	if help.Err != nil || help.Code != 0 || !strings.Contains(help.Stdout+help.Stderr, usage) {
		s.Issue = "auth_unavailable"
		if ctx.Err() != nil {
			s.Issue = "timeout"
		}
		return s
	}
	result := a.runner.Run(ctx, path, args...)
	if ctx.Err() != nil {
		s.Issue = "timeout"
		return s
	}
	if result.Err != nil {
		s.Issue = "auth_unavailable"
		return s
	}
	if a.id == "claude" {
		// Deliberately decode only this boolean: CLI JSON can contain email, account
		// identifiers, project paths and other details which must not be advertised.
		var value struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if json.Unmarshal([]byte(result.Stdout), &value) == nil && value.LoggedIn != nil {
			if *value.LoggedIn && result.Code == 0 {
				s.Authentication = "signed_in"
			}
			if !*value.LoggedIn && result.Code == 1 {
				s.Authentication = "signed_out"
			}
		}
	} else {
		for _, line := range strings.Split(result.Stdout+"\n"+result.Stderr, "\n") {
			line = strings.TrimSpace(line)
			if result.Code == 0 && strings.HasPrefix(line, "Logged in using ") {
				s.Authentication = "signed_in"
			}
			if result.Code == 1 && line == "Not logged in" {
				s.Authentication = "signed_out"
			}
		}
	}
	if s.Authentication == "unknown" {
		s.Issue = "auth_unavailable"
	}
	return s
}
func probeIssue(ctx context.Context) string {
	if ctx.Err() != nil {
		return "timeout"
	}
	return "probe_failed"
}
