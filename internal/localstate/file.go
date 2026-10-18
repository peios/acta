// Package localstate provides crash-safe local files for Acta command-line tools.
package localstate

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Write atomically replaces path with private JSON and syncs the file and its directory.
func Write(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return err
	}
	// After rename the temporary name is absent; cleanup failure must not hide a write error.
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	// Sync reports durability failures; closing the read-only directory adds no data to flush.
	defer d.Close()
	return d.Sync()
}

// Read decodes the JSON file into value and returns filesystem or decoding errors.
func Read(path string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}
