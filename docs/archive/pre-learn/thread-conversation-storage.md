# Persisted conversation state and reverse pagination

Implemented 8 September 2026 (ACT-63).

The immutable frame journal retains raw debug evidence and normalized frames.
A server reducer applies accepted frames in order to persisted conversation
items and a current-state singleton in the same transaction as ingestion.
Messages, tools, thinking and automatic reviews update their existing item;
configuration observations remain individual historical items as well as
updating the singleton. Lifecycle start/completion times remain on the item.
The browser renders assembled items, not replayed frame history.

History pagination uses stable first-position cursors. Live polling uses the
last applied capture sequence and item revisions, so an old item updated now
is delivered. Initial items and singleton share a repeatable-read snapshot.
Older-page results cannot overwrite newer item revisions or current state.
Removed/collapsed items retain tombstones for live reconciliation.

Pending approvals are in the singleton even when their items are outside the
loaded window. Debug filtering is performed before pagination; Unknown Frames
remain visible. Debug raw payloads are retained verbatim and formatted only in
the display copy. Existing histories are backfilled once before serving pages.

Newest items load first (50 by default), in chronological display order. The
transcript starts at the bottom; scrolling near the top fetches older items and
preserves a visible-item anchor. Live updates only follow the bottom when the
reader is already there. Thread navigation aborts outstanding requests.

## Persistence and API

`provider_thread_frames` is the immutable capture journal.
`thread_conversation_items` holds each assembled item, its first position,
latest revision, lifecycle timestamps and reducer state. Hook completion is an
intentional exception to fixed position: the running placeholder moves to the
response position. Turn summaries similarly become visible at completion.
`provider_threads.conversation_state` holds current configuration, status,
context/account usage and unresolved approval requests. Its internal MCP batch
references are not returned to the browser.

`GET /api/threads/{id}/conversation` returns the newest 50 visible items,
chronologically ordered, together with current state and the latest revision.
The owner check is applied to every read. `before` is an opaque position cursor;
`after` is an independent live revision cursor. They cannot be combined.
`debug=true` adds debug items before pagination. Unknown Frames remain normal
visible items. State-only historical events remain persisted but hidden.

A changes response advances `cursor_revision` through at most 64 source captures,
without splitting the outputs of one capture. `revision` describes the current
singleton snapshot; it may be ahead of the catch-up cursor. Changed items carry
complete payloads, not deltas requiring previous pages. Tombstones remove items
that disappeared during assembly. Clients retain newer revisions when an older
page arrives late, and do not advance their live cursor from history reads.

The reducer runs only for newly accepted captures under the existing per-thread
row lock. Journal writes, assembled items, singleton and source watermark commit
atomically before acknowledgement. Identical replay does not apply deltas twice.
A projection failure rolls back the entire transaction. Startup backfill replays
one thread per transaction, marks its assembly version only at commit and can be
retried after interruption. It preserves old Unknown captures as Unknown.

## Browser behaviour

History and live updates use the same rendering components. Browser-side history
assembly has been removed; only display formatting and interaction state remain.
Scroll anchoring handles page insertion and later Markdown/layout resizing.
User message bubbles align to the right using an automatic inline-start margin;
they do not depend on being direct children of a grid (including in Firefox).
Readers at the bottom follow new content; readers above it retain their visible
item. Debug changes reload the newest page for the selected visibility mode.
Pending approvals remain actionable through current state even outside the
loaded history window.

Pagination bounds initial/history reads by item count, not bytes. Large raw debug
payloads remain intact. Loaded history stays in memory for the current route;
this slice does not virtualize or evict already loaded rows.

## Verification

See [the implementation review](reviews/2026-09-08-thread-conversations.md).
Offline parity can be repeated with a private JSON array of normalized captures:

```sh
ACTA_CONVERSATION_REPLAY=/tmp/captures.json go test ./internal/conversation -run TestCapturedConversationReplay -count=1
node web/tests/compare-conversation-replay.mjs /tmp/captures.json
```

The frozen browser assembler lives under `web/tests/reference` solely as a
migration test oracle. It is not included in the production browser bundle.

## Subagent lanes

Conversation items and independent current-state projections are scoped by
`lane_id`. Lane pagination, cross-lane pending interactions and native control
routing are described in [Subagent conversations](subagent-lanes.md).

## Scroll intent and regression checks

Any upward wheel, touch or keyboard gesture pauses following immediately. Late
scroll events from earlier motion cannot turn it back on. Scroll down to the
bottom (or press End) to follow again. Scrollbar dragging starts a new gesture.
New messages arriving below a paused reader do not move the viewport. Loading
older pages and content growing above the reader preserve the visible item.
After an older page arrives, pagination rechecks the top boundary: pages that
fold into the same collapsed activity group can load consecutively without
needing a new scroll event. An upward gesture at the top can also retry loading.
Requests remain serialized; no automatic retry occurs if history did not advance
(including a failed request). The Load older messages button remains available.
Already-loaded conversation views are positioned on mount as well as after the
initial fetch.

`cd web && npm run test:scroll` builds the real transcript component into a
local headless browser fixture and tests live appends, upward gestures, delayed
offsets, history prepends, content growth, idle polling and resuming at the bottom.
It also checks passive wheel handling and bounded anchor measurements in a
1,000-row history. Chromium is the default; set `ACTA_SCROLL_BROWSER=firefox`
to run with Firefox and native smooth scrolling enabled. It requires
Playwright and the selected browser runtime; `ACTA_PLAYWRIGHT_MODULE` can point to an
existing Playwright module. It uses synthetic conversation data and no provider
sessions. The smaller intent-state regressions run in the normal `npm test` suite.

Unchanged poll responses preserve conversation and singleton object identities
and do not publish a new view state. Cursor, error and lifecycle changes still
publish normally. Wheel intent is observed passively, and locating the visible
anchor measures logarithmically many top-level rows rather than scanning all
messages and the hidden contents of collapsed activity groups. These rules keep
idle polling and long histories from adding unnecessary work during scrolling.
