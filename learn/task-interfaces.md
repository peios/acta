# Task workflows for CLI and MCP

ACT-58 brings the terminal and agent interfaces up to date with workspace tasks.
Both use the same authorized task services as the browser. They do not expose
site administration or saved browser display presets.

## Find, inspect, change

Start with workspace discovery, then narrow a task list. Inspect a task before
editing so its field versions and surrounding work are available.

```sh
acta2 workspace list
acta2 task list -w acta --state unfinished --sort updated --direction desc
acta2 task list -w acta --query "login"
acta2 task get ACT-151
acta2 task edit ACT-151 --field title --version 3 --value "Improve login feedback"
acta2 task create -w acta --title "Check keyboard navigation" --parent ACT-151
acta2 task create -w acta --title "Review" --description-file review.md
cat review.md | acta2 task edit ACT-151 --field description --version 2 --value-file -
acta2 task edit ACT-151 --field description --version 3 --clear
acta2 task edit ACT-151 --field assignees --version 1 --assignee ACCOUNT_UUID
acta2 task edit ACT-151 --field assignees --version 2 --clear
```

Versions above are illustrative; use the actual version of the field returned by
get or the previous successful mutation. `--profile`/`-p` works on every command.
`--json` writes the complete structured result to stdout. Errors go to stderr
and exit nonzero; API errors preserve `status`, `code`, `message`, optional
`fields` and optional `current` conflict details. CLI argument errors use
`command_error`. Human output uses tables for lists and readable task details;
terminal control characters in task text are neutralized.

Workspace arguments accept UUIDs or exact current/previous slugs, case
insensitively. A UUID-shaped argument is interpreted as a UUID. Display names
are not guessed. Task arguments accept UUIDs or references such as `ACT-151`,
including reserved previous prefixes. Responses always include stable UUIDs and
current references. Status and assignee mutations accept UUIDs, discovered with
`task statuses` and `task people --query ...`. People search returns at most 50
candidates; narrow the query rather than guessing between similar names.

## Lists and inspection

`task list` and MCP `tasks_list` return at most 50 compact summaries: UUID,
workspace UUID, reference, title, named status, parent, direct assignees and
child count. Descriptions, field versions and descendant assignment sources
belong to inspection rather than search output.

- Without a parent, only root tasks are listed, including when filtering.
- `--parent ACT-151` selects only its direct children.
- State defaults to `all`; `unfinished` and `completed` use the workspace's
  configured completed status. Each task's own status is decisive.
- `--status` matches any selected status; `--assignee` matches any selected
  direct account assignment. `--unassigned` is ORed with selected assignees.
  Status, assignment, state and text query categories intersect.
- Sort by `number`, `title`, `status`, `created` or `updated`, in `asc` or `desc`
  direction. Default is task number descending. Number breaks equal-sort ties;
  status sorting follows configured status order.
- Follow `cursor` while `more=true`, passing `--cursor` with the same query.
  `total` counts all matching tasks before pagination. Pages are independent
  reads: concurrent edits can move tasks across a sort boundary.

`task groups -w acta --group assignee` discovers human grouping IDs, combining
each human's direct assignments with their agents. `--group agents` returns
Unassigned, Assigned to me and the current human's agents. For an agent caller,
the human is its owner. Use the returned ID with list's `--group` and
`--group-id`. Agents grouping excludes assignments solely to other humans and
their agents. Tasks assigned to several people can appear in several groups.

`task get` returns the description as Markdown, direct and descendant assignees,
ancestor references, timestamps and per-field versions. Its `subtasks` page
contains direct children in number-ascending order, including completed ones.
Continue with `task get ACT-151 --subtask-cursor CURSOR`. Grandchildren are not
flattened into that page.

## Explicit changes and conflicts

Editable fields are `title`, `description`, `status_id`, `parent_id` and
`assignees`. Each mutation changes one field with its expected version.
Omitting a value or supplying null is an error. Empty string clears description
or parent; `[]` clears assignments. Assignment input is the complete replacement
set and does not start an agent. Parent changes preserve task identity.

If the field has changed since inspection, the service returns:

```json
{
  "error": {
    "code": "conflict",
    "message": "This task field changed. Review its current value before retrying.",
    "current": {"field": "title", "version": 4, "value": "Another person's title"}
  }
}
```

Reconcile your intended edit with that value before retrying with version 4.
Unrelated field edits do not conflict. Retrying the already-saved value succeeds
without incrementing its version. Creation is not idempotent: after an uncertain
response, search/inspect before retrying to avoid duplicate tasks.

## MCP tools

| Tool | Workflow role | Grant |
| --- | --- | --- |
| `whoami` | Current authenticated account | `identity.read` |
| `workspaces_list` | Find workspace UUIDs/slugs; query and offset pagination | `tasks.read` |
| `task_search` | Ranked search across workspaces and every subtask depth | `tasks.read` |
| `tasks_list` | Compact filtered/sorted task pages | `tasks.read` |
| `task_get` | Full inspection and direct-subtask page | `tasks.read` |
| `task_activity` | Grouped task history; read-only, older-page cursor | `tasks.read` |
| `task_statuses` | Status UUIDs and creation/completed defaults | `tasks.read` |
| `task_people` | Search assignable humans and agents | `tasks.read` |
| `task_groups` | Resolve assignee/agents group IDs | `tasks.read` |
| `task_create` | Create task or subtask | `tasks.write` |
| `task_update` | Update one explicit field with its version | `tasks.write` |

Task tools return `{"data": ...}` on success. Service errors return
`{"error": ...}` and set `isError`; structured JSON is also emitted as text for
clients without structured-content support. Tool schemas describe inputs and
outputs. Schema/protocol errors can use the SDK's own error representation.
`whoami` returns its identity directly.

Example calls:

```json
{"workspace":"acta","state":"unfinished","sort":"updated","direction":"desc"}
```

Use that with `tasks_list`, then inspect with `task_get`:

```json
{"task":"ACT-151"}
```

An explicit `task_update` for text or assignments:

```json
{"task":"ACT-151","field":"title","version":3,"text":"Improve login feedback"}
{"task":"ACT-151","field":"assignees","version":2,"assignees":[]}
```

Tool grants and workspace permissions both apply. Existing identity-only
connections do not acquire task access automatically: reconnect and approve the
new grants. Disabled accounts, revoked sessions, lost workspace access and
agent permission changes are checked again during each operation. Error
classification is shared with HTTP and never forwards unknown internal errors.

## Validation

On 2026-09-07, `make check` passed: production frontend build, formatting,
31 frontend tests, zero Svelte errors/warnings, Go vet and Go unit tests.
The complete PostgreSQL race suite passed (integration package 39.581s), using
isolated schemas. Checks exercised every task tool through the MCP Go SDK,
real Cobra commands with temporary profiles, stdin Markdown, explicit clears,
structured conflicts and reconciliation, slug aliases, root-only filtering,
50-item cursor pages, bounded direct-subtask inspection and live permission/grant
revocation. Client tests also preserve large integer versions/counts and encode
queries without changing their meaning. No live development tasks were created
or edited by these checks.

The existing desktop MCP connection remained identity-only. Task tool behavior
was verified through the SDK with fixture OAuth approvals; a fresh desktop
consent for task access remains a separate user action.

Linux server/client builds and Windows amd64 client cross-compilation passed.
The installed `acta2` command points to the rebuilt client. CLI help and JSON
argument-error output were checked from the executable. The rebuilt server is
running on localhost:8081; web and OAuth discovery checks returned HTTP 200.

See [Task comments and replies](comments.md) for threaded discussions, read state and CLI/MCP comment commands.

## Task metadata

HTTP task create accepts `priority`, `type`, `size`; task edits use those field names
and their independent versions. Responses (including summaries) contain all three.
GET list queries accept repeated `priority`, `type`, `size` parameters. MCP `tasks_list`
uses `priorities`, `types`, `sizes` arrays. All match any selected value within the
category and intersect across categories. Sort accepts `priority`, `type`, `size`;
`task_groups` accepts the same names and returns the fixed options for group queries.

CLI examples:

```sh
acta2 task create -w acta --title "Repair reconnect" --priority high --type bug --size m
acta2 task list -w acta --priority high,urgent --type bug --sort priority --direction desc
acta2 task edit ACT-151 --field size --version 1 --value l
acta2 task edit ACT-151 --field priority --version 2 --clear
```

MCP `task_create` accepts the same singular property names. `task_update` uses
`field`, `version` and `text`, with `none` (or explicit empty text) to clear.
See [Tasks](tasks.md#optional-task-properties-act-88) for the fixed values and semantics.

## Global search

`acta2 task search "permission approval"` searches all accessible workspaces.
Add `--workspace acta` to narrow the scope, or `--cursor <returned cursor>` to
continue. `--json` returns the same compact contract as MCP `task_search` and
`GET /api/tasks/search?q=permission+approval&workspace=acta`. See
[Task search](task-search.md) for ranking, excerpts and pagination semantics.

## Archiving

`task_archive` and `task_restore` (MCP `tasks.write`) take a task UUID/reference and
its current `versions.archived`. CLI equivalents are `task archive <ref> --version N`
and `task restore <ref> --version N`. List archived roots with `tasks_list`'s
`archived: true` or CLI `task list --archived`; ordinary lists omit them. Search
includes archived matches only with `include_archived: true` / `--include-archived`.
See [the archiving contract](task-archiving.md) for subtree restoration and read-only
behavior. Completion never automatically archives a task.
