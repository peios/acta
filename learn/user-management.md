# User management

Implemented in ACT-51. Site Settings → Users (`/site-settings/users`) is governed
by the direct permissions described in [Permissions](permissions.md). Custom
groups and Default membership are described in [Groups](groups.md). Hidden navigation is not the
authorization boundary.

## Accounts and administration

The searchable list supports Active, Pending and Disabled filters and 50-account
pages. Search matches display names and usernames literally and without regard
to case. Each account opens a management page immediately, including pending
accounts. That stable UUID-based page includes permission management before the owner
redeems an invitation.

Creation requires a username and permits an optional display name. It atomically
creates an explicit pending root account, reserves the name, and creates its
invitation. No password credential or application session exists yet. The new
account page opens with the invitation-link modal. Closing that modal does not
delete the account. A new invitation can be generated from its management page. Saving permissions
invalidates any previously issued invitation or recovery link; generate a new
link after configuring access. Creation without View users shows the invitation
in place instead of navigating to restricted account details.

Administrator profile edits reuse the Profile form, validation, rename
confirmation, optimistic profile version and username-reservation rules.
Disabled and pending profiles remain editable. An administrator's name change
does not change identity, credential ownership or existing references. There
is no account deletion in this slice. Superuser is managed through permissions.

## Invitations and password recovery

Invitation and reset links use the same transport and redemption UI with
explicit purposes. Each is a random 256-bit bearer token, stored as a SHA-256
digest, valid for 24 hours and single use. Only one can be outstanding per
account: generating another replaces the previous link. An account security
change also invalidates a link issued under the old security revision.

Links use the configured public origin and `/account-link#token=…`. The fragment
keeps the secret out of server request URLs. The browser removes it from the
address bar and retains it in tab-scoped session storage for reloads until
successful redemption. The administrator's newly created link is handed between
pages in memory, never placed in route state or persistent browser storage.
Link APIs are JSON-only, Origin-checked and no-store. Do not share links publicly.

Invitation redemption shows the current username and allows choosing an optional
display name, initially filled with the administrator's value, alongside the new
password. An intervening admin profile edit produces a version conflict instead
of being overwritten silently. Activation, profile editing, password creation
and link consumption commit together. Success leads to ordinary sign-in, not an
automatically authenticated application session. Display-name editing is
conditional on Change own display name; protected assigned names are retained.

**Reset password** offers **Also disable MFA** and **Also remove passkeys**, both
off by default. Issuing a link changes no credentials or protection. Redemption
sets the new password, applies exactly the selected options, consumes the link,
and revokes all existing sessions atomically. A normal reset preserves MFA and
passkeys; the next sign-in follows the resulting account policy. Recovery links
replace proof of the old password for this operation only; they do not grant
normal application access. Only Superusers may issue credential links for access
managers or Superusers. Clearing MFA credentials preserves any Require MFA
requirement and forces enrolment on the next sign-in. Resetting one's own account also revokes that session.

## Disabling and re-enabling

Disabling immediately prevents application access, revokes sessions and
outstanding links, and invalidates in-flight authentication authorizations. The
account and its work/history remain. The last active Superuser cannot be
disabled. Pending accounts retain their pending state beneath Disabled; enabling
one again still requires a newly issued invitation.

A browser presenting a revoked session from a now-disabled account receives
`account_disabled` and is taken to **Account disabled. Contact your administrator.**
A new password or passkey login receives that explanation only after valid
credential proof. Wrong credentials keep the ordinary generic error. Passkey
verification still checks origin, challenge, signature and required user
verification before revealing disabled status. No application session is issued.

Disabled-session notices retain only token digests, account IDs and the former
absolute expiry, with no authorization value. Re-enabling never restores these
sessions or revoked links; the owner must sign in again. A new disable operation
cannot turn an older revoked token back into a session.

## API and storage contracts

| Endpoint | Behavior |
| --- | --- |
| `GET /api/users?q=&status=&offset=` | Administrator list, up to 50 entries and a `more` indicator. |
| `POST /api/users` | Create a pending account from username and display name; return account and invitation URL. |
| `GET /api/users/{uuid}` | Administrator account detail, including status and profile version. |
| `POST /api/users/{uuid}/profile` | Version-checked username/display-name edit. |
| `POST /api/users/{uuid}/link` | Replace outstanding invitation/reset link; accepts `disable_mfa` and `remove_passkeys` for active-account recovery. |
| `POST /api/users/{uuid}/disabled` | Disable/re-enable using the `disabled` boolean. |
| `POST /api/account-link/open` | Inspect the bearer grant and account form metadata. |
| `POST /api/account-link/redeem` | Submit token, new password, and invitation display name/profile version. |

Management transactions recheck the active actor, permission, MFA requirement and session, serialize
administration through the installation row, then lock the target account before
its flows, links or sessions. This coordinates last-Superuser checks and changes to
other users. Link redemption uses the same account lock and encrypted security
record as normal authentication. Password hashing occurs before the final lock;
the link, expiry, status, purpose and security revision are rechecked afterwards.
Consequently, a competing reset or disable cannot leave a surviving old login.

Admin events record both target UUID and acting administrator UUID. Secrets,
passwords and link plaintext are not included. A future storage adapter must
preserve these transactional and authorization guarantees, not merely reproduce
the tables. Migration 005 adds pending state, account links, disabled-session
notices and event actors without changing existing credentials or accounts.

## Validation

The PostgreSQL race suite covers pending activation, owner-chosen display names,
admin rename reservations, authorization failures, link replacement/expiry and
concurrent redemption, reset scope, MFA/passkey preservation and clearing,
disabled-session explanations, re-enable without resurrection, last-Superuser
protection, verified disabled-passkey behavior, and reset racing with login.
HTTP tests cover management access denial, invitation redemption, no-store
responses and disabled-account error signaling.

Browser review on localhost:8081 uses a temporary account for creation,
invitation redemption, profile editing/rename, recovery, disabling and the actual disabled-password-login page.
Temporary review data is removed afterwards; the operator's account credentials
are not changed. Real device passkey and MFA combinations were previously
confirmed by Jack during the Security slice.
