---
title: "Claude Code Execution"
description: >-
  Claude process configuration, settings readback, file diffs, context
  measurements, and supported controls.
---

Claude uses the same local controller, durable pipe and server frame stream as
Codex. The local child runs `claude --print` with streaming JSON input/output,
partial messages and user-message replay. Local authentication, hooks, and MCP
configuration remain the provider's own configuration. Acta does not copy
Claude credentials onto the server or auto-approve provider requests.

Create, Kill, Resume, idle, and mid-turn message sending and model settings
work for both providers. Claude's echoed UUID correlates with the immutable
Acta submission; local command receipts are consumed separately. Resume reuses
the native session UUID, restores the confirmed model settings and does not
inject a message. Commitment requires a complete stored user record in the
matching local Claude session file, rather than merely seeing a process or a
filename.

Claude model aliases advertise their resolved model IDs. Models without effort
levels show Not applicable. Where no default effort is advertised, Default
means letting Claude choose. Settings are read back after applying them: the
effective model/effort and actual fast-mode state confirm the UI, without
rewriting the original command payload. Failed or uncertain changes retain an
explicit result.

Read-only observations refresh at startup, after results, and every 30
seconds: MCP status, context summary, and account allowance. Pending MCP
startup is checked every second until settled. Context summaries are marked
estimated. Observations do not send model turns or perform an extra
token-count API request. Current Claude permission modes are mapped
independently of OS access boundaries; absent filesystem/network evidence
stays unknown.

Empty Codex terminal-interaction polls for a known shell operation are Dropped
Frames; output and completion come from their own events. Nonempty stdin and
unmatched identities remain Unknown pending a reviewed representation.

## Claude file diffs

Successful native Write and Edit results attach an operation diff and update
the turn's collected Changes panel. The adapter reconstructs before/after
contents from provider-reported snapshots and actual result fields,
cross-checks supplied structured patches, and joins edits only when successive
snapshots match exactly. Created files remain additions; edits reversed within
a turn disappear from its net diff. Files are listed in path order. No
filesystem reads participate, so replay and reconnection preserve the same
result.

Generated patch headers quote unusual filenames using Git-compatible escapes,
so spaces, quotes, tabs and newlines remain part of the filename. The
operation and collected diff use the same encoding.

When continuity or a snapshot cannot be verified, the panel shows **Individual
changes**, preserving reported patches in arrival order. If a successful
operation provides no usable patch, the panel also notes that its diff is
incomplete. This covers native Write/Edit operations; it does not claim to
reconstruct changes made through shell commands or external processes.
Denied/failed operations do not contribute applied changes. Raw provider
evidence remains available.

The turn/diff frame accepts optional `mode` (`combined` or `sequential`) and
`incomplete` fields. Codex's authoritative turn diff retains its existing
shape. The conversation projection persists these fields alongside the latest
diff.

## Context measurement freshness

The context gauge keeps the most recent usable measurement for the current
provider run. Usage updates with missing context counts continue to update
turn statistics and history but do not replace the gauge measurement. Its
popup's **Last reported** timestamp remains the original measurement time,
rather than being refreshed by later token-usage events. A fresh measurement,
including zero usage, replaces it; a new provider run starts without a
carried-over estimate. Existing conversation projections are rebuilt from
stored frames on upgrade so this retention also applies to saved history.

## Tool discovery results

Claude ToolSearch results containing tool references complete the existing
tool card with an available-tool list. Plain text remains in order. Malformed
or unrecognized content, or tool references returned by an unreviewed tool,
remain Unknown Frames rather than being partially consumed. Raw references
remain in the corresponding debug frame.

## Background commands

Claude background Bash commands keep their original tool card marked Running
in background until a correlated task completion/failure arrives. A compact,
expandable completion notice appears where the provider delivers the
notification. The composer footer shows the running background count alongside
Working or Waiting for approval, and keeps showing it after the launching turn
ends. This state is persisted separately from paginated history, so refresh
and loading older messages cannot lose the count. Unavailable provider runs do
not claim their tasks are still running. Automatic replies to idle-time
completions have their own turn ending rather than reopening an already
completed turn.

Codex uses the same presentation for commands that still have a running
process when their turn ends. Their later command completion updates the
original card, clears the count, and places a completion notice at that event.
Commands without a provider process ID are not assumed to be background work.
Codex notices stay at the completion event because this provider supplies no
separate context echo.

Harness startup also retries transient failures of the initial account lookup
(connection failures, HTTP 429, and server errors) with bounded exponential
backoff. Authentication/permission failures, redirects, and invalid account
responses still fail explicitly. Ctrl+C cancels the wait. No local process
ownership or provider startup occurs until the account identity has been
verified.

## Provider questions

Claude AskUserQuestion and Codex request_user_input use a question card in the
conversation and an automatically opened composer popup. Select the requested
number of choices, or type a custom answer, then choose Answer. Draft answers
are shared between both views and saved in this browser for the
account/thread/run. Answering uses a durable command bound to the original
request UUID and provider run. Repeat deliveries cannot change a submitted
answer. The harness validates question identities and answer cardinality
against its original provider capture; the browser cannot replace the tool
input or native reply routing.

A confirmed submission displays the answers on the card. Stop cancels a
waiting turn through the existing interrupt control. Provider resolution
closes a question, even if the card is outside the loaded history page. Codex
questions explicitly marked nonblocking remain answerable after turn
completion until the provider resolves them. Secret-input requests and visual
option previews remain unreviewed Unknown Frames rather than being silently
rendered as ordinary text.

Context compaction appears as "Compacting context…" beside the composer. Once
finished, a compact conversation notice shows token counts and duration when
available. Expand it to read the provider's summary when supplied. Codex
supplies the compaction lifecycle without summary or before/after counts. This
presentation does not add a new compaction command or erase Acta history.

Recognized Claude slash-command echoes are displayed as the original
`/command` plus arguments rather than the provider's command-name/message/args
wrapper.

Resolved permission/question requests without an Acta command outcome are
checked once rather than polled indefinitely. Pending requests, submitted
commands, and uncertain delivery continue to be checked until their outcome is
known.

When Claude repeats a stopped-background-command notice after resuming, Acta
shows it at the resume point as well. The original tool card remains
unchanged. Repeated transport delivery within that run does not duplicate the
notice; if Claude later echoes it into context, that run's notice moves to the
echo position.

Image tool results supplied by Claude appear as thumbnails when the tool card
is expanded. Select a thumbnail for a larger preview; Escape closes it. Text
output can accompany images. A local filename alone does not transfer an image
to Acta. Whole PDFs returned by a tool appear as attachments with Open and
Download links. Open uses the browser's document handler; individual PDF pages
returned as images use the image preview instead.

Claude local commands can acknowledge a submitted command without echoing its
text. Acta settles the pending message from a matching provider command UUID,
using the persisted send text and exact run identity. This prevents a
completed local command from leaving the composer stuck on Sending.
