---
title: "Older Turn Activity Summaries"
description: >-
  Collapsed tool activity, verified task and memory references, and live-
  output following.
---

After a newer turn starts, consecutive successful tool calls and completed
thinking from older turns render under an expandable **Worked for …** line.
The latest turn remains visible even after completion. Expanding shows the
original cards and their own details. Messages, approvals, questions, errors,
unknown frames, and unfinished work split summaries; running background tools
stay visible. Groups never cross a turn, provider run, or subagent lane.

Duration spans the first known activity start to the last completion, so
simultaneous tools are not double-counted. When a provider omits lifecycle
timing, the persisted conversation item's observed start/completion timestamps
supply the interval instead. If neither source has a complete interval the
line says **Worked**. The current turn comes from the persisted turn-start
singleton, so its older activity collapses before its first visible message
arrives. Grouping uses existing snapshots without rewriting history and
preserves member scroll anchors when older pages extend groups.

A child's Claude foreground-task notification can omit both
`parent_tool_use_id` and `task_type`. Its saved foreground task ID plus
`tool_use_id` route it to the owning subagent lane, as background-task
identities do. The lookup requires the current provider run and survives
adapter checkpoint restoration; unrelated identities remain Unknown Frames.
The ordinary child tool result still supplies output and completion.

## Task references in tool results

Agent tool cards recognize task UUIDs in known task arguments and JSON results
when the MCP connection name contains `acta` (case-insensitive). Claude's
`mcp__name__tool` and Codex's `name/tool` forms are supported, including JSON
MCP text/structured-result envelopes. Task results, lists, ancestors, and
explicit `task_id` fields can supply references. Prose, arbitrary object IDs,
and task numbers alone do not establish a link.

Before enabling a link, the browser verifies the UUID through this server's
task API using the signed-in user's permissions. Links use current titles and
references. Unresolved references remain plain text; there is no fallback to a
similarly numbered task or automatic cross-server navigation. Verification
reads are coalesced, limited to four concurrent requests and briefly cached
per page.

Opening a task preserves the mounted chat and draft. The shared task editor
uses the same resizable side panel, modal and mobile fullscreen layout as
Workspaces. The `task` query parameter preserves selection across refresh and
browser history. Subtask navigation, activity, reparenting, and editing use
the existing components and live workspace revision feed.

## Following live output

Scrolling upward pauses automatic following immediately, including small
wheel, touch or keyboard movements near the bottom. Incoming frames and layout
changes preserve the reading anchor without re-enabling following. Scrolling
back to the bottom resumes it; End explicitly returns to the live end. Queued
render updates cannot override a newer reader movement. Programmatic anchor
corrections do not count as user scrolling, and unchanged offsets are not
written because doing so interrupts native smooth scrolling.

The scroll intent rules have regression tests in
`web/tests/thread-scroll-intent.test.mjs`. For browser regression checks,
`web/tests/fixtures/ThreadScrollReview.svelte` renders the actual transcript
with rapid simulated output and older-page insertion. Temporarily mount it in
a local review route, check small upward scrolling, resuming at the bottom,
End, and history loading, then remove the route before the final build. No
provider calls or live conversation mutations are needed for this check.

Claude can promote a foreground Bash task with a task-only
`task_updated {patch: {is_backgrounded: true}}` event after a timeout. Acta
uses its recorded task identity to route this event into the original lane,
including a subagent lane, and keeps the tool running in the background even
when its launch receipt omits structured result metadata. Later killed/stopped
updates interrupt that tool using the provider's end time; they do not end the
owning turn. Repeated promotion or completion events do not resurrect work or
add duplicate notices. Unknown task identities and unreviewed patches remain
Unknown Frames. Previously stored debug frames are not rewritten by an adapter
upgrade.

Claude tool heartbeats identify the real call through `parent_tool_use_id` and
a synthetic heartbeat ID. Acta resolves the existing call in its current-run
lane, marks pending execution as running, and supplies a start-time estimate
from the reported elapsed time only when no start time exists. Repeated
heartbeats remain local debug frames; they neither create conversation rows
nor revive completed work. Unknown tool identities and malformed elapsed times
remain visible.

Claude Git push notifications render as compact "Pushed main" history items
with the repository directory name. Hover reveals the full directory and
timestamp. These use the provider-independent `vcs/push` frame (branch, cwd,
optional turn identity); other unreviewed Git event kinds remain Unknown
Frames.

## Memory references in tool results

Acta `memory_get`, `memory_save`, `memory_recall`, and `memory_delete` calls
also show verified reference chips and open memories without leaving the
conversation. They share the task viewer's responsive panel/modal/fullscreen
shell. See
[memories](~acta/technical-reference/work-records/memories#references-in-agent-conversations)
for recognition, access verification, navigation, and editing behavior.
