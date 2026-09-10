package accounts

import "slices"

const (
	ManageBackups     = "site.backups.manage"
	WriteSiteGuide    = "site.guide.write"
	ReadSiteMemories  = "site.memories.read"
	WriteSiteMemories = "site.memories.write"
	WriteUserMemories = "own.memories.write"
	CreateWorkspaces  = "site.workspaces.create"
	Superuser         = "site.superuser"
	ChangeUsername    = "own.username"
	ChangeDisplayName = "own.display_name"
	ViewUsers         = "site.users.view"
	CreateUsers       = "site.users.create"
	EditUsers         = "site.users.edit"
	ResetCredentials  = "site.users.reset_credentials"
	DisableUsers      = "site.users.disable"
	ManagePermissions = "site.permissions.manage"
)

type Permission struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func PermissionCatalogue() []Permission {
	return []Permission{
		{ManageBackups, "Site Administration", "Manage backups", "View recovery evidence, change backup policy within operator limits and request backups or restore drills. Does not permit production restore or access to backup secrets."},
		{WriteSiteGuide, "Site Administration", "Edit site guide preferences", "Edit essential policy included in the guide for everyone on this installation."},
		{ReadSiteMemories, "Memories", "Read site memories", "Read shared memories for this installation."},
		{WriteSiteMemories, "Memories", "Write site memories", "Create, edit and delete site memories."},
		{WriteUserMemories, "Memories", "Write user memories", "For agents, write memories belonging to the human owner."},
		{ChangeUsername, "Own Account", "Change own username", "Rename your account. Previous usernames remain reserved."},
		{ChangeDisplayName, "Own Account", "Change own display name", "Edit your display name, including during account activation."},
		{CreateWorkspaces, "Site Administration", "Create workspaces", "Create a workspace and manage its details, members and permissions."},
		{Superuser, "Site Administration", "Superuser", "Grant every capability, including capabilities added in the future. MFA requirements still apply."},
		{ViewUsers, "Site Administration", "View users", "Browse users and view their account details."},
		{CreateUsers, "Site Administration", "Create users", "Create pending accounts and generate invitations."},
		{EditUsers, "Site Administration", "Edit users", "Change other users’ usernames and display names."},
		{ResetCredentials, "Site Administration", "Reset user credentials", "Generate recovery links, with options to clear MFA and passkeys. Privileged accounts require a Superuser."},
		{DisableUsers, "Site Administration", "Disable users", "Disable and re-enable accounts. The last active Superuser is protected."},
		{ManagePermissions, "Site Administration", "Manage permissions", "Manage direct grants, groups and memberships. You can only change capabilities you possess; only Superusers can change Superuser."},
	}
}

// ResolvePermissions is the single composition point for effective access.
// Direct and group grants are additive. Requirements
// remain independent: Superuser expands capabilities, never account policy.
func ResolvePermissions(a Account) []string {
	if a.IsAgent() {
		out := []string{}
		if a.Owner == nil || a.Owner.IsAgent() {
			return out
		}
		for _, p := range PermissionCatalogue() {
			if p.ID != Superuser && p.ID != CreateWorkspaces && p.ID != WriteSiteGuide && p.ID != ManageBackups && slices.Contains(a.DirectPermissions, p.ID) && CheckPermission(*a.Owner, p.ID) {
				out = append(out, p.ID)
			}
		}
		return out
	}
	grants := slices.Clone(a.DirectPermissions)
	for _, g := range a.Groups {
		grants = append(grants, g.Permissions...)
	}
	out := []string{}
	for _, p := range PermissionCatalogue() {
		if slices.Contains(grants, Superuser) || slices.Contains(grants, p.ID) {
			out = append(out, p.ID)
		}
	}
	return out
}
func CheckPermission(a Account, permission string) bool {
	return slices.Contains(ResolvePermissions(a), permission)
}
func RequiresMFASetup(a Account) bool {
	if a.IsAgent() {
		return a.Owner != nil && RequiresMFASetup(*a.Owner)
	}
	return RequiresMFA(a) && !a.MFAEnrolled
}
func DefaultPermissions() []string { return []string{ChangeUsername, ChangeDisplayName} }
func Privileged(a Account) bool    { return CheckPermission(a, ManagePermissions) }

// CanChangeProfile applies identical own-account rules on both edit surfaces.
func CanChangeProfile(a Account, username string, display *string) bool {
	sameDisplay := (a.DisplayName == nil && display == nil) || (a.DisplayName != nil && display != nil && *a.DisplayName == *display)
	return (a.Username == username || CheckPermission(a, ChangeUsername)) && (sameDisplay || CheckPermission(a, ChangeDisplayName))
}

// Agent grants are explicit and restricted to capabilities currently held by
// the human owner. Never accept Superuser, inherited groups or nested delegation.
func AgentGrantsAllowed(owner Account, grants []string) error {
	if owner.IsAgent() {
		return ErrPermissionDenied
	}
	for _, g := range grants {
		if g == Superuser || g == CreateWorkspaces || g == WriteSiteGuide || g == ManageBackups || !CheckPermission(owner, g) {
			return ErrPermissionDenied
		}
	}
	return GrantChangesAllowed(owner, nil, grants)
}
func AgentCatalogue(owner Account) []Permission {
	out := []Permission{}
	for _, p := range PermissionCatalogue() {
		if p.ID != Superuser && p.ID != CreateWorkspaces && p.ID != WriteSiteGuide && p.ID != ManageBackups && CheckPermission(owner, p.ID) {
			out = append(out, p)
		}
	}
	return out
}
