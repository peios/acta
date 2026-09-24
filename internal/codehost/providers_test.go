package codehost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProviderAuthParsing(t *testing.T) {
	for _, tc := range []struct {
		name, id, out, stderr string
		code                  int
		want                  string
	}{
		{"claude signed in", "claude", `{"loggedIn":true,"email":"private@example.test"}`, "", 0, "signed_in"},
		{"claude signed out", "claude", `{"loggedIn":false}`, "", 1, "signed_out"},
		{"missing field", "claude", `{}`, "", 0, "unknown"},
		{"bad json", "claude", `{"loggedIn":true} junk`, "", 0, "unknown"},
		{"contradictory exit", "claude", `{"loggedIn":true}`, "", 1, "unknown"},
		{"codex stderr", "codex", "", "Logged in using ChatGPT\n", 0, "signed_in"},
		{"codex key", "codex", "", "Logged in using an API key - sk-private\n", 0, "signed_in"},
		{"codex signed out", "codex", "", "Not logged in\n", 1, "signed_out"},
		{"codex failed", "codex", "", "Unable to read credentials", 1, "unknown"},
		{"unsupported CLI", "codex", "Usage: codex", "", 0, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAuth(tc.id, []byte(tc.out), []byte(tc.stderr), tc.code); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderDiscoveryAndBoundedProbes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	missing := discoverProviders(t.Context())
	if !missing.Valid() || missing.Providers[0].Installed || missing.Providers[1].Installed {
		t.Fatal("missing providers reported installed")
	}
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("claude", "[ \"$*\" = 'auth status --json' ] || exit 2\nprintf '%s' '{\"loggedIn\":true}'\n")
	write("codex", "[ \"$*\" = 'login status' ] || exit 2\nprintf '%s' 'Not logged in' >&2\nexit 1\n")
	report := discoverProviders(t.Context())
	if !report.Valid() || report.Providers[0].Auth != "signed_in" || report.Providers[1].Auth != "signed_out" {
		t.Fatal("incorrect provider discovery", report)
	}
	path := write("slow", "exec /bin/sleep 10\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := probeAuth(ctx, path, dir, "codex"); got != "unknown" || time.Since(start) > time.Second {
		t.Fatal("probe ignored cancellation", got)
	}
	path = write("overflow", "i=0; while [ $i -lt 7000 ]; do printf '0123456789'; i=$((i+1)); done\nprintf '\\nLogged in using ChatGPT\\n'\n")
	if got := probeAuth(t.Context(), path, dir, "codex"); got != "unknown" {
		t.Fatal("truncated output trusted", got)
	}
	path = write("unsupported", "exit 2\n")
	if got := probeAuth(t.Context(), path, dir, "claude"); got != "unknown" {
		t.Fatal("failed probe marked signed out", got)
	}
}
