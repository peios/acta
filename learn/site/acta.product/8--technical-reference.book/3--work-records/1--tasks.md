---
title: "Tasks"
description: >-
  Identity, boards, independent field versions, task queries, saved views,
  and transactional state.
---

A task belongs to one workspace and has a stable UUID and sequential local
number. Status, assignments, parentage, description, and release targeting are
independent properties. A mutation records its activity and refresh revision
atomically.

## Identity and hierarchy

Workspace task prefixes contain 2–10 ASCII letters, fold to uppercase, and are
unique across the installation. Previous prefixes remain reserved and resolve
to the same workspace. Numbers begin at one, are allocated transactionally,
and are never reused. Every subtask has its own number.

Parentage stays within the workspace and rejects cycles. Reparenting retains
the task identity. Parent and child statuses and assignments do not propagate
into one another. Descendant assignments shown on a parent are derived
indicators with source task references, not additional direct assignments.

## Boards and statuses

Tasks and Backlog share task numbering but have separate workflows. Status
names are unique within a board. Tasks accepts 2–50 statuses and distinct
entry and completed selections. Backlog accepts 1–50 statuses and an optional
completed selection; when present, it differs from the entry selection.

A task's status determines its board. Board moves use the status field
revision and select the destination entry status. A specific status change can
select another lane. Children retain their own board and status. A child whose
parent is on another board is also a root in its own board's list.

Status removal replaces affected task statuses and updates the workflow in one
transaction. Stale configuration revisions are rejected. Completion is
computed from each task's board configuration; changing the completed
selection changes which existing tasks count as finished.

## Permissions

Workspace access permits reading tasks. Changes use the corresponding
capability:

| Capability | Identifier | Controlled operation |
| --- | --- | --- |
| Create tasks | `tasks.create` | Create tasks and subtasks. |
| Edit tasks | `tasks.edit` | Edit fields, archive, restore, and change documents. |
| Comment | `tasks.comment` | Post and manage the caller's own comments. |
| Manage others' comments | `tasks.comments.manage` | Edit or delete another author's comments. |
| Manage statuses | `tasks.statuses.manage` | Configure each board's workflow. |
| Manage releases | `tasks.releases.manage` | Create and edit release records. |
| Edit workspace | `workspace.edit` | Change the task prefix and workspace details. |

New workspace creators receive its capabilities. Superusers resolve every
workspace capability. Agents use owner-bounded inherited or explicit workspace
permissions. Assignment does not grant access or start execution.

## Fields and concurrency

Descriptions contain at most 100,000 UTF-8 bytes. A task has up to 100 direct
assignees. A newly added assignee has workspace access; an existing
unavailable assignment can remain while other assignments change.

Each editable field has its own revision. Unrelated field edits can commit
independently. A stale same-field edit returns a conflict with the current
value and revision. Repeating the saved value is a no-op. Assignment updates
replace the complete direct set; clearing one assignment is not an implicit
merge.

Priority, type, and size use the shared domain catalog:

| Property | Values in ascending order |
| --- | --- |
| Priority | `none`, `low`, `medium`, `high`, `urgent` |
| Type | `none`, `bug`, `chore`, `feature` |
| Size | `none`, `xs`, `s`, `m`, `l`, `xl` |

Size represents relative effort, not hours. None of these properties rolls up
from children. An omitted update does not clear a field; explicit empty text
clears these properties to `none`. The target release is also direct,
optional, and independently versioned, with no inheritance from the parent.

## Descriptions and drafts

Rich text and Markdown modes share one draft. Supported structure survives
round trips, while Markdown spelling and whitespace can normalize. Raw HTML
and images remain in source mode. Task documents are stored separately from
the description.

Accessible task references and account handles in prose become UUID-backed
links on save. Existing links, code, and HTML are not rewritten. Unknown or
inaccessible references remain literal. Link labels retain the author's
wording; the UUID target survives renaming.

Title and description autosave after a 700 ms pause. Title blur flushes a
pending save; property choices save immediately. Out-of-order responses do not
regress a newer revision. Conflicts retain the local draft for explicit
reconciliation.

Recovery drafts are scoped by account, task, field, and browser tab. They are
local, not synchronized across devices. A recovered draft is not evidence of a
successful server save. If browser storage is unavailable and navigation would
lose unsaved text, the application asks before leaving.

## Queries and pagination

Lists use 50-item cursor pages. Ordinary lists show board roots; explicit
parent queries show direct children across boards. `all_depths` selects a flat
hierarchy and cannot be combined with a parent. Filters match any selected
value within a category and intersect across categories. Filtering does not
promote matching children into an ordinary root query.

Sorting occurs before pagination. Task number breaks equal-key ties; status
sorting follows workflow order. Cursor pages are separate live reads, so
concurrent changes can move a task across a boundary. The browser deduplicates
overlapping pages by task UUID and preserves its loaded window during refresh.

Global search covers all depths and both boards. It is distinct from the local
board filter. Archived content is excluded unless explicitly requested.

## Views and display state

Personal views belong to an account, workspace, and board. Names are distinct
within that scope, have 1–60 characters, and have a limit of 50 views. At
least one view remains. Editing a personal view requires workspace access, not
task editing permission, and compares its revision.

Filters and display choices are saved on the server. A draft remains while
switching views during a visit; reload restores the saved baseline. Search
text is temporary. Column order and percentage widths are browser-local per
preset and do not make the saved view dirty. Hidden columns retain their local
layout.

The display representation has these fields:

| Field | Accepted values |
| --- | --- |
| `mode` | `table`, `board` |
| `columns` | Up to six of `status`, `assignees`, `priority`, `size`, `type`, `release` |
| `sort` | `number`, `title`, `status`, `created`, `updated`, `priority`, `type`, `size` |
| `direction` | `asc`, `desc` |
| `group` | `none`, `status`, `assignee`, `agents`, `priority`, `type`, `size`, `release` |
| `density` | `comfortable`, `compact` |

Grouping and filtering apply before pagination and each group pages
independently. A task can appear in several assignment groups. Assignee
grouping combines a human with their agents; agents grouping is relative to
the authenticated human owner. Retained assignments to unavailable accounts
remain visible.

Dragging changes the property represented by the destination. Assignment moves
remove only the source account in agents grouping, or the source human and
their agents in assignee grouping. Other assignments survive. Unassigned means
no direct assignments remain; dropping there does not silently clear unrelated
assignments.

## Browser navigation and access changes

Opening a task retains its board and view beneath the detail surface. A wide
workbench uses a side panel, intermediate widths use a modal, and phones use a
full-screen surface. Browser history identifies the selected task. Resizing or
changing presentation preserves mounted drafts.

Temporary connection failures retain loaded work and retry the revision feed.
Access loss and expired sessions clear or redirect the affected view.
Superseded reads are canceled, and an old poll cannot roll back a newer focus
refresh. The server rechecks access independently of the browser's cached
capability list.

## Persistence

Task mutations serialize with workspace authority changes. Number allocation,
cycle checks, field versions, activity, and refresh revisions commit together.
Read snapshots include authorization. PostgreSQL constraints guard workspace,
parent, and status relationships; clients do not cache authoritative grants.
