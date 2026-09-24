package codehost

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"acta/internal/codebases"
)

func (c *Catalogue) Directory(ctx context.Context, in codebases.DirectoryQuery) (codebases.Directory, error) {
	out := codebases.Directory{Entries: []codebases.Entry{}}
	path := in.Path
	if path == "" {
		path = "."
	}
	if !fs.ValidPath(path) || strings.Contains(path, "\\") {
		return out, problem("invalid_path", "Use a relative directory path within this codebase root.")
	}
	var base string
	c.mu.Lock()
	for _, item := range c.items {
		if item.ID == in.CodebaseID {
			for _, root := range item.Roots {
				if root.ID == in.RootID {
					base = root.Path
				}
			}
		}
	}
	c.mu.Unlock()
	if base == "" {
		return out, problem("not_found", "This codebase root does not exist on the host.")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return out, problem("folder_unavailable", "The codebase root is no longer accessible.")
	}
	defer root.Close()
	// OpenRoot requires a directory, so a malicious path cannot block on a FIFO.
	directory, err := root.OpenRoot(filepath.FromSlash(path))
	if err != nil {
		return out, problem("folder_unavailable", "This directory is unavailable or outside the codebase root.")
	}
	defer directory.Close()
	f, err := directory.Open(".")
	if err != nil {
		return out, problem("folder_unavailable", "The host cannot read this directory.")
	}
	defer f.Close()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	entries, err := f.ReadDir(codebases.MaxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return out, problem("folder_unavailable", "The host could not list this directory.")
	}
	if len(entries) > codebases.MaxEntries {
		out.Truncated = true
		entries = entries[:codebases.MaxEntries]
	}
	for _, entry := range entries {
		kind := "file"
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			kind = "symlink"
		case entry.IsDir():
			kind = "directory"
		case !entry.Type().IsRegular():
			kind = "other"
		}
		out.Entries = append(out.Entries, codebases.Entry{Name: entry.Name(), Kind: kind})
	}
	slices.SortFunc(out.Entries, func(a, b codebases.Entry) int {
		if (a.Kind == "directory") != (b.Kind == "directory") {
			if a.Kind == "directory" {
				return -1
			}
			return 1
		}
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}
