package accounts

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

type Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Default     bool     `json:"is_default"`
	Permissions []string `json:"direct_permissions"`
	RequireMFA  bool     `json:"direct_require_mfa"`
	Version     int64    `json:"permissions_version"`
	MemberCount int      `json:"member_count"`
}

func GroupProfile(name, description string) (string, string, error) {
	label, err := DisplayName(name)
	if err != nil || label == nil {
		return "", "", &FieldError{Field: "name", Message: "Use a group name of 1–100 characters on one line."}
	}
	description = strings.TrimSpace(description)
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 500 {
		return "", "", &FieldError{Field: "description", Message: "Use a description of at most 500 characters."}
	}
	return *label, description, nil
}

var ErrPermissionDenied = errors.New("permission denied")

// GrantChangesAllowed is shared by direct grants and group grants. Membership
// changes use the same rule against an empty set, so every carried capability
// must be delegable, even if another source currently supplies it too.
func GrantChangesAllowed(actor Account, before, after []string) error {
	seen := map[string]bool{}
	for _, p := range after {
		if seen[p] || !slices.ContainsFunc(PermissionCatalogue(), func(item Permission) bool { return item.ID == p }) {
			return &FieldError{Field: "permissions", Message: "Choose valid, distinct permissions."}
		}
		seen[p] = true
	}
	for _, p := range PermissionCatalogue() {
		if slices.Contains(before, p.ID) != slices.Contains(after, p.ID) && !CheckPermission(actor, p.ID) {
			return ErrPermissionDenied
		}
	}
	return nil
}
func CanAssignGroup(actor Account, group Group) bool {
	return GrantChangesAllowed(actor, nil, group.Permissions) == nil
}
func CanCreateUser(actor Account) bool {
	return CheckPermission(actor, CreateUsers) && GrantChangesAllowed(actor, nil, actor.DefaultGrants) == nil
}
func RequiresMFA(a Account) bool {
	if a.IsAgent() {
		return false
	}
	if a.RequireMFA {
		return true
	}
	for _, g := range a.Groups {
		if g.RequireMFA {
			return true
		}
	}
	return false
}
