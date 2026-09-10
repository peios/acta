package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilesAndAtomicUpdates(t *testing.T) {
	r := Repository{t.TempDir()}
	c, e := r.Load()
	if e != nil || c.Active != "default" || len(c.Profiles) != 1 {
		t.Fatal(c, e)
	}
	if e = r.Update(func(c *Config) error { c.Profiles["work"] = Profile{URL: "https://acta.example.org"}; return nil }); e != nil {
		t.Fatal(e)
	}
	if e = r.Update(func(c *Config) error { c.Active = "work"; return errors.New("cancelled") }); e == nil {
		t.Fatal("missing error")
	}
	c, e = r.Load()
	if e != nil || c.Active != "default" {
		t.Fatal("failed mutation committed")
	}
	info, e := os.Stat(filepath.Join(r.Dir, "config.json"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("configuration permissions")
	}
}
func TestCredentialFallbackAndEnvironment(t *testing.T) {
	v := Vault{Dir: t.TempDir(), Set: func(string, string, string) error { return errors.New("no keyring") }}
	p, notice, e := v.Save("cli_private", false)
	if e != nil || p.Storage != "file" || notice == "" {
		t.Fatal(p, e)
	}
	token, e := v.Read(p)
	if e != nil || token != "cli_private" {
		t.Fatal("credential read", e)
	}
	t.Setenv("ACTA_TOKEN", "environment-private")
	a := App{vault: v}
	token, source, e := a.credential(p)
	if e != nil || source != "ACTA_TOKEN" || token != "environment-private" {
		t.Fatal("environment precedence")
	}
	if e = v.Remove(p); e != nil {
		t.Fatal(e)
	}
	if _, e = v.Read(p); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("credential not removed")
	}
}
func TestNoninteractiveLoginDoesNotMutateProfiles(t *testing.T) {
	for _, args := range [][]string{{"login"}, {"-p", "missing", "login", "localhost:8081"}, {"login", "localhost:8081"}} {
		dir := t.TempDir()
		r := Repository{dir}
		if len(args) == 2 {
			if e := r.Update(func(c *Config) error { c.Profiles["default"] = Profile{URL: "https://old.example.org"}; return nil }); e != nil {
				t.Fatal(e)
			}
		}
		before, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		var out bytes.Buffer
		a := &App{repo: r, vault: newVault(dir), input: strings.NewReader(""), out: &out, errOut: &out}
		cmd := a.command()
		cmd.SetArgs(args)
		if e := cmd.ExecuteContext(t.Context()); e == nil {
			t.Fatal("missing noninteractive choice accepted", args)
		}
		after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
		if !bytes.Equal(before, after) {
			t.Fatal("failed login mutated config")
		}
	}
}
