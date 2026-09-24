package codehost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"acta/internal/accounts"
	"acta/internal/codebases"
	"acta/internal/codehosts"
	"github.com/google/uuid"
)

type Catalogue struct {
	mu    sync.Mutex
	path  string
	items []codebases.Codebase
}
type catalogueFile struct {
	Version   int                  `json:"version"`
	Codebases []codebases.Codebase `json:"codebases"`
}

const maxCatalogueBytes = 512 * 1024

// OpenCatalogue is called while the server/account's host process lock is held.
func OpenCatalogue(dir string) (*Catalogue, error) {
	c := &Catalogue{path: filepath.Join(dir, "codebases.json"), items: []codebases.Codebase{}}
	f, err := os.Open(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxCatalogueBytes+1))
	if err != nil {
		return nil, err
	}
	var data catalogueFile
	if len(raw) > maxCatalogueBytes || codehosts.Decode(raw, &data) != nil || data.Version != 1 {
		return nil, errors.New("invalid Code codebase catalogue")
	}
	ids := map[string]bool{}
	for _, item := range data.Codebases {
		if !validID(item.ID) || ids[item.ID] || len(item.Roots) == 0 || len(item.Roots) > codebases.MaxRoots {
			return nil, errors.New("invalid codebase identity or roots")
		}
		ids[item.ID] = true
		rootIDs := map[string]bool{}
		for _, root := range item.Roots {
			if !validID(root.ID) || rootIDs[root.ID] || !filepath.IsAbs(root.Path) {
				return nil, errors.New("invalid codebase root")
			}
			rootIDs[root.ID] = true
		}
	}
	if data.Codebases != nil {
		c.items = data.Codebases
	}
	return c, nil
}

func validID(id string) bool {
	v, err := uuid.Parse(id)
	return err == nil && v != uuid.Nil && v.String() == id
}
func problem(code, message string) error { return &codehosts.Problem{Code: code, Message: message} }

func (c *Catalogue) List() []codebases.Codebase {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]codebases.Codebase, len(c.items))
	for i, item := range c.items {
		out[i] = item
		out[i].Roots = slices.Clone(item.Roots)
	}
	return out
}

func (c *Catalogue) Add(ctx context.Context, in codebases.Add) (codebases.Codebase, error) {
	var empty codebases.Codebase
	name, err := accounts.DisplayName(in.Name)
	if err != nil || name == nil || !validID(in.ID) || len(in.Paths) == 0 || len(in.Paths) > codebases.MaxRoots {
		return empty, problem("invalid_codebase", "Enter a name and 1–16 existing absolute folder paths.")
	}
	paths := make([]string, 0, len(in.Paths))
	for _, path := range in.Paths {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if !filepath.IsAbs(path) || !utf8.ValidString(path) || len(path) > 4096 {
			return empty, problem("invalid_path", "Use an absolute folder path on the selected host.")
		}
		canonical, err := filepath.EvalSymlinks(filepath.Clean(path))
		if err != nil {
			return empty, problem("folder_unavailable", "A folder could not be found or accessed on the host.")
		}
		root, err := os.OpenRoot(canonical)
		if err != nil {
			return empty, problem("folder_unavailable", "Each root must be an accessible directory on the host.")
		}
		root.Close()
		if slices.Contains(paths, canonical) {
			return empty, problem("duplicate_root", "List each root folder only once.")
		}
		paths = append(paths, canonical)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.items {
		if item.ID != in.ID {
			continue
		}
		old := make([]string, len(item.Roots))
		for i, root := range item.Roots {
			old[i] = root.Path
		}
		if item.Name == *name && slices.Equal(old, paths) {
			return item, nil
		}
		return empty, problem("codebase_conflict", "This codebase request was already saved with different values. Refresh the list.")
	}
	if len(c.items) >= 256 {
		return empty, problem("catalogue_full", "This host has reached the limit of 256 codebases.")
	}
	item := codebases.Codebase{ID: in.ID, Name: *name, CreatedAt: time.Now().UTC(), Roots: []codebases.Root{}}
	for _, path := range paths {
		item.Roots = append(item.Roots, codebases.Root{ID: uuid.NewString(), Path: path})
	}
	next := append(slices.Clone(c.items), item)
	raw, err := json.MarshalIndent(catalogueFile{Version: 1, Codebases: next}, "", "  ")
	if err != nil {
		return empty, err
	}
	if len(raw)+1 > maxCatalogueBytes {
		return empty, problem("catalogue_full", "The codebase catalogue is full.")
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = writeCatalogue(c.path, append(raw, '\n')); err != nil {
		return empty, problem("storage_error", "The host could not save its codebase catalogue.")
	}
	c.items = next
	return item, nil
}

func writeCatalogue(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".codebases-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), path)
}

func (c *Catalogue) Handle(ctx context.Context, in codehosts.Request) codehosts.Response {
	out := codehosts.Response{ID: in.ID}
	var result any
	var err error
	switch in.Method {
	case codebases.ListMethod:
		var params struct{}
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result = map[string]any{"codebases": c.List()}
		}
	case codebases.AddMethod:
		var params codebases.Add
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = c.Add(ctx, params)
		}
	case codebases.DirectoryMethod:
		var params codebases.DirectoryQuery
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = c.Directory(ctx, params)
		}
	default:
		err = problem("unknown_method", "This host operation is not supported.")
	}
	if err != nil {
		var p *codehosts.Problem
		if errors.As(err, &p) {
			out.Error = p
		} else {
			out.Error = &codehosts.Problem{Code: "invalid_request", Message: "The host could not complete this request. Check its parameters and try again."}
		}
	} else {
		out.Result, err = json.Marshal(result)
		if err != nil {
			out.Error = &codehosts.Problem{Code: "invalid_response", Message: "The host could not encode its response."}
		} else if len(out.Result) > codehosts.MaxWireBytes-1024 {
			out.Result = nil
			out.Error = &codehosts.Problem{Code: "response_too_large", Message: "This listing is too large to send. Try a smaller directory."}
		}
	}
	return out
}
