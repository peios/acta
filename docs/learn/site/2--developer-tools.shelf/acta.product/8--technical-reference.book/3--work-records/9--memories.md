---
title: "Memories"
description: >-
  Knowledge scopes, access boundaries, revision-checked writes, recall
  pagination, and explicit client reads.
---

Memories are core Acta records available across authorized client sessions.
They store durable standalone knowledge. Task-specific progress, decisions,
and findings belong in task descriptions/comments; public documentation
belongs in maintained Learn documentation. Do not store facts readily
rediscovered from current source.

## Scopes and permissions

- Workspace is the normal destination. Anyone with workspace access can read;
  `memories.write` controls create/edit/delete and follows the existing agent
  workspace policy and owner permission ceiling.
- User is only for person-specific knowledge across projects. Humans manage
  their own memories. Agents automatically read their owner's user memories;
  writes require an explicit `own.memories.write` agent grant and that
  permission on the owner. Sibling agents do not share agent-scoped memories.
- Agent is rare, for knowledge specific to a persistent Acta agent
  identity/role. The agent and its human owner can read/write it. Clients
  using the same agent identity share these records. Learning something does
  not make it agent-specific.
- Site is rare, for truly global knowledge across the installation.
  `site.memories.read` and `site.memories.write` govern it; writing also
  requires read access. Superusers have these capabilities. No automatic
  cross-user access to private memories is granted to site administrators.

Save always requires an explicit destination, never a fallback scope. UUIDs
are stable; keys are unique within a scope and use 1–100 lowercase
letters/numbers/ hyphens. Summaries are up to 500 bytes; Markdown content up
to 64 KB. Author and editor account UUIDs and timestamps are retained.
Updates/delete require the revision read. There is no append mode: read the
current memory, reconcile the new knowledge into its content, and save using
that revision. Concurrent changes return HTTP 409 `memory_changed`. A memory
cannot be moved to another scope by updating it. Duplicate creation keys
return validation errors instead of silently overwriting. Invalid
scope/selector combinations (such as workspace on an agent memory) return
validation errors. Creating at site scope without its required account
permissions returns forbidden. Reading an inaccessible memory by ID still
returns not_found, protecting private records. Deletion is permanent.

## MCP and HTTP

Authorize `memories.read` and `memories.write` on the normal Acta MCP
connection. Existing connections do not silently gain these grants: use **Edit
access** on the connection in session settings and select the memory
checkboxes. Tools share the same transactions and permissions as the browser
and CLI-authenticated HTTP requests.

- `memory_recall({workspace:null,query?,cursor?})` combines accessible site,
  user/owner, and own-agent summaries. Providing a workspace UUID/current or
  old slug adds that workspace. Search matches key, summary and content
  literally, case-insensitively. MCP entries contain only `id`, `scope`,
  `key`, `summary`, and `revision`; IDs allow retrieval and scope
  distinguishes repeated keys. Browser recall retains its metadata for the
  management UI. Pages contain 50 summaries sorted by key/UUID. Cursors bind
  the query and scopes and access is checked on every request.
- `memory_get({id})` retrieves Markdown, attribution, revision, and write
  ability.
- `memory_save({scope,workspace?,id?,key,summary,content,revision})` creates
  with revision 0/no ID, or edits using ID/current revision. User/agent
  identity is derived from authentication. Its tool description contains scope
  guidance.
- `memory_delete({id,revision})` deletes with write permission.

HTTP endpoints: GET `/api/memories`, GET `/api/memories/{id}`, POST
`/api/memories`, POST `/api/memories/{id}/delete`. The human browser at User
Settings → Memories can select an owned agent using `agent_id` and add a
workspace. MCP does not expose agent selection. Recall never injects full
memory bodies into external tools automatically. Clients read relevant
records explicitly.
