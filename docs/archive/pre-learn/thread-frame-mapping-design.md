# Provider frame mapping — working design

Status: generic frame shape agreed; provider mapping verification outstanding;
not implemented. Updated 2026-09-07.
Related work: ACT-63 (initial thread lifecycle and raw frame feed).

This is the continuation record for the frame-by-frame discussion with Jack.
Update it as decisions are agreed, keeping open proposals separate. Do not
interpret this document as permission to implement additional mappings.

The complete proposed wire schemas and examples are now in
[Thread frame schema](thread-frame-schema.md). These consolidate the ten
non-debug and five debug slash-named frame types; envelope fan-out, exact fields
and processing-error policy remain review proposals, not implemented behaviour.

IMPORTANT follow-up: Jack identified provider-specific leakage in the first
schema/examples. The initial schema is not approved. Sections 20–21 record the
generic revision. Do not implement either draft before the open decisions are
agreed; the original provider-shaped configuration is superseded.

## Agreed approach

- Review a bounded batch: the captured initial turn and its resumption.
- For each provider frame, decide whether it produces a Thread Frame, updates
  hypercontrol state or metadata, is consumed locally, or remains an Unknown
  Frame until understood.
- Discuss the batch before implementing it. The Unknown Frame fallback means
  we do not need an exhaustive catalogue of every possible provider event.
- Validate the mappings together using the captured sequence as a replay
  fixture, then one live Create → response → Kill → Resume check. Add focused
  tests for tricky behaviour rather than repeating the live flow per frame.
- Current runtime still exposes these frames as Unknown Frames. The decisions
  below describe the intended next implementation.

## 1. Codex initialization response — agreed

Recognized shape: an RPC response matching the hyperharness's `initialize`
request, with a successful `result` containing `userAgent`, `codexHome`,
`platformFamily`, and `platformOs` in the observed sample.

Handling:

- Match the response to the outstanding initialization request and validate
  successful initialization locally. This establishes handshake success; it
  does not prove that a thread is persisted or resumable.
- Retain the exact reported `userAgent` and platform information as provider
  runtime metadata associated with this particular provider run.
- Publish useful runtime metadata to Acta through hypercontrol, available in
  thread details. Emit no conversational Thread Frame for this response.
- Initialization occurs again on Resume. Metadata must be associated with the
  run, because a thread can resume under a different provider version.
- Preserve `userAgent` verbatim rather than extracting a version by parsing its
  wording. Keep `codexHome` local unless a concrete need to surface it is agreed.

Rationale: initialization describes the provider process, not a conversation
event. Its metadata is useful for diagnostics and should not be discarded or
presented as one permanent version for the lifetime of a thread.

Still to specify during implementation design: the exact metadata contract and
thread-details presentation. No particular schema or UI layout is agreed yet.

## 2. `remoteControl/status/changed` — agreed drop

The installed Codex 0.153.4 generated protocol describes this as the current
remote-control connection status and remote identity exposed to clients. It
belongs to Codex's own remote-control facility, with separate enable, disable,
pairing and client-management methods. It is not Acta's hyperharness connection
status or a thread lifecycle notification.

Observed sample: `status: "disabled"`, a server name and installation ID,
`environmentId: null`, and an envelope emission timestamp. Disabled here does
not indicate failure of Acta's connection to the local provider.

Agreed handling: recognize and consume locally, emitting neither a Thread
Frame nor an Acta hypercontrol state update. Do not enable Codex remote control
in response. No current product need to send its remote identity to Acta.
Existing raw diagnostic capture is separate from the normalized feed.

Evidence: generated `v2/RemoteControlStatusChangedNotification.ts` and the
related remote-control types from the installed binary, inspected 2026-09-07.
The public app-server documentation did not document this notification.

## 3. Provider diagnostics — agreed keep

Current implementation captures provider stderr separately from stdout. The
controller labels stderr records `diagnostic`; this is an Acta classification,
not a Codex RPC method. The pipe JSON-encodes stderr text as a string, so a
structured JSON log line appears quoted and escaped in the raw feed.

The supplied warning says the provider ignored a plugin's small-icon setting
because a path containing `..` did not resolve under that plugin's assets
directory. Its level is WARN and its logging target is `codex_skills::interface`.
This entry concerns icon metadata; it is not evidence that the thread failed.
The repeated text in the pasted sample alone does not establish duplicate
capture in Acta.

Agreed: preserve diagnostics for troubleshooting but keep
them out of the normal conversation feed, available in a diagnostics view.
Do not interpret arbitrary stderr or its severity as a thread lifecycle change;
use explicit provider protocol and process outcomes for lifecycle state.
Exact presentation and any structured-log parsing remain undecided.

Evidence: `internal/harnesspipe/engine.go` capture and
`internal/hyperharness/controller.go` frame conversion, inspected 2026-09-07.

## 4. `thread/start` response — agreed Configuration Thread Frame

The matched RPC response supplies the native provider thread identity and its
initial state, plus the resolved configuration for this start. The outer RPC
request ID is distinct from `result.thread.id`, the native thread identity;
Acta continues to use its own UUID. Existing controller logic already consumes
the native identity for resumption.

Agreed handling (supersedes the earlier hypercontrol-only proposal):

- Validate/correlate the response and persist the native identity locally using
  the existing lifecycle machinery. Do not create another server lifecycle
  path alongside discovery.
- Emit one normalized `Configuration` Thread Frame containing the resolved
  model, model provider, reasoning effort, service tier, working directory,
  and sandbox/approval settings. These arrive as a coherent snapshot; do not
  split them into artificial Model Set, CWD Set, etc. events.
- The UI uses the Configuration frame to populate controls/details rather
  than rendering a chat bubble. Thread Frames are not limited to conversation
  entries. This decision does not authorize new configuration editing controls.
- Use the same frame type for creation, resumption and subsequent known
  configuration changes, publishing a complete updated snapshot of known
  configuration. Distinguish unavailable fields from explicitly unset values;
  partial provider information must not erase settings already known.
- `thread.cliVersion` supplies an explicit version, unlike the initialization
  user-agent string. It can populate a version field without parsing wording.
- Keep provider storage paths and history implementation details local. There
  is no present requirement to normalize every nullable/native-only field.
- Emit no conversational Thread Frame for the response itself. `idle` with
  empty `turns` is the initial snapshot, not a completed assistant turn. Neither
  `ephemeral: false` nor a reported storage path proves durable resumption.
- Do not infer that blank sandbox writable roots means no write access: the
  returned sandbox mode and working directory must be interpreted together.
- Repeated delivery must not duplicate lifecycle effects or visible entries.
  The doubled pasted sample alone is not evidence of duplicate capture.

Still open: exact normalized configuration schema and which provider-specific
details to expose, including placement of the explicit CLI version.

## 5. `thread/started` notification — deduplication agreed, readiness discussion

In the supplied creation sequence, `params.thread` repeats the thread object
from the matched `thread/start` response. This is a provider notification rather
than a request response: its envelope has `method` and no matching RPC `id`.
It omits the response's outer resolved configuration, including service tier,
approval policy and sandbox configuration.

Agreed: consume this matching notification locally with no duplicate
Configuration frame. Check the native identity against the expected provider
thread; do not overwrite the fuller snapshot with this partial repeat. It is
not evidence of a started model turn or durable resumability.

Readiness discussion supersedes the earlier proposal to give this notification
no discovery significance: Jack requires advertising a usable thread before it
is durably resumable, so a user can send the first message. Do not gate discovery
on successful Test Message submission or persistence. The exact readiness gate
remains under discussion; the assistant proposed receiving both the successful
start response and matching started notification, in either order.

This proposal concerns the matching notification in our explicit creation flow;
unexpected identities or genuinely different content should not be silently
discarded as duplicates. Exact handling of those cases remains to be specified.

## 6. Persistence acknowledgement — proposed `Committed` hypercontrol frame

Jack proposed a separate hypercontrol frame once the provider thread has been
saved to disk. The agreed distinction is that availability precedes durable
resumability; advertising must not wait for the first message to be persisted.

Agreed follow-up: discovery explicitly advertises the thread as uncommitted
until commitment is established. Acta must not infer commitment from presence
in discovery. The committed/uncommitted state belongs in discovery snapshots,
with a `Committed` hypercontrol notification for the transition.

Proposed semantics: `Committed` identifies a provider session with sufficient
persisted state to resume after its process exits. It does not claim every later
message or turn is saved. Persist this fact locally and include it in discovery
on reconnect, so a lost one-time notification cannot lose the state on Acta.

Open implementation question: what provider-specific evidence establishes this
fact? A path in a response, file existence alone, or an Acta journal write is not
by itself proof of usable provider history. Verify a trustworthy persistence
signal before claiming the guarantee. No probing/killing the active process just
to establish commitment. Exact protocol and UI presentation remain undecided.

## 7. `mcpServer/startupStatus/updated` — forwarding agreed, mapping proposed

Jack wants these updates sent to Acta. The observed notification identifies a
native thread and MCP server name, with `status: "starting"`, `error: null`,
and `failureReason: null`. It reports that server's startup state, not successful
initialization of every MCP server or readiness of the whole provider thread.

Proposed normalized mapping: one `MCPServerStatus` Thread Frame per update,
containing server name, status and any reported error/failure reason, within the
existing Acta thread/run envelope. Resolve and validate the native thread ID
locally. Treat the server name as scoped to that run, not a global identity.

The UI maintains the latest status per server for the current run, suitable for
a tools/connections area rather than a chat bubble for each transition. A fresh
run must not inherit a stale ready indication from the preceding process. These
updates must not gate discovery or imply provider-history commitment.

Exact type name, status vocabulary, error representation and UI presentation
remain proposals pending agreement and review of subsequent notifications.

Jack reports further notifications with the same method and `ready` status;
these use the same mapping, updating the server's status rather than introducing
a different frame type. Full ready payload was not supplied for field comparison.

## 8. `turn/start` response — proposed turn lifecycle mapping

The matched RPC result acknowledges a turn and supplies its provider turn ID.
The sample reports `inProgress`, no error, unavailable timing fields, and
`items: []` with `itemsView: "notLoaded"`. An unloaded item list is not evidence
that no user message or other turn items exist. This is neither assistant text
nor a provider-history commitment acknowledgement.

Proposal: correlate the response with the pending local start request and retain
the provider turn identity for later events and controls. Normalize the observed
start to one `TurnStarted` Thread Frame, so Acta can track the active turn and
show running state. The generated protocol also exposes `turn/started` with a
thread ID and turn object; both sources must reconcile to the same transition,
not create duplicate turns, regardless of arrival order. Inspect the subsequent
notification during this review before finalizing that mapping.

Do not invent an assistant message or provider start timestamp from this
acknowledgement. Keep local observation time distinct from provider timing.
Exact normalized frame contract remains proposed, awaiting agreement.

## 9. `thread/status/changed` — proposed thread execution status mapping

The sample reports `active` with no active flags for the provider thread. The
installed generated protocol defines `notLoaded`, `idle`, `systemError`, and
`active`, whose flags may include `waitingOnApproval` and `waitingOnUserInput`.

Proposal: normalize to a `ThreadStatus` Thread Frame representing current
provider execution state, consumed by sidebar/header status indicators rather
than rendered as a chat entry. Preserve waiting conditions as flags because the
provider represents them as a set. The supplied sample means active with no
reported waiting condition; it does not establish a specific internal activity
such as model reasoning or tool execution.

Keep this separate from `TurnStarted`: the latter identifies a turn boundary;
thread status describes present execution/waiting state and has no turn ID.
Do not synthesize turn completion from idle, a turn failure from systemError,
or process death from notLoaded. Likewise, execution status must not overwrite
connection availability or commitment state. Approval/input flags indicate a
wait but do not contain actionable request details; those require their own
provider events. Exact normalized names and UI remain proposals.

Evidence: installed generated `v2/ThreadStatus.ts`, `ThreadActiveFlag.ts`, and
`ThreadStatusChangedNotification.ts`, inspected 2026-09-07.

Follow-up sample: the same method reports `status: { type: "idle" }` after
assistant output. Use the same proposed ThreadStatus mapping, updating the
provider execution indicator to idle and clearing prior active/waiting flags.
This does not mean the process exited, the connection dropped, or the turn
completed successfully. Await the explicit turn outcome for that conclusion.

## 10. `item/started` with `userMessage` — proposed UserMessage frame

The supplied notification contains the provider-observed user message, its item
ID, native thread and turn IDs, and an item start timestamp. The content is one
text part, `Test Message`, with an empty `text_elements` array. This is the
temporary message sent by the hyperharness, not a new message typed in Acta.

Proposal: emit a `UserMessage` Thread Frame rendered as a user message in the
conversation. Preserve item identity and turn association, ordered content
parts and provider timestamp. Do not infer a specific human author merely from
the provider's user-message role. The existing fixture remains visible exactly
as sent; do not send another message when processing the notification.

Reconcile subsequent notifications for the same item rather than appending a
second message. In particular, review `item/completed` when encountered before
finalizing update semantics. Scope live native item identity to the provider
thread. IMPORTANT: mapping 18 demonstrates that reconstructed resume history
does not preserve these live item IDs; do not assume cross-resume identity
stability. If clientId is available later it can
support matching an optimistic outgoing message; the current null value cannot.

Do not flatten the canonical content model to a single text string. This sample
does not establish how non-empty text elements or other content types should
map; retain the fallback until those are reviewed. Receipt of a user-message
notification is not by itself a commitment/persistence guarantee.

## 11. `item/completed` with `userMessage` — proposed same-message update

The sample has the same item ID, turn ID and content as the preceding started
notification, now with a completion timestamp. It completes that user-message
item; it does not report completion of the model turn or provider persistence.

Proposal: normalize to an update/upsert of the same `UserMessage`, carrying the
completed state and final content. Keep one visible bubble. If completion is
the first observation of that item, its self-contained payload can establish
the message. Replayed started events must not regress an already completed
item. Exact wire representation of lifecycle updates is not yet agreed.

Completion must not trigger `Committed`: no durability guarantee is supplied
by this payload. Preserve completion time separately from start time rather
than shifting the message's original timeline position on completion.

## 12. `item/started` with `agentMessage` — proposed AssistantMessage start

The supplied item has its own provider item ID, an empty text string and phase
`final_answer`, with native thread/turn IDs and a start timestamp. The optional
memoryCitation, delivery and questions fields are null in this sample; no
non-null mapping for those fields has been agreed.

Proposal: start one `AssistantMessage` in streaming/in-progress state, preserving
item identity, turn association, timestamp and phase. Subsequent text events
update this same message, and item completion finalizes it. Use shared message
identity/lifecycle handling with user messages where appropriate, while keeping
role-specific content/phase semantics distinct. Exact wire names remain open.

An empty start establishes the message internally; avoid rendering an empty
bubble, using the existing working indicator until content arrives. Preserve
`final_answer` as a message phase, not as proof of message or turn completion.
Only the corresponding lifecycle events should establish completion. Review
the actual delta and completion payloads next before specifying their contracts.

## 13. `item/agentMessage/delta` — proposed AssistantMessageDelta

The sample identifies the same assistant item and turn as its start event, and
supplies `delta: "Message"`. This is an incremental text chunk, not a complete
replacement message and not necessarily one token or word.

Proposal: emit `AssistantMessageDelta` with item/turn association and the exact
text chunk. Append to the existing streaming message in captured event order,
preserving whitespace and punctuation without inserting separators. Render one
continuously updated message rather than one bubble per delta.

Delivery replay must not append the same event twice. Use stable captured-event
identity/sequence, not text equality, for deduplication: identical chunks can be
legitimate successive output. Preserve ordering through the adapter and Acta
delivery paths. Do not use emission timestamps as unique event IDs.

Delta emission does not complete the message or turn. Review the forthcoming
item completion's full content as a possible final reconciliation source. Exact
normalized schema and replay/reconstruction implementation remain proposed.

## 14. `item/completed` with `agentMessage` — proposed AssistantMessage completion

The supplied completion references the same assistant item and turn, with full
text `Message received.`, phase `final_answer` and a completion timestamp.
Deltas carry incremental streamed content; this payload supplies the final
complete content of the message item.

Proposal: finalize/upsert the same `AssistantMessage`, replacing its accumulated
text with the completion's full text rather than appending it. This reconciles
the visible message if earlier deltas were missed by a client, without claiming
the underlying event history was recovered. Preserve phase and distinct start
and completion timestamps; remove the streaming indicator. Do not create a
second bubble. Completion alone can establish the item when its start/deltas
have not been observed.

Replayed completion must be idempotent, and late/replayed starts or deltas must
not mutate a finalized message. Use item lifecycle state together with ordered,
deduplicated events. Completion of this message, even with final_answer phase,
does not replace the separate turn-completion event or establish commitment.
The repeated pasted JSON does not alone prove duplicate delivery in Acta.

Exact wire representation remains proposed; no runtime mapping is implemented.

## 15. `thread/tokenUsage/updated` — agreed ContextUsage snapshot

Agreed (Jack's name): emit a `ContextUsage` Thread Frame carrying the provider's `total` and
`last` breakdowns, reported model context window, and thread/turn association.
Consume as usage UI state, not a chat bubble. Treat updates as snapshots to
replace, not increments to sum; replay must not inflate usage.

The sample reports 20,901 input tokens, 7 output tokens and 20,908 total tokens,
with 12,928 cached input tokens, zero cache-write/reasoning tokens, and a 258,400
token model context window. Both breakdowns are identical in this sample.

Keep cache/reasoning counters as breakdowns rather than adding all fields to
totalTokens. Do not infer a monetary charge or a current context-usage percentage
from cumulative usage divided by modelContextWindow. Precise `total` versus
`last` accumulation/reset semantics, especially across multiple model requests,
resumption and compaction, need source verification before UI labels/calculations
claim those meanings. The installed schema confirms the fields but does not
document these semantics; public app-server docs describe this as thread usage
updates without defining the two counters in detail.

Evidence: installed `ThreadTokenUsage.ts`, `TokenUsageBreakdown.ts` and official
app-server Events documentation, inspected 2026-09-07.

## 16. `account/rateLimits/updated` — proposed AccountUsage snapshot

The notification has no thread ID. It describes provider-account limits rather
than this thread's token usage. The observed `codex` bucket reports 80 percent
used in a 10,080-minute (seven-day) window, implying 20 percent remaining in that
reported window. `secondary: null` means no secondary window was supplied, not
that its usage is zero. Preserve the supplied reset timestamp and bucket ID.

Proposal: emit an `AccountUsage` Thread Frame as a provider-account snapshot
observed through this provider run, used by a separate account-limit UI indicator.
Do not label it as usage attributable to this thread or sum duplicate snapshots
across threads. The Acta owner may use different provider accounts on different
machines; do not merge them merely because they share an Acta owner or limitId.
The limitId identifies a limit bucket, not a unique provider account.

Preserve reported window, credit, plan and restriction fields with null/unknown
semantics. No credits does not mean included plan allowance is exhausted; do not
infer a blocked state from this sample. Exact normalized type/transport and UI
remain proposed. ContextUsage's name does not change the outstanding need to
verify token-counter semantics before computing a context-occupancy percentage.

## 17. `turn/completed` — proposed TurnCompleted outcome

The supplied notification identifies the existing turn and reports
`status: "completed"`, `error: null`, provider start/end timestamps and
`durationMs: 4364`. This is the explicit successful turn outcome, unlike an
idle thread status or completion of one message item.

Proposal: emit `TurnCompleted` with turn identity, terminal outcome, error when
present, and timing. Use the same terminal event shape for completed,
interrupted or failed outcomes, preserving their distinction. Retain the
reported millisecond duration rather than recomputing it from second-resolution
timestamps. Replay must not repeat completion side effects.

The supplied `itemsView: "summary"` contains the final assistant message already
observed. It is not an exhaustive replacement for the turn's item history: do
not delete the user message or other items absent from it. Reconcile included
known items by identity if needed; do not append duplicate message bubbles.
Individual item events remain the primary source for the item stream. Exact
summary fallback reconciliation is still to be specified during implementation.

Turn completion does not kill the provider process or establish a documented
provider-history persistence guarantee. Do not equate it with Committed without
separately verified provider evidence. Mapping remains proposed, not implemented.

## 18. `thread/resume` response — proposed configuration refresh and restoration

The response restores the same native thread and original completed turn, and
supplies resolved configuration for the new provider run. The full turn history
contains the previously observed user and assistant messages. Successful
restoration of this history is concrete evidence of resumability at this point;
it does not tell us which earlier event originally established persistence.

Proposal: validate the returned native thread identity against the requested
one, refresh the run's Configuration frame using the shared start/resume mapping,
and report resumed availability through existing hypercontrol/discovery. Mark
commitment when supported by the successful persisted-history restoration.
Do not issue new TurnStarted, UserMessage, AssistantMessage or TurnCompleted
events as though historical work were executing again.

Critical observed identity difference: the live user-message UUID and assistant
`msg_...` ID have become `item-1` and `item-2` in the reconstructed full history,
while the turn ID is preserved. This disproves the earlier assumption that
native item IDs are stable across live events and resumed history. This is an
observation of the supplied legacy-history sample, not a claim about all modes.

Proposed scope: retain Acta's captured conversation as its existing history;
do not automatically append/import the resume snapshot into it. Keep restored
history separate from live events. If missing-history reconciliation or external
thread import becomes necessary, design it explicitly; matching solely by text
or assuming item IDs are stable would be unsafe. Do not silently discard this
limitation if a gap is found during implementation.

The response includes history/pagination fields; their null values in this
sample do not establish general pagination behaviour. Exact history recovery
policy remains open. Shared Configuration mapping and resume handling proposed,
not implemented.

## 19. Complete debug feed alongside normalized frames — Jack's proposal

Jack has finished supplying the unique frames from this captured sequence and
proposes exactly one debug classification for every incoming provider frame:

- **Unknown Frame**: the hyperharness does not recognize the provider frame.
- **Dropped Frame**: recognized but discarded as having no useful information.
- **Local Frame**: used locally but produced no non-debug Thread Frame.
- **Resolved Frame**: used to produce at least one non-debug Thread Frame.
- **Provider Diagnostic**: existing separate stderr diagnostic capture.

All incoming provider frames are emitted as one of these debug frames, retaining
their raw content. In addition, the adapter emits zero or more non-debug Thread
Frames for UI value. Earlier entries saying drop, consume locally or emit nothing
refer to the non-debug output only; they no longer imply absence from the debug
feed. This explicitly supersedes a debug UI that displays only unknown frames.

Assistant recommendation: classify by the actual processing outcome, with
Resolved taking precedence if a frame both updates local state and emits
non-debug frames. Local includes frames used solely for hypercontrol; it does
not mean they were ignored. Diagnostics are classified by their source stream.
Keep hypercontrol itself separate from the debug/non-debug Thread Frame split.

Recommended provenance: assign stable identity to each captured input; link its
debug frame and zero or more normalized outputs. Preserve original capture
order and deterministic output order so replay neither duplicates text nor
loses raw frames. Add a short reason to debug disposition and links to derived
frames. Exact schema and durable acknowledgement treatment remain to be designed
against the existing one-capture/one-output sequence implementation.

Do not silently classify adapter failures as ordinary intentional drops. Preserve
the input and expose processing failure explicitly; error details/retry contract
remain open without introducing a sixth debug category at this point.

Proposed UI separation: normal conversation/details consume non-debug frames;
the debugger exposes all five classes with raw content and derived-frame links.
Exact debugger layout remains to be discussed. This is design, not permission
to implement unspecified UI or change the runtime yet.

## 20. Generic contract correction — current discussion

Jack requires an Acta-owned provider-independent schema, with Codex and Claude
adapters translating into it. Carrying provider settings under snake_case names
does not satisfy this. The original schema/examples remain a superseded draft
for affected configuration fields, not an approved implementation contract.

- Jack requests `fast_mode` as a boolean instead of `service_tier`. Use true/false
  only when known; omit when unknown rather than inventing false. Capability
  availability is a separate concern. Exact provider tier mapping needs checking.
- `thread_id` identifies the persistent Acta conversation; `run_id` identifies
  one provider process lifetime. Resume creates a new run, while reconnecting a
  hyperharness to a surviving detached process retains that run. `turn_id` is a
  separate message/response work cycle; many turns fit in one run.
- `output_index` was the assistant's delivery/provenance proposal, not provider
  vocabulary or an agreed UI requirement. It distinguishes the debug output
  and any derived outputs sharing one capture sequence. Keep this as transport
  bookkeeping; naming/placement remains open.
- The installed Codex MultiAgentMode type describes delegation instructions.
  Its ThreadStartResponse field is deprecated and documented as always
  explicitRequestOnly in this version. Assistant recommends omitting it from
  generic configuration for now, leaving provider details in debug capture.
- `client_id` was intended as outgoing-message correlation, not account identity.
  The installed turn/start accepts clientUserMessageId and user-message items
  expose clientId. Exact propagation needs verification. Assistant recommends
  keeping native correlation local and defining Acta's own sending identity
  only when the send flow is designed.
- Message phase distinguishes interim commentary from a final answer. It is
  independent of streaming/completion. Assistant proposes generic purpose values
  `progress` / `answer`, with unknown preserved and no need for providers to
  fabricate a distinction they do not report. Name/retention not yet agreed.

Permissions: Jack proposes starting with Claude's modes and mapping Codex onto
them. Official Claude docs list default/manual, acceptEdits, plan, auto, dontAsk
and bypassPermissions. Codex separately represents sandbox access and approval
policy; the two products are not proven strict subsets of one another. In
particular, workspace-write plus on-request is not automatically equivalent to
Claude acceptEdits, and read-only sandbox is not a full planning workflow.

Assistant recommendation: define the meaning of each Acta permission mode first,
keep sandbox/access boundaries distinct from who approves an action, and have
adapters declare supported faithful mappings. Do not silently choose a broader
setting or report an arbitrary external custom configuration as an exact preset.
Planning/workflow mode may deserve its own field. Exact mode set and mappings
remain a discussion item, not decided by this note.

Sources checked 2026-09-07: official Claude permission-modes documentation,
official Codex security/app-server docs, and installed Codex generated types
MultiAgentMode, ThreadStartResponse, TurnStartParams and MessagePhase.

## 21. Generic example/schema revision requested

Jack prefers to retain the word commentary and requests reissuing the examples
with a provider-independent schema. The revised draft retains `phase` with
commentary/final_answer (or null), and removes multi_agent_mode, native approval
fields, instruction-source fields and client_id from normalized configuration
and messages. fast_mode replaces service_tier. Runtime version stays outside
normalized configuration in the previously discussed runtime metadata.

New explicit proposals, not yet agreed: permission summary with independent
approval mode and filesystem/network access categories, separate execute/plan
work mode, generic account limit windows array, and ContextUsage separating
actual occupancy from cumulative and last-model-request consumption. Mapping
the provider's counters to these meanings still needs verification; unknown
values must remain null. Exact modes/capabilities require discussion before
implementation. Thread status now uses error/unknown instead of native
systemError/notLoaded, preserving active/idle and waiting flags.

The machine schema/examples were revised consistently with these proposals and
all 15 examples validated. Native details remain in complete raw debug capture.
The output_index transport proposal and processing-error policy are still open.

## 22. Generic frame shape approved

Jack agreed to the reissued generic examples: all ten non-debug slash-named
frame types, the five debug types, commentary/final_answer phases, fast_mode,
generic permission/access categories and execute/plan work modes, generic account
windows and the context-occupancy/consumption distinction. The shared envelope
and debug output references were included in the approved examples.

This approval supersedes earlier proposal labels for that presented frame shape.
It does not assert that every provider setting has an equivalent mapping or that
the provider persistence signal has been verified. Remaining technical work:
verify permission-mode mappings/capabilities; verify token-counter scope/reset
semantics; establish evidence for commitment; and make fan-out delivery/replay
and adapter-error handling satisfy the documented reliability invariants.
Unsupported/unknown information must remain explicit rather than guessed.

No runtime implementation or additional UI design was authorized by this brief
agreement. Continue in lockstep before starting the implementation batch.

## 23. UI scope for the frame implementation — agreed

Jack explicitly wants the existing raw-frame feed UI retained for this slice,
now showing the five debug classifications plus the additional normalized
non-debug frames. Display their type and raw JSON using the existing presentation.
Only the minimal changes needed to expose these new frame types are in scope.

The assistant's proposal for conversation bubbles, commentary styling, a metadata
header, details popover and a separate debugger panel is deferred. Do not build
those surfaces, filters, resizing or follow-latest controls in this slice.
Design polished rendering for each frame together in the next slice. Earlier
notes about UI consumers describe intended eventual uses, not current UI work.

This scope correction does not itself authorize starting the implementation
batch; the provider-mapping verification items remain outstanding.

## 24. Permission mapping investigation

Jack authorized starting with permission mappings, independently of commitment
and usage investigation. Findings are in
[Provider permission mapping](thread-permission-mapping.md).

Result: common review-routing labels are feasible, but native action coverage is
not identical. Codex workspace-write + untrusted is a closer acceptEdits candidate
than the previously discussed on-request combination, yet command allowances
differ. Codex never means no new approvals, not an explicit tool allowlist, so
renaming preapproved_only to dont_ask is proposed. Standalone bypass cannot be
assumed independent of Codex access restrictions. Plan remains a work-mode concern.

No schema enum changes made pending discussion; no runtime tests or mutations.
Do not mark native enforcement equivalence verified solely from this source/help
inspection. Preserve custom/unknown classifications where evidence is insufficient.

## 25. Practical permission mappings accepted

Jack considers the investigated mappings close enough and accepts them as Acta
categories: ask maps to Claude manual/default and Codex on-request with human
review; accept_edits maps to Claude acceptEdits and Codex workspace-write plus
untrusted; automatic maps to Claude auto and Codex on-request with auto_review;
dont_ask maps to Claude dontAsk and Codex never within its configured sandbox;
bypass maps to Claude bypassPermissions and the Codex never/unrestricted
combination. Keep access scope separately reported.

Rename preapproved_only to dont_ask in the current schema. Native differences
within these accepted mappings do not force custom classification. Reserve
custom for genuinely different known configurations, and unknown for missing
information. This supersedes the previous recommendation to block these labels
pending exact equivalence. No runtime changes; commitment detection is the next
outstanding investigation, followed by context-usage mapping.

## 26. Commitment detection agreed

Jack accepted this design. Verification of the Codex active-thread read behaviour
and a disposable end-to-end resumability check remain implementation validation
requirements. This agreement does not itself start the implementation batch.

Keep commitment as a provider-adapter concern. Advertise a usable new thread as
uncommitted. After the first user message is accepted, probe for saved history;
for Codex the candidate is thread/read with includeTurns=true. The documented API
reads stored history without resuming a thread, and the installed generated type
describes hydration from rollout history. Before implementation relies on this,
verify that the relevant history read for an active thread comes from persisted
storage rather than a live-only cache. A successful metadata-only response, a
returned rollout path, and item/turn completion alone are not sufficient evidence.

When saved history includes the initial user message and the native identity
matches, persist the local committed flag before emitting Committed hypercontrol.
Include that flag in subsequent discovery snapshots so reconnect recovers a lost
notification. Retry transient not-yet-saved results with bounded backoff while
the thread remains live and uncommitted; surface other failures diagnostically.
Stop probing after commitment. Do not resume or kill a live thread as a probe.

Validate the criterion once with a disposable create/message/kill/resume
integration check in the implementation batch. This is initial resumability, not
an acknowledgement that every later event is saved or an fsync/power-loss guarantee.
Claude requires its own evidence behind the same adapter boundary.

Sources: installed Codex 0.153.4 ThreadReadParams and
https://learn.chatgpt.com/docs/app-server#read-a-stored-thread-without-resuming.
No runtime changes or provider process mutations made during this proposal.

## 27. Context usage mapping accepted for implementation

Retain the agreed separation between context occupancy, cumulative consumption,
and last-request consumption. Proposed Codex mappings are modelContextWindow to
context.capacity_tokens, total to cumulative, and last to last_request. The
installed generated types confirm the fields but do not document reset semantics
or prove that last.totalTokens is current context occupancy. Public app-server
documentation describes thread/tokenUsage/updated only as active-thread usage
updates. Therefore these consumption mappings remain candidates to verify during
the implementation batch, especially across multiple model calls and resumption.

Do not infer context.used_tokens from cumulative consumption. Leave it null until
the adapter has a supported occupancy measurement; any future approximation must
be explicitly identified as an estimate rather than silently changing the field's
meaning. A request footprint also predates any subsequent tool output/compaction.
Snapshot updates replace previous values; never sum repeated usage notifications.
Preserve provider breakdown totals rather than adding overlapping subcategories.
Unknown remains null. No additional provider calls are proposed solely for usage.

Example rationale: three requests each processing 20k input tokens can consume
60k input tokens cumulatively while each individual request still fits in roughly
20k context. A context gauge must not treat that cumulative consumption as 60k
occupied context. Original provider counters remain available in debug frames
while any mapping is unresolved. No schema or runtime changes in this discussion.

## Remaining frame review

The supplied sequence's unique frames and generic output shape have been reviewed
and agreed as consolidated in section 22. Historical proposal labels above show
the decision progression; use section 22 for current approval status. Provider
evidence, implementation reliability and UI presentation still require work.

## 28. Implementation authorized and validated

Jack authorized implementation, explicitly retaining only the automatic initial
Test Message and no user message sending. Detailed consumption-counter behaviour
can be refined through use; unknown context occupancy is not an implementation
blocker. Existing JSON feed presentation remains the scope.

The local Codex adapter now emits five debug classifications and the ten reviewed
non-debug kinds. Bundles plus adapter state are atomically persisted before they
are offered; the server validates complete bundles and acknowledges only after
transaction commit. Page boundaries preserve complete captures. Old acknowledged
raw-only history is not backfilled. Provider errors/mapping validation failures are
explicit Local Frames; unsupported shapes stay Unknown.

Readiness is published after successful provider thread creation/resumption before
the fixture turn is sent. Commitment probes use fresh durable RPC IDs, back off
while uncommitted, and stop on successful stored user history. Discovery carries
commitment and runtime metadata; transitions also use committed hypercontrol.
Resumption history is inspected for commitment, never imported as new messages.

Live Codex 0.153.4 validation: reading an empty thread failed; after Test Message,
thread/read returned user history also independently present in the JSONL rollout.
A fresh process resumed the same provider identity/history. Captured-frame replay
produced every reviewed normalized kind with no mapping failures; an unreviewed
configWarning remained Unknown. This establishes initial process-exit resumability
for the installed version, not per-event fsync or power-loss guarantees.

Final browser verification on port 8081: creation auto-opened a new thread,
the single Test Message and streamed response produced the expected normalized
frames alongside their debug inputs, and the uncommitted indicator cleared after
saved user history appeared. No message-sending controls were added. An embedded
asset contract test now prevents browser/server harness protocol-version drift.
Full make check passed, including 36 frontend tests; PostgreSQL and lifecycle
integration suites passed with the race detector. Code and documentation remain
in the existing untracked worktree; no commit or staging was performed.
