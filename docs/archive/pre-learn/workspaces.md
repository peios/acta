# Workspaces

ACT-55 adds the workspace foundation: identity, navigation, membership, management
permissions and bounded agent access. A workspace contains [tasks](tasks.md) plus Details and Members settings. Projects, archival and
workspace deletion are not part of this slice.

## Identity and navigation

Each workspace has a stable UUID, a name of 1–100 grapheme clusters, a unique
case-insensitive slug of 1–63 ASCII characters, and an optional description of
up to 1,000 Unicode code points. Slugs contain lowercase letters, digits and
internal hyphens. Names need not be unique. Renaming a slug reserves the old slug
for the same UUID; old browser links redirect to the current slug only after
checking access. Unavailable workspaces and aliases return not found.

The Workspaces scope sits above Site Settings. Within that scope, the top of the
sidebar becomes a searchable workspace switcher, including a Create workspace
button when permitted. Sign-in and the signed-in root open the most recently
visited accessible workspace. Visits are stored per account on the server. With
no accessible workspaces, the page offers creation or asks the user to contact
their administrator. The switcher supports search and 50-item pagination.

Details edits name, slug and description. Previous slugs appear in an expandable
section. Forms protect unsaved edits and reject stale revisions. The sidebar and
page header remain fixed while the main content scrolls, with an icon rail on
collapse and a navigation drawer on narrow screens.

## Human access

Workspace creation is human-only and requires the site capability
`site.workspaces.create` (Create workspaces). Superuser includes it. Creation
atomically adds the creator as a member with all workspace permissions, including task capabilities.
Agents cannot receive this site capability or create workspaces.

A human can access a workspace only through explicit membership or site
Superuser. Site groups do not confer membership. Membership alone permits basic
workspace access; management capabilities are separate:

| Permission | Identifier | Allows |
| --- | --- | --- |
| Edit workspace | `workspace.edit` | Edit name, slug and description. |
| Manage members | `workspace.members.manage` | List members, search candidates, add and remove human members. |
| Manage workspace permissions | `workspace.permissions.manage` | List members and site groups; edit their workspace grants. |

Effective workspace permissions are the union of direct membership grants and
grants assigned to the member's site groups. Superuser has every workspace
capability without requiring membership. There are no deny rules. A shared
permission editor shows inherited sources and preserves direct/inherited overlap.
Every added or removed grant must be held by the actor; unchanged grants outside
the actor's authority can remain.

Adding a member may activate greater authority through existing group grants.
This is intentional: the grant belongs to the group, not a snapshot of its
current workspace members. Assigning site groups remains under site permission
management and cannot be done from workspace settings. Removing a member requires
holding every workspace capability that member currently has. Removing explicit
membership does not remove Superuser access.

## Agent access

Agents have no independent workspace membership. Their owner manages access in
User Settings → Agents → an agent's Workspaces section. Defaults apply to both
existing and newly created agents:

- All workspaces: every workspace the owner can access, including future ones.
- Selected workspaces: only selected UUIDs, always bounded by owner access.
- Inherit from User: enabled separately for each workspace by default; effective
  permissions follow the owner's current workspace permissions.

Unticking Inherit from User reveals the full workspace permission set, initially
copied from the owner's current grants. Saving creates an explicit set that
replaces inheritance, including when the set is empty. Restoring inheritance
clears that explicit set. Access selection and permission edits are staged until
saved; save access changes before opening a workspace permission editor.

Explicit agent grants are always capped by the owner and are permanently pruned
when the owner loses corresponding grants or workspace access. Site Superuser
changes, site group membership changes and workspace membership/grant changes
participate in this pruning. Restoring owner access does not restore pruned
explicit grants. Inherited permissions naturally follow restoration. Selection
and inheritance choices remain intact when the owner temporarily loses access.

These defaults apply only to workspace permissions. Agent **site** permissions
remain explicitly delegated as described in [Agents](agents.md). Owner/agent
status, session expiry and required MFA still apply to every workspace request.
[Tasks](tasks.md) adds workspace discovery and task operations to CLI and MCP.
Existing MCP connections keep their previously approved tool grants.

## API

All endpoints use existing session, MFA and Origin protections. List responses
include `more`; paginated lists accept `q` and `offset` with a page size of 50.

| Endpoint | Purpose |
| --- | --- |
| `GET /api/workspaces` | Accessible workspaces ordered by most recent visit, then name. |
| `POST /api/workspaces` | Create from `name`, `slug`, `description`. |
| `GET /api/workspaces/permissions` | Workspace capability catalogue. |
| `GET /api/workspace-slugs/{slug}` | Resolve current or reserved slug after access checking. |
| `GET /api/workspaces/{uuid}` | Workspace details and effective permissions. |
| `POST /api/workspaces/{uuid}/profile` | Save details with expected `version`. |
| `POST /api/workspaces/{uuid}/visit` | Record a visit after access checking. |
| `GET /api/workspaces/{uuid}/members` | Members; `candidates=true` lists human nonmembers. |
| `POST /api/workspaces/{uuid}/members/{account}` | Set `member` with expected workspace `version`. |
| `GET /api/workspaces/{uuid}/groups` | Site groups and their grants in this workspace. |
| `POST /api/workspaces/{uuid}/members/{account}/permissions` | Replace direct grants using `permissions` and workspace `version`. |
| `POST /api/workspaces/{uuid}/groups/{group}/permissions` | Replace group grants using `permissions` and workspace `version`. |
| `GET /api/agents/{uuid}/workspaces` | Owner's available workspaces and this agent's access policy. |
| `POST /api/agents/{uuid}/workspaces` | Save `all`, `selected` UUIDs and expected agent-access `version`. |
| `POST /api/agents/{uuid}/workspaces/{workspace}/permissions` | Save `inherit`, `permissions` and agent-access `version`. |

A workspace revision covers profile, membership and local grants. Agent access
has its own revision covering selection and all per-workspace policies. Pruning
advances affected agent revisions. Stale writes return 409 `permissions_changed`.
Visit updates do not change the workspace revision. The browser refreshes access
on focus and relevant saved edits; it is not a push subscription.

## Storage guarantees

`internal/workspaces` owns pure validation and access composition. Auth services
own authorization and orchestration through `WorkspaceStore` / `WorkspaceTx`;
PostgreSQL owns persistence. Feature checks consume resolved permissions rather
than reading direct grants independently.

Migration 011 stores workspaces, durable slug reservations, human memberships,
site-group grants, account visits, agent access policies and workspace events.
Database triggers enforce human membership and agent-only policies. Writes
serialize with existing site administration using the installation lock and
revalidate the actor, session, owner policy and MFA inside the transaction.
Permission edits, revision increments, explicit-agent pruning and workspace
events commit together. Reads use a repeatable-read snapshot, including session
and access checks, without acquiring write locks. Another database adapter must
preserve these atomicity and consistency guarantees.

## Validation

Domain tests cover names, slugs, grants, membership, Superuser and agent resolution.
PostgreSQL integration tests cover private visibility and aliases, human-only
creation, creator grants, recent visits, stale/concurrent edits, additive group
authority, delegation ceilings, member removal, future workspace selection,
inheritance and permanent pruning after both workspace and site group changes.
HTTP tests exercise routes and Origin enforcement. The complete existing account,
CLI and MCP regression suite passed with the race detector after migration 011.

Browser review on localhost:8081 exercised creation, renaming and old-link
redirects, member/group editors, automatic agent inheritance and saving an empty
explicit set. Desktop, collapsed navigation and the mobile drawer were reviewed.
A local Workspace review workspace remains available for further design review.

## Returning between scopes

The Workspaces and My Agents scope buttons return to the last visited location
in their respective sections, including the workspace task query and URL fragment.
Locations are remembered per account in the current browser and survive reloads.
A section without a saved location opens its normal index. Navigation still
remembers locations for the current session when browser storage is unavailable.
Site Settings and User Settings retain their existing entry destinations.
