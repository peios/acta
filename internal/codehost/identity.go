// Package codehost is the local Code host process, separate from agent runtimes.
package codehost

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

// LockIdentity gives each server/account pair a stable host ID, independent of
// editable profile names. Hold the lock for the lifetime of the process.
func LockIdentity(dir, server, account string) (string, func(), error) {
	dir = IdentityDir(dir, server, account)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	lock := flock.New(filepath.Join(dir, "host.lock"))
	ok, err := lock.TryLock()
	if err != nil || !ok {
		lock.Close()
		if err == nil {
			err = errors.New("acta-code-host is already running for this server and user")
		}
		return "", nil, err
	}
	release := func() { _ = lock.Close() }
	fail := func(err error) (string, func(), error) { release(); return "", nil, err }
	path := filepath.Join(dir, "host-id")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		id := uuid.NewString()
		f, err := os.CreateTemp(dir, ".host-id-*")
		if err != nil {
			return fail(err)
		}
		defer os.Remove(f.Name())
		if _, err = f.WriteString(id + "\n"); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
		if err = os.Rename(f.Name(), path); err != nil {
			return fail(err)
		}
		return id, release, nil
	}
	if err != nil {
		return fail(err)
	}
	id := strings.TrimSpace(string(raw))
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return fail(errors.New("invalid saved Code host identity"))
	}
	return id, release, nil
}

func IdentityDir(dir, server, account string) string {
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(server+"\x00"+account)))
	return filepath.Join(dir, "code-host", scope)
}
