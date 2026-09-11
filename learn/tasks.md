# Tasks

ACT-56 adds workspace-owned tasks for humans and agents. Projects may organise
these tasks later; they are not required to capture work. Comments and activity are now supported. Labels, dates and task deletion remain outside the task metadata slice.

## Identity and hierarchy

A task has a permanent UUID and a workspace-local sequential number. A workspace
prefix produces a readable reference such as ACT-151. Prefixes contain 2–10 ASCII
letters, normalise to uppercase and are unique site-wide. Workspace Details lets
an account with Edit workspace change the prefix. Previous prefixes remain
reserved and resolve existing references to the same UUID. A generated prefix
from the workspace slug is provided when task settings are first created.

Numbers start at one and are allocated transactionally; they are never reused.
Every subtask gets its own number. Parentage is optional, can nest, and must stay
inside the workspace without cycles. Reparenting preserves identity. Each task
has its own status and assignments independently of its ancestors.

The task page starts directly with its Tasks heading and creation action, without
a separate workspace-name header or tagline. On mobile, the navigation button
sits inline with Tasks and Search. It is hidden while a task is open.
On mobile, Create task floats at the bottom right with safe-area clearance, for
both Tasks and Backlog. It is hidden while viewing a task or archived tasks.
Mobile creation opens a full-width bottom sheet above the visible keyboard,
with a title field, close button and the two creation actions. It uses the board's
default status and leaves priority, type and size unset for editing afterwards.
Title focus happens synchronously in the opening tap, after flushing bindings,
to preserve the browser's software-keyboard activation. The header
identifies the current workspace and board. Subtasks retain their parent's board
and default status. Enter submits, failed requests retain the entered values,
and closing or reloading preserves the existing account/workspace/parent-scoped
title draft. Successful creation clears that draft. Create task (also Enter)
closes the dialog and keeps the current view; Create and open opens the new task.
Desktop retains the compact title-first dialog.
The mobile toolbar uses a tighter left inset. Its sidebar slides in and out with
a short backdrop fade; reduced-motion preferences disable these transitions.
On screens up to 720px wide, swipe right from the leftmost 28px to open navigation,
or swipe left in the open drawer to close it. A deliberate horizontal swipe is
required; vertical scrolling, form fields and other open dialogs/popovers retain
their own gestures. The navigation button and backdrop remain available.
Search sits to the left of the plus-icon Create task button, with its magnifier
integrated at the right of a softly filled field. The icon stays anchored beside
Create task while the input expands to its left. In narrow task lists, the magnifying-glass
button reveals the field inline. Personal view tabs appear below. Beside them,
Filter opens a popover with multiple status, priority, type, size and assignee selections, including
Unassigned and searchable people/agents. Selections match any value within a
category and all selected categories together. A badge counts active selections; Clear
removes them. Display opens the table presentation controls described below.
At the same 620px task-list breakpoint as Search, Filter and Display become
icon-only controls so they stay
on the tab row. Tabs scroll horizontally without a visible scrollbar. Touch and
trackpad scrolling remain native; mouse users can drag the tab strip. Subtle edge
fades appear only where more tabs are off-screen. Dragging does not select or
rename a tab, and inline name fields remain editable normally. Filter keeps its active
highlight, with the selection count available in its accessible name and tooltip.
The main list is a table with Task, Title, Status and Assignees columns. Open a
task to add subtasks from its detail view. Expanded children share the same table
columns; narrow views scroll the table horizontally. The task card's own subtask
list keeps its compact editable rows.
By default, the list shows roots newest first, with expandable children and keyset pagination
of 50 tasks at each level. Without status/assignee selections, search applies to
the displayed hierarchy level. With selections, the main list shows matching
top-level tasks only, without promoting matching children into the results.
Assignee filtering uses direct assignments, and filtering happens
before pagination. The initial view includes completed tasks. Completion never
propagates automatically in either direction.

Live refreshes preserve the number of pages already opened with Load more. The
visible window is replaced only after its replacement pages have loaded; a
failed later page leaves the previous window available. Changing the query,
filters, group or sort starts a new window. Overlapping cursor pages are
deduplicated by task UUID.

Temporary connection failures preserve the loaded workspace, task cards and
editor state. The workspace revision feed retries and reports one shared error
instead of repeating the same failure in every column. Recovery refreshes task
lists again. Access loss and expired sessions remain authoritative and clear or
redirect the affected view. Superseded reads are cancelled, and an older poll
cannot roll back a newer revision obtained by a focus refresh.

Clicking a picker or menu trigger again closes its popup. Escape and outside
clicks also dismiss it; closing a nested picker leaves its parent menu open.
Task menus share anchored popup placement: they follow scrolling, flip above
their trigger when needed, stay within the visual viewport and adapt when
asynchronous search results change their height. Opening a task focuses its
close control; closing returns focus to the invoking control. Query-only task
navigation does not redirect focus to the page heading. Description rendering
and editing code loads when a description is first shown, rather than as part
of the initial task-list bundle. Switching editor modes focuses the active
editor, and invalid stored mode preferences fall back to rich text.

## Personal view tabs

Views belong to one account in one workspace and are stored on the server. The
first visit creates an ordinary, unfiltered All tasks view. When the active tab
has unsaved filters or display changes, the plus icon saves that configuration into a new named tab
and switches to it, restoring the original tab's saved view only after creation
succeeds. Cancelling or a failed request preserves the original draft. Without
unsaved changes, the plus icon creates an unfiltered tab with default display options. Names must be distinct within that account's workspace,
contain 1–60 characters, and there may be up to 50 tabs.

Changing filters or display options creates an unsaved draft for that tab. Drafts
survive switching tabs during the visit. A save icon beside each changed tab
updates that tab's saved filters and display. Undo, immediately left of Filter, restores the
active tab's saved view. Both controls disappear when there are no changes.
Reloading restores the saved view and reselects the last active preset for this
account and workspace in this browser. If that preset no longer exists, the first
available tab is selected. Switching workspaces remembers each selection
independently; the selected tab is scrolled into view when restored. Search text is
temporary and is not part of a saved view. Workspace defaults and shared views
remain future work.

Double-click a tab name, or press F2 on a focused tab, to rename it inline. Enter
or clicking away saves the name; Escape cancels. Renaming preserves the tab's
saved settings and any unsaved view draft. A trash icon appears beside the name
while editing. Deleting a tab switches to its next neighbour, or the previous
one if it was last in the row. Only tabs with unsaved filter, display or name changes require confirmation.
The final remaining tab cannot be deleted, and its trash icon is hidden.

Listing and creating views use GET and POST respectively at
`/api/workspaces/{workspace}/task-views`. Creation accepts `name` and optional
`filters` and `display`, saved together in one transaction. Saving uses POST at
`/api/workspaces/{workspace}/task-views/{view}`, with `version`, `filters` and `display`.
Workspace access is required; editing one's personal views does not require task
editing permission. Every operation scopes views to the authenticated account,
including for superusers. Saves use an expected version to prevent silent
overwrites from another browser. A conflict preserves the local draft and loads
the latest saved baseline, allowing Undo to restore it or Save to apply the draft.
Renaming uses POST at `/api/workspaces/{workspace}/task-views/{view}/rename`
with `version` and `name`; deletion uses POST at the corresponding `/delete`
endpoint with `version`. Both enforce personal ownership and expected versions.
Concurrent deletions cannot remove the final tab. Deleting the original All tasks
tab does not cause it to be recreated on the next visit.

## Table and board display

The Table / Board selector at the top of Display is saved per preset. Display
controls the main workspace view; the task card's compact subtask list
keeps its existing presentation. Task number and title are always visible. Status
and Assignees appear as alphabetically ordered pills under Properties. Remove a
property with its × button or restore it from the small + popup; there is no
manual ordering in this menu. Group by and Sort by use custom keyboard-accessible popups, with
a direction icon beside Sort by. Comfortable and compact density adjust
row spacing. The table uses quiet headers, subtle row separators and hover states,
status indicators and overlapping assignee avatars. Long titles stay on one line,
with the full title available on hover and through the task link's accessible name.

Sort by task number, title, status, creation time or last update, ascending or
descending. Titles sort case-insensitively; status sorting follows the workspace's
configured status order. A task number tie-breaker keeps equal values deterministic.
Sorting and filtering run on the server before the 50-task page limit.

Group by Status adds collapsible sections in workspace status order. Only matching
statuses are shown when a status filter is selected. Each section loads and pages
its root tasks independently; expanded children stay with their parent, even if
those children have different statuses. Collapsed sections are temporary UI state.
Grouping alone does not disable child expansion; explicit filters still do.

Group by Assignee starts with Unassigned, followed by humans alphabetically.
Each human's group combines assignments to them and their agents. Group by
Agents starts with Unassigned and Assigned to me, followed by your own agents
alphabetically. Tasks assigned only to other people or their agents are omitted
from Agents grouping. Unassigned always means no direct assignments at all.
A task appears once in each applicable group; hovering highlights all visible
instances of that task. These groups work as board columns and table sections.
Their queries intersect the existing filters before counting and pagination.
Retained assignments to unavailable accounts remain visible, but those accounts
cannot receive new assignments. Group membership uses UUID parentage, not handles.

Saved display JSON has `mode` (`table` or `board`), `columns` (an array containing `status` and/or `assignees`),
`sort` (`number`, `title`, `status`, `created`, `updated`), `direction` (`asc`, `desc`),
`group` (`none`, `status`, `assignee`, `agents`) and `density` (`comfortable`, `compact`). Defaults show
table mode with both optional columns, sort by descending task number, use no grouping, and use
comfortable rows. Existing personal tabs receive these defaults. An empty columns
array hides both optional columns. Display changes share filters' draft, Save,
Undo, new-tab handoff and conflict semantics.

The table and board use the same horizontal scrolling as the preset tab bar:
hidden scrollbars, edge fades where content remains off-screen, native touch/
trackpad scrolling, and mouse dragging on non-interactive areas. Card dragging,
header reordering and column resizing retain their own interactions.

Board columns have transparent backgrounds, with cards sitting directly on the
page. A column is highlighted when it is a drag-and-drop target.
Board columns come from Group by: Status gives one column per included status in
workspace order; No grouping gives one Tasks column. Each column independently
loads and pages root tasks through the same server query as the table. Filters,
search, sort field and direction all apply before pagination. Children are not
separate cards; a parent's card shows its direct subtask count. Cards always show
the task number and title, and Properties controls status and assignee details.
Density changes card padding and spacing. Opening a card uses the shared task
panel/modal/full-screen presentation.

Column headers show the total matching root tasks, including unloaded pages.
The shared task list response provides `total`, independent of pagination.
Users with Create tasks can enter a title inline at the top of each column.
Enter or Add creates the task in that column's status; an ungrouped column uses
the workspace creation status. An assignment column uses the workspace creation
status and assigns its human or agent directly. Unassigned creates without any
assignees. Escape or Cancel closes the entry. Failed saves
retain the title for retry. Creation refreshes the board without opening a task
panel; active search and assignee filters still apply to the new task.

In a status-grouped board, users with Edit tasks can drag cards between columns
to change status. The existing field-version check rejects conflicting edits;
failed moves leave the task in its original column, show the error, and refresh
the data. Successful moves refresh through the live task revision flow. A board
with no grouping has no status-changing drag. There is no manual card ordering:
the preset's sort determines order within each column. Keyboard users can open
a card and use its status, property or assignment picker. Switching between Table
and Board preserves settings and browser-local table column layouts.

On mobile (up to 720px), horizontal board scrolling snaps to each column, with a
small glimpse of the next column. Touch users can hold a card for 350ms to pick
it up. Moving before the hold
completes retains native scrolling; tapping opens the task normally. A floating
preview identifies the task and destination, and the destination column is
highlighted. Holding near either horizontal edge scrolls to other columns;
vertical edge scrolling keeps longer boards accessible. Snapping is disabled
only while dragging. Releasing over an available different column applies the
same version-checked move as desktop dragging. Releasing elsewhere, cancelling
the touch or leaving the page does not move the task. Long-press pickup is disabled
without edit access or on ungrouped boards. A card's three-dot **Move to…**
control opens a searchable destination list. The current column is marked and
unavailable destinations cannot be selected. It uses the same version-checked
status, property or assignment move as dragging, including conflict recovery.
Task pickers remain available on ungrouped boards and in task details.

In assignment grouping, dragging changes only the source assignment. In Assignee
view, it removes assignments to the source human and all their agents. In Agents
view, it removes only the source account. Dropping into another column adds that
account directly, without duplicates; dropping into Unassigned only removes the
source assignments. Other assignments survive, so the task appears in Unassigned
only when none remain. Changes use the existing assignees field version to reject
stale moves. Status and other fields are untouched by assignment moves.

`GET /api/workspaces/{workspace}/task-groups?group=assignee|agents` provides the
workspace-authorized group directory, including retained assignments. Task list
queries accept `group=assignee|agents` and `group_id=<account UUID>|unassigned`.
Agents grouping is relative to the authenticated human owner. Task people include
`owner_id` for agents so clients can apply owner roll-up assignment changes.

Opening or closing a task keeps the selected preset, unsaved filter/display
drafts, and its local column layout. Workspace metadata refreshes do not reset
the preset list; it reloads when the workspace identity changes or a failed load
is explicitly retried.

The side panel slides in briefly when opened from a closed state. Switching
tasks in an already open panel does not replay the animation. Reduced-motion
preferences disable it.

### Column layout in this browser

Click the Task, Title or Status header to sort by it; clicking the active sort
header again reverses direction. A newly selected sort starts ascending and is
part of the preset draft, with the same Save/Undo behaviour as Display.
Drag a table header to reorder its column; dragging does not also sort. Drag the divider between headers to
resize the two neighbouring columns. Header buttons also support Alt + Left/Right
to reorder; focused dividers support Left/Right to resize (Shift for larger steps).
Escape cancels a drag. The same order and widths apply to grouped and expanded
rows in the main table, while the task card's subtask list stays unchanged.

Each personal preset has its own browser-local layout, stored immediately without
making the preset dirty. Creating a preset in this browser copies the current
preset's layout; subsequent changes are independent. A preset first opened in
another browser uses property defaults: Task is small, Status and Assignees are
medium, and Title is big. The size-class weights (1, 1.5 and 4) are normalised to
percentages. The default order is Task, Title, Status, Assignees.

Stored widths are percentages, not pixels. Hidden properties retain their saved
width and place; visible columns are normalised to fill the table. Minimum usable
widths constrain rendering and dragging, with horizontal scrolling when the
available space is too small. Resizing the viewport does not overwrite the saved
percentages. Layout storage is keyed by workspace and preset UUID and is removed
when that preset is deleted here. Unavailable or invalid local storage falls back
to defaults; filters and other preset settings remain server-stored.

## Statuses and permissions

Workspace Details contains the prefix and status configuration, alongside the
existing workspace fields. New workspaces start with To do, In progress and Done.
Status names are workspace-local, distinct and editable. There must be 2–50
statuses and two distinct configured selections: Creation Status and Completed
Status. New tasks use Creation Status unless another status is explicitly chosen.
Only the configured Completed Status counts as completed; moving elsewhere
reopens a task. Changing that selection reinterprets completion for existing
tasks; this slice adds no historical completion metrics.

Removing a status requires a replacement for its tasks and valid new default
selections. The replacements, status revisions and configuration update are
atomic. Configuration saves use an expected revision; stale edits are rejected.

| Capability | Identifier | Controls |
| --- | --- | --- |
| Create tasks | `tasks.create` | Tasks and subtasks. |
| Edit tasks | `tasks.edit` | Title, description, status, parent and assignees. |
| Manage statuses | `tasks.statuses.manage` | Status definitions and creation/completed selections. |
| Edit workspace | `workspace.edit` | Task prefix, as well as the existing workspace fields. |

Workspace access permits reading tasks. Creation and editing are not implied by
membership. New workspace creators receive all workspace capabilities; existing
ordinary grants are not silently expanded by migration 012. Site Superusers
receive new capabilities through normal resolution. Agents inherit or receive
explicit workspace grants under the existing policy. Status managers can open
Details without Edit workspace; its workspace fields remain read-only.

## Assignees

A task has zero or more direct assignees, up to 100. Humans and agents must have
workspace access when newly assigned. Assignment grants no access and starts no
agent execution. Losing access or becoming unavailable preserves the assignment
with an Access removed indicator. Existing unavailable assignments may remain
while other assignments change.

Parents also show deduplicated assignees from all descendants, with source task
references. These are derived indicators, not additional parent assignments.
In the task card, tap the Assignees label to open a dropdown listing those people
and links to their subtasks. Tap outside or press Escape to dismiss it.
The UI distinguishes them from direct responsibility and does not show a person
twice in the combined list.

## Descriptions and saving

Descriptions are Markdown, limited to 100,000 UTF-8 bytes. Rich text and Markdown
modes share one draft, with the preferred mode remembered in the browser. Supported
formatting includes headings, emphasis, strikethrough, lists, checkboxes, links,
quotes, code and tables. Round trips preserve document structure; whitespace and
Markdown spelling may normalise. Images and raw HTML remain in source mode so
unsupported content is not silently dropped. Attachments are not implemented.

On save, recognised @handles and task references in prose resolve to UUID-backed
Markdown links. Code, existing links and HTML are not rewritten. References only
resolve when accessible to the caller; unknown references remain literal text.
A task link uses `/tasks/{uuid}`. Account references use the stored link target
`/references/accounts/{uuid}` and render as mention tokens in rich text, rather
than links to an unimplemented public account page. Labels preserve the author's
wording; UUID targets survive account, prefix and workspace renames. Use explicit
UUID links when a CLI/MCP client already knows the referenced identity.

Title and description autosave after a 700 ms typing pause. Title blur also
flushes its pending save. Status, assignment and parent actions save immediately.
Each editable field has its own revision, so independent changes can commit
without conflicts. A stale same-field change preserves the draft and presents
the latest saved text for review; the user explicitly chooses a version. Saving
the current value again is idempotent. Out-of-order responses never regress a
newer field revision.

The editor displays Saving, Saved, Unsaved, Could not save or Needs review.
Failed saves can be retried. Unsaved text is kept in local storage scoped to the
account, task, field and browser tab; session storage identifies the tab across
reloads. Different tabs cannot overwrite each other's recovery drafts. Normal
navigation retains drafts; if storage fails and unsaved text would be lost, the
UI asks before leaving. Pending saves from an unmounted editor cannot replace the
new task's visible state. Local drafts are not a substitute for a successful
server save and are not synced between devices.

## Responsive task view and live updates

The task card uses a directly editable, wrapping title and aligned property rows
for creation time, status and assignees. Status appears as a compact chip with a
custom dropdown, a current-choice checkmark and keyboard navigation. Its icon
distinguishes creation, completed and other statuses. Choosing a status saves
immediately; the control is read-only without Edit tasks. Assignees appear as
initial-and-name chips; the plus beside them opens an anchored, non-modal picker.
Its integrated search filters people and agents as you type. Selectable rows show
names, usernames, agent labels and checkmarks; assignment changes save immediately.
The popup fits the viewport and can be dismissed with Done, Escape or an outside tap.
Descriptions render in a subtly shaded inset box with a document icon and a
stronger section heading, without editing controls by default. Edit at the
top right opens the rich text/Markdown editor; clicking outside returns to the
rendered view once changes are saved. There is no Done button. Save failures or
conflicts keep the editor visible, and draft recovery remains active. Save
indicators appear while editing or when attention is needed. Close and the task reference sit at the left of the card header. Expand/contract
sits on the right, immediately before the vertical three-dot action dropdown. The
contract icon returns to automatic layout. The action dropdown offers Reparent to accounts with Edit tasks, opening a dialog to choose a
new parent by reference/UUID or make an existing subtask top-level.
Below the description, a tab bar currently contains only Subtasks, with an
underline marking it as selected. A borderless Add subtask plus icon sits beside the tabs, and the
subtask list appears in the panel beneath.
Subtask rows reuse the task list's loading, pagination and live refresh. They show
an editable status icon, task reference, inline-editable title, assignee avatars
and a chevron that opens the task. Title edits use the same autosave, retained
drafts and conflict handling as the task card. Status and title editing require
Edit tasks. The shared status picker opens above or below its trigger to fit the
viewport; opening a child exposes its own subtasks.

Opening a task sets `?task={uuid}` on the workspace URL. The same detail component
is shown as a side panel when at least 1,150 CSS pixels of workbench width remain,
a modal otherwise, or a full-page view below 760 CSS pixels of viewport width.
These thresholds use available content space, including sidebar width. Panel and
modal offer Full screen; that explicit preference persists per browser until
Automatic layout is chosen. Drafts and editor state stay mounted when the
presentation changes. `/tasks/{uuid-or-reference}` resolves into the owning
workspace URL after authorization. Browser history and copied links work in all
presentations.

The side panel fills the available height with matching top and bottom content
margins. It can be resized by dragging its left edge. Its width is remembered
per browser, starts at 600 pixels, and is bounded between 440 and 1,000 pixels
while keeping at least 360 pixels available for the task list. A smaller viewport
temporarily clamps the width without replacing the preference. Left/Right keys
resize the focused separator (Shift uses larger steps), Home/End select the
limits, double-click restores the default, and Escape cancels an active drag.
The centred modal and full-page view do not have resize handles.
On narrow phones, the full-page task view gradually sheds its outer margins,
reaching the screen edges at 480 CSS pixels and below. Inner content and
description padding also shrink with the viewport, retaining a small text inset.

The task page uses bounded long polling. Every poll checks current session, owner,
MFA and workspace access; task/configuration writes advance a durable workspace
revision. Changes from another person, CLI or MCP client refresh visible data
without reloading. Active drafts are preserved and same-field changes trigger
conflict handling. Requests finish after ten seconds and reconnect; database
checks occur at one-second intervals. This works across server processes without
sticky sessions or an in-memory event bus.

## HTTP, CLI and MCP

Existing cookie/CLI bearer, Origin and MFA protections apply. Task writes accept
up to 256 KiB JSON request bodies. Task service authorization is shared by all
three interfaces.

Task-list HTTP requests accept repeated `status={uuid}` and `assignee={uuid}`
parameters plus `unassigned=true`. These combine with the existing `state`, `q`,
`parent` fields. Sorting uses `sort` and `direction`; pass the response's opaque
`cursor` with the same sort options to load the next page. The legacy `before`
number remains supported only for default descending-number order. Invalid sort
options, malformed cursors and mismatched cursor sort orders are rejected. Selected statuses are ORed; selected accounts and
Unassigned are ORed; these categories intersect. An explicit parent still limits
results to its direct children. With no explicit parent, selections search only
root tasks. UUID validation and selection limits are enforced in the shared service.

| HTTP endpoint | Purpose |
| --- | --- |
| `GET /api/workspaces/{uuid}/task-config` | Prefix, statuses, defaults and configuration/change revisions. |
| `POST /api/workspaces/{uuid}/task-prefix` | `prefix`, expected configuration `version`. |
| `POST /api/workspaces/{uuid}/task-statuses` | Complete `statuses` array, `creation_status`, `completed_status`, removed-ID-to-replacement-ID map `replacements`, expected `version`. |
| `GET /api/workspaces/{uuid}/tasks` | Filters, sort and cursor; returns `tasks`, `total`, `more`, `cursor`, `next`. List entries omit description content. |
| `POST /api/workspaces/{uuid}/tasks` | Required `title`; optional `description`, `status_id`, `parent_id`, `assignees` UUID array. |
| `GET /api/tasks/{uuid-or-reference}` | Full detail, ancestors, assignment sources and field `versions`. |
| `POST /api/tasks/{uuid-or-reference}` | One `field`, expected field `version`, new `value`. |
| `GET /api/workspaces/{uuid}/task-people?q=` | Up to 50 accessible human/agent candidates; narrow the search if needed. |
| `GET /api/workspaces/{uuid}/task-changes?after=` | Wait for a changed revision or bounded timeout. |

CLI and MCP use the shared [task interface contract](task-interfaces.md), including
compact pages, workspace UUID/slug resolution, direct-subtask inspection and
explicit field updates. `acta task --help` lists commands; MCP tool schemas
describe the corresponding inputs. Browser display presets remain personal UI
configuration and are not exposed through MCP.

Task workspace paths accept UUIDs or exact current/previous slugs; all responses
return the canonical workspace UUID. `summary=true` on task lists returns compact
entries with identity, title, named status, direct assignees and child count,
plus `total`, `more` and `cursor`. `include=subtasks` on task detail includes a
bounded direct-child summary page; `subtask_cursor` continues it. These options
are used by CLI/MCP without changing the browser's existing response shapes.

Task edits require a positive expected field version and an explicit non-null
value. Empty string clears description or parent; an empty array clears direct
assignees. A stale changed value returns HTTP 409 with `error.code=conflict` and
`error.current={field,version,value}`. The current value is returned only after
access and edit permission checks. Sending the already-saved value is an
idempotent success. Other fields are never changed by the request.

MCP adds `workspaces_list`, `tasks_list`, `task_get`, `task_statuses`, `task_people`,
`task_groups`, `task_create` and `task_update`. New OAuth approvals offer task read
and write capabilities alongside identity. Existing connections retain their
original grants and need new approval for additional capabilities. Tool consent
is an additional ceiling on current workspace permissions. The SDK invokes the
same task services through an internally authenticated request context; MCP
access tokens still cannot be used as browser/CLI API credentials. Every tool
request reloads its grants and every operation revalidates its session and
account inside the transaction. SDK cancellation propagates to the service.

## Architecture and validation

`internal/tasks` owns types and validation. Auth task services own scope checks,
delegation and orchestration. `TaskTx` extends the workspace transaction boundary;
PostgreSQL owns queries and locking. Migration 012 stores configuration, durable
prefix reservations, statuses, tasks and direct assignments. Same-workspace
foreign keys guard parent and status references. Workspace writes currently use
the existing installation/actor lock order, which serializes numbering, cycle
checks, account access changes and field updates. Read snapshots are consistent.
A future adapter must preserve these guarantees; no grants are cached in clients.

Tests cover concurrent numbering and opposing parent moves, private visibility,
old references, independent/same-field edits, status replacement, configuration
permissions, unavailable assignees, reference parsing, inherited/custom agent
access and actual CLI HTTP/MCP SDK calls. Existing account/security/CLI/MCP tests
continue to pass. Frontend tests check Markdown structure/reference round trips,
source-only content and out-of-order save responses. Browser review covers
creation/subtasks, assignment, prefix changes, autosave, concurrent edit conflicts,
full-screen preference and the responsive panel/modal/mobile layouts.

## Activity

[Task activity](activity.md) records new changes atomically, groups consecutive edits,
and adds the Activity tab, server-side read tracking and CLI/MCP history reads.

See [Task comments and replies](comments.md) for threaded discussions, read state and CLI/MCP comment commands.

## Optional task properties (ACT-88)

Priority, Type and Size use a shared fixed catalogue (`internal/tasks/properties.json`)
embedded by the domain and imported by the browser. Every existing or newly created task
starts with `none` unless a value is supplied:

- Priority: `none`, `low`, `medium`, `high`, `urgent`.
- Type: `none`, `bug`, `chore`, `feature`.
- Size: `none`, `xs`, `s`, `m`, `l`, `xl`. Size means relative effort, not hours.

Each property is independent, including on subtasks; there is no automatic size roll-up.
Task details offer keyboard-accessible custom pickers. Display can show these properties
on table rows and board cards, and group or sort by any of them. Ordering follows the
catalogue above (None first ascending); task number breaks ties for stable cursor paging.
Table headers toggle sort direction. Column widths/order remain browser-local per preset.
Dragging a board card between property groups patches only that property, with its current
field version. Creating in a property column supplies the corresponding value.

Filter selections use OR within each category and AND across categories. `none` explicitly
matches unset values. Filtering never promotes children into root results. Saved presets
include property filters and display settings; unsaved drafts, reset and new-tab copying
retain the existing behavior. Existing presets retain their visible properties.

Mutations have independent versions and activity entries with readable before/after labels.
A stale change conflicts; retrying the already saved value is idempotent. Invalid values
are rejected by domain validation and database constraints. Empty text explicitly clears
a property to `none`; omission on an update never clears it.

Validation: `go test -race ./internal/integration -run TestTask` against an isolated
database schema covers filters, sorting, activity, conflicts, presets, CLI and MCP.
`npm run test:task-properties` runs a real-component browser regression with mocked
HTTP, covering group changes sharing a None column, sorting, filtering, board drag
and inline creation. Set `ACTA_PLAYWRIGHT_MODULE` to an installed Playwright module
path if it is not on the local package resolution path.

## Following and notifications

The bell beside the task menu follows updates. Creating or assigning tasks also
follows them for the human owner. See [Task notifications](task-notifications.md).

## Search

Use the sidebar Search tasks button or Ctrl/Cmd+K to find tasks across workspaces
and all subtask depths, including matching descriptions and comments. Results
open the task over the current page. See [Task search](task-search.md).

## Archived tasks

Use **Archive task** in the task actions menu to put away a task and its active
subtree. Archived tasks keep their status, content, assignments, documents and
followers. They are read-only until restored, and direct links remain available.

The archive icon immediately before Filter opens **Archived tasks**, a separate
workspace view. Its temporary filters and display settings do not change the
selected active preset or its unsaved drafts. Back to tasks restores those drafts.
Archive roots include archived subtasks whose parent remains active; expand/open
these roots to inspect their archived children. Board drag/drop and creation are
disabled in this view.

Restore returns the subtree to its previous parent. If an ancestor is archived,
restore it first. A child archived separately before its parent stays archived
when that parent is restored. Following and read acknowledgements remain usable.
Archiving does not happen automatically when work reaches a completed status.
See [archive contracts](task-archiving.md) for API, CLI and MCP details.

## Tasks and Backlog boards (ACT-105)

Each workspace has Tasks and Backlog in its sidebar. Backlog uses an inbox-tray
icon distinct from the Tasks board icon, including in the collapsed sidebar.
Tasks retains the existing
workflow and presets on upgrade. Backlog begins with one Backlog status and no
completed status; workspace status managers can add lanes and optionally choose
a completed status. Status names are unique within a board, so both workflows
can independently use names such as Review or Done. Prefixes and task numbering
remain shared across the workspace.

Board membership comes from the task's status. Choose a status under either
board heading in the task status picker, or drag a table row or board card onto
a sidebar board to move it to that board's entry status. The move uses the
status field revision and records activity. IDs, references, assignments,
comments, documents and parent links remain intact; children keep their own
statuses. A child whose parent is on another board appears as a root in its own
board so it remains discoverable, and remains visible under its actual parent.

Each board has its own personal display presets, filters, archive view and
last-selected preset. Task dialogs preserve the underlying board URL. Root
creation defaults to the visible board; subtask creation defaults to its parent's
board. The status editor in Workspace Details selects a board before editing.
Global search searches both boards automatically. The local title/reference
filter stays within the visible board.

HTTP/CLI/MCP task lists accept `board: tasks` (default), `backlog`, or `*`.
Explicit parent queries list all direct children regardless of board. Creation
accepts an optional board; an explicit board and status must agree. The task
configuration response includes both boards and their entry/completed status
IDs, and each status includes its board. `field: board` edits accept tasks or
backlog and use `versions.status_id`; they resolve to the destination entry status
inside the same authorized transaction. Edit `status_id` instead to choose a
specific destination lane. Views are permanently board-scoped; their board
cannot be changed by saving a preset.

The database owns status-to-board and board-to-default-status foreign keys.
Existing workspaces, statuses and preset IDs are retained by migration 040;
Backlog is seeded without changing existing task membership. New workspaces
receive both workflows in their creation transaction.

## Mobile forms and task navigation

On touch devices, buttons and menu summaries have at least 44px touch targets,
and text fields use at least 16px text to avoid automatic focus zoom. Phone layouts
follow the visual viewport so the software keyboard does not cover the composer
or task dialog. Pinch zoom remains available.

Task details use a full-screen modal on phones with a visible **Back** control.
The board remains mounted at its existing horizontal and vertical scroll position
beneath the modal, and becomes available again on closing it. The URL still
identifies the selected task, including browser back/forward navigation.

The create-task title is saved as it changes, separately for each signed-in
account, workspace, board and parent task. Closing the dialog or reloading the
page preserves that draft; successful creation clears it. Existing comment and
description draft recovery continues to apply.

Search also follows the visible viewport while the keyboard is open. Filter
checkbox rows have at least 44px touch targets. Leaving the description editor
closes it after the destination click is delivered, so tapping another field or
the comment composer works on the first tap.
