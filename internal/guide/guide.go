// Package guide owns the scoped, always-needed policy appended to Acta's guide.
package guide

import (
	"acta/internal/accounts"
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxCharacters = 8000

var ErrConflict = errors.New("These preferences changed since you loaded them. Review the latest version before saving again.")

type Preference struct {
	Content  string `json:"content"`
	Revision int64  `json:"revision"`
	CanWrite bool   `json:"can_write"`
}
type Preferences struct {
	Site Preference `json:"site"`
	User Preference `json:"user"`
}
type Save struct {
	Scope    string `json:"scope"`
	Content  string `json:"content"`
	Revision int64  `json:"revision"`
}
type Document struct {
	Preferences
	BuiltIn  string `json:"built_in"`
	Markdown string `json:"markdown"`
}

func Validate(in Save) error {
	if in.Scope != "site" && in.Scope != "user" {
		return &accounts.FieldError{Field: "scope", Message: "Choose site or user preferences."}
	}
	if in.Revision < 0 {
		return &accounts.FieldError{Field: "revision", Message: "Use the revision of the preferences you read."}
	}
	if !utf8.ValidString(in.Content) || strings.ContainsRune(in.Content, 0) || utf8.RuneCountInString(in.Content) > MaxCharacters {
		return &accounts.FieldError{Field: "content", Message: "Use valid Markdown of at most 8,000 characters."}
	}
	return nil
}

// Composition is shared by the browser preview and MCP, so neither can drift.
func Compose(builtIn string, prefs Preferences) Document {
	text := builtIn
	if strings.TrimSpace(prefs.Site.Content) != "" || strings.TrimSpace(prefs.User.Content) != "" {
		text = strings.TrimRight(text, "\n") + "\n\n---\n\n# Guide preferences\n\nThese appendices contain essential, always-needed policy. Site preferences apply to everyone on this installation; user preferences apply to the human owner and their agents. User preferences take precedence over conflicting site defaults. Preferences do not grant permissions or expand tool access.\n"
		if strings.TrimSpace(prefs.Site.Content) != "" {
			text += "\n## Site preferences\n\n" + prefs.Site.Content + "\n"
		}
		if strings.TrimSpace(prefs.User.Content) != "" {
			text += "\n## User preferences\n\n" + prefs.User.Content + "\n"
		}
	}
	return Document{Preferences: prefs, BuiltIn: builtIn, Markdown: text}
}
