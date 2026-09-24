package cli

import "testing"

func TestConnectionSharesProfilesCredentialsAndPinsSelection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ACTA_CONFIG_DIR", dir)
	t.Setenv("ACTA_TOKEN", "")
	p, _, err := newVault(dir).Save("cli_saved", true)
	if err != nil {
		t.Fatal(err)
	}
	p.URL = "http://localhost:8080/"
	repo := Repository{Dir: dir}
	if err = repo.Update(func(c *Config) error { c.Profiles["local"] = p; c.Active = "local"; return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := OpenConnection("")
	if err != nil {
		t.Fatal(err)
	}
	if first.Profile != "local" || first.ConfigDir != dir || first.Client.Token != "cli_saved" || first.Client.URL != "http://localhost:8080" {
		t.Fatal("shared resolution failed")
	}
	if err = repo.Update(func(c *Config) error { c.Active = "default"; return nil }); err != nil {
		t.Fatal(err)
	}
	if first.Profile != "local" || first.Client.Token != "cli_saved" {
		t.Fatal("existing connection changed")
	}
	explicit, err := OpenConnection("local")
	if err != nil || explicit.Client.Token != "cli_saved" {
		t.Fatal("explicit profile failed", err)
	}
	if _, err = OpenConnection("missing"); err == nil {
		t.Fatal("unknown profile fell back")
	}
	t.Setenv("ACTA_TOKEN", "cli_environment")
	env, err := OpenConnection("local")
	if err != nil || env.Client.Token != "cli_environment" {
		t.Fatal("environment precedence changed", err)
	}
}
