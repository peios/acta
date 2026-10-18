# Accounts

This checkpoint provides first-run setup, password sign-in, a current-account
endpoint, profile editing, password changes, passkeys, authenticator MFA,
recovery codes, session management, and sign-out. Setup and login have the initial production UI
and interaction treatment. The original plain signed-in test page is now the
[application chrome mockup](chrome.md).
It does not complete the broader accounts and permissions work in ACT-51.

## First-run setup

A new installation prints a random 8-character setup code to its server terminal.
It uses uppercase letters and digits, excluding `I`, `O`, `0` and `1` to make
manual entry easier. Input is case-insensitive and surrounding whitespace is
ignored. Five cryptographically random bytes provide 40 bits of entropy. Open
`/setup`, enter that code, then choose the first administrator's username, optional display name and
password. The code is valid for one hour after that server process starts.
Restarting an uninitialised server produces a new code. Treat terminal logs as
operator-only data. The code is never returned by an HTTP endpoint.

A successful code exchange creates a browser setup grant valid for 15 minutes.
Reloading within that period retains the administrator form. If it expires,
enter the operator code again. Setup creates the administrator and credential,
consumes the grant, invalidates all other grants, and permanently marks the
installation as set up in one transaction. Concurrent submissions have exactly
one winner. The permanent marker is independent of whether an administrator is
subsequently disabled or demoted.

After creation, Acta takes the user to sign-in with a success message. It does
not sign them in automatically. Creating the real administrator is an operator
step; test fixtures never initialise the development installation.

## Identity

Account UUIDs are permanent references. Usernames are human-readable names,
not identifiers for relationships or future mentions.

The account domain owns segment validation and handle derivation:

- A segment is 1–32 ASCII letters/numbers with `.`, `-`, `_` permitted internally.
  First and last characters must be alphanumeric. Case folds to lowercase.
  Whitespace and slashes are not accepted as segment input.
- Root names are unique across the installation. The schema can represent a
  direct child through a parent UUID and a segment unique under that parent.
  Its handle is derived as `parent/child`; parent display text does not establish
  ownership. [User-owned agents](agents.md) expose creation and delegated CLI/MCP
  authorization through the human owner.
- A display name is optional and nonunique, Unicode NFC, trimmed at the ends,
  single-line, and at most 100 grapheme clusters. Blank means absent. Administrator setup accepts this optional name and persists it in the same
  transaction as the account. Omitting it or entering whitespace leaves it absent.
  These same rules apply to Profile edits.

## Profile

User Settings opens **Profile** at `/user-settings/profile`. It contains optional
Display name and Username fields directly in the main panel. Save changes is
available only when edited. A successful save updates the sidebar immediately,
resets the form to the normalized values, and announces a quiet success message.
Field errors are associated with their inputs and receive focus. Unsaved edits
are guarded when navigating away.

A username change opens a confirmation naming the old and proposed usernames.
Cancel receives initial focus; Escape cancels and returns focus to Save. A
successful rename retains the UUID and existing sessions, and child handles
follow the parent name. Sign-in uses the new current username. Historical names
are reserved for their owner, who may switch back to them. Reservations do not
act as login aliases. Another account cannot claim a current or reserved name,
either through creation or rename. Names are keyed by parent UUID and segment,
so a parent's rename does not change its children's namespace ownership.

A collapsed **Previous usernames · N** disclosure appears below the username
help text when this account has reserved names. Expanding it shows a plain,
alphabetically ordered list. **Use again** fills and focuses the username field;
it does not save. The normal Save and rename confirmation still apply. The
current username is excluded, and the list updates after a successful save.

`GET /api/account` returns the current account, `profile_version`, and
`previous_usernames` (an array, empty when no previous names are reserved). The
history is private to the authenticated account, not a public name directory.
`POST /api/account/profile` accepts `username`, `display_name` (string or null),
and the expected `profile_version`. The authenticated session supplies the UUID;
the caller cannot choose an account or edit permissions. Each changed name field
requires its corresponding Own Account permission. Display name is replaced in
full (null/omitted/blank clears it). Username is required. The response is the
updated current-account representation. All existing Origin, JSON-only and
no-store protections apply. A stale version receives HTTP 409 `profile_changed`
and makes no changes; the UI lets the user explicitly load the latest profile
before editing again. Validation failures receive 422 with field messages.

Account changes, version increment, name claims, and the profile event commit
atomically. PostgreSQL uses a shared name registry for current and historical
names, with a unique constraint and a claim trigger covering both inserts and
renames. Failed claims roll back the entire edit. Migration 002 backfills names
for existing accounts and preserves their credentials and sessions. A future
storage adapter must preserve these guarantees under concurrency, including
active-account checks, owner reclamation, and stale-edit rejection.

Administrator release of reservations remains agreed future work. Structured UUID mentions and alias resolution are not yet
implemented; future references must bind to UUIDs, never mutable usernames.

## Passwords and browser sessions

Passwords have 15–128 Unicode code points after NFC normalization. There are no
composition requirements or periodic expiry. Spaces are preserved. A bundled
common-password list and repeated-character check reject obvious choices;
no password is transmitted to a breach-checking service. The list is a modest
baseline, not exhaustive. Its provenance and license are in
`internal/auth/data/README.md`.

Argon2id hashes use random 16-byte salts, 19 MiB memory, two iterations, one lane,
and a 32-byte key. Stored hashes carry their parameters and the verifier bounds
those parameters before allocating memory. At most two password hashes run
concurrently per server; excess work receives a retryable busy response.
This profile follows the [OWASP password storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).

Successful sign-in sets a persistent, opaque browser cookie. Only its SHA-256
digest is stored in PostgreSQL. Sessions expire after seven days without use or
30 days from creation, whichever comes first. Activity refreshes only the idle
deadline. Restarting the application does not invalidate sessions. Sign-out
revokes the server-side record; copying an old cookie cannot restore it.
Signing in again in the same browser also revokes that browser's prior session.
Disabled accounts cannot authenticate or use a session.

Cookies are HttpOnly, SameSite=Strict, and path `/`. HTTPS installations also
use Secure and `__Host-` cookie names without a Domain attribute. HTTP is
supported only on explicitly configured loopback origins for development.
All API responses have `Cache-Control: no-store`. Mutations require JSON and
an exact allowed Origin; login and setup receive the same CSRF protection.

Rate limits are stored in PostgreSQL and shared by server instances: 60 login
attempts per remote IP and 10 per normalized username per five-minute fixed
window; setup-code checks allow 10 per remote IP per five minutes. Successful
attempts count too. Invalid and nonexistent usernames receive the same generic
credential error. Unknown-account checks still perform a password hash.

Client IPs default to the direct TCP peer. Deployments can set
`ACTA_TRUSTED_PROXIES` to explicit comma-separated proxy IPs/CIDRs. Only requests
from those peers use `X-Forwarded-For`: walk right to left through trusted hops,
stopping at the first untrusted address. Invalid, absent or oversized chains fall
back to the direct peer. Other forwarding headers do not select client identity,
public URLs, cookie security or allowed origins. See [deployment](deployment.md).

Expired grants, security flows, rate-limit buckets and sessions are removed at startup and
every 15 minutes. Expiry is enforced on each operation regardless of cleanup.
Administrator creation, successful login, profile changes and session revocation append account
events with UUIDs; this is a small internal event record, not a finished audit UI.

## Boundaries and persistence guarantees

- `internal/accounts`: identity, naming policy, profile service and its storage contract.
- `internal/auth`: authentication policy, password handling, session lifetime,
  setup workflow, and behavioral storage contracts.
- `internal/postgres`: the SQL implementation, pool and embedded migrations.
- `internal/httpapi`: JSON, origin enforcement, cookies and error translation.
- `web/src/lib`: shared form components and typed HTTP client; route components
  own screen behavior. The Go server serves the compiled frontend.

Storage operations describe guarantees rather than generic CRUD. A replacement
backend must preserve atomic setup, active-account checks, session expiry and
revocation, and concurrent rate limits. Domain and HTTP code do not import pgx.
The real PostgreSQL integration suite tests these guarantees in isolated schemas.
Adding another adapter would also require translating migrations and moving
existing data; a storage interface cannot make data migration automatic.

Migrations are embedded, ordered, checksummed, and applied in a transaction
under a database advisory lock. A changed applied migration or an unknown
migration fails startup. During development we may deliberately replace an
obsolete schema, but doing so still requires an explicit, reviewed data reset;
normal startup never drops or recreates it.

## Next account work

The [Security design](security-design.md) and [implementation notes](security.md)
cover the implemented password, passkey, MFA, recovery-code and session flows.
Invitations, administrator recovery, profile management and disabling are now
implemented in [User management](user-management.md). [Direct permissions](permissions.md)
replace the administrator flag; setup grants Superuser to the first account.
[Groups](groups.md) now provide shared grants and Default membership.
Workspace membership and agent credentials remain deferred.

## Appearance

A sun/moon toggle is available in the top-right corner on setup and login,
and beside the profile in the signed-in sidebar. It defaults to the operating system's color preference until
explicitly changed. A choice is remembered in this browser and synchronized
across its open Acta tabs; it is not an account preference yet. Without browser
storage, toggling still works for the current page.

An early, self-hosted theme script applies the preference before first paint,
avoiding a light-page flash during dark-mode reloads. Shared semantic color
values cover surfaces, fields, focus rings, errors and success messages. The
sun's rays rotate away as the disc becomes a crescent, with a small hover/press
movement on the toggle. Reduced-motion preferences disable these animations.
