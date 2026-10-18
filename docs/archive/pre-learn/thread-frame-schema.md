# Thread frame schema

Implemented for the Codex adapter on 2026-09-07 using harness protocol v6.
The server validates the documented schema and stores complete capture bundles
atomically. Reviewed frames have dedicated UI; other frames retain their JSON
rendering. Plain text sending is described in [harnesses](harnesses.md). New
threads open empty; the harness does not inject a test message.

The decision history remains in [the mapping design](thread-frame-mapping-design.md).

- [Full JSON Schema](schemas/thread-frame.schema.json): JSON Schema Draft 2020-12,
  containing the discriminated union and all shared/payload definitions.
- [Examples](schemas/thread-frame.examples.json): one independently valid example
  per frame type. These are synthetic shape examples, not a complete ordered
  stream; referenced companion frames need not appear in this example collection.

The schema uses `$defs` and `$ref` to share contracts without repetition. A
validator can validate any complete frame against the document root. Enable
format validation for UUIDs and timestamps. Unknown fields are rejected outside
the original raw provider text. Extending the draft does not require preserving
the existing prototype protocol.

## Common envelope

```json
{
  "schema_version": 1,
  "thread_id": "11111111-1111-4111-8111-111111111111",
  "run_id": "22222222-2222-4222-8222-222222222222",
  "sequence": 42,
  "output_index": 1,
  "provider": "codex",
  "received_at": "2026-09-07T17:00:31.670Z",
  "occurred_at": "2026-09-07T17:00:31.668Z",
  "kind": "message/assistant/delta",
  "data": {
    "message_id": "example-message",
    "turn_id": "example-turn",
    "text": " received."
  }
}
```

Every field in this envelope is required. `occurred_at` is null when the
provider supplies no event timestamp. `received_at` is always the original
capture timestamp, not the time of a retry. Lifecycle timestamps inside `data`
are distinct from the provider envelope's event timestamp.

`sequence` retains the durable capture sequence already used by Acta, increasing
across provider runs in one thread. The `output_index` allows
one captured input to produce several output frames:

- Index `0`: exactly one debug frame.
- Indices `1..N`: non-debug outputs, in deterministic order.
- Frame identity: `(thread_id, sequence, output_index)`. The matching `run_id`
  and provider are immutable attributes and must be checked on replay.
- A `debug/resolved` frame lists its outputs' indices and kinds. The references
  inherit its thread, run and capture sequence.

The server supports multiple outputs per capture. Use the original raw input
only on the debug frame; all derived outputs link back through the envelope.

## Complete frame catalogue

Each row names the definition in the schema's `$defs`. Each frame definition
combines the common envelope, its literal `kind` and its full data definition.

| Kind | Frame definition | Data definition |
| --- | --- | --- |
| `thread/configuration` | `ConfigurationFrame` | `Configuration` |
| `thread/status` | `ThreadStatusFrame` | `ThreadStatus` |
| `turn/started` | `TurnStartedFrame` | `TurnStarted` |
| `turn/completed` | `TurnCompletedFrame` | `TurnCompleted` |
| `message/user` | `UserMessageFrame` | `UserMessage` |
| `message/assistant` | `AssistantMessageFrame` | `AssistantMessage` |
| `message/assistant/delta` | `AssistantMessageDeltaFrame` | `AssistantMessageDelta` |
| `usage/context` | `ContextUsageFrame` | `ContextUsage` |
| `usage/account` | `AccountUsageFrame` | `AccountUsage` |
| `mcp/server/status` | `MCPServerStatusFrame` | `MCPServerStatus` |
| `approval/request` | `ApprovalRequestFrame` | `ApprovalRequest` |
| `approval/resolved` | `ApprovalResolvedFrame` | `ApprovalResolved` |
| `approval/review` | `ApprovalReviewFrame` | `ApprovalReview` |
| `debug/unknown` | `DebugUnknownFrame` | `DebugUnknown` |
| `debug/dropped` | `DebugDroppedFrame` | `DebugDropped` |
| `debug/local` | `DebugLocalFrame` | `DebugLocal` |
| `debug/resolved` | `DebugResolvedFrame` | `DebugResolved` |
| `debug/provider-diagnostic` | `ProviderDiagnosticFrame` | `ProviderDiagnostic` |

Discovery, connection/process lifecycle, provider initialization metadata and
commitment remain hypercontrol. Their wire schemas are intentionally outside
this thread-frame document.

## Choices made explicit for review

### Configuration snapshots

The adapter emits a complete snapshot of what it currently knows for the run:
omitted fields mean unknown, and explicit null means known to be unset. Before
emitting a snapshot from a partial provider observation, the adapter merges that
observation with its known state. The client replaces its snapshot; it does not
try to infer whether incoming properties were a patch. Do not carry assumptions
from an old provider run into a new run without evidence.

The generic draft includes model, effort, fast_mode, cwd, work_mode and a
permissions summary. fast_mode is boolean when known and omitted when unknown.
Provider identity/version remain runtime metadata; native instruction sources,
approval policies and delegation hints remain in debug capture. Model IDs are
necessarily provider identifiers; supported effort choices must be advertised
and cannot be silently approximated.

Work modes are execute, plan and unknown. Permission modes:
ask, accept_edits, automatic, dont_ask, bypass, custom and unknown.
Automatic means a provider-supported automatic approval reviewer, not blanket
permission. Don’t ask denies anything requiring a new approval. Bypass
describes approval behaviour and does not by itself remove access restrictions.

The separate filesystem/network summaries report access boundaries. Filesystem
categories are read_only, workspace_write, unrestricted, custom and unknown;
network categories are allowed, blocked, restricted and unknown. Use the accepted practical mappings in
[the permission mapping](thread-permission-mapping.md). Reserve custom for
genuinely different configurations, and unknown for missing information. These reported summaries are not authorization
grants. Permission changes require their own future control flow.

### Message snapshots and streaming

`message/user` and `message/assistant` are full upserts with a lifecycle `state`
of `in_progress` or `completed`. A completed assistant snapshot replaces the
accumulated text. The delta frame appends exact text once. Later replayed starts
or deltas must not regress finalized content. An unknown start/completion time is
null; observing a completion must not invent a start time.

Message and turn IDs are opaque, scoped to the Acta thread. A live native item ID
can be used for normalized identity in this slice, but reconstructed resume
history has demonstrated different item IDs. Do not import that history as new
live messages or match it to captured history merely by text. Full reconciliation
of missing historical items remains an explicit open design item.

Only plain text content and the observed commentary/final-answer phases are in
the first schema. Images, structured text elements, citations, delivery and
question payloads need their own reviewed mappings when encountered. The debug
payload preserves them in the meantime. A frame with partly understood content
must not falsely claim full resolution while silently losing those fields.

User-message role alone does not establish the author as a human or an Acta
account. Native client correlation is adapter-local. The optional `submission_id`
is an Acta command UUID, emitted only when the provider echo matches a known
submission for the current run. It correlates the local Sending placeholder
with the provider message without text matching. Historical/provider-originated
messages without that correlation omit it.

### Usage and MCP status

Usage updates replace snapshots rather than incrementing client counters.
Unavailable values are null rather than zero. ContextUsage separates current
context occupancy from cumulative provider-thread model-call consumption and the
latest model-call consumption. The Codex adapter maps total/last into consumption snapshots; detailed reset
behaviour remains open to observation during use. Occupied context tokens remain
null, so no context percentage is inferred. The synthetic example leaves
consumption snapshots null to illustrate unavailable data. Credit balances remain decimal strings, with no inferred
currency. Individual TokenBreakdown fields still use the shared schema definition.

Account limits use a generic windows array, not Codex primary/secondary fields.
An empty array means no windows were supplied. Account usage is observed through a
run. A bucket ID does not establish provider
account identity; do not combine different runs' limits based solely on the Acta
owner or provider name. Structured individual-limit fields have not been reviewed
and remain in raw debug capture rather than an invented normalized shape.

MCP startup statuses are keyed by `(thread_id, run_id, server_name)`. A previous
run's ready status must not appear as current readiness after a new run begins.
Startup status is not a continuous connection-health guarantee.

### Raw debug data and processing failures

The `raw` field is **text**, retaining the captured provider line before
parsing, excluding its framing newline. This avoids precision loss for numbers
and retains exact provider spelling/unknown fields. Stderr is stored as its
original text, not JSON-stringified once before insertion into the debug field.
Normal JSON transport escaping still applies. In the browser display copy,
valid JSON raw payloads are embedded as formatted JSON rather than escaped strings;
this does not change the stored or wire raw text. Formatting uses raw JSON values
on the server to retain large integer precision. This is an explicit change from today's embedded raw JSON value.

Recognized frames producing non-debug output are Resolved even if also used
locally. Hypercontrol-only handling is Local. Resolved references must link to
actual outputs. Unknown covers unsupported methods/types/shapes. Dropped is an
intentional no-value disposition, never a catch-all error path.

A recognized frame whose normalized output fails validation becomes `debug/local`
with a non-null `processing_error` and no derived outputs. Its adapter state changes
are discarded. That classification is persisted once; reconnect never retries a
different interpretation under the same capture identity. Unsupported input shapes
remain `debug/unknown`. Provider RPC failures also retain their error in Local Frames.
Storage failures abort delivery without acknowledgement.

## Invariants beyond JSON Schema

JSON Schema validates individual shapes; these require stream/transaction checks:

1. Every captured input has exactly one debug frame and contiguous non-debug
   indices `1..N`. `Resolved` has at least one such output; other debug types
   have none. References agree with the actual output kinds and indices.
2. A capture's complete output bundle is durable before its capture sequence is
   acknowledged. Prefer committing the debug frame and all derived outputs in
   one transaction; never acknowledge only a debug record while losing its UI
   outputs. Existing cursor semantics must be updated accordingly.
3. Replayed frame identities have identical payloads. Persist derived bundles or
   pin the mapping version so adapter upgrades do not silently reinterpret an
   acknowledged capture under the same identity. Do not silently backfill old
   captures under already-used identities.
4. Acknowledgement advances only through complete contiguous capture bundles.
   Output filtering in the UI does not change capture ordering/acknowledgement.
5. Source thread/run identities match the adapter's known provider association.
   User/assistant snapshots and deltas resolve to the correct message/turn.
6. Snapshot replacement and lifecycle monotonicity are enforced by reducers;
   a late event must not reopen a completed message or duplicate a turn outcome.

The adapter persists its output bundle and mapping state before delivery. The
server checks the schema, complete output references, contiguous captures and
identical replays in one transaction before acknowledging. Browser pages include
whole captures (up to 64 capture sequences); they never split outputs at a cursor.
Previously captured raw-only history remains raw-only; it is not silently reinterpreted.
New pipe journals additionally retain exact original text. A detached pipe started
before this upgrade retains its previous JSON-normalizing capture behaviour until
its next restart; its provider processes need not be interrupted for this upgrade.

Validation covers documented examples, the reviewed provider sequence, unsupported
content, late/repeated events, replay after controller restart, failed local saves,
transaction rollback, pagination, and a real provider start/message/kill/resume.

The thread feed includes a **Show debug frames** toggle, initially enabled.
Turning it off hides all five `debug/` kinds, including provider diagnostics,
without affecting capture, pagination, or the stored history.
The feed retains the same available width when debug visibility changes.

## Model settings updates

Codex `thread/settings/updated` uses the same `thread/configuration` normalization
as start/resume snapshots (mapping `effort` and `sandboxPolicy` to the existing
configuration inputs). Duplicate snapshots remain local debug frames. Model
catalogue responses and settings RPC acknowledgements are local debug frames;
they belong to hypercontrol command results, not a new conversation frame type.
The UI uses the provider-confirmed configuration for its model controls.


### Claude adapter observations

Claude reuses the existing kinds. Its message UUIDs correlate real Acta sends;
streaming text deltas append to a stable assistant message, and full snapshots
replace accumulated text before completion. Claude does not report commentary
versus final-answer phase, so phase is null. Provider local-command receipts are
not user messages. Unsupported content remains in Unknown Frames.

`context.estimated` is an optional boolean on context occupancy; true labels a
provider estimate in the gauge. Claude's read-only summary supplies estimated
occupancy/capacity, while model stream usage supplies per-request counters.
Thread-lifetime cumulative consumption and separate reasoning tokens remain null
when their semantics cannot be established. Native per-process and per-turn
counters must not be relabelled as a thread-lifetime total.

`Result.settings` optionally carries the provider-confirmed model settings;
`ModelOption.resolved_model` resolves advertised aliases. The original immutable
control is retained separately from readback. A model with no effort choices
uses an empty effort string for control and null in observed configuration.


### Local configuration redaction

Debug data may include `redacted: true`. Claude initialization/settings/MCP status
responses have server-bound diagnostic copies minimized to reviewed fields so
local environment variables, source settings and transport headers are not
forwarded. The raw field is therefore a redacted copy for these responses; the
original provider bytes remain in the private local pipe. Every input still has
one debug frame and unchanged capture identity. Ordinary unknown conversation
frames remain exact captures. This is not general message/stderr secret detection.

## Hook lifecycle

Both adapters emit `hook/started` and `hook/completed`, correlated by `hook_id`
within the thread's provider run. Both carry the hook name, triggering event and
an optional turn ID. Completion adds an outcome, optional exit code/duration,
status message, separate output/stdout/stderr fields, and labelled output entries.
Claude streams remain separate and unchanged; Codex entries retain their labels.
Only an explicit `context` entry is described as added to context. Hook output is
untrusted display text and cannot execute HTML or instructions in Acta.

The feed removes the matching running row when completion arrives and inserts an
expandable result at the completion frame's position. Completion does not move
back to the start. A completion without a captured start still renders; repeated
observations do not duplicate rows, and IDs from separate runs do not collide.
JSON output is pretty-printed for reading; other output preserves whitespace.
Identical combined output and stdout are shown once as Response · stdout; both
original fields remain in the frame.
The response's position records when the provider reported it, not proof that
all its output was inserted into model context. Hook progress events remain
Unknown Frames pending their own design. Historical debug classifications are
not rewritten by this adapter change.

Explicit hook context is rendered with the shared Markdown viewer: Claude
`hookSpecificOutput.additionalContext` and Codex `context` entries. Claude’s
full structured response remains expandable under Raw hook response. Other
output remains plain text or formatted JSON; stored frames are unchanged.

### Thinking lifecycle

`thinking/started`, `thinking/delta`, and `thinking/completed` identify one thinking
item using `thinking_id` and `turn_id`, scoped to its provider run. Start and
completion carry full `text`; deltas carry text fragments with `channel`
(`summary` or `content`) and zero-based `section_index`. Sections are joined with
paragraph breaks. The UI prefers readable summaries when provided, otherwise
content. Completion is authoritative and replaces streamed text. Token-only
updates have an empty text fragment. `estimated_tokens` is a nullable absolute
estimate, never a delta or a billed usage counter. Start/completion timestamps
are nullable, using provider item timestamps where supplied and capture times
for observed Claude lifecycle boundaries. A completion without a known start
does not invent a duration.

Codex maps reasoning item lifecycle and its indexed summary/content deltas.
It does not expose per-item token estimates. Claude maps thinking block start,
text deltas, and block stop. Its final assistant thinking record reconciles with
the streamed block using the opaque signature (or the sole unambiguous block);
it never creates an empty assistant bubble. Signatures remain local adapter and
debug data, never readable UI text. Ambiguous or unsupported records remain
Unknown Frames. Every captured input retains its usual debug classification.

Claude's cumulative thinking-token estimates are attributed only when the turn
has one thinking item. The system and stream paths update the same maximum,
so repeated values do not add together; null estimates do not erase a known
estimate. If another thinking item starts in that turn, per-item estimates are
cleared rather than dividing a cumulative total by guesswork.

The thinking row stays at its first frame's position. It reads `Thinking…`, then
`Thinking for Ns · ~N tokens` when estimates exist, and `Thought for Ns` on
completion. The local timer runs without requiring deltas. Available text expands
automatically during thinking and collapses on completion, with a manual toggle
for reading it afterwards. Empty thinking has no disclosure. A terminal turn
freezes an unfinished item as interrupted; an unavailable or replaced provider
run never keeps ticking or claims successful completion. Historical debug frames
are not reinterpreted by adapter upgrades; these mappings apply to new captures.

### Tool calls

`tool/call` provides an authoritative snapshot of one operation, identified by
`tool_id` and `turn_id` within a provider run. It includes the provider tool name,
a presentation category (`command`, `read`, `write`, `edit` or `generic`), label, arguments, status, optional
output/streams, working directory, exit code, duration and lifecycle timestamps.
Unknown values are null. `tool/arguments/delta` and `tool/output/delta` carry
incremental text with the same IDs. Snapshots replace corresponding accumulated
text when supplied; a null output does not erase streamed output. Replayed frame
identities are deduplicated and terminal calls ignore stale deltas.

Supported mappings are Codex `commandExecution` and Claude tool-use envelopes
with any nonempty tool name. Unknown tool names get the generic presentation;
Read, Write, Edit and Bash have optional label/icon decoration. Codex's explicit `commandActions` read annotation supplies a readable
file label; arbitrary shell commands are not parsed to infer their effects.
Claude tool-use starts are Preparing; streamed JSON is assembled locally and
reconciled with the authoritative assistant tool-use record. Finished arguments
are Pending, not proof of execution. Claude content-block stop closes argument
generation; a matching tool_result completes foreground operations; a background launch receipt keeps the operation running. Tool results
are not human messages. Claude's is_error and interrupted flags determine failure
or interruption; Codex supplies execution state, exit code and duration. We never
invent Claude execution timing from argument-generation timestamps. Non-text results, unknown identities and mixed/unreviewed envelopes remain
Unknown Frames for a later slice.

The UI keeps one compact row at the first call frame, collapsed by default, with
an icon, description, lifecycle state and duration when available. Expanding it
shows exact arguments and output as text, plus separate stdout/stderr when useful.
File reads use the same renderer. A turn ending marks unfinished calls interrupted;
a disconnected or replaced provider run displays unfinished calls as unavailable.
Generic Claude tool calls can include MCP tools when they use the same text-result
shape. Rich MCP results remain outside this slice. Background commands use the lifecycle below.
Approval interaction is described in [Thread permissions and approvals](thread-approvals.md).

Claude's structured `system/permission_denied` and matching `tool_result_meta`
`user-rejected` records establish the terminal `permission_denied` state. The
`permission_denial` object preserves the provider message, reason and reason type
(null when unavailable). A denial and its subsequent result update the same row;
a late denial can refine a failure without losing output. The UI labels it
Permission denied and exposes its reason separately from the exact result.
Ordinary `is_error` results remain Failed; error prose is not parsed to guess
permission decisions. Denials do not offer approval controls: the call is no
longer waiting. New mappings apply to new captures; historical debug frames stay
unchanged.

### File changes and collected turn diffs

Codex `fileChange` items use `tool/call` snapshots with a `changes` array (null
for tools without supplied changes). Each entry preserves the path, operation
kind, optional move destination, diff text and format. Add/delete content is
rendered as whole-file additions/removals; updates are unified patches. One
operation may affect multiple files. Its changes stay attached to its tool ID,
with the provider's lifecycle state. Changes on unfinished or unsuccessful
operations are labelled Requested changes, not presented as successful writes.
The shared expandable tool UI shows coloured diffs and full paths. Patch-looking
file contents are not misinterpreted as diff headers, and source text is escaped.

Codex `turn/diff/updated` maps to `turn/diff` with `{turn_id, diff}`. This is the
provider's combined turn snapshot, not an append-only patch and not a delta for
the most recent tool. Every capture retains its debug frame. The UI associates
the latest snapshot by thread, provider run and turn with the existing Turn ended
divider; expanding it shows Turn changes. Late snapshots enrich the same divider,
replays do not roll it back, and an empty snapshot clears previous changes. We do
not concatenate operation patches to invent a combined diff. Providers that do
not supply one have no collected-diff section. Historical Unknown Frames remain
unchanged; these mappings apply to newly captured provider output.


### Background commands

A tool snapshot can have status `background` and a `background_task_id`.
The task identity survives the launching turn. Turn completion or interruption
does not imply that a background process exited. The original tool item changes
to Completed, Failed, or Interrupted only on a correlated terminal task update.

Claude local_bash task lifecycle records and Bash backgroundTaskId launch receipts
establish this association. Inventory without a tool identity is consumed locally;
unreviewed task types and uncorrelated notifications remain Unknown Frames.
A correlated replayed task-notification user envelope produces one
`tool/notification` with task_id, tool_id, originating turn_id, label, terminal
status, summary, and context_entry. It first appears on system notification delivery;
a subsequent context echo updates and relocates the same stored item. An idle provider can deliver system/task_notification
directly and start an autonomous reply without a user echo; that path emits the
notice on delivery and assigns the new response a distinct turn identity from
its first provider message. It is not a human message.
context_entry is false on initial delivery and true on a later context echo.
Duplicate notifications do not create duplicate notices or move an already echoed notice.

For Codex, a commandExecution still in progress with a processId when its turn
ends becomes a background tool snapshot before the turn/completed frame. Its
run-scoped identity is `command/<tool_id>`. Subsequent output deltas continue to
update the original card; item/completed ends it and emits one tool/notification
with context_entry false. Checkpoints preserve process identity and notice
delivery state. A missing processId is insufficient evidence for this mapping.

Current conversation state stores active background tasks independently of history
pagination. The composer footer shows the live count alongside Working, or by
itself after the turn ends. Unavailable/replaced provider runs do not claim tasks
are still running. The original card remains at its first position and completion
notices have a compact expandable summary.

Codex MCP calls with text or structured JSON results also use the generic tool
card, named server/tool. Text blocks retain their order. Structured data is shown
as formatted JSON when it adds information; a JSON text block that already
contains the same value is not repeated. Provider errors produce Failed with the
reported error text. Rich content blocks (images/resources/etc.) remain Unknown
Frames until their presentation is designed.

### Questions

`question/request` contains question_id (the run-bound request UUID), nullable
turn_id/tool_id, blocking, and questions. Each question has id, header, text,
multiple, and options (label/description). Custom text is accepted as an answer.
`question/resolved` contains question_id; it closes the original persisted request.
Pending requests are in current state independently of paginated history.

The `answer` hypercontrol command uses id = question_id, current run_id, and an
answers object mapping question IDs to arrays of strings. Its durable result
retains those answers. This shares the approval command's immutable decision and
pipe-write retry machinery, but is not an approve/deny decision. The local harness
rejects replies to expired or replaced requests and never trusts browser-supplied
provider input. Single-choice questions accept one string; multiple-choice answers
may contain several selections and custom text.

Claude's can_use_tool/AskUserQuestion envelope maps question positions to stable
q1, q2 identifiers within the request. The local reply retains the original
questions and maps their text to answer strings (multiple values joined by comma
and space), per the [Agent SDK user-input contract](https://code.claude.com/docs/en/agent-sdk/user-input).
Codex item/tool/requestUserInput retains the provider question IDs and replies
with answers[id].answers, as defined by the installed app-server JSON schema.
Codex's isBlocking flag controls whether turn completion closes an unanswered
request. Provider cancellation/resolution always closes it.

### Context compaction

`context/compaction` is a full snapshot keyed by run and `compaction_id`, with
`turn_id`, `state` (`in_progress` or `completed`), nullable `started_at`,
`completed_at`, `duration_ms`, `before_tokens`, `after_tokens`, and `summary`.
The running snapshot feeds the composer status. Completion places one notice at
the completion frame; a later summary updates that stored notice without moving
it. Missing counts, timing and summaries remain null. Turn termination clears an
unfinished status; connection loss suppresses it in the UI.

Claude `system/status: compacting` starts the snapshot; `compact_boundary` closes
it with provider token counts and duration. Its explicit preserved-message anchor
UUID correlates a subsequent synthetic summary. Unrelated synthetic user messages
remain Unknown. Codex `contextCompaction` items supply start/end identity only in
the installed protocol: elapsed receipt time is used when both ends were observed,
and no token counts or summary are invented. Raw captures remain in debug frames.

For submitted Claude user-message echoes, the complete recognized
command-name/command-message/command-args wrapper is normalized to plain slash
command text. Mismatched or incomplete wrappers are preserved literally.

A successful Claude `result` with task-notification origin, zero model turns,
zero output tokens and empty text is a dropped debug receipt. It does not create
or complete a conversation turn. Errors and nonempty results retain their normal
handling.

Resumed background notices retain the original known task/tool identity and label
in the local adapter checkpoint, without restoring that tool into the new run's
live tool map. They emit only `tool/notification`, scoped to the receiving run;
the old card and old notice stay intact. Unknown task/tool pairs remain Unknown.

Tool results can carry `images`, an array of `{media_type, base64}` raster images
(or null). Claude base64 PNG/JPEG/GIF/WebP tool-result blocks populate this field;
text in the same result remains in `output`. Image-only results complete the tool
normally. Unsupported content or invalid base64 remains Unknown atomically.
Native duplicate image metadata is retained only in raw debug data. Expanded tool
cards show thumbnails, with a keyboard-accessible enlarged preview closed by
Escape or its close button. This does not fetch files from development machines.

Whole-PDF results use `attachments`, an array of `{name, media_type, base64}`
(or null). Currently only application/pdf is mapped. The filename comes from the
read operation's basename, defaulting to document.pdf; source bytes and MIME
are provided by the result. The card retains neighboring text and shows compact
Open/Download actions with the decoded byte count. Open uses the browser's PDF
handler in a separate tab; there is no embedded Acta PDF reader. Browser-local
object URLs are revoked when the snapshot changes or the card unmounts. No local
provider file is fetched by the server.

### Git push notifications

`vcs/push` records a provider-reported push with `branch` and `cwd` strings and a
nullable `turn_id`. It is a chronological event, not the current branch or working
directory singleton. The UI shows a compact repository/branch notice. Claude's
`system/vcs_state_changed` with `kind: push` maps here; other event kinds remain
unreviewed. Tool execution heartbeats update the existing `tool/call` when needed
and otherwise remain `debug/local`.
