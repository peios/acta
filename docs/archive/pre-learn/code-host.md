# Code host registration

`make build` produces `bin/acta-code-host`. This first slice registers a private
host with the selected Acta server and shows its presence in Code's host picker.
It also owns [codebases and directory browsing](codebases.md) and can spawn
[Claude Code or Codex threads, send plain-text messages and relay CAT frames](code-threads.md). It does not
handle permissions, return file contents or open a local
listener. The editor remains the [UI preview](code-preview.md).

## Run

```sh
acta login http://localhost:8080
acta-code-host
# Or use an existing named profile:
acta-code-host --profile work --name "Development desktop"
```

The host uses the existing [CLI profiles and credential stores](cli.md), including
the active profile, `--profile` / `-p`, `ACTA_CONFIG_DIR`, keyring/file credentials
and the CLI's `ACTA_TOKEN` override. It takes a startup snapshot of the selected
server and credential. Changing the active profile does not redirect a running
host. Sign in with your human account; agent logins cannot register or list hosts.
The server must be reachable for the initial account lookup. Once running,
temporary connection failures retry automatically. Authentication or other
non-retryable client errors terminate the process; sign in again and restart.

The display name defaults to the OS hostname. A stable random host ID is stored
under the Acta config directory at `code-host/<server-and-account-hash>/host-id`.
The local lock prevents two processes for the same server/account from running
under that identity. Different profile names for the same server/account share
the identity; different accounts and servers use separate identities. The
directory contains no credentials. Each process has a fresh instance UUID.

## Privacy and presence

Hosts are strictly per human user, independent of Acta Workspaces. The server
derives ownership from the authenticated account, never a client-supplied owner.
Listing and mutations always use that owner; even a site superuser sees only
their own hosts. There is no sharing or administrator host-selection endpoint.
Host names and OS/architecture are self-reported labels, not device attestation.

The host opens an outbound authenticated WebSocket through the shared HTTP
client. Each connection gets a fresh instance UUID. The server pings every ten
seconds, requires a response within five seconds, rechecks the login and renews a
thirty-second presence lease. Closing a connection releases the lease; crashes
and network loss also expire it. Offline records survive server restarts.
Revoking the issuing login makes its host offline immediately. The browser
refreshes every three seconds while Code is visible; a failed refresh displays
status unavailable rather than asserting that cached presence is current.
Closing the browser does not stop the host.

## Provider discovery

The host discovers `claude` (Claude Code) and `codex` on its inherited `PATH` on
each connection, then once a minute. It uses `claude auth status --json` and
`codex login status`, from the OS home directory, with inherited provider config
and environment. Executable paths must be absolute; relative PATH entries are
not used. Installing or signing in outside the host is picked up on the next
scan; changing the host process's PATH/environment requires a restart.

Each status process has a five-second deadline and bounded stdout/stderr. Claude
uses its JSON `loggedIn` field plus exit status; Codex uses recognized login
status lines plus exit status. Unsupported output, execution failures and
timeouts yield `unknown`, never an assumed sign-out. Checks report the CLI's
local authentication state, not a live guarantee that credentials remain valid
or that a model request will succeed. They do not launch agent conversations.

Only provider ID, installation flag and `signed_in` / `signed_out` / `unknown`
are sent to Acta. CLI output, account details and credentials remain local and
are not logged. The host sends an unsolicited `provider_status` notification
over its existing private WebSocket, separate from request results. Acta validates
the two known provider entries and adds a server receipt timestamp. The latest
report lives only on that connection, is omitted after 90 seconds without a
report, and is absent for disconnected or revoked hosts. No database migration
or public provider-discovery endpoint is needed.

The host picker shows installed provider marks beside OS/architecture. Signed-in
icons use the same muted gray as the status text; signed-out and unknown icons
are orange, with no status dots. Hover text
and the host option's accessible name state the status. A browser refresh error
changes cached icons to unknown. Icons use the existing Acta visual marks only;
discovery does not depend on the Threads agent implementation.

CLI references: [Claude Code](https://code.claude.com/docs/en/cli-usage) and
[Codex authentication](https://learn.chatgpt.com/docs/auth#check-authentication-or-sign-out).

## API and storage contract

- `GET /api/code/hosts`: the authenticated human's hosts, including offline ones.
- `GET /api/code/hosts/connect`: native WebSocket requiring a CLI bearer token
  and no Origin header. Browser cookies cannot register a host. The initial
  hello contains `id`, `instance_id`, `name`, `os`, `arch`.
- `POST /api/code/hosts/{id}/request`: authenticated human request with
  `{method, params}`. Acta assigns a request ID, relays it to that user's host and
  returns the matching result. Codebase/directory and Code thread start/list/read
  methods are supported.

HTTP operation inputs reject unknown fields. UUIDs and labels are validated. The
normal Acta session, MFA, Origin and TLS rules apply. Recovery instances disable
Code host endpoints. No new agent permissions or MCP tools are introduced.

The PostgreSQL `code_hosts` table is keyed by `(owner_id, id)`. Presence upsert
atomically prevents a different live connection from taking over (`host_in_use`),
while retrying the same instance is safe. An expired or revoked lease can be
replaced. A release for an older instance cannot take a newer one offline.
Presence requires both an unexpired lease and a live issuing session. The
database-independent contract lives in `internal/codehosts`; PostgreSQL owns
the atomic operations. Listing additionally reports `connected`, requiring a
live connection on this server process as well as valid lease/session presence.

The relay allows 32 pending requests per connection, waits up to ten seconds,
and fails pending requests on disconnect without replaying them. The host runs
at most four operations concurrently. Requests and responses are size-bounded.
Both caller and native sessions are rechecked before returning data. Host error
codes are prefixed so they cannot impersonate browser authentication failures.

Routing is currently local to one Acta server process. Multiple server processes
need connection-aware routing before this relay can span nodes. Direct
client-to-host connectivity is a later iteration.
