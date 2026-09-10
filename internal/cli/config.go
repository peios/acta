package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gofrs/flock"
	"os"
	"path/filepath"
	"regexp"
)

type Profile struct {
	URL        string `json:"url,omitempty"`
	Credential string `json:"credential,omitempty"`
	Storage    string `json:"storage,omitempty"`
}
type Config struct {
	Active   string             `json:"active"`
	Profiles map[string]Profile `json:"profiles"`
}
type Repository struct{ Dir string }

var profilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func validName(name string) error {
	if !profilePattern.MatchString(name) {
		return errors.New("Profile names must be 1–64 letters, digits, dots, underscores or hyphens, starting with a letter or digit")
	}
	return nil
}
func configDir() (string, error) {
	if dir := os.Getenv("ACTA_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "acta2"), err
}
func (r Repository) Load() (Config, error) {
	c := Config{Active: "default", Profiles: map[string]Profile{"default": {}}}
	raw, err := os.ReadFile(filepath.Join(r.Dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("read profile configuration: %w", err)
	}
	if c.Profiles == nil {
		return c, errors.New("Invalid profile configuration")
	}
	if _, ok := c.Profiles[c.Active]; !ok {
		return c, errors.New("Active profile does not exist")
	}
	return c, nil
}
func (r Repository) Update(fn func(*Config) error) error {
	if err := os.MkdirAll(r.Dir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(r.Dir, "config.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("Another acta2 command is updating profiles; try again")
	}
	defer lock.Close()
	c, err := r.Load()
	if err != nil {
		return err
	}
	if err = fn(&c); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(r.Dir, "config.json"), append(raw, '\n'))
}
func atomicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".acta-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = restrictFile(f); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
