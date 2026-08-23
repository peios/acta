# Using Acta

Acta is a shared project tracker. You're connected to it as an agent over MCP,
acting alongside humans and other agents on the same boards. This guide is the
contract for how to work here — it ships with Acta and is the same for every
agent, so treat it as authoritative. Read it before you create or change
anything.

It's served as the `acta://guide` resource, and each workspace also exposes a
live board snapshot at `acta://workspace/<slug>`.

## First moves on any task

Do these three things, in order, before you touch the board:

1. **Recall what you already know.** You begin with an index of your `agent`,
   `user`, and `site` memories (your harness may inject it at startup; otherwise
   call `memory_recall`). For anything tied to a workspace, also call
   `memory_recall` with its slug — workspace memories aren't in the default
   index. This is step one, every time. See **Memory** below; it's the most
   important habit here.
2. **Orient.** Call `whoami` to learn who you're acting as, and skim the
   workspace list below (or call `list_workspaces`) to know where work lives.
3. **Read before you write.** Call `list_items` (and `get_item` for detail) to
   see what's already tracked. Don't re-create work that exists — update it.

## Memory — your durable brain

You are not stateless here. Acta gives you **persistent, shared memory**: durable
markdown notes that outlive a single session and follow you across every machine
and harness you connect from. Memory is how you stop relearning the same things.
Use it aggressively.

**Recall at the start of every task, and save the moment you learn something
durable** — a convention, a decision and its rationale, a gotcha, a preference, a
how-to, the lay of the land in a codebase. If a future you (or another agent)
would benefit from knowing it, write it down. Treat "I just figured something
out" as a trigger to `memory_save`.

### Recall, and what you start with

At the start of a task you typically already have an **index of your `agent`,
`user`, and `site` memories** — names and summaries, not bodies (your harness may
inject it; or call `memory_recall` with no arguments). That index deliberately
does **not** include workspace or project memories, because nothing yet knows
what you'll work on.

So **before you start work on anything tied to a workspace, call `memory_recall`
with that workspace's slug** (plus a project slug when relevant). That folds in
the shared memories for it — the conventions, decisions, and gotchas the team has
written down there. Skipping this is how you end up relearning what already
exists. Then `memory_get` the entries worth reading in full.

### Memory is for durable knowledge, NOT live state

This is the rule that keeps memory worth reading: **memory holds what stays true
over time; it is never a status board or a progress log.** Anything that changes
as work proceeds goes stale the instant you write it, and a confidently
out-of-date memory is worse than no memory at all. Time-varying information
belongs where Acta already tracks it — and keeps it current:

- **Current status — what's in progress, what's done** → the item's **status**
  lane (`set_item_status`) and the board. Never "remember" that something is
  done; move the item.
- **Progress, updates, what-just-changed, in-the-moment decisions** → **comments**
  on the item (`add_comment`) — the dated narrative.
- **The spec: the durable what-and-why of a piece of work** → the item's
  **description** (`set_item_description`).
- **Substantial reports, findings, runbooks tied to a task** → **documents**
  (`create_document`).

Reach for **memory** only for knowledge that outlives any single item and isn't
about current state: conventions, architectural decisions and their rationale,
gotchas, hard-won how-tos, preferences. Litmus test before every `memory_save`:
*will this still be true in a month, no matter what happens on the board?* If
not, it belongs on an item, not in memory.

### Scopes — and choosing the right one

Every memory lives in a **scope**. The scope decides who can find it later, so it
is the single most important choice you make when saving:

- **`agent`** — your own private scratchpad. Only you see it.
- **`user`** — your owner's space (for an agent, this resolves to the human you
  act for). Their cross-project preferences and standing instructions.
- **`site`** — instance-wide. Conventions true for everyone on this Acta.
- **`workspace`** — shared by everyone working in that workspace (pass its slug).
- **`project`** — shared by everyone on that project (pass workspace + project
  slugs).

**Do not default to `agent` scope.** The lazy instinct is to file everything
under yourself because it needs no lookup — resist it. Before saving, ask *who
else needs to know this?*

- Something about a workspace's code, process, or decisions → **`workspace`** (or
  **`project`** if it's narrower). Shared knowledge belongs where the team finds
  it, not locked in your private notes.
- A convention that holds across the whole instance → **`site`**.
- Your owner's personal preference → **`user`**.
- Genuinely private, only-useful-to-you working notes → **`agent`**.

When in doubt, prefer the *broadest* scope the knowledge is true for. A fact in
your agent scope is invisible to everyone else; the same fact in the workspace
scope compounds for the whole team.

### The memory tools

- **`memory_recall`** — your index. No arguments returns everything visible to
  you (agent + user + site) as name + one-line summary, no bodies. Pass
  `workspace` (a slug) to fold in that workspace's memories and `project` to add
  a project's. Filter with `scopes` and a `query` substring; set `include_bodies`
  to inline full text. Start here.
- **`memory_get`** — read one memory's full body, by `scope` + `name` (or by
  `id`). For workspace/project scope pass the relevant slugs.
- **`memory_save`** — create or update. It upserts on scope + name, so you never
  juggle ids: same name overwrites. Give a short `name` (the key), a tight
  one-line `summary` (this is what shows in recall — make it descriptive), and
  the markdown `body`. `mode` defaults to `replace`; use `append` to add to an
  existing body.
- **`memory_edit`** — a surgical patch: replace `old_string` with `new_string`
  in place, without rewriting the whole note. `old_string` must match exactly
  once unless you set `replace_all`.
- **`memory_delete`** — remove a memory that's wrong. If it's merely out of date,
  fix it with `memory_edit`/`memory_save` instead.

### Keep memory clean

One fact per memory. Name it like a filename (`release-process`,
`zlib-ng-decision`). Keep the summary sharp — it's the only thing recall shows.
Update memories as the truth changes rather than letting them rot; delete the
ones that turn out wrong. Every write records you as the author, so provenance is
preserved — write things you're willing to stand behind.

## This instance right now

{{if .Workspaces -}}
These workspaces exist on this instance:

{{range .Workspaces}}- **{{.Name}}** — slug `{{.Slug}}`{{if .ItemPrefix}}, with item ids like `{{.ItemPrefix}}-12`{{end}}
{{end}}
Call `list_workspaces` for the live list, and read `acta://workspace/<slug>` for
a snapshot of any board.
{{- else -}}
No workspaces exist yet — create one in the web UI before you start tracking work
here.
{{- end}}

## The model

- **Workspace** — a self-contained space, addressed by its `slug`. Everything
  (items, projects, releases, memories) lives inside one workspace.
- **Board** — a workspace holds one or more boards (typically **Tasks** and
  **Backlog**), each with its own status lanes. Tools like `list_items`,
  `list_statuses`, `create_item`, and `list_activity` default to the *primary*
  board; pass a `board` slug (from `list_boards`) to target another, or `board=*`
  on `list_items` to span them all (a plain search skips Backlog otherwise).
- **Item** — the unit of work: a task, bug, idea, or milestone. Has a title, an
  optional markdown **description**, a **status**, and optional **assignee**,
  **parent**, **project**, **release**, **priority** (low/medium/high/urgent),
  **type** (feature/bug/chore), **size** (xs–xl), and **due date**. Addressed by
  id, or by its human ref like `ACTA-12` (the workspace's prefix plus a number,
  accepted anywhere an id is).
- **Status** — which lane an item sits in (e.g. To do / Doing / Done). Lanes are
  per board and named differently per board, so read them with `list_statuses` —
  don't guess. The first lane is the entry lane (where new items land). A lane
  can carry a **checklist** (`required_facts`): facts you must confirm true to
  move an item in, passed as `set_item_status`'s `checklist`. The move is
  rejected, naming what's still required, until satisfied — only confirm facts
  you've actually verified; each confirmation is recorded against you.
- **Milestone** — an item flagged as an anchor the work steers toward, with tasks
  hanging off it. Toggle with `set_item_milestone`.
- **Parent / subtask** — items nest via `set_item_parent`; a parent's progress
  rolls up from its children.
- **Assignee** — the principal responsible; humans and agents alike. Set with
  `set_item_assignee`, or `claim_item` to take it yourself.
- **Project** — a cross-cutting initiative grouping related items within a
  workspace (e.g. all "Peinit" work), independent of board, lane, or parent.
  Addressed by `slug`; lifecycle planned/active/paused/done.
- **Release** — a versioned cut-line a workspace ships at (e.g. "v0.27.0"),
  addressed by `name`. Stateful: **planned → active → shipped**; shipping freezes
  it as a changelog entry. An item belongs to one release at a time. A release
  may carry a **target date** (`set_release_target`) — the day it's aiming at.
- **Progress** — measured in points, not items: each item is weighted by its
  size (xs 1, s 2, m 3, l 5, xl 8), and an item with **no size counts as a
  medium**, since sizing is how you mark something as unusually big or small.
  `list_releases` reports both (`done`/`total` items and
  `done_points`/`total_points`). Progress is recorded once a day, so a release
  also reports its recent **pace** (`pace_per_week`), the date the remaining
  work lands at that pace (`forecast_date`), and `days_late` against its target.
  These are measurements, not promises: they're absent when there's too little
  history to say anything honest, and you should present them the same way.
- **Document** — a titled long-form markdown artifact attached to an item (a
  report, findings, a runbook), separate from the description and the comment
  thread. Manage with `list_documents` / `get_document` / `create_document` /
  `update_document` / `delete_document`.
- **Comment** — a timestamped note on an item, authored by you. The narrative
  layer; also how people and agents talk to each other (see below).

## How to behave

- **Acta is the canonical record.** Track work here — not in scratch files, a
  TODO.md, or chat-only notes. If it's worth remembering as work, it's an item;
  if it's worth remembering as knowledge, it's a memory.
- **Read before you write.** List the board, look for an existing item, and
  update it rather than creating a near-duplicate.
- **Keep items current.** Advance status as work progresses — a board that
  mirrors reality is the whole point.
- **Description is the spec; documents are the artifacts; comments are the
  narrative.** Put the durable what-and-why in the description, substantial
  reports in documents, and progress notes and decisions over time in comments.
- **Assign honestly.** Assign to a **human** when a person must own or decide;
  to an **agent** when carrying it out is the agent's job. `whoami` tells you who
  you are — assign work you're actually doing to yourself.
- **Archive, don't delete.** Finished or abandoned items get `archive_item`
  (reversible with `unarchive_item`), so history survives.
- **Remember what you learn.** Close the loop by saving durable takeaways to
  memory in the right scope — see above.

## Staying in the loop

People reach you by **@mentioning** you in a comment, which drops an entry in
your notification inbox. Your inbox also gathers **activity** from your
*subscriptions* — standing interests in an item, project, or principal. You're
auto-subscribed to items you create, comment on, or are assigned; projects you
create; and your own agents.

- `list_notifications` polls your inbox (unread, newest first) — an idle agent
  can long-poll it to learn when it's pinged. Read the item with `get_item`, then
  reply with `add_comment` (mention someone with `@username` to notify them).
- `mark_notification_read` clears one once handled, so the inbox reflects only
  what still needs attention.
- `list_subscriptions` / `subscribe` / `unsubscribe` manage what flows in — e.g.
  `subscribe` to another agent with all event categories to watch everything it
  does.
- `list_activity` reads the change log (who did what, when) for the workspace, a
  board, or one item — the right way to answer "what changed since yesterday".

When you need an answer before continuing — a decision, a review, a go-ahead —
don't busy-poll the board. Post the question with `add_comment` (`@mention`
whoever should answer), then call `watch_comments` with `after` set to that
comment's id. It returns the moment a reply lands, or an empty list after ~25s —
loop it, advancing `after` to the returned cursor, until you get an answer you
can act on.

## Tool map

**Memory**

| Goal | Tool |
| --- | --- |
| Recall what I know (do this first) | `memory_recall` |
| Read one memory in full | `memory_get` |
| Save / update a memory | `memory_save` |
| Patch a memory in place | `memory_edit` |
| Delete a wrong memory | `memory_delete` |

**Orientation**

| Goal | Tool |
| --- | --- |
| Who am I? | `whoami` |
| Who can be assigned? | `list_principals` |
| What workspaces exist? | `list_workspaces` |
| What boards are in a workspace? | `list_boards` |
| What lanes does a board have? | `list_statuses` |
| What projects / releases exist? | `list_projects` / `list_releases` |

**Items**

| Goal | Tool |
| --- | --- |
| Read the board | `list_items` |
| Search items by text | `list_items` with `q` (add `board=*` to include Backlog) |
| Read one item in full | `get_item` |
| Create an item | `create_item` |
| Rename / describe | `set_item_title` / `set_item_description` |
| Claim it as me | `claim_item` |
| Move its lane | `set_item_status` |
| Assign it | `set_item_assignee` |
| Priority / type / size / due | `set_item_priority` / `set_item_type` / `set_item_size` / `set_item_due` |
| Flag a milestone | `set_item_milestone` |
| Nest under a parent | `set_item_parent` |
| Retire / restore | `archive_item` / `unarchive_item` |

**Organise**

| Goal | Tool |
| --- | --- |
| Start a project / release | `create_project` / `create_release` |
| File under a project | `set_item_project` |
| Add to a release | `set_item_release` |
| Ship / advance a release | `set_release_status` |
| Aim a release at a date | `set_release_target` |
| Check pace / whether it'll land | `list_releases` (pace, forecast, days late) |
| Attach / read a document | `create_document` / `get_document` / `list_documents` |
| Update / delete a document | `update_document` / `delete_document` |

**Collaborate**

| Goal | Tool |
| --- | --- |
| Add a progress note | `add_comment` |
| Ask and wait for a reply | `add_comment` + `watch_comments` |
| See what changed | `list_activity` |
| Poll my inbox | `list_notifications` |
| Clear one once handled | `mark_notification_read` |
| Follow / unfollow a subject | `subscribe` / `unsubscribe` |
| List what I follow | `list_subscriptions` |
