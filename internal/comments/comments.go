// Package comments owns comment input and validation, independent of storage.
package comments

import (
	"acta/internal/accounts"
	"github.com/google/uuid"
	"strings"
	"unicode/utf8"
)

type Create struct {
	Body      string `json:"body"`
	ReplyTo   string `json:"reply_to,omitempty"`
	RequestID string `json:"request_id"`
}
type Update struct {
	Body    string `json:"body"`
	Version int64  `json:"version"`
	Delete  bool   `json:"delete,omitempty"`
}

func Body(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || len(s) > 100000 {
		return "", &accounts.FieldError{Field: "body", Message: "Write a comment of 1–100,000 bytes."}
	}
	return s, nil
}
func ValidateCreate(in Create) error {
	if _, e := uuid.Parse(in.RequestID); e != nil {
		return &accounts.FieldError{Field: "request_id", Message: "Supply a UUID for this post; reuse it only when retrying the same post."}
	}
	if in.ReplyTo != "" {
		if _, e := uuid.Parse(in.ReplyTo); e != nil {
			return &accounts.FieldError{Field: "reply_to", Message: "Use a comment UUID from this task."}
		}
	}
	return nil
}
