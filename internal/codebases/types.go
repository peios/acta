// Package codebases defines the host-owned catalogue and directory protocol.
package codebases

import "time"

const (
	ListMethod      = "codebases.list"
	AddMethod       = "codebases.add"
	DirectoryMethod = "directories.list"
	MaxRoots        = 16
	MaxEntries      = 2000
)

type Root struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type Codebase struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Roots     []Root    `json:"roots"`
	CreatedAt time.Time `json:"created_at"`
}
type Add struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}
type DirectoryQuery struct {
	CodebaseID string `json:"codebase_id"`
	RootID     string `json:"root_id"`
	Path       string `json:"path"`
}
type Entry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type Directory struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}
