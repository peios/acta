# Task archiving

ACT-99 adds manual subtree archiving. This is separate from completion status and
from deletion. Archives preserve task UUIDs, references, parent relationships,
status, assignments, Markdown, comments, documents and followers.

## Transactions and restoration

`archived` and `archived_at` appear on task detail; compact lists/search include
`archived`. `versions.archived` is an independent optimistic concurrency version.
Archive/restore requires the existing workspace `tasks.edit` permission.

Archiving stamps every active descendant with one batch UUID and timestamp and
increments each changed task's archive version. Existing archived descendants
keep their own batch. Restoration clears only the target's batch within its
subtree. A target with an archived parent cannot be restored independently.
This preserves separately archived children, while restoring ordinary descendants
atomically. A same-state retry is idempotent; a stale opposite-state change is a
conflict. Normal task writes and archive operations share the existing serialized
workspace authority transaction, so creation/reparenting cannot strand an active
child under an archived parent.

Archived tasks reject task edits, child creation/reparenting into them, comment
creation/edit/deletion, and document upload/replacement/deletion. Reads, download,
following and read acknowledgements still work. The UI hides edit controls and
shows an Archived banner with Restore. Archive/restore records a root task
activity entry and notifies its followers through existing notification rules.

## Interfaces

- `POST /api/tasks/{task}/archive` with `{archived: true|false, version: N}` returns
  the task. `version` comes from `versions.archived`.
- Ordinary lists default to active only. `GET /api/workspaces/{workspace}/tasks?archived=true&state=all`
  lists only archived roots (including archived children whose parent is active).
  Supply `parent` for direct archived children. Other filters and pagination apply.
- `GET /api/tasks/search?...&include_archived=true` includes both active and
  archived matches. The cursor binds this option along with query/workspace.
- MCP `task_archive` / `task_restore` take `task` and `version`, under `tasks.write`.
  `tasks_list` takes optional `archived`; `task_search` takes `include_archived`.
- CLI: `acta2 task archive ACT-12 --version 1`, `task restore ACT-12 --version 2`,
  `task list -w acta --archived`, `task search query --include-archived`.

The separate workspace Archived view keeps independent temporary filters and
preserves active preset drafts. Global search hides archived matches by default;
its Include archived checkbox exposes them explicitly.

## Verification

`TestTaskArchive*` tests atomic subtree behavior, previously archived descendants,
search/list defaults, direct inspection, content preservation, following,
permissions, conflict retries and a concurrent creation/archive race.
`TestTaskCLIAndMCPShareAuthority` exercises real archive/restore tools and CLI
commands. `web/tests/task-archive.browser.mjs` checks the separate view, archive
query filtering, active draft preservation and responsive controls.
