# Task search

Open **Search** from the sidebar's separate, bordered button or Ctrl/Cmd+K.
The popup searches every accessible
workspace and every subtask depth, including completed tasks and tasks excluded
by the current view's filters. The workspace picker optionally narrows the scope.
It accepts 2–200 characters, waits briefly while typing, and cancels obsolete
requests. Arrow keys or Ctrl+N/Ctrl+P select the next/previous result, Enter opens
it, and Escape closes search.

Below 760px, Search fills the visible screen. A horizontally scrollable workspace
pill bar sits at the top, including All workspaces. Results scroll between that
bar and the focused search input with a 44px X control at the bottom. Mobile hides
the Include archived checkbox; desktop retains its workspace picker and checkbox.
The overlay follows the visual viewport so the controls stay above the
software keyboard, respecting safe areas. Selecting a result, X or Escape closes
it; tapping empty space does not dismiss it or activate the page underneath.
Desktop retains its centred popup. Backdrop dismissal occurs on click rather
than pointer-down so the page remains inert throughout the dismissal gesture.

Results contain one row per task: reference, title, status, workspace, ancestors
and an excerpt of the best matching description or comment. Highlighted spans
are plain text, never provider-supplied HTML. Clicking opens the existing task
viewer beside the current page when space permits, as a modal when narrower,
and full screen on mobile. The current page and its URL remain unchanged. A
comment match opens its discussion in a highlighted Matching comment section
of Activity, even when outside the currently loaded activity window. Back to
latest returns to ordinary activity. Access is checked again when opening it.

## Matching and storage

PostgreSQL stored generated vectors and GIN indexes cover task titles,
descriptions and non-deleted comment bodies. They update in the same transaction
as their source rows; there is no separate indexing service or synchronization
queue. Existing content is indexed by the migration. No extension is required.

Exact task UUIDs and current or previous references rank first. Exact titles,
title-word matches and title-word prefixes rank above description matches, then
comments. Descriptions/comments use English word stemming; title prefixes use
the simple dictionary to retain names and short terms such as “IT”. Queries are
plain words, not a Boolean query language. This is lexical search, not semantic
or typo-tolerant search. Alternative wording can find different results.

The best source per task is selected before paging, so many matching comments
cannot fill a page with duplicates. Matching title/description words may span
both fields. If content changes or a comment is deleted, the next query sees
the new content. Workspace filtering uses the normal human/agent access rules
inside the same consistent database read as search.

## Shared API

- HTTP: `GET /api/tasks/search?q=words&workspace=optional-slug&cursor=optional`.
- MCP: `task_search({"query":"words","workspace":"optional-slug"})`, under
  the existing `tasks.read` grant. Omit workspace to search everywhere allowed.
- CLI: `acta task search "words" [--workspace slug] [--cursor cursor] [--json]`.

Each page contains `tasks`, `more` and `cursor`, with at most 25 compact task
results. A result includes `id`, `reference`, `title`, `status`, `workspace_id`,
`workspace_slug`, `workspace_name`, `ancestors`, `source`, `excerpt` and an
optional `comment_id`. Excerpts are arrays of `{text, match?}` spans. Clients
should render text normally and highlight spans with `match:true`. Use `task_get`
to read full details, or `comment_get` for the matched comment.

The opaque cursor binds the query and workspace and continues by rank and UUID,
not offset. Keep these inputs unchanged while paging. Each request rechecks
permissions. Search is a live view: edits between pages can change rank or
membership; restart the search for a fresh result set rather than treating a
multi-page search as an immutable export.

The [agent guide](mcp-agent-guide.md) directs agents to search and try alternative
wording before creating work. Exhaustive checking is reserved for concrete
evidence that a task already exists; ordinary creation must not require scanning
hundreds of tasks.

## Verification

`TestTaskSearch*` integration tests cover ranking, nested tasks, old references,
live edits/deletions, comment deduplication, pagination, workspace/agent access,
revocation, HTTP and CLI. `TestTaskCLIAndMCPShareAuthority` also exercises the
actual MCP search tool. `web/tests/task-search.browser.mjs` checks keyboard
navigation, workspace changes, pagination, escaped excerpts and error recovery.

PostgreSQL references: [full-text controls](https://www.postgresql.org/docs/17/textsearch-controls.html)
and [text-search indexes](https://www.postgresql.org/docs/17/textsearch-indexes.html).

Archived tasks are excluded by default. The popup offers Include archived; API/MCP
use `include_archived`, and CLI uses `--include-archived`. Keep that setting
unchanged with a pagination cursor. Archived results are explicitly labelled.
