package codehost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"acta/internal/codebases"
	"acta/internal/codehosts"
	"github.com/google/uuid"
)

func TestCatalogueMultipleRootsPersistenceAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenCatalogue(dir)
	if err != nil {
		t.Fatal(err)
	}
	input := codebases.Add{ID: uuid.NewString(), Name: "peios", Paths: []string{t.TempDir(), t.TempDir()}}
	first, err := c.Add(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Roots) != 2 || first.Roots[0].ID == first.Roots[1].ID {
		t.Fatal("roots missing identities")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			again, err := c.Add(t.Context(), input)
			if err != nil || !reflect.DeepEqual(again, first) {
				t.Error("retry was not idempotent", err)
			}
		})
	}
	wg.Wait()
	loaded, err := OpenCatalogue(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.List(); len(got) != 1 || !reflect.DeepEqual(got[0], first) {
		t.Fatal("catalogue did not persist", got)
	}
	input.Name = "Changed"
	if _, err = c.Add(t.Context(), input); err == nil {
		t.Fatal("request ID reused with different values")
	}
	info, err := os.Stat(filepath.Join(dir, "codebases.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("catalogue file permissions", err)
	}
	other, err := OpenCatalogue(t.TempDir())
	if err != nil || len(other.List()) != 0 {
		t.Fatal("catalogue crossed host state directories")
	}
	copy := c.List()
	copy[0].Roots[0].Path = "mutated"
	if c.List()[0].Roots[0].Path == "mutated" {
		t.Fatal("caller mutated catalogue")
	}
}

func TestDirectoryContainmentAndEntryKinds(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"sub", "empty"} {
		if err := os.Mkdir(filepath.Join(base, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "sub", "visible.txt"), []byte("never returned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Fatal(err)
	}
	c, err := OpenCatalogue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item, err := c.Add(t.Context(), codebases.Add{ID: uuid.NewString(), Name: "code", Paths: []string{base}})
	if err != nil {
		t.Fatal(err)
	}
	query := codebases.DirectoryQuery{CodebaseID: item.ID, RootID: item.Roots[0].ID, Path: "."}
	listing, err := c.Directory(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 3 || listing.Entries[0].Name != "empty" || listing.Entries[2].Kind != "symlink" {
		t.Fatal("entry kinds/order", listing)
	}
	for _, path := range []string{"..", "../other", "/etc", "escape", "sub/../../other"} {
		query.Path = path
		if _, err = c.Directory(t.Context(), query); err == nil {
			t.Fatal("escaped registered root", path)
		}
	}
	query.Path = "sub"
	listing, err = c.Directory(t.Context(), query)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "visible.txt" {
		t.Fatal("nested directory", listing, err)
	}
	query.Path = "sub/visible.txt"
	if _, err = c.Directory(t.Context(), query); err == nil {
		t.Fatal("file treated as directory")
	}
	query.Path = "empty"
	listing, err = c.Directory(t.Context(), query)
	if err != nil || len(listing.Entries) != 0 {
		t.Fatal("empty directory", err)
	}
	query.RootID = uuid.NewString()
	if _, err = c.Directory(t.Context(), query); err == nil {
		t.Fatal("unknown root accepted")
	}
}

func TestCatalogueRejectsInvalidRegistrationAndBoundsListing(t *testing.T) {
	c, err := OpenCatalogue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	for _, paths := range [][]string{{"relative"}, {base, base}, {filepath.Join(base, "missing")}, {}} {
		if _, err = c.Add(t.Context(), codebases.Add{ID: uuid.NewString(), Name: "code", Paths: paths}); err == nil {
			t.Fatal("invalid roots accepted", paths)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = c.Add(ctx, codebases.Add{ID: uuid.NewString(), Name: "code", Paths: []string{base}}); err == nil {
		t.Fatal("cancelled registration persisted")
	}
	for i := range codebases.MaxEntries + 1 {
		if err = os.WriteFile(filepath.Join(base, fmt.Sprintf("file-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	item, err := c.Add(t.Context(), codebases.Add{ID: uuid.NewString(), Name: "code", Paths: []string{base}})
	if err != nil {
		t.Fatal(err)
	}
	listing, err := c.Directory(t.Context(), codebases.DirectoryQuery{CodebaseID: item.ID, RootID: item.Roots[0].ID})
	if err != nil || !listing.Truncated || len(listing.Entries) != codebases.MaxEntries {
		t.Fatal("listing not bounded", err)
	}
	response := c.Handle(t.Context(), codehosts.Request{ID: "x", Method: "files.read", Params: json.RawMessage(`{}`)})
	if response.Error == nil || response.Error.Code != "unknown_method" {
		t.Fatal("file content operation allowed")
	}
}
