# Provider threads (ACT-63)

## Current surface

**My Agents** is a sidebar scope. **New thread** selects a connected harness,
Codex or Claude Code, and an absolute working directory on that development
machine. Each thread has a dedicated local provider process.

Reviewed frames render as messages, working state, MCP status, usage gauges and
turn summaries. Unknown Frames always remain visible; other debug captures are
optional. Creation opens an empty thread without sending a message or starting a
model turn. The composer
sends messages to an idle or working connected provider; model settings apply without a
new model turn. See [Harnesses](harnesses.md) for current interaction details and
[Claude integration review](claude-provider-review.md) for provider differences.

The provider's existing local MCP configuration remains its source of Acta access;
thread ownership does not assign a separate Acta agent identity automatically.

**Kill** sends SIGINT to the dedicated provider process group, waits up to five
seconds, then escalates to SIGKILL and waits again. **Resume** launches another
process for the same native provider session. Stop is reserved for interrupting a
future turn. The page reports command outcomes, including provider errors.

All thread data and controls require the owning human account. Superuser does
not bypass that boundary. A connected CLI must also authenticate as a human.

## Creation and discovery

1. The browser generates a fresh Acta thread UUID and posts a live Start request
   to a selected connection. The server writes no creation or thread row.
2. The local controller records spawn intent before starting the provider. It
   initializes the provider, establishes its native identity, and submits the one-time test
   message using a durable run/method request ID.
3. Provider readiness makes the thread discoverable, initially uncommitted.
   Verified stored user history subsequently marks it committed.
   The authorized inventory advertisement creates its server record.
4. The browser watches the UUID for about 30 seconds and opens it on discovery.
   Timeout or disconnect produces a synthetic notice, not a cancellation or
   expiry tombstone. Late discovery remains valid.

Separate creates can produce separate similar threads. The same UUID is
idempotent locally and cannot spawn another provider. A different current
connection cannot simultaneously claim the same thread. Hostnames are labels;
there is no permanent harness identity.

Previously created empty Codex sessions may still return `no rollout found`:
they have no native history to recover. This change applies to new creations and
does not replace old native IDs. The test message is never sent during Resume,
normal reconnect, or reattachment to a completed creation. Recovery of an
interrupted creation reconciles the same durable input ID and captured response,
so it does not blindly send another turn. Acta retains history and reports any
provider resumption error; it never silently substitutes another native session. See [the lifecycle investigation](thread-lifecycle-investigation.md).

## Local process ownership

The controller, provider adapters and generic process pipe have separate roles:

- The **controller/adapter** owns lifecycle intent and results, native protocol
  interpretation, discovery descriptors and server acknowledgement cursors.
- The **pipe** owns child processes, idempotent input writes and fsynced raw
  output journals. It does not understand provider methods or hold Acta credentials.
- The **server** persists discovered threads, frames and established-thread
  lifecycle commands. It does not execute providers or interpret native JSON.

The normal `acta2 harness` embeds the pipe. Its orderly shutdown kills its
providers. `acta2 harness --separate-pipe` attaches to an independently running
helper, leaving providers alive when the hyperharness exits. Both modes use the
same engine. Do not run both against the same local account/server state at once.

State is under the CLI configuration directory's `harnesses/<server-account-hash>`:
`threads/` holds adapter state and `pipe/` holds run records and raw journals.
Directories are private (0700), files are 0600, and the detached Unix socket is
0600. Exclusive locks prevent competing local owners. `ACTA_TOKEN` is removed
from provider/helper environments. Other provider configuration and credentials
remain on the development machine and are used by the provider normally.

The detached helper is deliberately not upgraded by replacing its executable.
Stop its providers first and terminate that helper to update the pipe itself;
ordinary hyperharness/adapter rebuilds can reattach without restarting providers.
Its startup failures are reported with the path to `pipe/pipe.log`.
The process-group and parent-death behavior is verified on Linux.

## Reliability contracts

- Spawn intent and command IDs are committed locally before side effects.
  Repeated run IDs return the existing outcome. A crash across process launch or
  an input write can leave an **uncertain** result; that action is not retried as
  though it were proven unsent.
- Captured stdout JSON and stderr diagnostics share a per-thread monotonic
  sequence across provider runs. Each record reaches the fsynced journal before
  delivery. Torn final records are truncated on recovery; complete corruption
  fails closed. Capture failure terminates the affected provider.
- Frame delivery retries from a locally persisted acknowledgement cursor. The
  server commits before acknowledging and compares duplicate sequence payloads.
  Gaps or conflicting replay fail the batch. Browser reads use the same cursor.
- Kill/Resume requests for established threads are durable server commands.
  Discovery and heartbeat replay pending commands to the current connection.
  Local completion is persisted before its result is delivered. Repeated
  pending commands wait for the same operation; completed results are stable.
- The connection loop does not own provider lifetimes. Server/network loss causes
  reconnect and re-advertisement; detached hyperharness loss leaves raw capture
  with the pipe. The new adapter reconciles pending commands against captured
  responses before attempting any further protocol work.
- Failure to persist controller state stops the controller from advertising or
  accepting more work. A pipe restart does not signal or adopt stored numeric
  PIDs. On Linux, a saved positive PID that the OS confirms no longer exists is
  recorded as exited, allowing an explicit Resume with a new run. Its final
  outcome remains unknown, and pending input writes are not replayed. A live
  (including reused), inaccessible or missing PID remains uncertain. Recovery
  from a hard pipe crash is not automatic process adoption or relaunch.

Frames are limited to 16 MiB at capture, with bounded delivery batches. Journals
and server history are retained in this slice; there is no pruning, quota,
archive UI or retention policy yet. Operators must allow disk space for raw
provider output. Raw frames can include provider context and local paths; access
is owner-scoped, not a sanitized public transcript.

## Boundaries and validation

The initial slice added lifecycle and raw observation. Subsequent slices add
message sending, model controls and [permission modes and approvals](thread-approvals.md).
Task attachment and cross-machine session transfer remain outside this work. There is one
server presence registry; multi-replica routing remains outside this slice.

Tests cover repeated spawn/input delivery, pending command replay, missing native
sessions without replacement, capture failure isolation, torn/corrupt journals,
controller reattachment without process replacement, acknowledgement bounds,
failed persistence, owner isolation, precise browser JSON, and transactional
frame deduplication. The live checks exercise the actual installed Codex process
and the separate helper, without sending a model turn.

### Live verification, 7 September 2026

On localhost:8081 with Codex 0.153.4, browser creation auto-opened a discovered
thread and rendered 23 startup frames. Disconnecting and restarting the
hyperharness with `--separate-pipe` retained the same provider PID and native ID.
The UI changed to unavailable while disconnected, then recovered with the same
23 frames. Restarting Acta's server also rediscovered the same running provider
without duplicate frames. Kill exited the dedicated process; Resume returned
Codex's `no rollout found`, which the UI displayed while retaining the thread and
its history (26 captured records after the attempted Resume). Test providers were
terminated; no model turn or user prompt was sent.

`make check build` passed, including 36 frontend tests and zero Svelte errors or
warnings. `go test -race -count=1 ./...` passed with isolated PostgreSQL integration
schemas. An additional race-enabled pending-Start regression passed: reconnecting
an adapter after the pipe accepted Start reused the same process and produced
only one native Start response.

### First-message follow-up

Jack approved the one-time Test Message fixture to exercise real provider
persistence with minimal UI. Live validation on Codex 0.153.4 confirmed that a
new legacy-history thread saved its rollout, completed the test turn, and
successfully resumed the same native ID after Kill. The resume response contained
one turn with the original Test Message. The local pipe recorded exactly one
turn/start write across both provider runs. This supersedes the earlier empty
session limitation for new fixture-created threads, not existing empty sessions.

## Conversation storage and loading

My Agents opens the newest 50 assembled conversation items and loads older
history while scrolling upward. Current model, usage and approval state are
available independently of loaded history. See
[persisted conversation storage](thread-conversation-storage.md) for the
transaction, replay and pagination contracts.

Native provider children are presented as [subagent lanes](subagent-lanes.md),
with parent history cards and a scrollable running/open lane bar.

Image input and composer attachment behaviour: [Image messages](thread-image-input.md).

See [Thread names](thread-names.md) for renaming and local metadata persistence.

See [Agent notifications](thread-notifications.md) for unread updates, browser
alerts, subagent attention and acknowledgement guarantees.
