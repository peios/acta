package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileRoutesConfig checks that a profile moves both files the CLI keeps
// into their own directory, and that the default stays where it was.
func TestProfileRoutesConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x")
	t.Setenv("ACTA_PROFILE", "")
	defer func() { profile = "" }()

	if err := setProfile(""); err != nil {
		t.Fatal(err)
	}
	if p, _ := configPath(); p != filepath.Join("/x", "acta", "config.json") {
		t.Errorf("default config path = %s", p)
	}
	if p := harnessStatePath(); p != filepath.Join("/x", "acta", "harness-sessions.json") {
		t.Errorf("default state path = %s", p)
	}

	if err := setProfile("local"); err != nil {
		t.Fatal(err)
	}
	if p, _ := configPath(); p != filepath.Join("/x", "acta", "profiles", "local", "config.json") {
		t.Errorf("profile config path = %s", p)
	}
	if p := harnessStatePath(); p != filepath.Join("/x", "acta", "profiles", "local", "harness-sessions.json") {
		t.Errorf("profile state path = %s", p)
	}

	for _, bad := range []string{"../x", "a/b", ".hidden", "", " "} {
		if err := setProfile(bad); err == nil && bad != "" {
			t.Errorf("profile %q accepted", bad)
		}
	}
}

// TestTakeProfile checks the flag is lifted out of the arguments wherever it
// sits, in both spellings, and that the environment fills in when it is absent.
func TestTakeProfile(t *testing.T) {
	t.Setenv("ACTA_PROFILE", "")
	defer func() { profile = "" }()

	cases := []struct{ in, rest, want string }{
		{"--profile local harness", "harness", "local"},
		{"harness --profile=local", "harness", "local"},
		{"item --profile local abc status Done", "item abc status Done", "local"},
		{"whoami", "whoami", ""},
	}
	for _, c := range cases {
		profile = ""
		rest, err := takeProfile(strings.Fields(c.in))
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got := strings.Join(rest, " "); got != c.rest {
			t.Errorf("%q: rest = %q, want %q", c.in, got, c.rest)
		}
		if profile != c.want {
			t.Errorf("%q: profile = %q, want %q", c.in, profile, c.want)
		}
	}

	profile = ""
	t.Setenv("ACTA_PROFILE", "fromenv")
	if _, err := takeProfile([]string{"whoami"}); err != nil || profile != "fromenv" {
		t.Errorf("env profile: %q, %v", profile, err)
	}
	if _, err := takeProfile([]string{"--profile"}); err == nil {
		t.Error("a bare --profile should be refused")
	}
	if _, err := takeProfile([]string{"--profile", "../x", "whoami"}); err == nil {
		t.Error("a path-like profile should be refused")
	}
}
