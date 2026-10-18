# Memories

Memories are core Acta records, independent of threads and provider sessions.
Use them for durable standalone knowledge. Task-specific progress, decisions and
findings belong in task descriptions/comments; public documentation belongs in
learn/. Do not store facts easily rediscovered from current source.

## Scopes and permissions

- Workspace is the normal destination. Anyone with workspace access can read;
  `memories.write` controls create/edit/delete and follows the existing agent
  workspace policy and owner permission ceiling.
- User is only for person-specific knowledge across projects. Humans manage
  their own memories. Agents automatically read their owner's user memories;
  writes require an explicit `own.memories.write` agent grant and that permission
  on the owner. Sibling agents do not share agent-scoped memories.
- Agent is rare, for knowledge specific to a persistent Acta agent identity/role.
  The agent and its human owner can read/write it. Threads using the same agent
  identity share these records. Learning something does not make it agent-specific.
- Site is rare, for truly global knowledge across the installation.
  `site.memories.read` and `site.memories.write` govern it; writing also requires
  read access. Superusers have these capabilities. No automatic cross-user
  access to private memories is granted to site administrators.

Save always requires an explicit destination, never a fallback scope. UUIDs are
stable; keys are unique within a scope and use 1–100 lowercase letters/numbers/
hyphens. Summaries are up to 500 bytes; Markdown content up to 64 KB. Author and
editor account UUIDs and timestamps are retained. Updates/delete require the
revision read. There is no append mode: read the current memory, reconcile the
new knowledge into its content, and save using that revision. Concurrent changes return HTTP 409 `memory_changed`. A memory
cannot be moved to another scope by updating it. Duplicate creation keys return
validation errors instead of silently overwriting. Invalid scope/selector
combinations (such as workspace on an agent memory) return validation errors.
Creating at site scope without its required account permissions returns forbidden.
Reading an inaccessible memory by ID still returns not_found, protecting private
records. Deletion is permanent.

## MCP and HTTP

Authorize `memories.read` and/or `memories.write` on the normal Acta MCP
connection. Existing connections do not silently gain these grants: use **Edit
access** on the connection in session settings and select the memory checkboxes. Tools share the same transactions and permissions
as the browser and CLI-authenticated HTTP requests.

- `memory_recall({workspace:null,query?,cursor?})` combines accessible site,
  user/owner and own-agent summaries. Providing a workspace UUID/current or old
  slug adds that workspace. Search matches key, summary and content literally,
  case-insensitively. MCP entries contain only `id`, `scope`, `key`, `summary`,
  and `revision`; IDs allow retrieval and scope distinguishes repeated keys.
  Browser recall retains its metadata for the management UI. Pages contain
  50 summaries sorted by key/UUID. Cursors must
  retain the same query and scopes and access is checked on every request.
- `memory_get({id})` retrieves Markdown, attribution, revision and write ability.
- `memory_save({scope,workspace?,id?,key,summary,content,revision})` creates with
  revision 0/no ID, or edits using ID/current revision. User/agent identity is
  derived from authentication. Its tool description contains scope guidance.
- `memory_delete({id,revision})` deletes with write permission.

HTTP endpoints: GET `/api/memories`, GET `/api/memories/{id}`, POST
`/api/memories`, POST `/api/memories/{id}/delete`. The human browser at User
Settings → Memories can select an owned agent using `agent_id` and add a
workspace. MCP does not expose agent selection. Recall never injects full memory
bodies into a thread automatically. No provider files or hooks are modified.

## References in agent conversations

Memory MCP calls receive the same reference chips as task calls. The browser
recognises `memory_get`, `memory_save`, `memory_recall` and `memory_delete` on
MCP connections whose configured name contains `acta` (case-insensitive), using
Claude's `mcp__name__tool` and Codex's `name/tool` naming forms. IDs come from
reviewed arguments or structured memory results, including recall lists and MCP
JSON text/structured-result envelopes. Scope IDs, prose and arbitrary object IDs
do not become links.

Each candidate is checked against this Acta server's memory API using the signed-in
human's access. Accessible memories show their current scope and key; unresolved,
deleted or inaccessible references remain plain text. There is no cross-server
lookup. Verification shares the task resolver's coalescing, four-request concurrency
limit, short-lived per-page cache, and cancellation on account/navigation teardown.
Opening a chip performs a fresh read, so a memory deleted or access-revoked since
verification shows the API error rather than cached content.

Opening a memory keeps the chat, lane and composer draft mounted. It uses
`?memory=<uuid>` and replaces an open task panel; opening a task replaces it in
turn. Browser back/forward can restore the selection. Task and memory viewers
share their responsive shell: a resizable right panel when space permits, a modal
at intermediate widths, and full screen on mobile. Memory panel width/fullscreen
preferences are browser-local and independent of task preferences. Markdown,
revision-checked editing and deletion use the same detail component as the
Memories settings page; writes still require the memory's scope permissions.
