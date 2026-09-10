# Migration Assistant

Migration Assistant is an explicitly delegated MCP capability for creating and
editing Acta content with historical attribution and protected metadata. It is
independent of the source system: there is no old-Acta record format, import job,
or mandatory migration batch.

## Enable and revoke

A **human superuser** can enable **Migration Assistant** during MCP authorization,
or use **Edit access** on an existing connection under User Settings → Security
or User Settings → Agents → their agent. It is off by default. No new login or
credential replacement is needed when amending an existing connection. A client
that caches its tool list may need a refresh or reconnect.

The grant is `migration.assistant`. It authorizes all site content independently
of the ordinary workspace and memory permissions of the connected identity.
Each write selects `acting_as`, an existing user or agent UUID. Historical authors
may be disabled or pending. The actual caller must still be active, satisfy MFA,
have a valid MCP session and hold this grant; its human owner must still be a
superuser. All of these checks repeat inside the write transaction. Revocation
or loss of superuser authority prevents subsequent operations, including those
using a previously obtained request context.

Ordinary tools retain ordinary authorization and attribution. Migration Assistant
cannot write accounts, memberships, permissions, credentials, security settings,
or login sessions. Create historical identities through normal administration
before attributing records to them.

## Tools and workflow

1. `migration_schema({})`: read the accepted fields and their semantics.
2. `migration_find({"kind":"account","query":"jack"})`: resolve existing actors.
   Find workspaces with `kind:"workspace"`; find tasks with `kind:"task"` and
   `parent` equal to their workspace UUID. Comments, documents and activity use
   a task UUID as parent. Memories accept an optional scope UUID as parent.
   `query` is a case-insensitive substring, not ranked task search. Follow
   `next_offset` while `more` is true. Pages contain at most 50 records.
3. `migration_create` creates a record and returns its generated ID and revision.
4. `migration_get` inspects an existing record by kind and ID (or task reference).
5. `migration_edit` requires that exact opaque revision. A normal edit also
   invalidates it. A conflict returns `migration_changed`; inspect and reconcile.

All mutations return a record with `kind`, `id`, `revision` and an object `data`.
The revision is a digest of the inspected record and relevant metadata. Treat it
as opaque; it is not a timestamp or a normal field version. Creation is not
idempotent: inspect before retrying after an uncertain connection failure.

Example, after resolving the workspace and actor UUIDs:

```json
{
  "kind": "task",
  "acting_as": "<existing-actor-uuid>",
  "fields": {
    "workspace_id": "<workspace-uuid>",
    "reference": "PEI-123",
    "title": "Historical task",
    "description": "Original Markdown description",
    "created_at": "2024-01-15T09:30:00Z",
    "updated_at": "2024-02-01T17:00:00Z"
  }
}
```

Omitted fields are preserved on edit, except automatic update timestamps. Null
only clears a task's `parent_id` or `archived_at`, or denotes no `reply_to` on a
new comment. Unknown fields, invalid types, invalid relationships and timestamp
ordering mistakes fail atomically. UUIDs cannot be chosen or changed.

## Content rules

- **Workspaces:** name, slug, description, prefix, status names and default
  creation/completion statuses, creation timestamp. Existing status names retain
  IDs. Set statuses before migrating tasks; a status used by a task cannot be
  removed. Normal slug and prefix reservation rules remain in force.
- **Tasks:** title, description, status, priority/type/size, parent, assignments,
  archive date, creator and timestamps. `reference` uses the workspace's current
  prefix and a positive number. Collisions fail and allocation advances past an
  explicitly set number. Renumbering does not retain an alias for the old number.
  The workspace is immutable; parents must belong to it, without cycles.
  Historical assignments may reference existing inactive accounts. Migration
  archive edits affect the specified record, not its descendants.
- **Comments:** body, displayed author, creation/update times and an optional
  reply target on creation. The task and reply relationship are immutable.
  Deleted comments cannot be rewritten. `author_id` defaults to `acting_as` on
  creation and otherwise remains unchanged unless explicitly supplied.
- **Memories:** all four scopes, key, summary, content, creator and timestamps.
  `scope_id` identifies a workspace, user or agent, matching the scope. Site
  memories omit it. Scope is immutable; normal scope/key uniqueness and content
  validation remain. `updated_by` is the acting identity.
- **Documents:** task, title, filename, base64 content and historical version
  timestamp. Edits append a version, reusing omitted content or names. The task
  is immutable. Decoded files have the normal 20 MiB limit; authorized migration
  requests allow 28 MiB to accommodate base64 and metadata. Document inspections
  return metadata, not file bytes.
- **Activity:** task, a recognized task/document `change` object and timestamps.
  These are historical statements, not commands that modify the task. Changes
  use the same `kind`, `field`, `reason`, `before` and `after` format as normal
  activity; unknown fields and kinds are rejected. Correcting an entry appends
  an event and updates its display projection; it never rewrites a raw event.

Task/document creation and edits produce normal-shaped activity automatically.
Only create additional history for events not already represented. Timestamps
control display; history pagination retains insertion order. Create historical
entries chronologically. No migration-specific activity grouping combines
separate submitted records.

## Integrity and operator attribution

A mutation, its derived state and its operator record commit together. The
transaction serializes with ordinary content and permission changes. Task
search, references, counters and versions remain consistent; creators and
assignees auto-follow newly created tasks. Migration changes send no inbox or
push notifications. They do not mark existing activity read.

`migration_operations` is an append-only ledger containing the actual MCP
operator, session ID, acting identity, operation, target kind/UUID, before/after
metadata and the real operation timestamp. Historical timestamps and displayed
authors cannot replace this ledger. Sessions need not remain stored for their
IDs to remain in it. Raw activity events also remain immutable. Document bytes
remain in version storage rather than being duplicated in the operator ledger.
This ledger has no migration write tool; inspecting content does not expose
credentials or session secrets. It is included in ordinary database backups.

### Board workflows

Workspace workflow edits accept `status_board: "tasks"` (default) or `"backlog"`.
The names in `statuses`, `creation_status` and `completed_status` apply only to
that board. Backlog accepts one status and an empty completed status. Configure
each board before importing its tasks; the inspected workspace includes `boards`
and status-to-board IDs. Task `status_id` determines its board, so migration
keeps board membership without a second task field. Used statuses cannot be
removed by Migration Assistant.
