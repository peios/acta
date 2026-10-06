# Releases

ACT-15 adds workspace releases, so work can be sequenced into named cuts. A task
can target one release. Lists and boards filter and group by release, and each
release reports how many of its tasks are finished.

## Release records

A release belongs to one workspace and has:

- a **name**, 1–60 characters, unique in the workspace case-insensitively
  (for example `2026.9`);
- an optional **codename**, at most 60 characters (for example `Bedrock`);
- a **state**;
- Markdown **notes**, at most 100,000 bytes, which also serve as the release
  notes. Task references in the notes are linked, as in task descriptions;
- one **version** for the whole record.

| State | Meaning |
| --- | --- |
| `planned` | The release exists and work can target it, but nobody is working toward it yet. The default. |
| `open` | The release is taking work. Several releases in a workspace can be open at once. |
| `frozen` | No new work starts; only work already in flight finishes. |
| `released` | The release has shipped. |

Any transition is allowed, including backwards (for example frozen → open), so a
mistaken transition can be undone. States are informational: Acta does not stop
a task from targeting a frozen or released release.

Releases cannot be deleted, so a task's target never dangles. Rename a release
instead of replacing it.

Releases are listed in natural name order everywhere: runs of digits compare as
numbers and other text compares case-insensitively, so `2026.8` < `2026.9` <
`2026.10`. There is no manual ordering.

Creating and editing releases requires the workspace's **Manage releases**
permission (`tasks.releases.manage`). Workspace creators receive it with every
other workspace permission, and site superusers hold it everywhere. Existing
members are not granted it automatically. Reading releases needs only workspace
access.

Edits change only the supplied fields and require the release's current
version. An unchanged retry succeeds without a new version. A stale edit fails
with `release_changed` (HTTP 409); read the release again and reconcile.

## Progress

A release reports `total` and `finished`. Both count active (unarchived) tasks
that target the release directly, at any depth. A task is finished when it holds
its own board's completed status. The Backlog board has no completed status, so
Backlog tasks count toward `total` only.

## Target release on tasks

`release_id` is an optional task field with its own field version, like
`parent_id`. An empty value clears it. It is set directly on each task and is
**not inherited**: subtasks of one parent can target different releases. The
release must belong to the task's workspace. Setting it requires Edit tasks.

Tasks, summaries and search results carry a compact `release` object with the
release's `id`, `name`, `codename` and `state`, or `null`. Readers can see that a
release is frozen without a second request. Changes appear in task activity with
the release names before and after.

## Filtering and grouping

List queries accept release selections: release UUIDs, or `none` for tasks
without a release. They match any selected value and intersect with the other
filter categories. Group by Release returns a `none` group named "No release"
first, then every release in natural order.

Like other filters, release filters match one hierarchy level: roots by default,
or a parent's direct children. Set `all_depths` to match tasks at every depth
instead, which is how to list everything targeted at a release. `all_depths`
cannot be combined with a parent.

Saved views store release selections in `filters.releases`. Display options
accept `release` as a Group by value and as an optional column.

## Web

**Releases** sits in the workspace navigation beside Tasks and Backlog. The
Releases page lists every release in natural order with its codename, state and
finished/total progress; released releases are dimmed. Accounts with Manage
releases see **New release**, which asks for a name, an optional codename and a
starting state.

Opening a release shows its progress, notes and tasks. Managers can switch its
state with the state control (any direction), rename it, and edit its notes.
A stale save reloads the latest release and says so, rather than overwriting
another change. The task list shows every task that targets the release
directly, at any depth and on either board, as a flat list; opening a task goes
to it on its own board.

On a task, **Release** sits beside Priority, Type and Size. Its label shows the
target release's state, for example "Release · Frozen". The filter menu has a
Release section (including None), Display offers Release as a Group by option
and as a column, and board cards show the release when that column is selected.
Grouping the board by release gives a No release column first; dragging a card
between columns changes its release, and creating a task in a release column
targets it.

## Interfaces

HTTP:

| Method and path | Purpose |
| --- | --- |
| `GET /api/workspaces/{workspace}/releases` | `{"releases": [...]}`, in natural order, with notes and progress. |
| `POST /api/workspaces/{workspace}/releases` | Create from `name`, optional `codename`, `state` and `description`. Returns 201. |
| `GET /api/workspaces/{workspace}/releases/{release}` | One release. |
| `POST /api/workspaces/{workspace}/releases/{release}` | Update the supplied `name`, `codename`, `state` and `description`, with `version`. |

Task list queries accept repeated `release` parameters and `all_depths=true`.
Task creation accepts `release_id`; task edits use field `release_id`.

MCP:

| Tool | Purpose | Grant |
| --- | --- | --- |
| `releases_list` | Releases with state and progress, without notes | `tasks.read` |
| `release_get` | One release, including notes | `tasks.read` |
| `release_create` | Create a release | `tasks.write` |
| `release_update` | Change the supplied fields with the current version | `tasks.write` |

`tasks_list` accepts `releases` and `all_depths`; `task_create` accepts
`release_id`; `task_update` accepts field `release_id`; `task_groups` accepts
`release`.

CLI:

```sh
acta release list -w acta
acta release create -w acta --name 2026.9 --codename Bedrock --state open --notes-file notes.md
acta release edit RELEASE-UUID -w acta --state frozen --version 3
acta task list -w acta --release RELEASE-UUID --all-depths --state unfinished
acta task edit ACT-151 --field release_id --version 1 --value RELEASE-UUID
acta task edit ACT-151 --field release_id --version 2 --clear
```
