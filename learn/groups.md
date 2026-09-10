# Groups

Implemented in ACT-51. Groups add shared, additive permission grants and MFA
requirements to the existing permission model. There are no nested groups,
negative permissions or group deletion in this slice.

## Management

Site Settings → Groups lists groups with their member counts and optional
descriptions. Search and pagination use 50 entries per page. Each group has a
stable UUID, a case-insensitively unique name of 1–100 characters, an optional
500-character description, a permission set and a Require MFA setting.

Group pages provide profile editing, the same Manage permissions modal used for
users, and a searchable member list. Members can be removed there. Add membership
from a user's Groups section using Add to group. That section also supports
removal. There is no role ordering, and a user may belong to multiple groups.

All group management requires Manage permissions. Users with View users may see
a user's membership names but cannot change membership without Manage permissions.
Access managers can view group members without View users; links to complete user
pages and the Users navigation still require the corresponding permission.
Groups keep Site Settings accessible to access managers who cannot view users.

Permission tabs remain Own Account and Site Administration. On a user, inherited
switches appear enabled and disabled for editing, naming every source group.
If a permission is also direct, Remove direct grant removes that explicit copy
while inherited access stays enabled. Require MFA behaves the same way, with a
separate direct requirement and named group sources. Group permission changes
are staged until Save changes; the modal shows the current member count.

## Workspace grants

Existing site groups can also receive [workspace permissions](workspaces.md)
from that workspace’s permission manager. These grants apply only to explicit
human members of the workspace. They do not admit group members automatically.
Site group assignment remains a site-level privilege; the workspace UI does not
change group membership.

## Default

A built-in Default group starts with Change own username and Change own display
name. Its profile, grants and MFA requirement are editable; its automatic role
is identified by a stable database flag, independent of its editable name.

Newly created accounts join Default in the same transaction as account creation,
without receiving duplicate direct name grants. This includes the first account
on a fresh installation, which also receives direct Superuser. Individual users
can be removed from Default. Migration 007 does not enrol existing accounts or
remove any existing direct grants. Existing accounts can be added explicitly.

Creating a pending account automatically assigns Default and issues an invitation.
The creator must possess every capability Default grants, as well as Create users.
This prevents an elevated Default group becoming an indirect privilege-escalation
route. The UI hides creation when this condition is not met and explains why.
Fresh installation setup has separate operator proof and creates the Superuser.

## Authorization and consistency

Effective capabilities are the union of direct and group grants, expanded by
Superuser at the existing `accounts.ResolvePermissions` / `CheckPermission`
choke point. Individual feature checks do not need to distinguish grant sources.
Require MFA is the OR of the direct requirement and all group requirements;
Superuser does not bypass it. Removing a source leaves other sources intact.

Direct and group permission edits share `GrantChangesAllowed`: every added or
removed capability must be possessed by the actor. Adding or removing membership
requires authority over every capability carried by that group, even when another
source already grants one of them. Only a Superuser can assign a Superuser group
or change its Superuser grant. Group names/descriptions need only Manage permissions.

Changes to group grants/requirements refresh all affected members atomically.
Membership changes refresh the affected account. These operations increment each
affected account's permissions revision, invalidate its outstanding invitation or
recovery link, and refresh the non-secret MFA-enrolment projection from the
existing encrypted security record. They do not revoke sessions or change
credentials. Existing sessions resolve effective access on their next request.

An unenrolled member newly subject to Require MFA enters the existing restricted
setup flow. Enrolled members retain their authenticator, recovery codes, passkeys
and extra-code preference. Removing the requirement does not disable existing MFA.
Credential recovery protection uses effective permissions, so accounts made
privileged through groups can only receive credential links from a Superuser.

At least one active, non-pending account must retain effective Superuser access.
The invariant covers direct-grant removal, group-grant edits, membership removal
and disabling. Multiple sources on one account count as one Superuser account.
Administration shares the installation lock, then locks its actor, group/target
and affected account records before committing. All member refreshes, link
invalidation and events share one transaction. Concurrent access changes cannot
each mistake the other for a remaining Superuser.

Group profile and permission forms use the group revision for optimistic
concurrency. Membership changes supply both the current group revision and the
target account's permissions revision. A stale edit is rejected with
`permissions_changed` rather than applied against different grants. Group renames
change that group revision but do not invalidate credentials or member links.

## API

All group endpoints require Manage permissions. JSON mutations retain the existing
Origin validation, no-store responses and session/MFA checks.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/groups?q=&offset=` | List groups with member counts and whether the actor can assign each group. |
| `POST /api/groups` | Create from `name` and optional `description`, with no grants or members. |
| `GET /api/groups/{uuid}` | Group profile, default flag, grants, requirement, revision and count. |
| `POST /api/groups/{uuid}/profile` | Save `name`, `description`, expected `permissions_version`. |
| `POST /api/groups/{uuid}/permissions` | Save `permissions`, `require_mfa`, expected `permissions_version`. |
| `GET /api/groups/{uuid}/members?q=&offset=` | Paginated member account representations. |
| `POST /api/groups/{uuid}/members/{account}` | Set `member`, with expected `group_version` and account `permissions_version`. |

Account representations add `groups`, `direct_require_mfa` and `can_create_users`.
`require_mfa` remains the effective requirement. Each membership includes its group
ID, name, revision, direct permission set, direct requirement and default flag for
provenance. Capabilities in `permissions` remain fully resolved by the server.

Migration 007 creates group, membership and group-event tables and the shared
Superuser-source view, changes future direct-grant defaults to empty, and creates
Default without enrolling existing accounts. The view exists for the transactional
last-Superuser invariant; it is not a second feature permission engine. Group
events record group, actor, optional member UUID, operation and timestamp. Account
audit events record group access updates. No credential material is logged.

## Validation

The PostgreSQL race suite covers automatic Default assignment and removal,
multiple inherited sources, direct/inherited overlap, delegation and elevated
Default creation, inherited MFA on enrolled/unenrolled accounts, pending security
flows, credential-link invalidation, group Superuser protection, concurrent
membership removals, stale revisions, unique names and HTTP authorization.
The earlier direct-permission tests explicitly remove Default membership when
testing isolated direct grants. Existing profile fixtures now grant their own
name-edit permissions explicitly instead of relying on a database default.

Browser validation on localhost:8081 covered creating and editing a group,
automatic Default membership for a new pending account, adding membership,
named inherited grants and MFA requirements, removing a duplicate direct grant
without losing inherited access, and removing membership from the group page.
The group detail layout was checked at desktop and 390px width without horizontal
overflow. Disposable review accounts and groups were removed after verification.
`make check build` and the full PostgreSQL integration suite with the race detector
passed for this slice.
