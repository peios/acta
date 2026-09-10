# Connected harnesses

ACT-61 introduces the connected hyperharness; ACT-62 adds local provider discovery.
In the UI it is called a **Harness**; Codex and Claude Code are **Providers**.
Provider discovery itself does not start agent turns. ACT-63 adds explicit Codex
thread lifecycle controls through [My Agents](threads.md).

## Connect a machine

```sh
acta2 login localhost:8081
acta2 harness
acta2 -p work harness
```

The command uses the normal selected/active profile URL and credentials,
including the normal `ACTA_TOKEN` override. It requires a human CLI session.
Agent sessions cannot run a hyperharness. There is no separate registration,
credential, permanent machine identifier or added permission.

The command runs in the foreground until Ctrl+C or SIGTERM. It establishes an
outbound WebSocket to Acta and advertises the operating-system hostname. It does
not open a listening port on the development machine. HTTPS is required except
for loopback development; redirects are never followed with credentials.
`--json` emits connection states as JSON lines; ordinary output reports connection,
retry and shutdown status. No credentials are included in these messages.

User Settings → Harnesses displays the current user's connections, updated live.
Hostnames are labels, not identities. Multiple connections with the same hostname
remain separate entries. Each transport has an ephemeral UUID used only for list
keys. Disconnecting removes that entry, and reconnecting creates a new one.
There are no offline-machine records. Superusers see only their own connections.
If the browser loses its own connection, it clears the stale list and shows a
reconnecting state instead of pretending its cached list is still current.

## Connection and authorization guarantees

- The server holds a synchronized in-memory registry, scoped by human account.
  No database migration or durable registration is involved.
- `/api/harnesses/connect` requires CLI bearer authentication, rejects browser
  origins/cookie authority, and accepts the `acta-harness-v7` WebSocket subprotocol.
  The client advertises a bounded hostname before appearing in the list.
- `/api/harnesses` returns an owner-scoped snapshot; `/api/harnesses/live`
  publishes snapshots on changes. Browser streams require exact configured
  origins when an Origin header is present and use normal session cookies.
- Both live transports recheck current account/session policy every 15 seconds.
  They send a WebSocket ping with a five-second response deadline. Lost transports
  and revoked/expired credentials are removed within approximately 20 seconds
  under normal server scheduling. Clean EOF/disconnect removes them immediately.
  Authorization is also rechecked before pushing list changes.
- A live connection counts as ongoing session activity under the existing CLI
  session policy. Absolute expiry, revocation, disablement and required MFA remain
  enforced. Logging out of the CLI session ends harnesses using that session.
- Network failures retry automatically with jittered exponential backoff, capped
  at 30 seconds. A stable connection resets backoff. Terminal authentication,
  policy or protocol failures stop the CLI; log in again using the chosen profile
  where appropriate. Cancellation interrupts dialing, reading and retry waits.
- Server shutdown cancels live connections through its base context. Connections
  and registry state are not persisted; clients advertise again after restart.
- These streams are exempt from the ordinary 15-second HTTP request context;
  handshake, authentication, reads/writes and liveness have their own bounds.
- A single Acta server process owns the live registry. Multi-replica routing and
  shared presence are not implemented; do not load-balance these endpoints across
  unrelated server processes and expect one complete list.

Reverse proxies must forward WebSocket upgrades and allow long-lived connections.
The development Vite proxy forwards WebSockets as well as ordinary API requests.

## Provider discovery

Expand a connected machine in User Settings → Harnesses to see **Codex** and
**Claude Code**. Both remain visible when not installed. Each row separates the
installed version from sign-in state. Initial checks show Checking; failures and
unrecognised output show Unknown with a short explanation on hover.

The harness finds executables on its own `PATH` and runs read-only probes:

- Codex: `codex --version`, `codex login status --help`, `codex login status`.
- Claude Code: `claude --version`, `claude auth status --help`, `claude auth status`.

The help preflight must identify the expected status command before invoking it.
An unsupported CLI is not probed by guessing a command, because older versions
may interpret unrecognised commands as conversational prompts. An unrecognised
version skips the sign-in probe. Versions are reported, not certified compatible
with future execution adapters.

Sign-in is the local CLI's report of stored authentication. It does **not** verify
network access, subscription entitlement, model availability, credential freshness,
or remaining usage. No login/logout/install operation, prompt, or model turn is
performed. The harness never reads provider credential files directly.

Each provider is checked immediately, then 30 seconds after its previous check
finishes. Independent serial loops prevent overlapping checks and allow one
provider to update while the other is slow. Each complete check has a five-second
deadline; command output is limited to 32 KiB per stream. stdin is closed, no shell
is used, and `ACTA_TOKEN` is removed from the child environment. Unix cancellation
kills the probe process group, including children that retain its output pipes.
Provider discovery is independent of the server reconnect loop and heartbeat.

Only adapter ID, installation state, normalised version, sign-in state and fixed
issue codes are advertised. Native status output can contain personal information;
it is decoded locally and is never sent to Acta or logged. The server validates
these fields and retains only the current connection's snapshot. A provider
change updates the same connection entry; reconnecting sends the latest complete
snapshot. No durable provider inventory or machine identity is introduced.

Both CLI and server must use protocol `acta-harness-v7`; update them together and
restart existing harness commands. Browser pages must reload after this protocol
change. Provider installation or sign-in changes are detected on the next check.

Official command references: [Codex CLI](https://developers.openai.com/codex/cli/reference/)
and [Claude Code CLI](https://code.claude.com/docs/en/cli-usage).

## Thread execution and development restarts

My Agents can create Codex and Claude Code threads on a connection. Thread identities and history
are durable; the harness connection remains ephemeral. An established thread is
available when a current authorized connection advertises it. Disconnect means
unreachable, not necessarily that its provider has exited.

Normally the provider pipe runs within `acta2 harness`, so exiting that command
terminates its provider processes. For development:

```sh
acta2 harness --separate-pipe
```

This starts or attaches to the same generic pipe engine in a detached helper.
Provider processes and raw capture survive hyperharness restarts. The replaceable
hyperharness retains the provider adapters; the helper has no Acta credential and no
provider-specific interpretation. It serves a private Unix-domain socket, not a
network port. See [Provider threads](threads.md) for ownership, shutdown,
durability and failure boundaries.

## Validation

Registry tests cover concurrent churn, notification coalescing, owner isolation,
duplicate hostnames and cleanup. WebSocket client tests cover reconnect,
cancellation, terminal credential failures and refusal to follow redirects.
PostgreSQL-backed integration tests exercise browser/CLI credential boundaries,
live additions/removals and authorization revocation after the handshake.

Provider tests cover recognised signed-in/out responses, unsupported command
surfaces, malformed output, bounded output, timeouts, descendant cancellation,
credential-field exclusion, independent refresh and cancellation. Registry and
WebSocket tests cover provider updates without identity changes, replay after
reconnection, copy isolation and rejection of invalid or disconnected updates.

## Thread frame mapping

Harness protocol v7 carries complete debug/normalized frame bundles. See
[the thread frame contract](thread-frame-schema.md) for kinds, replay identity and
commitment semantics. Reviewed frames have dedicated UI rendering; remaining
frames retain raw JSON. Creation opens an empty thread; only an explicit user
submission starts its first model turn.
A running thread may be advertised as uncommitted while saved history is checked.
Runtime version/platform metadata belongs to discovery, not the conversation.

Upgrade the server and CLI together. A detached pipe can remain running to retain
its provider processes. New controller outputs are persisted before delivery;
previously acknowledged raw captures are not reinterpreted during upgrade.

My Agents omits the shared page header so thread content starts at the top of
the main area. On small screens, navigation remains available beside the thread
heading (or above the empty state).

The thread header uses a power icon for Kill/Resume. Its adjacent vertical
three-dot options menu contains the Show debug frames switch.
Debug frames are hidden by default; Unknown Frames remain visible. Opening a
thread focuses its composer, and sending returns focus there without scrolling.

Threads have a bottom composer for messages sent to an idle or working, connected provider.
Enter sends; Shift+Enter inserts a newline. Drafts and pending delivery are stored
in this browser per account and thread, surviving navigation and reload. The
frame feed scrolls above the composer.

A local grey user bubble appears immediately with Sending… below it, and the
composer clears at the same time. A confirmed failure restores the submitted
text and attachments only if the composer has not been edited since sending,
even if later edits were erased. Uncertain delivery stays in the outbox without
restoring the draft. A provider
message with the matching Acta `submission_id` replaces that placeholder; text
alone is never used for correlation. Command status also confirms whether that
exact provider echo has reached stored conversation history. This releases a
restored outbox even when its message is outside the newest history page; it
does not resend the message or discard a newer draft. The placeholder remains
until its provider message arrives. Completed user
messages use the existing blue styling.

Sending uses a durable `send` hypercontrol command containing an immutable UUID,
run UUID and plain text (up to 64 KiB). The controller serializes it with lifecycle
operations. Codex uses a journalled status read, then `turn/start` when idle or
`turn/steer` bound to the current turn ID when active. Claude accepts messages
through its existing input stream while running. Replayed commands retain their
original target and write ID, preventing duplicate input or retargeting a later
turn after a reconnect. If the active Codex turn ends before it accepts the
message, the send is rejected and the draft can be retried. The adapter maps Codex's echoed `clientId` only when it
matches a known Acta submission for that run.

Outcomes distinguish accepted, rejected and uncertain delivery. Only a confirmed
rejection offers a fresh Retry; Check again preserves the original command UUID.
A lost connection never automatically submits a second turn. The pipe's durable
write identity protects recovery after controller restart; a later correlated
message can resolve an uncertain outcome as accepted. Failed message submission
does not kill the provider. The composer offers Stop turn while work is active,
including while approval is pending; this interrupts the turn without killing
the provider. Ordinary and tool-use interruption markers stay in debug history,
with the Turn interrupted line supplying their normal conversation summary.
While working, typing reveals Send beside Stop; Enter sends and Shift+Enter
inserts a newline. Model and permission changes remain idle-only. Claude may
queue input until it can consume it; its queue acknowledgement settles delivery
without prematurely changing which turn owns ongoing output. An Acta-managed
message queue is not part of this feature.

The composer starts as a compact single line with its send icon alongside,
then grows with the draft. A gap separates it from the feed, whose scrollbar
has a dedicated inset beside the cards.

The composer displays model settings from the latest `thread/configuration`
snapshot for the current provider run. Its popup loads the model catalogue from
that running provider, including supported effort levels and fast-mode support.
Catalogues are cached per account/thread in this browser (including reloads) and
per thread in the hyperharness, for up to 15 minutes. A new provider run fetches a
new catalogue. This avoids loading the list on every navigation and fetching it
again for each settings change; malformed browser caches are discarded.

Models appear as an always-visible list inside the popup. Effort uses a stepped
slider showing the supported levels; dragging previews the level and release
applies it once. Arrow keys also adjust the slider. A lightning-bolt button next
to Effort toggles fast mode, highlights when enabled and exposes the provider's
speed/usage description on hover.
Model changes retain compatible effort/speed choices; otherwise they use the new
model's default effort and disable unsupported fast mode. The provider's speed
and increased-usage description appears beside these controls.

Model, effort and fast-mode changes apply immediately while the thread is idle,
without sending a message or starting a turn. Controls show pending changes until
the provider reports its effective configuration. Sending and killing from this
page are disabled while a settings update is pending. A rejected change restores
the confirmed selection and shows its error. Pending changes survive browser
reload; Check again reuses the same immutable command UUID.

`models` and `configure` hypercontrol commands are scoped to the current run and
serialized with other thread operations. The hyperharness validates each change
against its cached provider catalogue and confirms idle state. Codex uses
`model/list` and `thread/settings/update`; fast mode maps to the advertised
`priority` tier and normal mode to `default`. Only model, effort and speed are
written: filesystem, network and approval settings are preserved. The native
settings notification becomes the existing generic configuration frame.

The detached pipe's durable write IDs prevent duplicate writes during recovery.
Late captured acknowledgements can resolve an uncertain settings outcome.
Confirmed settings are stored locally and restored through the same settings API
before a resumed thread is advertised as ready. No provider credentials or
hardcoded model catalogue are stored on the Acta server. These controls currently
support Codex; Claude provider thread execution remains deferred.

MCP startup appears inline at the first starting frame's position in the feed.
A single server shows Starting tool (name), without a count or disclosure. Further
starts in the same provider run join the open batch, turning it into an expandable
Starting tools item with a ready count. Unrelated intervening frames keep their
positions. When every remaining server is ready, the batch seals as Tools started
(N); a singleton shows Tool started (name). Later starts create a new item.

A failed or cancelled server leaves its batch and gets a separate error item at
the failure frame's position. Empty batches disappear; a batch that became a stack
keeps its disclosure even if only one successful server remains. Duplicate status
observations do not inflate counts. A ready observation without a captured start
appears as a completed singleton. The feed is derived from captured history, so
loading more frames and reopening a thread reproduce the same grouping. Raw debug
captures remain available through Show debug frames. Unknown Frames always remain
visible, even with debug frames hidden, so unhandled provider frames cannot be
overlooked.

The latest `thread/status` snapshot powers a quiet Working… indicator immediately
above the composer, outside the scrolling feed: active shows it, idle clears it.
The `thread/status` card is consumed by this indicator and hidden from the feed.
`turn/started` is hidden and has no UI behaviour; turn completion frames remain in
the feed for now and do not control the indicator. Only the current provider run can
activate it, and disconnected, killed, or unreadable threads do not claim to be
working. The subtle pulse respects reduced-motion preferences. This does not
enable sending messages.

User message snapshots render as one right-aligned plain-text bubble, anchored at
the first observation and updated by message ID within the Acta thread. In-progress
messages are grey; completion changes the bubble to a restrained blue tint with a
short colour transition (disabled for reduced motion). Completed content cannot
regress from a later in-progress observation. Unsupported content retains its raw
card, and the debug captures remain available separately. These are provider
lifecycle states, not client delivery acknowledgements.

Assistant messages render as left-aligned Markdown without a bubble, using the
existing Markdown renderer. Start snapshots and text deltas update a single item
at its first frame's position, keyed by message ID within the Acta thread. Empty
starts occupy no visible space. Each captured delta is applied once by its frame
identity; repeated text in distinct deltas is preserved. A completed snapshot
replaces accumulated text, and later starts or deltas cannot alter it. Commentary
and final-answer phases remain available in the projection but share the same
styling for now. Raw debug captures remain separately available.

Compact gauges beside the power control consume `usage/context` and `usage/account`
cards. Context fullness uses only reported `used_tokens / capacity_tokens`; unknown
usage shows a question mark, never an estimate from cumulative tokens. Each reported
account limit window has its own gauge, with a short duration label. Gauges fill
with usage, become amber at 80% and red at 100%. Click/tap opens details including
exact percentage, context counts or limit reset time, and when the snapshot was
last reported. Hover and accessible labels also describe the gauge. Snapshots are
scoped to the current provider run; each account bucket replaces its previous
windows rather than accumulating obsolete windows. Unknown values stay unknown.

Turn completion renders once as a quiet rule with Turn ended and its reported
duration. Failed and interrupted outcomes have distinct labels. Expanding reveals
the recorded time, exact duration and available error text, plus the latest model
call's token breakdown matched by thread, run and turn IDs. Late matching usage
enriches the original divider. Repeated completions do not create extra dividers.
Current usage frames do not establish a reliable per-turn token total: cumulative
snapshots are never summed, and last-call counts are explicitly labelled rather
than shown as turn totals. Missing metrics remain absent.


## Claude Code execution

Claude uses the same local controller, durable pipe and server frame stream as
Codex. The local child runs `claude --print` with streaming JSON input/output,
partial messages and user-message replay. Local authentication, hooks and MCP
configuration remain the provider's own configuration. Acta does not copy Claude
credentials onto the server or auto-approve provider requests.

Create, Kill, Resume, idle and mid-turn message sending and model settings work for both
providers. Claude's echoed UUID correlates with the immutable Acta submission;
local command receipts are consumed separately. Resume reuses the native session
UUID, restores the confirmed model settings and never sends the creation fixture.
Commitment requires a complete stored user record in the matching local Claude
session file, rather than merely seeing a process or a filename.

Claude model aliases advertise their resolved model IDs. Models without effort
levels show Not applicable. Where no default effort is advertised, Default means
letting Claude choose. Settings are read back after applying them: the effective
model/effort and actual fast-mode state confirm the UI, without rewriting the
original command payload. Failed or uncertain changes retain an explicit result.

Read-only observations refresh at startup, after results and every 30 seconds:
MCP status, context summary and account allowance. Pending MCP startup is checked
every second until settled. Context summaries are marked estimated. Observations
do not send model turns or perform an extra token-count API request. Current
Claude permission modes are mapped independently of OS access boundaries; absent
filesystem/network evidence stays unknown.

See [Claude integration review](claude-provider-review.md) for the verified native
version, current UI limitations and deliberately deferred common features.

Empty Codex terminal-interaction polls for a known shell operation are Dropped
Frames; output and completion come from their own events. Nonempty stdin and
unmatched identities remain Unknown pending a reviewed representation.

### Claude file diffs

Successful native Write and Edit results attach an operation diff and update the
turn's collected Changes panel. The adapter reconstructs before/after contents
from provider-reported snapshots and actual result fields, cross-checks supplied
structured patches, and joins edits only when successive snapshots match exactly.
Created files remain additions; edits reversed within a turn disappear from its
net diff. Files are listed in path order. No filesystem reads participate, so
replay and reconnection preserve the same result.

Generated patch headers quote unusual filenames using Git-compatible escapes,
so spaces, quotes, tabs and newlines remain part of the filename. The operation
and collected diff use the same encoding.

When continuity or a snapshot cannot be verified, the panel shows **Individual
changes**, preserving reported patches in arrival order. If a successful operation
provides no usable patch, the panel also notes that its diff is incomplete. This
covers native Write/Edit operations; it does not claim to reconstruct changes
made through shell commands or external processes. Denied/failed operations do
not contribute applied changes. Raw provider evidence remains available.

The turn/diff frame accepts optional `mode` (`combined` or `sequential`) and
`incomplete` fields. Codex's authoritative turn diff retains its existing shape.
The conversation projection persists these fields alongside the latest diff.

### Context measurement freshness

The context gauge keeps the most recent usable measurement for the current
provider run. Usage updates with missing context counts continue to update turn
statistics and history but do not replace the gauge measurement. Its popup's
**Last reported** timestamp remains the original measurement time, rather than
being refreshed by later token-usage events. A fresh measurement, including zero
usage, replaces it; a new provider run starts without a carried-over estimate.
Existing conversation projections are rebuilt from stored frames on upgrade so
this retention also applies to saved history.

### Tool discovery results

Claude ToolSearch results containing tool references complete the existing tool
card with an available-tool list. Plain text remains in order. Malformed or
unrecognized content, or tool references returned by an unreviewed tool, remain
Unknown Frames rather than being partially consumed. Raw references remain in
the corresponding debug frame.


### Background commands

Claude background Bash commands keep their original tool card marked Running in
background until a correlated task completion/failure arrives. A compact,
expandable completion notice appears where the provider delivers the notification.
The composer footer shows the running background count alongside Working or
Waiting for approval, and keeps showing it after the launching turn ends.
This state is persisted separately from paginated history, so refresh and loading
older messages cannot lose the count. Unavailable provider runs do not claim
their tasks are still running. Automatic replies to idle-time completions have
their own turn ending rather than reopening an already completed turn.

Codex uses the same presentation for commands that still have a running process
when their turn ends. Their later command completion updates the original card,
clears the count, and places a completion notice at that event. Commands without
a provider process ID are not assumed to be background work. Codex notices stay
at the completion event because this provider supplies no separate context echo.

Harness startup also retries transient failures of the initial account lookup
(connection failures, HTTP 429 and server errors) with bounded exponential backoff.
Authentication/permission failures, redirects and invalid account responses still
fail explicitly. Ctrl+C cancels the wait. No local process ownership or provider
startup occurs until the account identity has been verified.

### Provider questions

Claude AskUserQuestion and Codex request_user_input use a question card in the
conversation and an automatically opened composer popup. Select the requested
number of choices, or type a custom answer, then choose Answer. Draft answers
are shared between both views and saved in this browser for the account/thread/run.
Answering uses a durable command bound to the original request UUID and provider
run. Repeat deliveries cannot change a submitted answer. The harness validates
question identities and answer cardinality against its original provider capture;
the browser cannot replace the tool input or native reply routing.

A confirmed submission displays the answers on the card. Stop cancels a waiting
turn through the existing interrupt control. Provider resolution closes a question,
even if the card is outside the loaded history page. Codex questions explicitly
marked nonblocking remain answerable after turn completion until the provider
resolves them. Secret-input requests and visual option previews remain unreviewed
Unknown Frames rather than being silently rendered as ordinary text.

Context compaction appears as “Compacting context…” beside the composer. Once
finished, a compact conversation notice shows token counts and duration when
available. Expand it to read the provider's summary when supplied. Codex currently
supplies the compaction lifecycle without summary or before/after counts. This
presentation does not add a new compaction command or erase Acta history.

Recognized Claude slash-command echoes are displayed as the original `/command`
plus arguments rather than the provider's command-name/message/args wrapper.

Resolved permission/question requests without an Acta command outcome are checked
once rather than polled indefinitely. Pending requests, submitted commands and
uncertain delivery continue to be checked until their outcome is known.

When Claude repeats a stopped-background-command notice after resuming, Acta
shows it at the resume point as well. The original tool card remains unchanged.
Repeated transport delivery within that run does not duplicate the notice; if
Claude later echoes it into context, that run's notice moves to the echo position.

Image tool results supplied by Claude appear as thumbnails when the tool card is
expanded. Click a thumbnail for a larger preview; Escape closes it. Text output
can accompany images. A local filename alone does not transfer an image to Acta.
Whole PDFs returned by a tool appear as attachments with Open and Download
links. Open uses the browser's document handler; individual PDF pages returned
as images use the image preview instead.

Claude local commands can acknowledge a submitted command without echoing its
text. Acta settles the pending message from a matching provider command UUID,
using the persisted send text and exact run identity. This prevents a completed
local command from leaving the composer stuck on Sending.

## Consecutive thinking entries

Adjacent visible thinking entries in the same thread, provider run, turn and
subagent lane render as one expandable item. Their text is joined in order with
paragraph breaks. The activity indicator stays active while any constituent is
still thinking. Thinking text opens automatically and stays expanded after the
thinking block completes, until its owning turn ends (including interruption or
failure). At turn end it collapses to one line and can be manually reopened. Its elapsed time spans the known start/end times;
its token estimate is summed only when every constituent supplies an estimate.
Messages, tool calls, Unknown Frames and other visible entries split groups.
Hidden debug/state frames do not split them; enabling debug reveals those boundaries.

This is presentation grouping over the independently persisted thinking snapshots.
It works with existing history and incremental updates without rewriting captures.
Loading an older page can extend a group; constituent IDs remain scroll anchors.

Claude foreground Bash tasks may also emit `task_started` with
`is_backgrounded: false`, followed by `task_notification`. The adapter stores
that task-to-tool identity in its checkpoint and classifies both as local debug
bookkeeping. The ordinary tool result supplies completion, stdout and stderr;
these notices do not add a background indicator or duplicate completion notice.
If the same task later backgrounds, it switches to the normal background lifecycle.
Unrecognized identities or task shapes remain Unknown Frames.

## Older turn activity summaries

After a newer turn starts, consecutive successful tool calls and completed
thinking from older turns render under an expandable **Worked for …** line.
The latest turn remains visible even after completion. Expanding shows the
original cards and their own details. Messages, approvals, questions, errors,
unknown frames and unfinished work split summaries; running background tools
stay visible. Groups never cross a turn, provider run or subagent lane.

Duration spans the first known activity start to the last completion, so
simultaneous tools are not double-counted. When a provider omits lifecycle timing,
the persisted conversation item's observed start/completion timestamps supply
the interval instead. If neither source has a complete interval the line says
**Worked**. The current turn comes from the persisted turn-start singleton, so
its older activity collapses before its first visible message arrives. Grouping
uses existing snapshots without rewriting history and preserves member scroll
anchors when older pages extend groups.

A child's Claude foreground-task notification can omit both `parent_tool_use_id`
and `task_type`. Its saved foreground task ID plus `tool_use_id` route it to the
owning subagent lane, just as background-task identities do. The lookup requires
the current provider run and survives adapter checkpoint restoration; unrelated
identities remain Unknown Frames. The ordinary child tool result still supplies
output and completion.

### Task references in tool results

Agent tool cards recognise task UUIDs in known task arguments and JSON results
when the MCP connection name contains `acta` (case-insensitive). Claude's
`mcp__name__tool` and Codex's `name/tool` forms are supported, including JSON MCP
text/structured-result envelopes. Task results, lists, ancestors and explicit
`task_id` fields can supply references. Prose, arbitrary object IDs and task
numbers alone do not establish a link.

Before enabling a link, the browser verifies the UUID through this server's task
API using the signed-in user's permissions. Links use current titles and
references. Unresolved references remain plain text; there is no fallback to a
similarly numbered task or automatic cross-server navigation. Verification reads
are coalesced, limited to four concurrent requests and briefly cached per page.

Opening a task preserves the mounted chat and draft. The shared task editor uses
the same resizable side panel, modal and mobile fullscreen layout as Workspaces.
The `task` query parameter preserves selection across refresh and browser history.
Subtask navigation, activity, reparenting and editing use the existing components
and live workspace revision feed.

### Following live output

Scrolling upward pauses automatic following immediately, including small wheel,
touch or keyboard movements near the bottom. Incoming frames and layout changes
preserve the reading anchor without re-enabling following. Scrolling back to the
bottom resumes it; End explicitly returns to the live end. Queued render updates
cannot override a newer reader movement. Programmatic anchor corrections do not
count as user scrolling, and unchanged offsets are not written because doing so
interrupts native smooth scrolling.

The scroll intent rules have regression tests in
`web/tests/thread-scroll-intent.test.mjs`. For browser regression checks,
`web/tests/fixtures/ThreadScrollReview.svelte` renders the actual transcript with
rapid simulated output and older-page insertion. Temporarily mount it in a local
review route, check small upward scrolling, resuming at the bottom, End, and
history loading, then remove the route before the final build. No provider calls
or live conversation mutations are needed for this check.

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

Claude tool heartbeats identify the real call through `parent_tool_use_id` and a
synthetic heartbeat ID. Acta resolves the existing call in its current-run lane,
marks pending execution as running, and supplies a start-time estimate from the
reported elapsed time only when no start time exists. Repeated heartbeats remain
local debug frames; they neither create conversation rows nor revive completed
work. Unknown tool identities and malformed elapsed times remain visible.

Claude Git push notifications render as compact “Pushed main” history items with
the repository directory name. Hover reveals the full directory and timestamp.
These use the provider-independent `vcs/push` frame (branch, cwd, optional turn
identity); other unreviewed Git event kinds remain Unknown Frames.

### Memory references in tool results

Acta `memory_get`, `memory_save`, `memory_recall` and `memory_delete` calls also
show verified reference chips and open memories without leaving the conversation.
They share the task viewer's responsive panel/modal/fullscreen shell. See
[memories](memories.md#references-in-agent-conversations) for recognition, access
verification, navigation and editing behavior.
