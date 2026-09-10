package cli

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
	"os"
	"path/filepath"
)

// Credential references are random IDs, independent of editable profile names
// and URLs. New credentials are staged before the atomic profile update.
type Vault struct {
	Dir    string
	Get    func(string, string) (string, error)
	Set    func(string, string, string) error
	Delete func(string, string) error
}

func newVault(dir string) Vault { return Vault{dir, keyring.Get, keyring.Set, keyring.Delete} }
func (v Vault) path(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("Invalid credential reference")
	}
	return filepath.Join(v.Dir, "credentials", id), nil
}
func (v Vault) Read(p Profile) (string, error) {
	if p.Credential == "" {
		return "", nil
	}
	path, err := v.path(p.Credential)
	if err != nil {
		return "", err
	}
	switch p.Storage {
	case "keyring":
		return v.Get("acta2", p.Credential)
	case "file":
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if err = checkPrivateFile(path, info); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		return string(raw), err
	default:
		return "", errors.New("Unknown credential storage type")
	}
}
func (v Vault) Save(token string, file bool) (Profile, string, error) {
	p := Profile{Credential: uuid.NewString(), Storage: "keyring"}
	notice := ""
	if !file {
		if err := v.Set("acta2", p.Credential, token); err == nil {
			return p, "", nil
		}
		notice = "System credential store unavailable; saving the credential in an owner-only plaintext file."
	}
	p.Storage = "file"
	path, err := v.path(p.Credential)
	if err != nil {
		return Profile{}, "", err
	}
	if err = atomicFile(path, []byte(token)); err != nil {
		return Profile{}, "", err
	}
	if notice == "" {
		notice = "Credential saved in an owner-only plaintext file."
	}
	return p, notice + " " + path, nil
}
func (v Vault) Remove(p Profile) error {
	if p.Credential == "" {
		return nil
	}
	path, err := v.path(p.Credential)
	if err != nil {
		return err
	}
	if p.Storage == "keyring" {
		err = v.Delete("acta2", p.Credential)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	if p.Storage != "file" {
		return fmt.Errorf("Unknown credential storage %q", p.Storage)
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
