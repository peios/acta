# Hosted MCP and OAuth

ACT-53 adds a Streamable HTTP endpoint at `/mcp`. Go serves it alongside the
compiled web application; no CLI relay or separate service is required.
`acta_guide` returns the maintained agent working guide as plain Markdown.
`whoami` returns `id`, `username` and nullable `display_name` without arguments.
[Task tools](tasks.md#http-cli-and-mcp) add workspace discovery and task operations. User, group and permission administration
remain in the web application.

## Connect

Point an OAuth-capable Streamable HTTP client at the installation's canonical
MCP URL, such as `https://acta.peios.org/mcp` or `http://localhost:8081/mcp` for
local development. Use the URL configured by `ACTA_PUBLIC_URL`; discovery and
token audience checks use that exact origin and path.

The client discovers OAuth automatically and opens an Acta consent page.
Existing password, passkey and MFA sign-in flows return to that request.
Required MFA enrolment must complete before authorization. The page displays
the client-supplied name, identity access, task read/write choices and a shared Act as selector.
It defaults to the human and can select an active [owned agent](agents.md). Every new
connection requires explicit approval. Cancel returns `access_denied` to the
validated client callback. “Use another account” signs out the browser first.
Client names are labels, not verified publisher identities.

For Codex, after building/running Acta:

```sh
codex mcp add acta2 --url http://localhost:8081/mcp
codex mcp login acta2
```

The server implements dynamic client registration for public clients; clients
need no manually provisioned client ID or secret. It does not advertise client
ID metadata documents or fetch client-supplied URLs. HTTPS callbacks and HTTP
loopback callbacks are supported. Loopback IP callbacks may change port while
retaining their registered address, path and query (RFC 8252).

## Consent, grants and sessions

The OAuth scope `acta` requests Acta access. It does not grant every present or
future tool. Approval snapshots the connection's explicit grants in its session;
`identity.read` exposes `whoami`; optional `tasks.read` and `tasks.write` expose
the task tools; `memories.read` and `memories.write` expose memory tools. Existing
connections do not automatically gain new capabilities. The human can amend
access in session settings without repeating OAuth.

Each request reloads the active account and connection grants before creating
the MCP server's tool catalogue. Both listing and calling tools use that same
catalogue. Account permissions remain a separate, additional boundary for any
task operations. Disabling an account or imposing an unmet MFA requirement takes
effect on the next MCP request or refresh.

Human connections appear in User Settings → Security; agent connections appear
on that agent’s detail page. Both show `MCP · <client name>`, with
the connection’s approved capabilities. Sign out revokes the entire connection, including
all of its access and refresh tokens. Sign out all other sessions and the
existing credential-change revocation rules apply to MCP too. Browser and CLI
sessions are independent. Removing a connection does not sign its approving
browser out.

## Protocol and security boundaries

- Discovery: `/.well-known/oauth-protected-resource/mcp` (also the root metadata
  route) and `/.well-known/oauth-authorization-server`.
- Registration: `POST /oauth/register`, JSON metadata. Unknown metadata is ignored.
- Authorization: `GET /oauth/authorize`, code flow with S256 PKCE and exact
  resource audience. An opaque, browser-bound consent request lasts ten minutes.
- Browser consent: `/login/oauth`, using authenticated, Origin-checked
  `/api/oauth/consent` and `/api/oauth/approve`. Merely visiting never grants access.
- Token exchange: `POST /oauth/token`, form-encoded public-client requests.
  Authorization codes are single-use and expire after one minute. Client,
  redirect URI, resource and PKCE verifier must match. Responses include the
  issuer on the authorization callback and echo the client's state.
- Revocation: `POST /oauth/revoke`, form-encoded `token` and `client_id`.
  Unknown tokens are an idempotent success.

Access tokens last up to five minutes. Refresh tokens rotate on use and never
outlive the connection's thirty-day absolute expiry. Both access and refresh
check the seven-day idle deadline. Refreshing alone does not update last activity.
Clients must register the refresh-token grant to receive refresh tokens.
Reusing a consumed refresh token, or replaying an otherwise valid consumed
unexpired authorization code, revokes the connection. Clients must serialize
refreshes and reconnect if a refresh response is lost.

Tokens are random opaque credentials, stored as digests. `mcp_` access and
`mcp_refresh_` refresh credentials are distinct from browser and `cli_` tokens.
MCP accepts access tokens only through the Authorization bearer header, never
cookies or URL parameters. MCP credentials cannot call the ordinary application
API. Browser mutations retain exact Origin checks; native OAuth and MCP routes
allow absent Origin but reject an explicitly untrusted Origin.

Registration, authorization, exchange and revocation are rate-limited by the
connection's source address (20, 30, 120 and 60 requests per five minutes,
respectively). As with existing authentication endpoints, untrusted forwarded
IP headers are not used. Requests have bounded bodies and deadlines. Expired
requests and tokens are removed by the existing maintenance job; unused client
registrations become eligible for cleanup after thirty days.

## Implementation and validation

`internal/auth/oauth*.go` owns the policy and persistence contract.
`internal/postgres/oauth.go` implements account-first transaction locking,
single-use requests, rotation and revocation. OAuth tokens refer to the existing
session table with cascading deletion. Migration 009 adds session kind, grants,
client registrations, authorization requests and tokens.

`internal/httpapi/oauth.go` adapts OAuth's standard HTTP envelopes and discovery.
`internal/httpapi/mcp.go` uses the official MCP Go SDK v1.7.0 with stateless
Streamable HTTP and JSON responses. No background streams or protocol-session
state are needed for this tool. Durable connections belong to Acta's session
system. Shared safe return-path helpers let browser authentication resume both
CLI and MCP authorization without accepting arbitrary redirect targets.

Integration tests cover SDK discovery/tool calls, persisted grant filtering,
credential and audience separation, PKCE/client/redirect binding, approval
concurrency, denial, Origin checks, refresh rotation/replay, expiry, MFA policy,
credential changes and session revocation. A real Codex OAuth login was completed
on localhost:8081, then its session was revoked through Security and its test
credentials removed. Claude Code has not been exercised in this environment.

Validation on 2026-09-07: `make check build` passed, including frontend tests and
zero Svelte diagnostics. The complete PostgreSQL race suite passed (integration
package 22.596s); final OAuth-focused race checks also passed (4.939s). Browser
review covered desktop and 390px consent layouts, successful Codex authorization,
visible session grants, browser revocation and cancellation. Some clients leave
the browser on Acta after their callback, so consent retains an explicit
completed/declined state rather than a disabled loading form.

Observed client limitation: the tested Codex build retries once after the first
`access_denied`, displaying a misleading scope-retry message. Declining its
second request ends login with `access_denied`. Acta creates no connection for
either declined request. This is client behavior; the server preserves the
standard OAuth denial response.

## Task tools

[Tasks](tasks.md#http-cli-and-mcp) extends the endpoint with task read/write tool
grants. Existing identity-only approvals remain identity-only until the human
adds task access using Edit access in session settings. Workspace permissions still
bound every action, including agent inheritance and explicit overrides.

Task tools use a consistent typed envelope: `{"data": ...}` on success and
`{"error": {"code": ..., "message": ..., "fields": ..., "current": ...}}`
for service errors. Optional error fields are omitted when irrelevant. Errors
set MCP `isError`; the SDK supplies JSON text as well as structured content.
Schema/protocol validation errors may use the SDK's own error response.
`whoami` retains its small identity object. See [task interfaces](task-interfaces.md)
for discovery, filters, subtask pagination and conflict reconciliation examples.

`task_activity` exposes grouped [task history](activity.md) through the existing
`tasks.read` grant. Fetching it does not mark history read.

See [Task comments and replies](comments.md) for threaded discussions, read state and CLI/MCP comment commands.

## Editing connection access

Use **Edit access** beside an MCP session in User Settings → Security, or under
User Settings → Agents → the owned agent. Choose capabilities and Save access.
A live human browser session is required, just as for initial consent. Humans
can amend their own connections and their own agents’ connections; agents cannot
amend their own access. Account/workspace permissions still independently limit
ordinary tool actions. Migration Assistant is the explicit superuser-only
exception described below. Nothing is enabled automatically.

The update retains the session, tokens, identity, authorization proof and expiry.
Additions and removals apply when the next MCP request is dispatched, including
calls using an already cached tool name. In-flight requests are not cancelled.
The stateless endpoint does not push tool-list change notifications: clients
which cache discovery may need to refresh tools or reconnect with their existing
credentials. They do not need another browser authorization solely for this change.

`POST /api/security/sessions/{id}/grants` accepts `account_id` (optional; defaults
to the human, otherwise an owned agent), `previous_grants` and `tool_grants`.
An empty grant set leaves only `acta_guide` available while preserving the connection. Updates
compare the previous grant set under the session lock; a stale save returns
409 `session_grants_changed`. Reload the session before trying again. Expired,
revoked, foreign and non-MCP sessions cannot be amended. Successful changes
record an `mcp_access_changed` account event. No migration is required.

## Agent working guide

Every authenticated MCP connection exposes `acta_guide({})`, including connections
with no data-access grants. It returns the built-in Markdown from
[mcp-agent-guide.md](mcp-agent-guide.md), embedded at compile time, followed by
applicable site and human-owner policy appendices. It has no side effects and
needs no new OAuth grant. Normal connection validity and revocation checks still
apply. Task, memory and identity tools retain their existing grant checks.

The MCP initialization response includes short instructions explaining Acta's
purpose and directing agents to read the guide before substantial work. The full
guide is fetched on demand rather than repeated during initialization or every
tool call. Actual client presentation of instructions depends on the client.
Existing clients may need to refresh their tool list or reconnect to discover the
new guide tool and receive initialization instructions; reauthorization is not
required.

The guide covers task/context discovery, record placement, truthful progress,
memory scope, collaboration, pagination, conflicts and uncertain writes. It is
product guidance under the user's instructions and project conventions, not an
additional source of permissions. Keep it and affected tool descriptions current
as agent-facing features change; this is recorded in the repository AGENTS.md.

## Guide preview and preferences

**User Settings → Guide** shows the exact combined Markdown returned by
`acta_guide`. The built-in guide is read-only. The Preferences tab edits the
human's own user appendix and, with `site.guide.write`, the installation's site
appendix. Agents read their owner's appendix and cannot edit either scope.
These preferences are separate from memories and their grants. They are for
short, truly global, always-needed essential policy within the chosen scope;
selectively useful knowledge belongs in memories, work-specific knowledge on tasks.
User preferences take precedence over conflicting site defaults without changing
access permissions. Empty appendices are omitted. Saved changes apply on the next
guide read, including through an existing authenticated MCP connection.

`GET /api/guide` returns `built_in`, the combined `markdown`, and `site` / `user`
preferences with `content`, `revision` and `can_write`. `POST /api/guide/preferences`
accepts `scope` (`site` or `user`), Markdown `content` (up to 8,000 Unicode
characters), and the revision just read. No caller-selected account is accepted.
Revision zero is an absent preference. A stale save returns 409 `guide_changed`;
the editor preserves the draft and offers the latest version for review.
Clearing content retains the revision so older editors cannot resurrect policy.
Migration 031 stores these records independently, with update author and time.

## Migration Assistant

The optional `migration.assistant` grant exposes `migration_schema`,
`migration_find`, `migration_get`, `migration_create` and `migration_edit`.
Only human superusers can grant it to their own or their owned agents' MCP
connections. Unlike ordinary tools, these operations have site-wide content
access and accept historical impersonation, task references and timestamps.
Normal tools retain their normal grants and permission checks. Delegation is
checked against the human owner's live superuser authority on every operation.
See [Migration Assistant](migration-assistant.md) for accepted content,
transactional checks, notification suppression and independent operator records.
