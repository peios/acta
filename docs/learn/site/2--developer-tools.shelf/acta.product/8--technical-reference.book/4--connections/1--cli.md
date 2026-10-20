---
title: "Command-Line Connections"
description: >-
  Named profiles, browser device authorization, credential storage, token
  overrides, and command output.
---

The command-line client stores named server profiles and obtains its
credentials through browser authorization. Profile selection, credential
storage, and server sessions are separate concerns.

## Profiles

`default` is an ordinary, automatically available initial profile. An active
profile is used unless a command supplies `--profile NAME` or `-p NAME`. An
unknown profile is an error, never a fallback. Creating a profile does not
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
saved URL, then an interactive prompt. URLs default to HTTPS; loopback
addresses can use HTTP. Paths, embedded credentials, queries, and fragments
are rejected. Redirects are not followed, preventing credential forwarding, or
silent changes of server. A differing saved URL requires confirmation, or
`--change-server`. Declining cancels sign-in. Canonically equivalent URLs do
not prompt.

A profile with a saved sign-in offers to create another profile, replace its
sign-in, or cancel. `--new-profile NAME` and `--replace` provide explicit
non-interactive choices. A new profile can use the selected profile's server;
an explicit URL still takes precedence. Interactive creation asks whether to
make it active.

URL and credential changes are saved only after successful authentication.
Config updates use a file lock and atomic replacement; sign-in checks that its
profile has not changed while browser approval was pending. Failed persistence
attempts revoke the new session. Replacing a sign-in attempts to revoke the
old profile session and reports failures without undoing the new sign-in.

## Browser sign-in

The CLI requests a ten-minute device handoff with a random 256-bit private
polling secret and a separate eight-character, case-insensitive user code. The
code uses an unambiguous alphabet and displays as `ABCD-2345`. Separators and
whitespace are ignored when entering it. Only digests of both codes are
stored.

The CLI opens `/login/device?code=...` and prints both the complete link and
the manual-entry URL `/login/device`. `--no-browser` suppresses automatic
opening. The existing sign-in page handles password, passkey and MFA
authentication and returns to the device page. Approval checks completion of
required MFA enrollment. The page displays the account, matching code, and
client-supplied machine label; the user explicitly authorizes or declines.
Visiting the link alone grants nothing. The shared Act as selector defaults to
the human and can select an active [owned
agent](~acta/technical-reference/accounts/agents). Choosing another human
account signs out the current browser account first.

Creation is limited to 10 requests per source address per five minutes, code
lookups to 30 per address per five minutes, and polling to once per five
seconds per valid request. The CLI backs off when asked to slow down. Client
addresses follow the deployment proxy trust policy. Forwarded addresses are
accepted only through explicitly trusted transport peers.

Approval locks the account and device request, revalidates the browser session
and MFA policy, and atomically creates a CLI session with an encrypted
temporary handoff credential. Only the private polling secret can retrieve it.
Retrieval rechecks active account policy and that the issued session has not
been revoked, then consumes the handoff. Concurrent approval or collection
cannot duplicate it. A lost successful collection response requires a new
sign-in; the uncollected session remains revocable from Security or the
selected agent's detail page. Expired device records are cleaned by the
existing authentication maintenance job.

CLI sessions have independent opaque `cli_` credentials and use the same
session store and policy as browsers: seven days idle and thirty days absolute
expiry. They appear in Security (or the selected agent's detail page) as
`CLI · acta · <machine>` and respond to ordinary session revocation, account
disabling and credential-change revocation. The machine label is descriptive,
not device attestation. CLI bearer credentials cannot authorize another device
request. Browser session credentials cannot be used through the bearer
transport. Browser mutations retain Origin checks; originless device
start/poll and CLI bearer requests remain JSON-only.

## Credentials and headless operation

The OS credential store is preferred (Secret Service on Linux, Keychain on
macOS, Credential Manager on Windows through go-keyring). If saving fails,
sign-in falls back to a private plaintext credential file and prints its
location. `--insecure-storage` explicitly selects this file mode. Unix files
are mode 0600; Windows files receive a protected DACL granting only the
current user access. Existing keyring read failures are reported, not silently
replaced with file credentials. No saved session secret is printed by status
or JSON output.

Profile configuration lives in the OS user config directory under `acta`:
`config.json` contains the active profile, server URLs, and opaque credential
references. File credentials live separately under `credentials/`. Set
`ACTA_CONFIG_DIR` to override the directory, useful for isolated
automation/tests. Credential references are random IDs, independent of profile
names and URLs.

`ACTA_TOKEN` overrides the selected profile's saved credential without writing
it to disk. The profile still supplies the server. `status` (also available as
the `whoami` alias) identifies the source as `ACTA_TOKEN`, keyring, or a
credential file. The sign-in command refuses while the override is set,
explaining how to unset it. Logout with an environment token revokes that
session and tells the caller to unset the variable; saved profile credentials
are retained. Ordinary sign-out revokes the saved session before removing its
local credential, retaining the profile URL. Acta does not add a separate
personal-access-token creation interface.

`--json` emits machine-readable results on stdout and notices/errors on
stderr. Failures exit nonzero. Prompts require a terminal; missing decisions
otherwise produce errors explaining the necessary arguments. Device approval
can still run headlessly with `--no-browser`: a human completes the printed
browser flow. `ACTA_ACCESSIBLE=1` uses Huh's accessible line-based prompts.
`completion` emits Cobra shell-completion scripts, including profile-name
completion.

## Tasks

[Task interfaces](~acta/technical-reference/connections/task-interfaces)
provides the find → inspect → change workflow, command examples,
query/pagination semantics and explicit edit/conflict handling. Commands
include workspace discovery, task list/get/create/edit, statuses, people, and
assignment groups. Workspace-scoped commands accept a UUID or slug. Human
output uses compact tables and detail text; `--json` retains typed fields,
UUIDs, versions, and cursors. API errors preserve code, field validation, and
current conflict details on stderr. Run `acta task --help` for the command
surface.

`acta task activity REFERENCE` reads grouped history without marking it read.
`--cursor` selects older pages and `--json` selects structured events. See
[activity](~acta/technical-reference/work-records/activity).

See [Task comments and
replies](~acta/technical-reference/work-records/comments) for threaded
discussions, read state and CLI/MCP comment commands.
