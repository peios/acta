# Acta Code Threads: send and observe

Code's Agents tab lists real, host-owned threads in the selected codebase. Use
the **+** button to choose Claude Code or Codex. For multiple roots, choose one working
folder; it can itself contain many repositories. The host resolves the root ID
against its catalogue. The browser cannot supply a command, executable, flags or
arbitrary working directory. Additional roots are not automatically passed as
provider tool-access grants in this slice.

## Adapter and process

`internal/codeagents.Adapter` accepts a provider-neutral launch directory and an
output callback, returning a session with `Wait()` and `Send(ctx, input)`. `Send`
waits for the turn's terminal event; user acknowledgement is a separate CAT frame.
The host owns thread identity,
lifetime, state and frame ordering; the adapter owns the provider process and
converts its output into CAT payloads. The host routes the required `provider`
field through an explicit adapter registry; the browser cannot name an executable.

### Claude Code

```sh
claude --print --input-format stream-json --output-format stream-json --verbose --replay-user-messages
```

`--print` selects the noninteractive protocol. The adapter starts reading both
output streams immediately and writes one initialization control request:

```json
{"type":"control_request","request_id":"<unique UUID>","request":{"subtype":"initialize"}}
```

The thread stays `starting` until stdout delivers a `control_response` whose
nested `response.request_id` matches and whose `response.subtype` is `success`.
Then it becomes `ready`. The acknowledgement is still emitted as a
`debug.unknown` frame before readiness changes, as are all early hook events.
Unrelated responses and stderr cannot acknowledge startup. A matching error or
malformed response fails startup; a missing acknowledgement times out after
30 seconds. Failed startup cancels and reaps the process. Provider exit before
acknowledgement is also a failure, even with a successful process exit code.

Stdin stays open after the handshake for explicit user messages. No permission
reply or stop button exists yet. Native startup hooks/configuration still apply;
the host does not bypass or override Claude permission settings. Startup hooks
can run before the client handshake completes. `ready` confirms initialization,
not a model turn or a successful authenticated model request.

### Codex

Each Codex thread gets its own private stdio process:

```sh
codex app-server --listen stdio://
```

The adapter sends a uniquely identified `initialize` request with
`clientInfo: {name: "acta_code", title: "Acta Code", version: "0.1.0"}`. After a
matching successful response it sends the `initialized` notification, then a
separate `thread/start` request with `params: {cwd: <selected root>}`. Only a
successful matching response containing a nonempty `result.thread.id` makes the
Acta thread `ready`. The adapter retains that native ID internally. All output,
including RPC responses and notifications understood during startup, is retained
as source output. The converter drops the known successful initialization response
(`id` plus `result` containing `userAgent`, `codexHome`, `platformFamily`, and
`platformOs`) from the CAT view. Hook lifecycle events become `hook.*`, and recognized stderr diagnostics become `debug.log`;
unmapped output remains `debug.unknown`. Stderr, unrelated IDs and server requests cannot
acknowledge startup. Errors, malformed matching responses, early exit and the
30-second total startup deadline fail the thread and reap the process.

Startup sends no `turn/start`; explicit user submission does. Model, permissions, auth and configuration use the local
Codex defaults; this UI does not change them. A ready thread does not establish
that an authenticated model request would succeed. Codex may create its own
local native history under its normal configuration; Acta does not yet resume
that history after a host restart. See the official
[app-server protocol](https://learn.chatgpt.com/docs/app-server). The installed
CLI's generated schema also validates `initialize` and `thread/start` inputs.

### Shared lifecycle and UI

The thread view includes a Codex-inspired composer: rounded draft area,
attachment and approval controls, model/effort, microphone and circular send
button. Enter or the send button submits plain text; Shift+Enter inserts a newline.
Attachment, approval, model/effort and microphone controls remain preview-only.
The temporary draft resets when switching threads or reloading. One turn may run
at a time: the send button is disabled until the provider reports completion.
Permissions are not answered or bypassed; a provider waiting for permission can
remain busy. There is no steering, queue, interrupt or permission UI in this slice.

## User input and acknowledgements

`threads.send` accepts `{id, codebase_id, thread_id, text, delivery:"start_turn"}`.
The host validates the input, marks the thread busy, records `input.message.user`
with `id`, `text`, and `delivery`, then submits asynchronously. The HTTP response
confirms host acceptance, not provider acknowledgement. Input is limited to 4 KiB
of UTF-8 text, leaving space for JSON escaping within the existing relay limit.

Claude receives a `type:"user"` envelope with the input UUID and plain text.
Its replayed top-level user message produces `message.user`. Codex receives
`turn/start` with `clientUserMessageId` and a text input; its `userMessage` item
produces `message.user`. That frame carries `input_id`, a message `id`, `text`,
and a native `turn_id` when supplied. A provider-supplied correlation ID must
match; when absent, exact plain-text matching is allowed against the single
pending input. Tool results and non-text content never acknowledge a submission.
Duplicate item lifecycle events produce only one acknowledged CAT frame.

The local smoke test confirmed that Claude preserves the submitted UUID in its
replayed user message, and Codex returns the submitted `clientUserMessageId` as
the user item's `clientId`. Both completed a plain-text turn and became sendable
again without any tool use.

Both original and acknowledged frames stay in the live CAT buffer. The UI hides
an `input.message.user` when its corresponding `message.user` is present. The
debug toggle only hides `debug.*`; user frames and `input.message.error` remain
visible. Every provider line is preserved in the source journal; unconverted
events also appear as `debug.unknown`.

The host remembers up to 1,000 input IDs and text digests per thread for its run,
independently of frame eviction. An unchanged retry returns the existing thread
without another write, including after completion or failure. Reusing an ID for
different text fails. No automatic provider resend occurs after uncertainty.
The composer keeps its request ID for an unchanged failed HTTP attempt.

Writes have a five-second bound and acknowledgement a 60-second bound. A stuck
write or absent acknowledgement cancels the provider process and records visible
uncertainty, rather than allowing an untracked late turn. After acknowledgement,
the turn can run until its provider terminal event or process/host shutdown.
Claude `result` and Codex `turn/completed` release the busy state; model/tool
errors remain in raw debug output. Rejection, missing acknowledgement or premature
process exit produces `input.message.error`, linked to the input ID.

The executable is resolved from the host PATH and must have an absolute path.
Provider credentials/configuration are inherited normally, while `ACTA_TOKEN`
is excluded from the child environment. Failures and exits update thread state.
Stopping the host cancels and reaps its children; on Unix it kills their process
groups too. Browser disconnects, HTTP timeouts and transient relay reconnects do
not own or cancel thread processes.

See the official [CLI reference](https://code.claude.com/docs/en/cli-usage) and
[streaming documentation](https://code.claude.com/docs/en/headless).

## Initial CAT shape

CAT frames are JSON objects. Each output line, including stderr and unrecognized
or malformed JSON, starts as a retained source. Unmapped lines become:

```json
{
  "type": "debug.unknown",
  "provider": "claude",
  "stream": "stdout",
  "raw": "{\"type\":\"system\",\"subtype\":\"hook_started\"}",
  "seq": 1,
  "at": "2026-09-15T12:00:00Z"
}
```

`raw` preserves the line without its terminator. The adapter interprets lifecycle
and user acknowledgements internally while retaining every line. The host assigns monotonic per-thread `seq` values
and receipt timestamps, serializing stdout/stderr arrival into one frame list.
Cross-stream ordering reflects observation, not a guaranteed provider order.
The UI renders escaped text, pretty-printing valid JSON for inspection. It never
executes output or renders it as HTML. **Show debug frames** hides these entries.

One line is limited to 64 KiB. An oversized/unreadable line fails the session
with an explicit error rather than silently pretending it was parsed. A final
line without a newline is captured on process exit.

### Diagnostic logs

`debug.log` contains `level` (trace/debug/info/warn/error/fatal), `message`,
optional `target` (provider logger/module) and optional `timestamp` (provider time).
The common `at` remains the host receipt time. `provider`, `stream` and `raw`
are retained, including extra structured fields, for inspection.

Codex stderr JSON with a valid RFC3339 timestamp, recognized level and nonempty
`fields.message` maps to a log; `target` is copied when present. Claude stderr
diagnostics in `RFC3339 [LEVEL] message` or `[LEVEL] message` form map to the same
frame. WARNING normalizes to warn. Unknown levels, malformed records, plain stderr
and protocol stdout remain unknown. Hook responses are not treated as logs.
Claude's current installed logger confirms the timestamped format; its diagnostic
logging can be enabled by the [CLI's debug options](https://code.claude.com/docs/en/cli-usage).
This mapping does not turn on
extra logging or read separate log files.

The UI shows a compact severity badge, source, timestamp and message, with amber
warnings and red errors. Expand Details to inspect the original line and source
sequence. Show debug frames controls both logs and unknown frames. Live conversion
and Reprocess use the same mapping.

### Hooks and supersession

`hook.started`, `hook.progress`, and `hook.completed` contain a self-contained
`hook` snapshot: execution `id`, normalized `event`, optional display `name`,
`status`, and `outputs: [{kind, text}]`. Status is running/completed/failed/blocked/
stopped/cancelled. A completion frame can represent failure; it means execution
ended, not that it succeeded or its output entered model context.

Claude's stdout `system` events `hook_started`, `hook_progress`, and `hook_response`
map by `hook_id`. Progress contains cumulative stdout/stderr/output snapshots,
not text deltas. The adapter keeps all three fields with their original kinds;
the UI prefers separate streams and leaves combined output in raw details when
both forms are present. Success/error/cancelled outcomes map to completed/failed/
cancelled; an optional numeric exit code is retained (including zero).
See [Claude's message types](https://code.claude.com/docs/en/agent-sdk/typescript#sdkhookstartedmessage).

Codex `hook/started` and `hook/completed` map by `run.id`, preserving status and
semantic entries (warning/stop/feedback/context/error). Optional metadata includes
handler type, execution mode, scope, configuration source/path, provider timing
fields, duration in milliseconds and status message. Event names shared by both
providers normalize to snake_case; unknown event names stay verbatim. Unrecognized
outcomes, entry kinds and malformed hook records remain `debug.unknown`. Codex
currently documents lifecycle notifications for synchronous hooks only. Context
insertion is handled separately from completion, as described below.
See [Codex app-server events](https://learn.chatgpt.com/docs/app-server#turn-events).

The host gives each hook frame a stable thread-local `id` derived from its source
sequence and converter output ordinal. This is distinct from `hook.id`, which
identifies the execution. A new snapshot links to the latest retained snapshot
for that provider/execution through the common-envelope `supersedes` field.
The host atomically marks the earlier frame `superseded_by`, then appends the new
frame at its actual arrival position. Earlier hook data, `seq`, and `at` do not
change. The host needs no unbounded execution index. If the predecessor was not
retained, the incoming snapshot stands alone without an invented link.

Superseded frames are hidden by default; **Show superseded** exposes the chain.
Hook rows show a starting/running indicator or terminal status, with expandable
output and original provider records. The collapsed view is a single unboxed row:
hook icon (spinner while running), name, status, disclosure chevron and relative
time. Event metadata, exit code and duration are inside the expansion. An uncompleted hook whose host/session is
unavailable displays Result unavailable, without fabricating a provider outcome.
Hooks remain visible when **Show debug frames** is disabled.

The main dropdown shows context content. Raw output, metadata and provenance are
under a secondary **Technical details** disclosure. Missing context is described
as not reported, never as proof that nothing entered the model's context.

`hook.context` maps Codex's completed `hookPrompt` thread items. Fragments group by
`hookRunId` in provider order into the hook snapshot's `context` field:
`{texts, source: "context_item"}`. Matching lifecycle metadata
is copied from the latest retained hook snapshot, which the context record
supersedes. Context stays at its insertion position. Multiple distinct context
insertions remain separate visible records. A missing lifecycle predecessor uses
unknown execution status and still displays its context. The validated
`item/started` counterpart is dropped to avoid displaying the same context twice.
Malformed context items remain unknown; sources remain replayable.

For Claude, successful hook responses populate `hook.context` with source
`hook_response` from matching, recognized `hookSpecificOutput.additionalContext`
or the documented plain-stdout context events (SessionStart, UserPromptSubmit,
UserPromptExpansion and PostModelSwitch). Failures, cancellation, nonzero exits,
blocked/stopped/deferred control responses, async handoffs, unsupported events
and malformed JSON-looking output do not get promoted to context. Diagnostics
remain in technical details. This is labeled **Reported context**: the SDK stream
does not expose a separate hook-context insertion acknowledgement, and this
payload is not an exact reconstruction of provider wrapping, truncation or later
context transformations. See [Claude's hook context contract](https://code.claude.com/docs/en/hooks#add-context-for-claude).

### Incremental frame updates

`seq` is chronological position. `change_seq` advances for every insert or
supersession annotation, independently of position. Reads return current-state
upserts in change order, not a historical change log. Multiple edits may coalesce.
Clients merge by position, keep the greater change sequence, and display by `seq`.
An earlier start therefore receives its hidden state even when a client has
already read past it. Loading only that record also reveals its supersession.

`first` is the oldest retained chronological position: clients prune older local
records even on an empty update page. Gaps in change numbers do not mean lost
history. `next` advances to the last delivered change (or the host's current
change cursor when caught up). Pagination may deliver a supersession annotation
before its replacement on the next page; clients immediately fetch subsequent
pages. Replay resets both counters under the new thread revision and deterministically
reconstructs IDs and supersession links from retained original sources.

## Reprocess retained output

The thread toolbar's **Reprocess** button rebuilds the actual CAT thread in place.
It is available when the host is connected and the thread is not starting or busy;
finished/failed threads can also be reprocessed. No provider is started, contacted,
or sent input, and replay never calls the live lifecycle/acknowledgement parser.

The host keeps a separate chronological source journal containing raw provider
lines plus the original input, acknowledgement and submission-error records.
Each source gets a stable `source_seq` and receipt timestamp. Live raw output and
replayed raw output both pass through `Adapter.NewConverter()` / `Converter.Convert`
in `internal/codeagents/convert.go`. A converter gets fresh state at replay start
and can emit zero, one or multiple CAT payloads per source. Its final state becomes
the converter for subsequent live output. Original non-debug records are copied
unchanged, preserving input IDs and acknowledgement links within retained history.

Both adapters map recognized stderr diagnostics to `debug.log`. Codex drops its known initialization
success acknowledgement on stdout, matching its structure rather than a particular
request ID, version or filesystem path. Errors, malformed acknowledgements and
responses with unknown fields remain inspectable. Codex also drops valid stdout
events whose top-level `method` starts with `remoteControl/`, including future
methods under that prefix. Matching is case-sensitive and does not inspect text
inside params. Both adapters also map recognized hook lifecycle snapshots and the
host reconstructs their supersession chains. Raw sources remain retained. Other events remain unknown;
new schema mappings belong in those pure converters.
Converters must not use process, network, filesystem or live session state. They
may keep bounded state across lines, but must handle incomplete retained history.

Publication is atomic with respect to reads, incoming output and message sending.
Each successful replay increments `thread.revision`, replaces the CAT projection,
and restarts its sequence numbers. `frame.source_seq` and timestamps continue to
identify the original source observation, including when a source emits multiple
frames. Repeated replay never consumes previously converted output. A cancelled
rebuild leaves the previous projection and revision intact.

`threads.read` includes the caller's `revision`. A mismatch returns `reset:true`
and starts at the first retained frame, even if the old cursor is beyond the new
sequence length. All clients discard their old frames on reset, including clients
that did not press Reprocess. `threads.reprocess` requires the expected revision,
so a retried old request cannot silently replay a second time.

The source journal and CAT projection each retain up to 1 MiB / 1,000 records.
After source eviction, `thread.source_truncated` stays true and the UI explicitly
warns that replay covers retained history only. Source records remain in host
memory; they are not persisted remotely or to disk. Converter changes currently
require rebuilding/restarting the host, which clears this history. Reusing captures
across code changes will need a later persistence or import facility.

## Relay, retention and limits

These methods use the existing per-human Code host request relay:

- `threads.start`: `{id, codebase_id, root_id, provider}` returns a thread.
  `provider` must be `claude` or `codex`. The UI supplies
  a UUID so a retry on the same host run returns the existing thread instead of
  spawning another process. Reusing an ID with different roots or providers is
  rejected. The dialog retains the ID for an unchanged retry and generates a new
  ID when its provider or folder changes after a failed request.
- `threads.list`: `{codebase_id}` returns `{threads}` in newest-first order.
- `threads.send`: the input contract above; returns the thread with `busy:true`
  on initial acceptance. Retried inputs return its current state.
- `threads.reprocess`: `{codebase_id, thread_id, revision}` returns the thread
  with its incremented revision after rebuilding retained output.
- `threads.read`: `{codebase_id, thread_id, after, revision}` returns `{thread, frames,
  first, next, more, reset}`. `after`/`next` are per-revision **change** cursors;
  `first` is the earliest retained chronological `seq`, used for pruning and
  indicating truncated history. Frames may update earlier positions.

The browser reads the selected thread every 500 ms while Code is visible,
fetching subsequent pages immediately when `more` is true. Lists refresh every
three seconds. Account/host/codebase changes abort old reads and clear scoped UI
state. All operations retain the relay's caller and native-session checks; no
Workspaces sharing, superuser bypass or new MCP tools are introduced.

This is intentionally a live, in-memory first slice: up to 64 threads and eight
active processes per host run, shared across both providers. Each thread retains at most 1 MiB of encoded
frames and 1,000 entries, evicting the oldest with cursor gaps. Read pages are
bounded to 512 KiB/100 frames; browser retention is also bounded. Frames remain
on the host across browser reloads and relay reconnects. Restarting the host
stops processes and clears this initial thread store; durable CAT storage and
provider session resumption are future work. The remote only relays these reads
and does not persist transcripts. Debug output can include local hook output and
paths, and is visible only to the host's authenticated human owner.
