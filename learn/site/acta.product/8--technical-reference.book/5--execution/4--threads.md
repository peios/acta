---
title: "Threads"
description: >-
  Local provider process ownership, discovery, durable journals, reconnects,
  and resumption failures.
---

A thread is an Acta record of a provider conversation. The connected machine
owns the provider process, while Acta stores normalized conversation frames
and command state.

## Creation and discovery

The browser supplies a fresh thread UUID in a Start request to a selected live
connection. The local controller records spawn intent before starting the
provider, initializes its protocol, and establishes native identity. Creation
sends no model message.

Provider readiness allows discovery before saved user history exists. The
thread can therefore be advertised as uncommitted. Verified stored user
history marks it committed after explicit user submission. Authorized
inventory advertisement creates the server record.

The browser waits about 30 seconds for discovery. Timeout reports uncertainty;
it is not a cancellation tombstone. A later authorized discovery remains
valid. The same local UUID does not spawn another provider, and another live
connection cannot claim it simultaneously. Separate UUIDs can create similar
threads.

Resume uses the stored native identity without injecting a message. A provider
without recoverable native history can reject resumption. Acta retains its
transcript and reports the error instead of substituting a different session.

## Local process ownership

The controller, provider adapters, and generic process pipe have separate
roles:

- The **controller/adapter** owns lifecycle intent and results, native
  protocol interpretation, discovery descriptors and server acknowledgment
  cursors.
- The **pipe** owns child processes, idempotent input writes, and fsynced raw
  output journals. It does not understand provider methods or hold Acta
  credentials.
- The **server** persists discovered threads, frames, and established-thread
  lifecycle commands. It does not execute providers or interpret native JSON.

The normal `acta harness` embeds the pipe. Its orderly shutdown kills its
providers. `acta harness --separate-pipe` attaches to an independently running
helper, leaving providers alive when the hyperharness exits. Both modes use
the same engine. Do not run both against the same local account/server state
at once.

State is under the CLI configuration directory's
`harnesses/<server-account-hash>`: `threads/` holds adapter state and `pipe/`
holds run records and raw journals. Directories are private (0700), files are
0600, and the detached Unix socket is 0600. Exclusive locks prevent competing
local owners. `ACTA_TOKEN` is removed from provider/helper environments. Other
provider configuration and credentials remain on the development machine and
are used by the provider normally.

The detached helper is deliberately not upgraded by replacing its executable.
Updating the pipe process involves stopping its providers and terminating the
helper; ordinary hyperharness/adapter rebuilds can reattach without restarting
providers. Its startup failures are reported with the path to `pipe/pipe.log`.
The process-group and parent-death behavior is verified on Linux.

## Reliability contracts

- Spawn intent and command IDs are committed locally before side effects.
  Repeated run IDs return the existing outcome. A crash across process launch
  or an input write can leave an **uncertain** result; that action is not
  retried as though it were proven unsent.
- Captured stdout JSON and stderr diagnostics share a per-thread monotonic
  sequence across provider runs. Each record reaches the fsynced journal
  before delivery. Torn final records are truncated on recovery; complete
  corruption fails closed. Capture failure terminates the affected provider.
- Frame delivery retries from a locally persisted acknowledgment cursor. The
  server commits before acknowledging and compares duplicate sequence
  payloads. Gaps or conflicting replay fail the batch. Browser reads use the
  same cursor.
- Kill/Resume requests for established threads are durable server commands.
  Discovery and heartbeat replay pending commands to the current connection.
  Local completion is persisted before its result is delivered. Repeated
  pending commands wait for the same operation; completed results are stable.
- The connection loop does not own provider lifetimes. Server/network loss
  causes reconnect and re-advertisement; detached hyperharness loss leaves raw
  capture with the pipe. The new adapter reconciles pending commands against
  captured responses before attempting any further protocol work.
- Failure to persist controller state stops the controller from advertising or
  accepting more work. A pipe restart does not signal or adopt stored numeric
  PIDs. On Linux, a saved positive PID that the OS confirms does not exists is
  recorded as exited, allowing an explicit Resume with a new run. Its final
  outcome remains unknown, and pending input writes are not replayed. A live
  (including reused), inaccessible or missing PID remains uncertain. Recovery
  from a hard pipe crash is not automatic process adoption or relaunch.

Frames are limited to 16 MiB at capture, with bounded delivery batches.
Journals and server history are retained; there is no pruning, quota, archive
UI, or retention policy. Retained raw provider output consumes local and
server storage. Raw frames can include provider context and local paths;
access is owner-scoped, not a sanitized public transcript.

## Conversation storage and loading

My Agents opens the newest 50 assembled conversation items and loads older
history while scrolling upward. Current model, usage, and approval state are
available independently of loaded history. See [persisted conversation
storage](~acta/technical-reference/execution/thread-conversation-storage) for
the transaction, replay and pagination contracts.

Native provider children are presented as [subagent
lanes](~acta/technical-reference/execution/subagent-lanes), with parent
history cards and a scrollable running/open lane bar.

Image input and composer attachment behavior: [Image
messages](~acta/technical-reference/execution/thread-image-input).

See [Thread names](~acta/technical-reference/execution/thread-names) for
renaming and local metadata persistence.

See [Agent
notifications](~acta/technical-reference/application/thread-notifications) for
unread updates, browser alerts, subagent attention and acknowledgment
guarantees.

## Mobile composer

On touch devices, opening a conversation does not automatically focus the
composer and open the keyboard. Enter inserts a new line; select Send or use
Ctrl/Cmd+Enter to send. Desktop Enter-to-send and Shift+Enter for a new line
are unchanged. The composer stays within the visible viewport above the
software keyboard. Draft text continues to be saved as it changes, so
switching apps or reloading does not require retyping it.

The effort slider has a 44px touch region on coarse-pointer devices while
keeping its compact visual track. Approval, question, and model popups remain
constrained to the visible viewport above the software keyboard.

The connection status sits beside the thread title on desktop and mobile. Long
labels truncate to preserve the header layout; their full text is available in
the tooltip. Errors and pending native-title synchronization details stay
below the header.
