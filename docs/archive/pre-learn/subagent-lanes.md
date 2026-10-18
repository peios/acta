# Subagent conversations

A lane is the main conversation or a provider-native subagent conversation inside
an Acta thread. Subagents do not create additional Acta thread records or separate
provider processes. The parent provider retains ownership of execution.

## Navigation

The scrollable pill bar below the header contains Main, running or waiting
subagents, and the currently open subagent. A completed, stopped, or failed lane
stays visible while selected; switching away removes its pill. Open its card in
the parent's history to return to it. Each lane retains its loaded history,
scroll position, and browser-local draft while switching.

A compact parent card shows the child name, task when supplied, status, and elapsed
time. Completion produces a separate expandable result notice. Claude may deliver
that result into context later; the same notice moves to that position. Unknown
frames remain inspectable. Nested cards belong to their immediate parent lane.

Approval and question requests are collected across all lanes and identify their
origin. They can be answered from the composer popup regardless of which lane is
open, or from the request in its lane's history. Completing the main turn does not
resolve a child's request. Stop applies to the selected lane; the header's power
control still controls the whole provider process. An interrupted command may
remain running in the background according to the provider's normal behavior.

Current model, usage, working state, tools and turn summaries are scoped to the
selected lane. Unreported settings remain unknown. Direct messages and model or
permission changes are currently unavailable for child lanes: the tested Codex
multi-agent v2 API rejects direct input, and Claude's SDK does not expose a direct
child-message operation. The main agent can still communicate with its children
through native provider tools.

## Storage and reliability

Every frame may carry `lane_id`; an absent or empty ID means Main. Native child
identities are learned from attributed provider events, never accepted as arbitrary
browser-supplied provider session IDs. Child controls are bound to the current run.

The capture journal remains authoritative. Each child has an isolated, persisted
adapter checkpoint. Conversation items include a lane and use lane-scoped storage
keys, preventing collisions between tools, turns and messages with identical
provider IDs. Each lane has its own current-state projection. The root projection
also contains lane inventory and all pending interactions, so the pill bar and
approval popup do not depend on loading old history pages.

`GET /api/threads/{id}/conversation?lane={id}` uses the same reverse pagination
and revision updates as Main. Lane item updates, current state and raw frames are
written in one transaction. A projection rebuild reproduces lane history from
normalized frames. Switching lanes rejects late responses for the previous lane.
Disconnected or previous-run unfinished lanes are shown unavailable, not completed.

## Provider details

- Claude runs with `--forward-subagent-text`. `task_started` establishes the child;
  `parent_tool_use_id` attributes forwarded messages and tools. Permission requests
  use `request.agent_id`. Forwarding supplies completed content blocks, rather than
  the child's token-by-token stream. Native `stop_task` stops a selected child.
- Codex runs with `features.multi_agent_v2=true`. `subAgentActivity` and reviewed
  collaboration events establish lanes; subsequent child `threadId` events use
  the shared adapters with separate state. A read-only `thread/read` snapshot
  obtains reported child model/cwd/effort without inheriting the parent's values.
  `turn/interrupt` targets the child's active native turn through the parent socket.
- Existing processes need resumption to acquire changed startup flags. Restarting
  only the Acta server or supervisor does not restart detached provider processes.
- Captures already delivered as Unknown Frames are not silently reinterpreted by
  this adapter change. New captures use the new mapping.

After resuming the parent provider, unfinished lanes from an earlier run remain in
history but are not listed as running pills. Reopening one shows its last recorded
configuration. A conversation page's `projection_version` lets clients discard
all cached lane pages after a server projection rebuild, even if the provider
frame sequence has not advanced.
