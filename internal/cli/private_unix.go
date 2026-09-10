//go:build !windows

package cli

import (
	"errors"
	"os"
)

func restrictFile(f *os.File) error { return f.Chmod(0600) }
func checkPrivateFile(_ string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("Credential file must be a private regular file (chmod 600)")
	}
	return nil
}
