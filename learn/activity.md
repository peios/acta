# Task activity

ACT-59 introduces a shared activity foundation, initially recording task changes.
The Activity tab precedes Subtasks. [Comments and reply threads](comments.md)
share this feed, with separate content storage and individual read positions.

Task-change entries appear as single compact rows. Long summaries truncate with
the complete text on hover. Activity, comments and replies show relative times
(such as “5 mins ago”); hover a timestamp for its full local date and time. A shared
clock refreshes relative labels while the page is open.

## Records and grouping

Task creation, title/description edits, status changes, assignment replacement
and reparenting record activity in the same database transaction as the change.
Workspace status removal also records each affected task's status replacement,
with the operator as actor and an explicit replacement reason. Saved-value
retries, failed validation and stale conflicts do not emit events. Existing tasks
receive no synthetic backfill: their activity starts with new changes.

Raw events have a stable sequence, group ID, workspace and subject references,
actor UUID, schema version, timestamp and an explicitly selected change payload.
Title changes retain their before/after text. Statuses retain UUIDs and names;
parents retain UUIDs and task references; assignments retain account UUIDs and
handles. Description events record that an edit occurred, never the description
body. This is not full revision history or comprehensive security auditing.

Actors are resolved by UUID for display. Agent ownership is immutable (already
enforced by the account schema), so ownership is derived from the agent account
instead of copied into every event. Event actor references prevent hard deletion
of historical identities. Historical change labels are preserved even if the
referenced status/account/task is subsequently renamed.

Only consecutive events on the same subject with the same actor, kind, field
and reason can group. Each successful edit extends a 60-second inactivity window;
60 seconds from the latest edit is inclusive. A different actor/field/reason or
another event kind breaks the group. Posting, editing or deleting a comment
creates a barrier, including mutations on older threads. Grouped entries show the first before value, latest after value,
latest timestamp and number of changes. Raw events remain separate.

`activity_events` is append-only; a trigger rejects row updates/deletes.
`activity_entries` is a mutable presentation projection with stable group IDs
and first/last event positions. Both are updated atomically under the existing
workspace mutation lock order. This is not tamper protection against a database
administrator. A future storage adapter must preserve serialization per subject,
atomic domain/event/projection writes, stable ordering and consistent read
snapshots. The shared table supports other subject types, but only task activity
is exposed in this slice.

## Visibility and read tracking

Anyone who can access the task can read its activity; there is no separate
activity permission. Every fetch and acknowledgement revalidates account/session
and current workspace access, including agent delegation. Workspace and site
feeds are deferred.

Read acknowledgements are server-side and belong to the calling account. They
identify a particular grouped entry and the last event actually displayed.
Acknowledgements only advance, cannot cross tasks or acknowledge nonexistent
positions, and never mark a later extension read. They are deliberately sparse:
seeing recent entries cannot mark unseen older entries read when the user reverses
order or has not loaded earlier pages. A user's own events are excluded from
unread calculation; their agent's events remain distinct.

The browser acknowledges visible entries after a short pause only while Activity
is selected, the document is visible and focused, and the entry is not covered
by another UI element. Simply opening Subtasks or fetching activity through CLI
or MCP does not mark it read. An unread dot appears on Activity; unseen entries
receive a subtle highlight and a New activity divider. Highlights remain for the
current task visit even after acknowledgement, while the dot reflects remaining
unread activity. A grouped item extended after acknowledgement becomes unread
again. Read changes from other browsers refresh on focus or the periodic refresh.

## Ordering, scrolling and live updates

The browser defaults to oldest first, and remembers the ordering preference in
local storage across tasks. The order button occupies the same tab-bar position
as Add subtask on Subtasks. Activity opens by default, but opening a task starts
at its title. Explicitly selecting Activity scrolls to the latest loaded entry
in the chosen order. Older entries load on demand above/below the feed as
appropriate.

The card body scrolls as one surface; the toolbar remains fixed. Incoming events
follow automatically only when the reader is already at the latest entry.
Otherwise the visible entry anchor is preserved and a New activity arrow offers
a jump to the latest entry. Reduced-motion preferences suppress smooth scrolling.
Workspace revision changes drive content refresh; focus/recovery and a 15-second
visible-document refresh also reconcile activity/read state. Refreshes retain
the loaded history window and cancel superseded requests.

## HTTP, CLI and MCP

`GET /api/tasks/{uuid-or-reference}/activity?cursor=...` returns grouped entries
newest first, at most 50 per page, with `more`, an older-page `cursor`, and task-wide
`unread`. Event positions/cursors are decimal strings to preserve 64-bit precision
in JavaScript. Entry IDs are UUIDs. Ordering uses immutable first-event positions;
extending the latest group does not move its identity across pages.

`POST /api/tasks/{uuid-or-reference}/activity/read` accepts up to 50 visible
positions:

```json
{"entries":[{"id":"ENTRY_UUID","through":"123"}]}
```

Ordinary API session, Origin and MFA protections apply. Read acknowledgements do
not themselves create activity or increment the workspace task revision.

```sh
acta task activity ACT-151
acta task activity ACT-151 --cursor 123 --json
```

MCP `task_activity` takes `task` and optional `cursor`, and uses the existing
`tasks.read` grant and typed data/error envelope. It neither marks history read
nor adds a new consent category. Clients may need to refresh their tool catalogue.
Both CLI and MCP return newest-first pages without applying browser preferences.

## Validation

On 2026-09-07, `make check build` passed with 34 frontend tests, zero Svelte
errors/warnings, formatting, Go vet/unit checks and server/client builds. The
full PostgreSQL race suite passed (integration package 36.234s); the final MCP
activity protocol check also passed. Integration tests use isolated schemas and
cover atomic rollback on event failure, immutable raw records, sliding grouping
and interruption, no-op/conflict exclusion, sparse read acknowledgements, extended
group unread state, cross-task/access denial, cursor pages, status replacement
labels, description-body exclusion, real CLI commands and MCP tool calls.

Browser checks on localhost:8081 covered default Activity and Subtasks actions,
unread retention on Subtasks, acknowledgement on visible Activity, grouped live
edits, both sort orders, initial title position, mobile and side-panel layouts,
latest-entry following and scroll-anchor preservation while reading older events.
A new event shifted the older entry's measured position by less than one pixel;
mobile checks placed the fixed toolbar at y=0 and latest activity at the viewport
bottom. The temporary viewport override was reset. REV-7, Activity review, is a
local demonstration task; its title and status were restored after checks.

See [Task comments and replies](comments.md) for threaded discussions, read state and CLI/MCP comment commands.
