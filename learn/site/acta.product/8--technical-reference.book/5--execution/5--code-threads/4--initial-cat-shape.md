---
title: "CAT Frame Representation"
description: >-
  Code host frame types, diagnostic levels, hook updates, and incremental
  change cursors.
---

CAT frames are JSON objects. Each output line, including stderr and
unrecognized or malformed JSON, starts as a retained source. Unmapped lines
become:

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

`raw` preserves the line without its terminator. The adapter interprets
lifecycle and user acknowledgments internally while retaining every line. The
host assigns monotonic per-thread `seq` values and receipt timestamps,
serializing stdout/stderr arrival into one frame list. Cross-stream ordering
reflects observation, not a guaranteed provider order. The UI renders escaped
text, pretty-printing valid JSON for inspection. It never executes output or
renders it as HTML. **Show debug frames** hides these entries.

One line is limited to 64 KiB. An oversized/unreadable line fails the session
with an explicit error rather than silently pretending it was parsed. A final
line without a newline is captured on process exit.

## Diagnostic logs

`debug.log` contains `level` (trace/debug/info/warn/error/fatal), `message`,
optional `target` (provider logger/module) and optional `timestamp` (provider
time). The common `at` remains the host receipt time. `provider`, `stream` and
`raw` are retained, including extra structured fields, for inspection.

Codex stderr JSON with a valid RFC3339 timestamp, recognized level and
nonempty `fields.message` maps to a log; `target` is copied when present.
Claude stderr diagnostics in `RFC3339 [LEVEL] message` or `[LEVEL] message`
form map to the same frame. WARNING normalizes to warn. Unknown levels,
malformed records, plain stderr, and protocol stdout remain unknown. Hook
responses are not treated as logs. Claude's current installed logger confirms
the timestamped format; its diagnostic logging can be enabled by the [CLI's
debug options](https://code.claude.com/docs/en/cli-usage). This mapping does
not turn on extra logging or read separate log files.

The UI shows a compact severity badge, source, timestamp and message, with
amber warnings and red errors. Expand Details to inspect the original line and
source sequence. Show debug frames controls both logs and unknown frames. Live
conversion and Reprocess use the same mapping.

## Hooks and supersession

`hook.started`, `hook.progress`, and `hook.completed` contain a self-contained
`hook` snapshot: execution `id`, normalized `event`, optional display `name`,
`status`, and `outputs: [{kind, text}]`. Status is
running/completed/failed/blocked/ stopped/canceled. A completion frame can
represent failure; it means execution ended, not that it succeeded or its
output entered model context.

Claude's stdout `system` events `hook_started`, `hook_progress`, and
`hook_response` map by `hook_id`. Progress contains cumulative
stdout/stderr/output snapshots, not text deltas. The adapter keeps all three
fields with their original kinds; the UI prefers separate streams and leaves
combined output in raw details when both forms are present.
Success/error/canceled outcomes map to completed/failed/ canceled; an optional
numeric exit code is retained (including zero). See [Claude's message
types](https://code.claude.com/docs/en/agent-sdk/typescript#sdkhookstartedmessage).

Codex `hook/started` and `hook/completed` map by `run.id`, preserving status
and semantic entries (warning/stop/feedback/context/error). Optional metadata
includes handler type, execution mode, scope, configuration source/path,
provider timing fields, duration in milliseconds, and status message. Event
names shared by both providers normalize to snake_case; unknown event names
stay verbatim. Unrecognized outcomes, entry kinds, and malformed hook records
remain `debug.unknown`. Codex documents lifecycle notifications for
synchronous hooks only. Context insertion is handled separately from
completion, as described below. See [Codex app-server
events](https://learn.chatgpt.com/docs/app-server#turn-events).

The host gives each hook frame a stable thread-local `id` derived from its
source sequence and converter output ordinal. This is distinct from `hook.id`,
which identifies the execution. A new snapshot links to the latest retained
snapshot for that provider/execution through the common-envelope `supersedes`
field. The host atomically marks the earlier frame `superseded_by`, then
appends the new frame at its actual arrival position. Earlier hook data,
`seq`, and `at` do not change. The host needs no unbounded execution index. If
the predecessor was not retained, the incoming snapshot stands alone without
an invented link.

Superseded frames are hidden by default; **Show superseded** exposes the
chain. Hook rows show a starting/running indicator or terminal status, with
expandable output and original provider records. The collapsed view is a
single unboxed row: hook icon (spinner while running), name, status,
disclosure chevron and relative time. Event metadata, exit code, and duration
are inside the expansion. An uncompleted hook whose host/session is
unavailable displays Result unavailable, without fabricating a provider
outcome. Hooks remain visible when **Show debug frames** is disabled.

The main dropdown shows context content. Raw output, metadata, and provenance
are under a secondary **Technical details** disclosure. Missing context is
described as not reported, never as proof that nothing entered the model's
context.

`hook.context` maps Codex's completed `hookPrompt` thread items. Fragments
group by `hookRunId` in provider order into the hook snapshot's `context`
field: `{texts, source: "context_item"}`. Matching lifecycle metadata is
copied from the latest retained hook snapshot, which the context record
supersedes. Context stays at its insertion position. Multiple distinct context
insertions remain separate visible records. A missing lifecycle predecessor
uses unknown execution status and still displays its context. The validated
`item/started` counterpart is dropped to avoid displaying the same context
twice. Malformed context items remain unknown; sources remain replayable.

For Claude, successful hook responses populate `hook.context` with source
`hook_response` from matching, recognized
`hookSpecificOutput.additionalContext` or the documented plain-stdout context
events (SessionStart, UserPromptSubmit, UserPromptExpansion, and
PostModelSwitch). Failures, cancellation, nonzero exits,
blocked/stopped/deferred control responses, async handoffs, unsupported
events, and malformed JSON-looking output do not get promoted to context.
Diagnostics remain in technical details. This is labeled **Reported context**:
the SDK stream does not expose a separate hook-context insertion
acknowledgment, and this payload is not an exact reconstruction of provider
wrapping, truncation or later context transformations. See [Claude's hook
context
contract](https://code.claude.com/docs/en/hooks#add-context-for-claude).

## Incremental frame updates

`seq` is chronological position. `change_seq` advances for every insert or
supersession annotation, independently of position. Reads return current-state
upserts in change order, not a historical change log. Multiple edits can
coalesce. Clients merge by position, keep the greater change sequence, and
display by `seq`. An earlier start therefore receives its hidden state even
when a client has already read past it. Loading only that record also reveals
its supersession.

`first` is the oldest retained chronological position: clients prune older
local records even on an empty update page. Gaps in change numbers do not mean
lost history. `next` advances to the last delivered change (or the host's
current change cursor when caught up). Pagination can deliver a supersession
annotation before its replacement on the next page; clients immediately fetch
subsequent pages. Replay resets both counters under the new thread revision
and deterministically reconstructs IDs and supersession links from retained
original sources.
