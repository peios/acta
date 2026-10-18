# Thread permissions and approvals

The composer offers three permission modes alongside the model selector:

| Mode | Claude Code | Codex |
| --- | --- | --- |
| Ask for approval | `default` | Workspace-write sandbox, `on-request`, user reviewer |
| Approve for me | `auto` | Workspace-write sandbox, `on-request`, automatic reviewer |
| Unrestricted | `bypassPermissions` | Full-access sandbox, `never` |

These are common presets, not identical enforcement contracts. Existing native
rules and managed restrictions still apply. Automatic review may reject an
action. Claude may reject automatic mode for the selected model (observed on
Haiku); Acta reports that error and retains the confirmed mode. Changing back
from Unrestricted restores the workspace sandbox on Codex. The control is
available during a turn, including while waiting for approval, on a connected
main thread. Sending or applying another settings change temporarily disables
it. Model settings retain their separate idle-only rule. A mode change does not
answer an existing approval request; pending requests keep their explicit
Approve/Deny controls until the provider resolves them. Already-running actions
are not undone or restarted by Acta. The provider controls application of its
updated policy to subsequent permission checks.

Mode commands use the durable thread control path. Acta displays the provider's
confirmed configuration, not the requested value. Claude is read back through
its initialization response; Codex confirms through effective settings notifications
or the correlated start/resume response for the same native thread and run.
A settings acknowledgement alone is insufficient. Restoring an unchanged mode
can reuse that effective snapshot because Codex may omit a change notification;
later effective settings take precedence.
Confirmed choices are restored when Acta resumes that thread. Unsupported,
custom or unknown native modes are reported rather than falsely labelled as one
of the three presets.

## Approval interaction

A new pending request opens a popup above the composer. Clicking away leaves a
Needs approval pill. The original request remains inline with the conversation;
both locations have Approve and Deny buttons controlling the same request.
Additional pending requests are counted and handled individually. The working
indicator reads Waiting for approval. Details can be expanded to inspect the
command, original inputs, reason, paths and any available file diffs.

A decision is for that request only: Acta does not create remembered rules or
session-wide allowlists. Codex's explicit permissions-grant request has a native
turn scope; its title explicitly says Grant access for this turn. It never grants
session scope. Native user-input and MCP elicitation forms are separate contracts
and remain Unknown Frames in this slice.

Approved/denied outcomes replace the buttons in both views. Native cancellation,
turn completion, provider exit or a different run makes unanswered requests
unavailable. A disconnected harness does not discard the request; it becomes
actionable again if the same run returns and it is still pending.

## Identity and recovery

`approval/request` frames carry an Acta approval UUID, title, reason, details and
optional turn/tool IDs. `approval/resolved` records native resolution. The request
also resolves when Claude's stdio bridge echoes a successful Allow/Deny response;
ordinary control acknowledgements do not resolve approvals. The request
UUID is derived from the provider run and exact native request ID. Numeric Codex
request IDs are preserved without converting through floating point.

The approval UUID is also the immutable decision command ID. The server and
hyperharness reject reuse with a different answer. Ownership is checked by the
normal human-owned thread endpoint. The browser submits only Approve or Deny;
the hyperharness loads the original request from its captured provider journal,
checks the current run and pending state, then constructs the native response.

Before writing, the hyperharness persists the exact reply and operation intent.
The pipe's persisted write ID prevents duplicate execution on replay. If delivery
is uncertain, Acta reports it as unconfirmed; it does not invent a successful
provider decision. The browser retains an unsent decision and retries only that
same immutable command. On refresh, both views recover results from server-side
command history. Provider resolution also ends the pending UI.

Claude print sessions are launched with `--permission-prompts host` and
`--permission-prompt-tool stdio` to route real requests through the hyperharness.
`--allow-dangerously-skip-permissions` enables selection of the explicit
Unrestricted option; it does not enable bypass at launch. Existing processes
need resuming once to acquire launch-time callback flags.

## Automatic review observations

Codex automatic-review start/completion notifications map to `approval/review`
snapshots. One review keeps its first position in the feed, updating from
Reviewing permission to Automatically approved/denied. Timed-out and aborted
reviews retain distinct outcomes. An unfinished review becomes unavailable when
its turn ends or provider run is no longer connected. Completion is authoritative
even if the start was not loaded; replayed starts do not reopen finished reviews.

The expanded item shows the provider's rationale, risk level and assessment of
user authorization, along with the reviewed action and duration when available.
These are attributed provider assessments, not Acta's authorization decisions.
View tool call opens the associated operation when it is in the loaded feed.
Reviews are keyed by run and review ID, not tool ID: one tool can have multiple
reviews. No manual approval buttons appear on an automatic review.

`guardianWarning` is a Dropped Frame: its raw prose remains available in debug
mode, while structured automatic reviews provide the conversation UI. Goal
notifications remain unmapped. Historical raw Unknown Frames remain unchanged;
new captures use the mapping.

## Stopping a turn

While a connected thread is working or waiting for approval, the composer's Send
button becomes a square Stop turn control. It preserves any draft and requests
an interruption without killing the provider session. The existing power button
still kills the provider process. Stop displays its pending state and reports
provider failures; normal provider status and completion events determine when
the conversation is idle again.

Interrupt commands are owner-checked, bound to the current provider run, and
persisted through the normal control path. Replays reuse the same provider write
identity. Codex resolves the active turn with a command-specific `thread/read`
response and sends `turn/interrupt` for that exact turn; recovery reuses the read
response rather than selecting a later turn. Claude uses its streaming
`interrupt` control request. Neither interrupts by sending a chat message or
changes the thread's permission mode. This is turn cancellation, not a UI for
clearing goals or managing background jobs.

Provider references: [Codex turn interrupt](https://learn.chatgpt.com/docs/app-server)
and [Claude SDK interrupt](https://code.claude.com/docs/en/agent-sdk/python).

Observed with Claude Code 2.1.263: a session goal can survive killing the provider
and resuming it. `/goal` reports the current goal without starting model work;
`/goal clear` removes it. Do not assume restarting a harness clears a goal.

Claude can report a successful result before a `command_lifecycle: cancelled`
notification. Cancellation for a known turn corrects its outcome to interrupted,
regardless of arrival order. The stored conversation updates the existing divider
in place, retaining its timing, token counts and collected changes. Duplicate
cancellations do not add dividers, and a later generic success cannot undo an
interruption. These mappings apply to new captures; existing raw history is not
rewritten.

Claude’s standalone `[Request interrupted by user]` provider marker is a Dropped
Frame, visible only in debug mode. It does not create a user message or determine
the turn outcome; the correlated lifecycle/result events remain authoritative.

## Deleting a thread from Acta

Thread options → Delete thread permanently removes the owner's Acta transcript,
conversation state and stored commands after confirmation. It works while the
harness is disconnected, and does not send Kill or any deletion command to the
development machine. A running provider keeps running; its local session and
files remain available there.

Acta retains only an owner-scoped UUID tombstone, provider type, deletion time
and ingestion watermark. Discovery cannot restore that UUID. Later valid frames
are acknowledged and discarded rather than retained or retried indefinitely.
Deletion, discovery, command acceptance and frame projection serialize on the
same thread row. Deletion is idempotent for its owner, and other owners receive
not found. There is currently no restore action in Acta.

## Live permission change verification (2026-09-10)

Isolated native probes verified Claude/Haiku changes between default and bypass
while a Write request remained pending. Initialization confirmed each mode with
session_state=requires_action. Auto mode was rejected for Haiku and the confirmed
mode remained default. No pending request was implicitly answered.

Codex/Luna confirmed Ask, Unrestricted and automatic review changes through
thread/settings/updated while a command approval was outstanding and during a
bounded sleep command. Acknowledgement could arrive before the settings event;
Acta continues to wait for the effective configuration. No serverRequest/resolved
was emitted merely by switching the mode. Probe captures live in
/tmp/acta94-permission-review for this development run.

Regression tests cover accepted, rejected and unconfirmed updates for both
providers while preserving the pending approval and the last confirmed mode.
The composer browser test verifies permission selection while Stop remains
visible, independent of disabled model settings, and disabled selection when
disconnected.

Claude's documented streaming setter is described in
[SDK permissions](https://code.claude.com/docs/en/agent-sdk/permissions).
Codex behavior above was verified against the installed app-server directly.
