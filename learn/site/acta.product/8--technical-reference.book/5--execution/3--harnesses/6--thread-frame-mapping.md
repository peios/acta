---
title: "Thread Frame Mapping"
description: >-
  Message delivery, outbox reconciliation, effective model settings, startup
  groups, and usage presentation.
---

Harness protocol v7 carries complete debug/normalized frame bundles. See [the
thread frame
contract](~acta/technical-reference/frames/thread-frame-schema/overview) for
kinds, replay identity and commitment semantics. Reviewed frames have
dedicated UI rendering; remaining frames retain raw JSON. Creation opens an
empty thread; only an explicit user submission starts its first model turn. A
running thread can be advertised as uncommitted while saved history is
checked. Runtime version/platform metadata belongs to discovery, not the
conversation.

Protocol upgrades apply to both server and CLI. A detached pipe can remain
running to retain its provider processes. New controller outputs are persisted
before delivery; previously acknowledged raw captures are not reinterpreted
during upgrade.

My Agents omits the shared page header so thread content starts at the top of
the main area. On small screens, navigation remains available beside the
thread heading (or above the empty state).

The thread header uses a power icon for Kill/Resume. Its adjacent vertical
three-dot options menu contains the Show debug frames switch. Debug frames are
hidden by default; Unknown Frames remain visible. Desktop thread navigation
focuses the composer. Touch navigation does not open the software keyboard
automatically.

Threads have a bottom composer for messages sent to an idle or working,
connected provider. Enter sends; Shift+Enter inserts a newline. Drafts and
pending delivery are stored in this browser per account and thread, surviving
navigation and reload. The frame feed scrolls above the composer.

A local gray user bubble appears immediately with Sending… below it, and the
composer clears at the same time. A confirmed failure restores the submitted
text and attachments only if the composer has not been edited since sending,
even if later edits were erased. Uncertain delivery stays in the outbox
without restoring the draft. A provider message with the matching Acta
`submission_id` replaces that placeholder; text alone is never used for
correlation. Command status also confirms whether that exact provider echo has
reached stored conversation history. This releases a restored outbox even when
its message is outside the newest history page; it does not resend the message
or discard a newer draft. The placeholder remains until its provider message
arrives. Completed user messages use the existing blue styling.

Sending uses a durable `send` hypercontrol command containing an immutable
UUID, run UUID and plain text (up to 64 KiB). The controller serializes it
with lifecycle operations. Codex uses a journaled status read, then
`turn/start` when idle or `turn/steer` bound to the current turn ID when
active. Claude accepts messages through its existing input stream while
running. Replayed commands retain their original target and write ID,
preventing duplicate input or retargeting a later turn after a reconnect. If
the active Codex turn ends before it accepts the message, the send is rejected
and the draft can be retried. The adapter maps Codex's echoed `clientId` only
when it matches a known Acta submission for that run.

Outcomes distinguish accepted, rejected, and uncertain delivery. Only a
confirmed rejection offers a fresh Retry; Check again preserves the original
command UUID. A lost connection never automatically submits a second turn. The
pipe's durable write identity protects recovery after controller restart; a
later correlated message can resolve an uncertain outcome as accepted. Failed
message submission does not kill the provider. The composer offers Stop turn
while work is active, including while approval is pending; this interrupts the
turn without killing the provider. Ordinary and tool-use interruption markers
stay in debug history, with the Turn interrupted line supplying their normal
conversation summary. While working, typing reveals Send beside Stop; Enter
sends and Shift+Enter inserts a newline. Model changes are idle-only.
Permission changes are available during a turn and retain pending approval
requests. Claude can queue input until it can consume it; its queue
acknowledgment settles delivery without prematurely changing which turn owns
ongoing output. An Acta-managed message queue is not part of this feature.

The composer starts as a compact single line with its send icon alongside,
then grows with the draft. A gap separates it from the feed, whose scrollbar
has a dedicated inset beside the cards.

The composer displays model settings from the latest `thread/configuration`
snapshot for the current provider run. Its popup loads the model catalog from
that running provider, including supported effort levels and fast-mode
support. Catalogs are cached per account/thread in this browser (including
reloads) and per thread in the hyperharness, for up to 15 minutes. A new
provider run fetches a new catalog. This avoids loading the list on every
navigation and fetching it again for each settings change; malformed browser
caches are discarded.

Models appear as an always-visible list inside the popup. Effort uses a
stepped slider showing the supported levels; dragging previews the level and
release applies it once. Arrow keys also adjust the slider. A lightning-bolt
button next to Effort toggles fast mode, highlights when enabled and exposes
the provider's speed/usage description on hover. Model changes retain
compatible effort/speed choices; otherwise they use the new model's default
effort and disable unsupported fast mode. The provider's speed and
increased-usage description appears beside these controls.

Model, effort and fast-mode changes apply immediately while the thread is
idle, without sending a message or starting a turn. Controls show pending
changes until the provider reports its effective configuration. Sending and
killing from this page are disabled while a settings update is pending. A
rejected change restores the confirmed selection and shows its error. Pending
changes survive browser reload; Check again reuses the same immutable command
UUID.

`models` and `configure` hypercontrol commands are scoped to the current run
and serialized with other thread operations. The hyperharness validates each
change against its cached provider catalog and confirms idle state. Codex uses
`model/list` and `thread/settings/update`; fast mode maps to the advertised
`priority` tier and normal mode to `default`. Only model, effort, and speed
are written: filesystem, network, and approval settings are preserved. The
native settings notification becomes the existing generic configuration frame.

The detached pipe's durable write IDs prevent duplicate writes during
recovery. Late captured acknowledgments can resolve an uncertain settings
outcome. Confirmed settings are stored locally and restored through the same
settings API before a resumed thread is advertised as ready. No provider
credentials or hardcoded model catalog are stored on the Acta server.
Provider-specific settings are confirmed through the corresponding Codex or
Claude adapter.

MCP startup appears inline at the first starting frame's position in the feed.
A single server shows Starting tool (name), without a count or disclosure.
Further starts in the same provider run join the open batch, turning it into
an expandable Starting tools item with a ready count. Unrelated intervening
frames keep their positions. When every remaining server is ready, the batch
seals as Tools started (N); a singleton shows Tool started (name). Later
starts create a new item.

A failed or canceled server leaves its batch and gets a separate error item at
the failure frame's position. Empty batches disappear; a batch that became a
stack keeps its disclosure even if only one successful server remains.
Duplicate status observations do not inflate counts. A ready observation
without a captured start appears as a completed singleton. The feed is derived
from captured history, so loading more frames and reopening a thread reproduce
the same grouping. Raw debug captures remain available through Show debug
frames. Unknown Frames always remain visible, even with debug frames hidden,
so unhandled provider frames cannot be overlooked.

The latest `thread/status` snapshot powers a quiet Working… indicator
immediately above the composer, outside the scrolling feed: active shows it,
idle clears it. The `thread/status` card is consumed by this indicator and
hidden from the feed. `turn/started` is hidden and has no UI behavior; turn
completion frames remain in the feed and do not control the indicator. Only
the current provider run can activate it, and disconnected, killed, or
unreadable threads do not claim to be working. The subtle pulse respects
reduced-motion preferences. The working indicator reports provider state
independently of message delivery.

User message snapshots render as one right-aligned plain-text bubble, anchored
at the first observation and updated by message ID within the Acta thread.
In-progress messages are gray; completion changes the bubble to a restrained
blue tint with a short color transition (disabled for reduced motion).
Completed content cannot regress from a later in-progress observation.
Unsupported content retains its raw card, and the debug captures remain
available separately. These are provider lifecycle states, not client delivery
acknowledgments.

Assistant messages render as left-aligned Markdown without a bubble, using the
existing Markdown renderer. Start snapshots and text deltas update a single
item at its first frame's position, keyed by message ID within the Acta
thread. Empty starts occupy no visible space. Each captured delta is applied
once by its frame identity; repeated text in distinct deltas is preserved. A
completed snapshot replaces accumulated text, and later starts or deltas
cannot alter it. Commentary and final-answer phases remain available in the
projection but share the same styling. Raw debug captures remain separately
available.

Compact gauges beside the power control consume `usage/context` and
`usage/account` cards. Context fullness uses only reported
`used_tokens / capacity_tokens`; unknown usage shows a question mark, never an
estimate from cumulative tokens. Each reported account limit window has its
own gauge, with a short duration label. Gauges fill with usage, become amber
at 80% and red at 100%. Selection opens details including exact percentage,
context counts or limit reset time, and when the snapshot was last reported.
Hover and accessible labels also describe the gauge. Snapshots are scoped to
the current provider run; each account bucket replaces its previous windows
rather than accumulating obsolete windows. Unknown values stay unknown.

Turn completion renders once as a quiet rule with Turn ended and its reported
duration. Failed and interrupted outcomes have distinct labels. Expanding
reveals the recorded time, exact duration and available error text, plus the
latest model call's token breakdown matched by thread, run and turn IDs. Late
matching usage enriches the original divider. Repeated completions do not
create extra dividers. Current usage frames do not establish a reliable
per-turn token total: cumulative snapshots are never summed, and last-call
counts are explicitly labeled rather than shown as turn totals. Missing
metrics remain absent.
