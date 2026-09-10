// Package accounts owns account identity and human-readable naming rules.
package accounts

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

var ErrUsername = errors.New("Use 1–32 letters or numbers, with dots, hyphens or underscores between them.")
var usernamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,30}[a-z0-9])?$`)

// Username is one namespace segment. Slash is never accepted as segment input.
func Username(raw string) (string, error) {
	// ASCII case folding only: do not turn Unicode lookalikes into valid names.
	for _, r := range raw {
		if r > unicode.MaxASCII {
			return "", ErrUsername
		}
	}
	name := strings.ToLower(raw)
	if !usernamePattern.MatchString(name) {
		return "", ErrUsername
	}
	return name, nil
}

func DisplayName(raw string) (*string, error) {
	if !utf8.ValidString(raw) {
		return nil, errors.New("Enter a valid display name.")
	}
	name := norm.NFC.String(strings.TrimSpace(raw))
	if name == "" {
		return nil, nil
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d') {
			return nil, errors.New("Use a single line without control characters.")
		}
	}
	if uniseg.GraphemeClusterCount(name) > 100 {
		return nil, errors.New("Use at most 100 characters for your display name.")
	}
	return &name, nil
}

// Account identifiers and parentage are independent of names. ParentUsername is
// resolved by storage, never accepted as evidence of parentage from a caller.
type Account struct {
	Owner               *Account // Current owner projection, populated by persistence for agents.
	Pending             bool
	DisabledAt          *time.Time
	CreatedAt           time.Time
	ID                  string
	Username            string
	ParentID            *string
	ParentUsername      string
	DisplayName         *string
	Groups              []Group
	DefaultGrants       []string
	DirectPermissions   []string
	RequireMFA          bool
	MFAEnrolled         bool
	PermissionsVersion  int64
	LastActiveSuperuser bool
	ProfileVersion      int64
	PreviousUsernames   []string
}

func (a Account) Handle() string {
	if a.ParentID != nil {
		return a.ParentUsername + "/" + a.Username
	}
	return a.Username
}

func (a Account) Status() string {
	if a.DisabledAt != nil {
		return "disabled"
	}
	if a.Pending {
		return "pending"
	}
	return "active"
}

func (a Account) IsAgent() bool { return a.ParentID != nil }
func (a Account) Available() bool {
	return a.Status() == "active" && (!a.IsAgent() || (a.Owner != nil && !a.Owner.IsAgent() && a.Owner.Status() == "active"))
}
