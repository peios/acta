# Permissions

Implemented in ACT-51. This slice replaces the bootstrap administrator flag with
named, direct account grants. Custom groups, membership and Default assignment are now implemented in
[Groups](groups.md). There are no negative grants or deny rules.

## Catalogue and defaults

| Tab | Permission | Identifier |
| --- | --- | --- |
| Own Account | Change own username | `own.username` |
| Own Account | Change own display name | `own.display_name` |
| Site Administration | Create workspaces | `site.workspaces.create` |
| Site Administration | Superuser | `site.superuser` |
| Site Administration | View users | `site.users.view` |
| Site Administration | Create users | `site.users.create` |
| Site Administration | Edit users | `site.users.edit` |
| Site Administration | Reset user credentials | `site.users.reset_credentials` |
| Site Administration | Disable users | `site.users.disable` |
| Site Administration | Manage permissions | `site.permissions.manage` |

Require MFA appears in Own Account alongside these switches, but is a separate
account requirement, not a capability. Direct and group requirements combine with OR. Superuser grants every known capability,
including future additions, without enabling or bypassing requirements.

The first setup account receives a direct Superuser grant. Migration 006 maps
existing administrators to Superuser and removes `is_admin`. Ordinary accounts existing at the direct-permissions migration retain their two
name-changing direct grants. New accounts now join Default instead; see [Groups](groups.md).

Password changes, passkey management, session inspection/revocation and optional
MFA management remain baseline authenticated-account capabilities. Profile edits
check each changed field, so an unchanged protected name can accompany another
permitted change. Editing yourself through user management also applies these
own-account restrictions. Invitation activation only shows and accepts a chosen
display name when Change own display name is effective; otherwise the assigned
name is preserved.

## Management and visibility

Each user detail page has a Manage permissions button for access managers. Its
modal uses Own Account and Site Administration tabs, with Superuser first in
Site Administration. There are no subcategories yet. Changes are staged until
Save changes; Cancel discards them. A permissions revision detects stale edits
and requires reopening the dialog to review the latest values.

Capabilities supplied by Superuser appear enabled and greyed out with their
source. If also granted directly, Remove direct grant removes that explicit
grant while showing that effective access remains enabled. Superuser never
checks Require MFA automatically.

Only access managers may change grants. For every added or removed grant, the
actor must possess that capability. Existing unowned grants may remain unchanged.
Only Superusers may add or remove Superuser. An access manager may change the MFA
requirement. An active installation must retain an active, non-pending Superuser:
concurrent demotions and disables cannot remove the last one.

View users controls list/detail access; the action grants do not implicitly
include it. Create users can still open the Users page and create accounts when
View users is absent, receiving the invitation in place. Other administration
controls appear on user detail pages, so their normal UI use also needs View
users. Site Settings is available when Users or Groups is accessible. The home route now opens the most recent accessible [workspace](workspaces.md).

Unavailable actions are hidden. Protected profile fields become readable text.
The last Superuser's disable and Superuser-removal controls are disabled with an
explanation. Restricted direct URLs and rejected requests show an access-denied
message. Account access is refreshed on tab focus and after a permission denial
or saved permission edit; an idle page is not a push-synchronised permission
subscription. Every backend operation independently checks current access.

## Credential authority

Create users permits invitations for pending accounts. Reset user credentials
permits recovery for active accounts, including the existing optional MFA and
passkey clearing. Only a Superuser can issue either kind of credential link for
an account that has Manage permissions or Superuser. A link can otherwise be an
indirect way to take over access-management authority.

Saving permissions invalidates outstanding invitation/recovery links for the
target account. The modal explains that links need replacing. This prevents an
old link acquiring new authority after an account is promoted. Existing sessions
remain, but their capabilities are resolved afresh; no grants live in cookies.

## Required MFA

Requiring MFA on an unenrolled account restricts both existing and new sessions.
The browser opens `/mfa-required` using the shared authenticator flow. Only
identity inspection, that setup flow and sign-out are available until enrolment
and recovery-code acknowledgement complete. Starting or advancing unrelated
security actions is blocked, including a flow opened before the requirement.

Password sign-in requires an authenticator or recovery code once enrolled.
A user-verified passkey satisfies sign-in MFA by default; the user's existing
extra-code preference may also require a code after a passkey. Required accounts
cannot disable their authenticator, including by finishing an older disable
flow. Replacing it remains available. Superusers obey these rules too.

Administrator-authorised recovery may clear MFA credentials but does not clear
the requirement. The user must enrol again after signing in. Sole-Superuser total
credential-loss recovery remains deferred; this slice introduces no bypass.

## API and persistence contract

- `GET /api/permissions`: central capability catalogue, requiring Manage permissions.
- `POST /api/users/{uuid}/permissions`: complete direct `permissions` array,
  `require_mfa` boolean and expected `permissions_version`; returns updated account.
- Account representations include effective `permissions`, `direct_permissions`,
  `require_mfa`, `direct_require_mfa`, `groups`, `can_create_users`,
  `mfa_setup_required`, `permissions_version` and
  `last_active_superuser`. They no longer contain `is_admin`.
- HTTP 403 `forbidden` denies capabilities; 403 `mfa_required` requests restricted
  enrolment; 409 `permissions_changed` rejects stale permission edits.

`accounts.ResolvePermissions` composes effective access and `CheckPermission` is
the capability choke point. Features never inspect direct grants for permission
checks. Group grants extend resolution here; UI checks consume the resulting
capability list, not their own Superuser expansion. The MFA requirement is
resolved independently from capabilities. [Agent accounts](agents.md) use this
same boundary to cap explicit grants by current owner permissions; owner access
changes transactionally prune delegations that are no longer held.

Administration locks installation, actor and target in that order and rechecks
actor status, session, MFA restriction and capability under the transaction.
Updates, revision increments, link invalidation and actor/target audit events
commit together. Self-profile and security operations recheck relevant rules
under the account lock. Last-Superuser invariants share the administration lock.
A future database adapter must preserve this ordering and atomicity.

`mfa_enrolled` is a non-secret projection of encrypted authenticator state,
maintained whenever that state is saved and refreshed before requiring MFA.
The encrypted security record remains authoritative. Existing accounts without
a requirement need no credential migration. The projection permits the common
session gate to enforce enrolment without decrypting security material per request.

## Validation

PostgreSQL integration tests cover direct-grant boundaries and changes during
existing sessions, protected own-profile fields, delegation limits, protected
credential recovery, pre-promotion link invalidation, invitation display-name
restrictions, stale permissions, concurrent Superuser demotions, restricted MFA
enrolment/acknowledgement, in-flight action denial, admin recovery retaining the
requirement, verified passkeys with/without an extra code, and HTTP enforcement.
Protocol authenticator fixtures verify signed server ceremonies, not hardware.

Browser validation on localhost:8081 covered a disposable account created through
the UI, saving direct permissions and Require MFA, generating a replacement
invitation, activation without display-name editing, password login into the
restricted MFA page, authenticator-code verification and recovery acknowledgement.
After enrolment, the account had no Site Settings button, no MFA-disable action,
and read-only profile names. A restricted direct URL showed access denied.
Desktop dark mode and mobile light-mode tabs were inspected; the long permission
list scrolls inside its modal while the tabs and actions remain visible. The
390px viewport had no horizontal overflow. Browser warning/error logs were empty.
The review account was removed and Jack's existing session restored, with no
changes to his credentials or MFA requirement.
