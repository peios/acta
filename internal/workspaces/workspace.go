// Package workspaces owns workspace identity and scoped authorization rules.
package workspaces

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"acta2/internal/accounts"
)

const (
	WriteMemories  = "memories.write"
	CommentTasks   = "tasks.comment"
	ManageComments = "tasks.comments.manage"
	CreateTasks    = "tasks.create"
	EditTasks      = "tasks.edit"
	ManageStatuses = "tasks.statuses.manage"
	Edit           = "workspace.edit"
	Members        = "workspace.members.manage"
	Permissions    = "workspace.permissions.manage"
)

func Catalogue() []accounts.Permission {
	return []accounts.Permission{
		{ID: WriteMemories, Category: "Memories", Label: "Write memories", Description: "Create, edit and delete workspace memories."},
		{ID: CommentTasks, Category: "Tasks", Label: "Comment", Description: "Post comments and replies, and edit or delete your own comments."},
		{ID: ManageComments, Category: "Tasks", Label: "Manage others’ comments", Description: "Edit or delete comments written by other accounts."},
		{ID: CreateTasks, Category: "Tasks", Label: "Create tasks", Description: "Create tasks and subtasks in this workspace."},
		{ID: EditTasks, Category: "Tasks", Label: "Edit tasks", Description: "Edit titles, descriptions, status, assignees, parentage and task documents."},
		{ID: ManageStatuses, Category: "Tasks", Label: "Manage statuses", Description: "Configure task statuses and the creation and completed selections."},
		{ID: Edit, Category: "Workspace", Label: "Edit workspace", Description: "Change the workspace name, slug and description."},
		{ID: Members, Category: "Workspace", Label: "Manage members", Description: "Add existing site accounts and remove members whose workspace permissions you hold."},
		{ID: Permissions, Category: "Workspace", Label: "Manage workspace permissions", Description: "Manage direct and site-group grants, limited to permissions you hold."},
	}
}
func All() []string {
	out := []string{}
	for _, p := range Catalogue() {
		out = append(out, p.ID)
	}
	return out
}
func Contains(grants []string, p string) bool { return slices.Contains(grants, p) }
func Subset(grants, ceiling []string) bool {
	for _, p := range grants {
		if !Contains(ceiling, p) {
			return false
		}
	}
	return true
}
func Intersect(grants, ceiling []string) []string {
	out := []string{}
	for _, p := range All() {
		if Contains(grants, p) && Contains(ceiling, p) {
			out = append(out, p)
		}
	}
	return out
}
func ValidateGrants(before, after, ceiling []string) error {
	seen := map[string]bool{}
	for _, p := range after {
		if seen[p] || !Contains(All(), p) {
			return &accounts.FieldError{Field: "permissions", Message: "Choose valid, distinct workspace permissions."}
		}
		seen[p] = true
	}
	for _, p := range All() {
		if Contains(before, p) != Contains(after, p) && !Contains(ceiling, p) {
			return accounts.ErrPermissionDenied
		}
	}
	return nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Profile(name, slug, description string) (string, string, string, error) {
	label, e := accounts.DisplayName(name)
	if e != nil || label == nil {
		return "", "", "", &accounts.FieldError{Field: "name", Message: "Use a workspace name of 1–100 characters."}
	}
	slug = strings.ToLower(slug)
	if !slugPattern.MatchString(slug) {
		return "", "", "", &accounts.FieldError{Field: "slug", Message: "Use 1–63 letters or numbers, with hyphens between them."}
	}
	description = strings.TrimSpace(description)
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > 1000 {
		return "", "", "", &accounts.FieldError{Field: "description", Message: "Use at most 1,000 characters."}
	}
	return *label, slug, description, nil
}

type Workspace struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Description   string    `json:"description"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	Permissions   []string  `json:"permissions"`
	PreviousSlugs []string  `json:"previous_slugs"`
}
type GroupGrant struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"direct_permissions"`
}
type Access struct {
	Allowed     bool         `json:"allowed"`
	Permissions []string     `json:"permissions"`
	Direct      []string     `json:"direct_permissions"`
	Groups      []GroupGrant `json:"groups"`
	Superuser   bool         `json:"superuser"`
}

func Resolve(a accounts.Account, member bool, direct []string, groups []GroupGrant) Access {
	out := Access{Allowed: member, Direct: direct, Groups: groups, Permissions: []string{}, Superuser: accounts.CheckPermission(a, accounts.Superuser)}
	if out.Superuser {
		out.Allowed = true
		out.Permissions = All()
		return out
	}
	if !member {
		return out
	}
	grants := slices.Clone(direct)
	for _, g := range groups {
		grants = append(grants, g.Permissions...)
	}
	out.Permissions = Intersect(grants, All())
	return out
}
func ResolveAgent(owner Access, all, selected, inherit bool, grants []string) Access {
	out := Access{Allowed: owner.Allowed && (all || selected), Direct: grants, Groups: []GroupGrant{}, Permissions: []string{}}
	if out.Allowed {
		if inherit {
			out.Permissions = slices.Clone(owner.Permissions)
		} else {
			out.Permissions = Intersect(grants, owner.Permissions)
		}
	}
	return out
}

type Member struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	DisplayName *string `json:"display_name"`
	Status      string  `json:"status"`
	Access
	CanRemove bool `json:"can_remove"`
}
type AgentPolicy struct {
	Workspace        Workspace `json:"workspace"`
	Selected         bool      `json:"selected"`
	Inherit          bool      `json:"inherit"`
	Grants           []string  `json:"permissions"`
	OwnerPermissions []string  `json:"owner_permissions"`
}
type AgentAccess struct {
	All        bool          `json:"all"`
	Version    int64         `json:"version"`
	Workspaces []AgentPolicy `json:"workspaces"`
}
