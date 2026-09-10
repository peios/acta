// Package documents defines task-owned documents and immutable file versions.
package documents

import (
	"acta2/internal/accounts"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBytes = 20 * 1024 * 1024
const PageSize = 50

var ErrConflict = errors.New("This document changed. Reload its latest revision before uploading a version or deleting it.")

type Version struct {
	FileID     string    `json:"file_id"`
	DocumentID string    `json:"document_id"`
	Revision   int64     `json:"revision"`
	Title      string    `json:"title"`
	Filename   string    `json:"filename"`
	MediaType  string    `json:"media_type"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}
type Document struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Version
	CanWrite bool `json:"can_write"`
}
type Page struct {
	Documents []Document `json:"documents"`
	Cursor    string     `json:"cursor,omitempty"`
}
type History struct {
	Versions []Version `json:"versions"`
	Before   int64     `json:"before,omitempty"`
}
type Save struct {
	ID        string
	Task      string
	Title     string
	Filename  string
	Revision  int64
	Content   []byte
	MediaType string
	SHA256    string
}

func Validate(s *Save) error {
	if s.ID == "" && s.Revision != 0 {
		return Invalid("id", "Supply the document UUID when uploading a new version.")
	}
	if s.ID == "" {
		s.ID = uuid.NewString()
	} else {
		id, err := uuid.Parse(s.ID)
		if err != nil {
			return Invalid("id", "Supply a document UUID.")
		}
		s.ID = id.String()
	}
	if s.Revision < 0 {
		return Invalid("revision", "Use revision 0 to create, or the latest revision to upload a new version.")
	}
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" {
		s.Title = s.Filename
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{{"title", s.Title, 200}, {"filename", s.Filename, 255}} {
		if f.value == "" || len(f.value) > f.max || !utf8.ValidString(f.value) || strings.ContainsAny(f.value, "\x00\r\n") {
			return Invalid(f.name, "Supply a valid title (200 bytes) and filename (255 bytes).")
		}
	}
	if strings.ContainsAny(s.Filename, "/\\") || s.Filename == "." || s.Filename == ".." {
		return Invalid("filename", "Supply a filename without a directory path.")
	}
	if len(s.Content) > MaxBytes {
		return Invalid("file", "Documents may be up to 20 MiB per version.")
	}
	s.MediaType = http.DetectContentType(s.Content)
	if strings.HasPrefix(s.MediaType, "text/plain") && utf8.Valid(s.Content) {
		s.MediaType = "text/plain; charset=utf-8"
		if strings.EqualFold(filepath.Ext(s.Filename), ".md") {
			s.MediaType = "text/markdown; charset=utf-8"
		}
	}
	hash := sha256.Sum256(s.Content)
	s.SHA256 = hex.EncodeToString(hash[:])
	return nil
}
func Invalid(field, message string) error {
	return &accounts.FieldError{Field: field, Message: message}
}
