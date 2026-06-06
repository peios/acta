package mcpcfg

import "github.com/peios/acta/internal/store"

// DefaultGuide is the built-in conventions document served as acta://guide when
// no custom guide is set. It is deliberately deployment-neutral — operators add
// their own project specifics by editing the guide in Settings.
const DefaultGuide = `# Using Acta

Acta is a shared project tracker. You're connected to it as an agent over MCP.
This guide is the source of truth for how to work in Acta — read it before you
create or change anything.

## Orient yourself first

- Call ` + "`whoami`" + ` to learn who you're acting as. You may be a **human** principal
  or an **agent** principal that belongs to a human (your "owner").
- Call ` + "`list_workspaces`" + ` to see the workspaces available. Items always live in
  one workspace, addressed by its ` + "`slug`" + `.
- Call ` + "`list_items`" + ` to read the board before you change it. Don't re-create work
  that's already tracked.

## The model

- **Item** — the unit of work: a task, bug, idea, or milestone. Has a title, an
  optional markdown **description**, a **status**, an optional **assignee**, and
  an optional **parent**.
- **Status** — which column an item sits in (e.g. To do / In progress / Done).
  Statuses are defined per workspace, so the names differ per board; read them
  with ` + "`list_statuses`" + ` — don't guess the names. A lane can carry a
  **checklist**: facts (e.g. "Provium tests", "Learn docs") that must be confirmed
  true before an item may enter it. ` + "`list_statuses`" + ` shows a lane's
  ` + "`required_facts`" + `; to move there, pass the ones you confirm as
  ` + "`set_item_status`" + `'s ` + "`checklist`" + `. The move is rejected, naming what's
  still required, until the checklist is satisfied — only confirm facts you've
  actually verified; the confirmation is recorded against you.
- **Milestone** — an item flagged as a milestone: an anchor point the project is
  steering toward, with ordinary tasks hanging off it. Toggle with
  ` + "`set_item_milestone`" + `.
- **Parent / subtask** — items nest. Break large work into a parent with child
  subtasks via ` + "`set_item_parent`" + `; the parent's progress rolls up from its
  children.
- **Assignee** — the principal responsible. Both humans and agents can be
  assignees. Set with ` + "`set_item_assignee`" + `.
- **Project** — a cross-cutting initiative within a workspace that groups related
  items (e.g. all "Peinit" work), independent of their board, status, or parent.
  Addressed by ` + "`slug`" + `; list them with ` + "`list_projects`" + `, and file an item under
  one with ` + "`set_item_project`" + ` (or ` + "`create_item`" + `'s ` + "`project`" + ` argument). A project
  is narrower than a workspace (which is the whole board) and longer-lived than a
  milestone.
- **Release** — a versioned cut-line a workspace ships at (e.g. "v0.27.0"),
  addressed by ` + "`name`" + `. Unlike a project (an open-ended theme), a release is
  stateful: **planned → active → shipped**. List them with ` + "`list_releases`" + `, add an
  item with ` + "`set_item_release`" + ` (or ` + "`create_item`" + `'s ` + "`release`" + ` argument), and advance
  the lifecycle with ` + "`set_release_status`" + ` — shipping freezes it as a changelog
  entry. An item belongs to one release at a time.

## Humans and agents

Each human can have agents acting on their behalf; agents appear nested under
their owning human. When you assign work, assign to a **human** when a person
must own or decide it, and to an **agent** when carrying it out is the agent's
job. ` + "`whoami`" + ` tells you which you are — assign work you're actually doing to
yourself when that's the honest answer.

## How to behave

- **Acta is the canonical record.** Track work here — not in scratch files, a
  TODO.md, or chat-only notes. If it's worth remembering as work, it's an item.
- **Read before you write.** List the board, look for an existing item, and
  prefer updating it over creating a near-duplicate.
- **Keep items current.** Advance status as work progresses — a board that
  reflects reality is the whole point.
- **Description is the spec; comments are the narrative.** Put the durable what
  and why in the markdown description; use ` + "`add_comment`" + ` for progress notes and
  decisions over time.
- **Archive, don't delete.** Finished or abandoned work gets ` + "`archive_item`" + `
  (reversible via ` + "`unarchive_item`" + `), so history is preserved.

## Staying in the loop

People talk to you through Acta by **@mentioning** you in a comment — that drops
an entry in your notification inbox. This is the channel humans use to ask you to
pick something up or weigh in on a thread.

- Call ` + "`list_notifications`" + ` to poll your inbox. It returns your unread
  notifications, newest first, each pointing at the item and comment that
  mentioned you. An idle agent can poll this to learn when it's been pinged.
- Read the item with ` + "`get_item`" + ` for full context, then respond in the thread
  with ` + "`add_comment`" + ` (mention someone back with ` + "`@username`" + ` to notify them).
- Once you've handled one, call ` + "`mark_notification_read`" + ` with its id to clear
  it from the unread set, so your inbox reflects only what still needs attention.

Your inbox also gathers **activity notifications** from your *subscriptions* —
standing interests in an item, a project, or a principal. Following a subject
delivers its activity to your inbox without anyone @mentioning you (these rows
have a ` + "`verb`" + ` and ` + "`summary`" + ` rather than a comment excerpt). You're
auto-subscribed to items you create, comment on, or are assigned; to projects you
create; and to your own agents. Manage them with ` + "`list_subscriptions`" + `,
` + "`subscribe`" + `, and ` + "`unsubscribe`" + ` — e.g. ` + "`subscribe`" + ` to another agent with all
five event categories to watch everything it does.

When you need an answer before you can continue — a decision, a review, a
go-ahead — don't busy-poll the board. Ask in a comment and block on the reply:

- Post your question with ` + "`add_comment`" + ` (` + "`@mention`" + ` whoever should answer, so
  their bell lights up), then call ` + "`watch_comments`" + ` with ` + "`after`" + ` set to that
  comment's id.
- ` + "`watch_comments`" + ` returns the moment a new comment lands, or an empty list
  after ~25s — so loop it, advancing ` + "`after`" + ` to the returned cursor each call,
  until you get a reply you can act on. You decide what counts as an answer.

## Tool map

| Goal | Tool |
| --- | --- |
| Who am I? | ` + "`whoami`" + ` |
| What workspaces exist? | ` + "`list_workspaces`" + ` |
| What projects exist? | ` + "`list_projects`" + ` |
| What releases exist? | ` + "`list_releases`" + ` |
| What columns exist? | ` + "`list_statuses`" + ` |
| Read the board | ` + "`list_items`" + ` |
| Read one item in full | ` + "`get_item`" + ` |
| Create an item | ` + "`create_item`" + ` |
| Move its column | ` + "`set_item_status`" + ` |
| Assign it | ` + "`set_item_assignee`" + ` |
| Edit the description | ` + "`set_item_description`" + ` |
| Flag / unflag a milestone | ` + "`set_item_milestone`" + ` |
| Nest under a parent | ` + "`set_item_parent`" + ` |
| Start a project | ` + "`create_project`" + ` |
| File an item under a project | ` + "`set_item_project`" + ` |
| Start a release | ` + "`create_release`" + ` |
| Add an item to a release | ` + "`set_item_release`" + ` |
| Ship / advance a release | ` + "`set_release_status`" + ` |
| Add a progress note | ` + "`add_comment`" + ` |
| Ask and wait for a reply | ` + "`add_comment`" + ` + ` + "`watch_comments`" + ` |
| Retire / restore | ` + "`archive_item`" + ` / ` + "`unarchive_item`" + ` |
| Poll your inbox (@mentions + activity) | ` + "`list_notifications`" + ` |
| Clear one once handled | ` + "`mark_notification_read`" + ` |
| List what you follow | ` + "`list_subscriptions`" + ` |
| Follow a subject | ` + "`subscribe`" + ` |
| Stop following | ` + "`unsubscribe`" + ` |
`

// DefaultPrompts are the starter prompts seeded on first run. They are ordinary
// editable rows once seeded — an operator can reword, delete, or add to them.
// Both reference acta://guide so they inherit the operator's conventions.
var DefaultPrompts = []store.MCPPrompt{
	{
		Name:        "standup",
		Title:       "Standup",
		Description: "Summarise my open items and suggest what to pick up next.",
		Arguments: []store.MCPPromptArg{
			{Name: "workspace", Description: "Workspace slug to scope to (blank = the default board)"},
		},
		Body: `Give me a standup in Acta.

Read the conventions in the ` + "`acta://guide`" + ` resource first if you haven't.
Target workspace (blank = use list_workspaces and pick the default board): {{workspace}}

1. Call ` + "`whoami`" + `, then ` + "`list_items`" + ` filtered to me.
2. Call ` + "`list_activity`" + ` to see what's actually changed recently across the board.
3. Group my open items by status, most-recently-active first (use the activity log for recency).
4. Flag anything stale or stuck — no recent activity, or sitting in progress too long.
5. Recommend the one or two items I should pick up next, and why.

Keep it tight — this is a quick orientation, not a report.`,
	},
	{
		Name:        "triage",
		Title:       "Triage",
		Description: "Walk un-triaged items and help sort them.",
		Arguments: []store.MCPPromptArg{
			{Name: "workspace", Description: "Workspace slug to scope to (blank = the default board)"},
		},
		Body: `Help me triage the backlog in Acta.

Read the conventions in the ` + "`acta://guide`" + ` resource first if you haven't.
Target workspace (blank = use list_workspaces and pick the default board): {{workspace}}

1. List items that are unassigned or sitting in the first/"inbox" status.
2. For each, propose one of: an assignee, a status, flag as a milestone, or
   archive if it's stale or already done.
3. Show me your proposed changes as a short list and wait for my go-ahead
   before applying them with the set_* / archive tools.

Work in small batches; don't make changes I haven't confirmed.`,
	},
}
