# CLI profiles and authentication

`make build` produces
`bin/acta` (client) and `bin/acta-server` (server). The client uses Go, Cobra for
commands/help/completion and Huh for interactive forms. The server still serves
the compiled Svelte frontend, without a separate frontend server.

## Profiles

`default` is an ordinary, automatically available initial profile. An active
profile is used unless a command supplies `--profile NAME` or `-p NAME`.
An unknown profile is an error, never a fallback. Creating a profile does not
change the active selection.

```sh
acta login localhost:8081
acta profile add work --server https://acta.peios.org
acta -p work login
acta profile use work
acta profile list
acta whoami
acta -p default status --json
acta logout
```

`acta login [server]` uses the supplied URL first, then the selected profile's
saved URL, then an interactive prompt. URLs default to HTTPS; loopback addresses
may use HTTP. Paths, embedded credentials, queries and fragments are rejected.
Redirects are not followed, preventing credential forwarding or silent changes
of server. A differing saved URL requires confirmation, or `--change-server`.
Declining cancels login. Canonically equivalent URLs do not prompt.

A profile with a saved login offers to create another profile, replace its login,
or cancel. `--new-profile NAME` and `--replace` provide explicit non-interactive
choices. A new profile can use the selected profile's server; an explicit URL
still takes precedence. Interactive creation asks whether to make it active.

URL and credential changes are saved only after successful authentication.
Config updates use a file lock and atomic replacement; login checks that its
profile has not changed while browser approval was pending. Failed persistence
attempts revoke the new session. Replacing a login attempts to revoke the old
profile session and reports failures without undoing the new login.

## Browser login

The CLI requests a ten-minute device handoff with a random 256-bit private
polling secret and a separate eight-character, case-insensitive user code. The
code uses an unambiguous alphabet and displays as `ABCD-2345`. Separators and
whitespace are ignored when entering it. Only digests of both codes are stored.

The CLI opens `/login/device?code=...` and prints both the complete link and the
manual-entry URL `/login/device`. `--no-browser` suppresses automatic opening.
The existing login page handles password, passkey and MFA authentication and
returns to the device page. Required MFA enrolment must finish before approval.
The page displays the account, matching code and client-supplied machine label;
the user must explicitly authorize or decline. Visiting the link alone grants
nothing. The shared Act as selector defaults to the human and can select an
active [owned agent](agents.md). Choosing another human account signs out the
current browser account first.

Creation is limited to 10 requests per source address per five minutes, code
lookups to 30 per address per five minutes, and polling to once per five seconds
per valid request. The CLI backs off when asked to slow down. As with other
current authentication limits, the server uses the connection address rather
than trusting forwarded IP headers.

Approval locks the account and device request, revalidates the browser session
and MFA policy, and atomically creates a CLI session with an encrypted temporary
handoff credential. Only the private polling secret can retrieve it. Retrieval
rechecks active account policy and that the issued session has not been revoked,
then consumes the handoff. Concurrent approval or collection cannot duplicate
it. A lost successful collection response requires a new login; the uncollected
session remains revocable from Security or the selected agent’s detail page. Expired device records are cleaned by
the existing authentication maintenance job.

CLI sessions have independent opaque `cli_` credentials and use the same session
store and policy as browsers: seven days idle and thirty days absolute expiry.
They appear in Security (or the selected agent’s detail page) as
`CLI · acta · <machine>` and respond to ordinary
session revocation, account disabling and credential-change revocation. The
machine label is descriptive, not device attestation. CLI bearer credentials
cannot authorize another device request. Browser session credentials cannot be
used through the bearer transport. Browser mutations retain Origin checks;
originless device start/poll and CLI bearer requests remain JSON-only.

## Credentials and headless operation

The OS credential store is preferred (Secret Service on Linux, Keychain on macOS,
Credential Manager on Windows through go-keyring). If saving fails, login falls
back to a private plaintext credential file and prints its location.
`--insecure-storage` explicitly selects this file mode. Unix files are mode 0600;
Windows files receive a protected DACL granting only the current user access.
Existing keyring read failures are reported, not silently replaced with file
credentials. No saved session secret is printed by status or JSON output.

Profile configuration lives in the OS user config directory under `acta`:
`config.json` contains the active profile, server URLs and opaque credential
references. File credentials live separately under `credentials/`. Set
`ACTA_CONFIG_DIR` to override the directory, useful for isolated automation/tests.
Credential references are random IDs, independent of profile names and URLs.

`ACTA_TOKEN` overrides the selected profile's saved credential without writing
it to disk. The profile still supplies the server. `status` (also available as the `whoami` alias) identifies the source as `ACTA_TOKEN`, keyring or a credential file.
Login refuses while the override is set, explaining how to unset it. Logout with
an environment token revokes that session and tells the caller to unset the
variable; saved profile credentials are retained. Ordinary logout revokes the
saved session before removing its local credential, retaining the profile URL.
This slice does not add a separate personal-access-token creation interface.

`--json` emits machine-readable results on stdout and notices/errors on stderr.
Failures exit nonzero. Prompts require a terminal; missing decisions otherwise
produce errors explaining the necessary arguments. Device approval can still
run headlessly with `--no-browser`: a human completes the printed browser flow.
`ACTA_ACCESSIBLE=1` uses Huh's accessible line-based prompts. `completion` emits
Cobra shell-completion scripts, including profile-name completion.

## Structure and validation

`internal/client` owns HTTP transport and response contracts without importing
server implementations. `internal/cli` owns selection, local persistence and
rendering. The auth service owns device policy; PostgreSQL implements its atomic
storage contract. Login and required-MFA pages share the existing security UI.

Unit checks cover URL policy, redirect refusal, atomic profile changes, private
storage, credential fallback and environment precedence. PostgreSQL integration
checks cover pending/approved/denied/expired handoffs, replay, polling limits,
concurrent approval, MFA enrolment, revocation before collection, bearer/Origin
boundaries and the shared session expiry policy. Native credential-store and
browser checks are performed on Linux; other OS paths require native validation.

Validation on 2026-09-07: `make check build` passed and the complete PostgreSQL
race suite passed (integration package 15.218s). Windows amd64 client compilation
also passed; native Windows/macOS credential behavior is not yet verified.
Linux browser/CLI checks covered explicit approval, manual lowercase code entry,
denial, active-profile URL reuse, profile overrides, interactive replacement and
server-change cancellation, file and real system-keyring storage, JSON status,
browser revocation and ACTA_TOKEN precedence/logout. Disposable sessions were
revoked and their file/keyring credentials removed after the checks.

## Tasks

[Task interfaces](task-interfaces.md) provides the find → inspect → change workflow,
command examples, query/pagination semantics and explicit edit/conflict handling.
Commands include workspace discovery, task list/get/create/edit, statuses, people
and assignment groups. Workspace-scoped commands accept a UUID or slug. Human
output uses compact tables and detail text; `--json` retains typed fields, UUIDs,
versions and cursors. API errors preserve code, field validation and current
conflict details on stderr. Run `acta task --help` for the command surface.

`acta task activity REFERENCE` reads grouped history without marking it read.
Use `--cursor` for older pages and `--json` for structured events. See [activity](activity.md).

See [Task comments and replies](comments.md) for threaded discussions, read state and CLI/MCP comment commands.

## Connected development machines

`acta [-p profile] harness` connects a human profile’s development machine to
Acta in the foreground. It uses the same credentials and URL selection as other
commands and discovers installed Codex and Claude Code versions and local sign-in
states. See [Connected harnesses](harnesses.md) for discovery, presence, heartbeat,
reconnection and authorization behaviour.

`acta harness --separate-pipe` keeps provider process ownership in a detached
local helper, allowing development restarts of the hyperharness. My Agents
provides the current Codex Start/Kill/Resume controls; see [Provider threads](threads.md).
