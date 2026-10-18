# Provider thread QA — 2026-09-08

Tracked in ACT-64. Authorized hands-on testing of existing provider workflows;
pause for product decisions. Dedicated Acta-QA harness and scratch directories,
with cheap models. Existing user sessions are excluded from disruptive tests.

## Results

Testing in progress. Record observed failures and verification here as work proceeds.

- Created dedicated Codex (Spark, low effort) and Claude (Haiku) threads via the
  normal UI. Both discovery and initial-message rendering succeeded.
- Codex shell read/write and intentional exit 7: failed operation rendered,
  subsequent final message and turn completion remained intact.
- Native edit/diff and approval tests in progress.

## Confirmed results and fixes

- Codex apply_patch twice: two operation diffs and a collected turn diff rendered.
- Claude Write approval succeeded; Edit denial rendered as denied and kept the
  original file intact. Retrying Edit and approving from the history card worked.
- Claude Haiku rejects automatic approval mode. The UI reports the provider
  error and retains Ask for approval; no unsupported mode is claimed as applied.
- Codex Stop during an awaited shell process produced Turn interrupted and kept
  the draft. Kill/Resume retained the provider identity, model and draft.
- Fixed the pending Stop label (previously said Resume requested).
- Codex empty terminalInteraction stdin polls now map to Dropped Frames. Nonempty
  input and unmatched identities remain Unknown. Added regression coverage.
- Targeted adapter/controller/conversation race tests and frontend checks passed;
  rebuilt and deployed server/harnesses without stopping their detached pipes.

## Claude file changes

Jack chose a combined before/after diff where reliable, with sequential patches
as fallback. The adapter now uses successful provider-reported Write/Edit results
and original file contents, verifies any reported structured patch, and checks
that each operation starts from the prior operation's exact result. It keeps each
operation's own patch and emits an updated turn/diff snapshot. Discontinuities or
missing/ambiguous snapshots select an explicitly labelled sequential fallback;
unavailable patches are flagged as incomplete. No live filesystem reads.

Tests cover create/overwrite/repeated edits/reverts, denial, replacement ambiguity,
replace-all, contradictory metadata, duplicate results and checkpoint recovery.
Generated patches were independently applied by Git, including empty files,
CRLF, missing final newlines, large inputs and randomized repeated-line edits.
Adapter/controller/conversation race tests and frontend checks/build passed.
Live Haiku validation passed in the isolated QA thread: create, edit twice,
overwrite, then read. All four operations contributed to one combined addition
containing only the final ALPHA/BETA/GAMMA text. The first Edit retained its own
beta-to-BETA patch. The final database diff was independently applied by Git and
matched the scratch file byte for byte; no new Unknown or processing-error frames.
The same combined panel persisted after browser reload. Both main and QA harnesses
reconnected, with their provider processes preserved.

## Further investigation

Fixed context-gauge flicker with Jack's approval: retain the last measured frame
for the same run, preserving its original Last reported timestamp in the popup.
Incoming call statistics/history still advance. New measurements replace the old
one; new runs do not inherit it. Projection version 2 rebuilds saved singleton
state from existing frames. Reducer race tests and PostgreSQL conversation
rebuild tests passed, including unchanged measurement time across missing counts
and replacement by a fresh zero count. Deployed; both harnesses reconnected. Background shell operation lifetime is distinct
from turn lifetime; no new background-task UI was added.

## Supervisor crash and reconnect checks

Two live QA rounds passed with Spark and Haiku. Only the isolated Acta-QA
supervisor was SIGKILLed; the real user harness and both detached provider
processes were preserved.

- Crash with Codex awaiting a 90-second command and Claude waiting for Write
  approval: both became Unavailable while disconnected, then recovered their
  running/waiting states. The same provider PIDs survived. The pending approval
  remained actionable through the Needs approval button; approval completed the
  original Write. Codex finished its original command and final response once.
- Completion while disconnected: a bounded Codex command waited on a scratch
  gate, released only after the supervisor crash. The final answer was captured
  at 19:30:00 UTC inside the 19:29:58–19:30:13 disconnect window. Reconnect
  replayed exactly one completed tool result, final answer and completed turn.
- The composer draft survived both rounds. Working cleared after completion.
  No new Unknown Frames or processing errors were recorded. The remaining
  recorded Unknown Frames predate their fixes or concern deferred goal events.

No application code changes were needed for these reconnect checks. Evidence:
QA capture inspection and assertions, UI state before/during/after reconnect,
and /tmp/acta-qa-20260908/offline-window.json (temporary timing fixture).

## Reverse history, route changes and off-page delivery

Browser checks passed on the dedicated QA threads:

- Newest-page loading and automatic older-page insertion (50 to 64 rendered
  items) retained the reading position rather than jumping to the beginning.
- Switching between Claude and Codex isolated transcripts, configuration and
  unsent drafts. A cleared draft stayed cleared after returning. The original
  Codex interruption-test draft was restored after testing.
- Rapid debug visibility changes returned to the normal feed without stray
  debug cards. Existing Unknown Frames remained visible as intended.
- With one view reading older history, a second view submitted one minimal Spark
  reply and immediately switched to Claude. The reader retained the exact same
  anchor and -176.171875 px offset as the new user message, answer and turn end
  arrived. The answer appeared exactly once; the other thread stayed isolated.

Found and fixed a separate off-page delivery bug in a deterministic outbox
reproduction: after restoring an accepted submission, the composer remained
locked when the confirming user message was outside the newest history page.
Polling only returned command acceptance; clearing the outbox depended on seeing
that exact message in loaded frames.

Command status now reports message_confirmed when the matching provider echo is
present in the stored conversation, independently of pagination. The lookup is
indexed and checks thread, command/submission UUID and provider run. The browser
uses this evidence to release the outbox while preserving a newer draft; it never
resends or matches by text. Merely accepting a command still does not claim its
message has arrived.

Regression checks cover a confirming message behind 75 newer messages, same-text
nonmatching submissions, owner/thread isolation, projection rebuild, restored
outboxes, newer drafts, and late responses after route closure. Targeted database
race tests passed; all 24 frontend test files, Svelte checks and production build
passed. Deployed server PID 1948329, preserving the main and QA harnesses and all
provider processes. Reloaded the QA view successfully with its original draft;
no browser errors or new Unknown/processing-error groups in QA captures.

## Approval races, interruption and unusual output

Continued autonomous QA with only Spark/Haiku and the isolated scratch threads.

- Stopping Claude at a pending Write approval resolved the request, produced
  Turn interrupted, and created no file. A fresh message immediately afterwards
  worked. The provider emitted a second marker, [Request interrupted by user for
  tool use]; it now maps to Dropped like the ordinary interruption marker.
  Regression tests preserve actual submitted messages containing either text.
- Opposite Approve/Deny clicks from two tabs both reached the application. Deny
  won, both views converged on Denied, and no file was created. Captures contain
  one tool ID, transitioning pending to failed, followed by one final response.
- The QA supervisor was SIGKILLed immediately after observing a newly durable
  approval write in its pipe journal, then restarted three seconds later. The
  same provider process survived; one tool ID transitioned pending to completed,
  the file contained exactly APPROVAL-RECOVERY-ONCE, and both views showed
  Approved plus the same final response. Timing evidence is retained temporarily
  at /tmp/acta-qa-20260908/approval-crash-window.json.
- Both providers read a Unicode filename containing spaces and returned bounded
  long output (an 8192-character line plus 80 markup/Unicode lines). Expanded
  panels wrapped within their 900px content width, had 360px scrollable output
  heights, and treated markup literally (no generated HTML elements).
- Independent Git tests exposed invalid Claude generated patches for tab/newline
  filenames. Git-compatible C-style header escaping now applies to reconstructed
  and reported fallback patches. Tests independently apply create/update patches
  for spaces, Unicode, tab/newline, quotes/backslashes, trailing space and leading
  hyphen names. Exact file contents match after application.
- A bounded background Bash test exposed ToolSearch tool_reference results. These
  now complete its existing tool card with the available names. Malformed/mixed
  unrecognized results stay atomically unknown. Tests cover valid references,
  empty names, wrong tool identity and an unknown content part.

Adapter, controller and conversation race tests passed. All three fixes were
built and deployed via supervisor restarts, preserving detached pipes/providers.
Historical Unknown Frames are unchanged.

### Product decision: background task lifecycle

The bounded two-second background Bash run completed successfully, but exposes
new reviewed-to-be-designed events: background_tasks_changed, task_started,
task_updated, task_notification and its user-message notification echo. They
are still Unknown and remain inspectable. The task ID is btbk8ca1h and its
originating tool ID is toolu_01HM5Hxx8DkXb86E7NjnE7Yz in the Claude QA thread.
The task start/result/completion carry enough correlation to update the original
tool card and separately render the notification where it entered context.
Need Jack's choice on presenting background work before implementing those
non-debug mappings. The probe is finished; no background command remains running.

### Background command lifecycle and composer count

Agreed: preserve the original tool card, add a completion notice at notification
delivery, and show the live background count beside Working (or independently
after the turn ends).

Implemented tool background task IDs in adapter checkpoints and active background
frames in conversation current state. Launch receipts no longer finish background
commands. Turn endings leave them running; task updates finish the original row.
Duplicate context notifications are suppressed. Unknown task types remain visible.

Live evidence on the dedicated Haiku QA thread:
- A 35-second background command kept the footer count after its launching turn
  ended, including after a browser refresh; its original row updated on completion.
- Two commands (sleep/print success, sleep/exit 7 failure) produced counts 1, 2, 1,
  0 at capture sequences 1657, 1676, 1700, 1726. Approval waiting coexisted with
  the background count. Both completion notices expanded to the provider summary.
- Idle-time completion revealed that Claude may start an autonomous response
  without a user-message echo. The old mapping reused the completed turn and
  dropped its result, leaving Working stuck. This path now emits the notice and
  creates a separate response turn from the first provider message. Both subsequent
  completion replies showed their own turn ending and returned to idle.
- No new unknown shapes in the fresh runs. Old unknown frames remain historical
  evidence, unchanged by adapter upgrades.

Validation: captured lifecycle ordering/checkpoint/dedup tests, autonomous-response
regression, persisted reducer tests, full Go race suite with isolated PostgreSQL
integration tests, frontend type check/build and 24 frontend test files. Only the
scratch Claude provider was killed/resumed to clear the pre-fix stale state; real
user provider sessions remained running.

Further checks: a notification label containing literal <branch> & stream rendered
correctly, including an active-turn context echo. TaskStop cancellation reports
task_updated status killed followed by task_notification status stopped. Both
now normalize to Interrupted (captured regression added). Cancellation may have
no user/context echo despite occurring during an active turn, so notice timing
needs the user's choice: emit immediately and relocate on a later echo, or keep
the first-notification position. Asked before changing the placement policy.

The user chose immediate notice delivery with relocation on a later context echo.
Implemented one run/task-keyed stored notice; duplicate echoes neither duplicate
nor move it again. A fresh live TaskOutput test produced delivery sequence 2108,
context echo 2110, and exactly one database item at 2110. Explicit cancellation
now shows a stopped notice even without an echo, with no new unknown status.
Conversation projection version 3 rebuilds notice identities; reverse pagination
and rebuild integration tests passed.

A subsequent read-only Codex whoami MCP test exposed unhandled mcpToolCall
started/completed records. Added the existing generic tool-card mapping for text
and structured JSON results, preserving distinct structured results and suppressing
redundant JSON text copies. Rich content remains explicitly unreviewed. Regression
cases cover text, structured-only, duplicate structured/text, failure, rich-content
fallback and duplicate terminal snapshots.

Fresh Codex whoami verification rendered one acta/whoami tool card (41ms) and
returned MCP-VERIFIED; the existing composer draft remained intact.

A server-restart deployment exposed a startup gap: normal acta harness exited
when /api/account was unavailable, before entering its WebSocket retry loop.
Its detached providers survived; reconnected the normal supervisor. Added initial
account lookup retries using the connection loop's shared backoff/cancellation
helper. Connection refusal, HTTP 503 and 429 recover; 401/403/redirects/invalid JSON
or missing account identity fail without retrying. Regression tests cover each
case and cancellation during retry, with no provider quota consumed.

Codex background equivalent: fresh bounded command sleep 25; printf completed
without write_stdin. Provider commandExecution started at sequence 381 with
processId 68601 and status inProgress; the turn ended at sequence 390 (9.747s)
while the command was still live; command completion arrived at sequence 394
(24.887s execution duration). The current projection labels it Interrupted in
the intervening window, then Completed. Asked whether to infer background state
for these still-running process-backed commands at turn end, use the same footer
count, and emit a completion notice at the provider completion event (there is
no Claude-style context echo). No Codex background mapping changed pending that
product choice.

The user approved the same Codex background UI. Implemented process-ID tracking
in the adapter checkpoint. Still-running process-backed commands receive a
background snapshot before their turn/completed frame; later output remains on
the original tool card and terminal completion emits one fixed-position notice.
Captured regressions cover success, nonzero exit, missing process identity,
checkpoint restoration, duplicate turn/completion frames and late output.
Targeted adapter/conversation/hyperharness race tests passed.

Fresh Spark QA: the 60-second command showed Running in background and a footer
count after LAUNCHED/Turn ended, then Completed and Finished in background with
an empty footer. The existing composer draft survived sending and refresh.
A first probe had a model-typed incorrect working directory and never launched;
the corrected exact-directory probe succeeded. No new Unknown Frames appeared.

Overlapping Codex recovery passed: tool starts 445/446 became background at turn
455; exit-7 failure at 458 cleared only its own count and emitted one failure
notice; successful completion at 462 cleared the second and emitted its notice.
The QA supervisor restarted between launch and completion while the native Codex
process stayed alive; counts 2, 1, 0 and the original draft survived refresh.

The next probe exposed Claude AskUserQuestion as an Approve/Deny UI. Captured
control_request 2382 (requires_user_interaction true) and stopped the QA turn.
Jack approved adding dedicated question answering. Implemented generic request/
resolution frames, shared popup/card with choices and custom input, persisted
browser drafts, durable immutable run-bound answer commands, native routing for
Claude and Codex, and off-page pending/resolution state. Nonblocking Codex
questions survive turn endings. Secret questions and visual previews stay Unknown.

Validation before live review: adapter mapping/custom/multiple/cancel cases,
controller checkpoint/replay/stale-request tests, database migration and
HTTP ownership/payload/replay tests, conversation lifetime tests, frontend draft/
answer tests, all Go packages, targeted race suites, frontend check (zero warnings),
24 frontend test files and production build. One integration test initially reused
a decoded WebSocket struct and retained old optional fields; corrected it to use
a fresh control value, as the production client already does.

Live question checks passed on both cheap providers. Claude's custom answer
Teal & silver survived selecting a choice, replacing it with text, closing the
popup and refreshing; the provider echoed it exactly. A grouped multiple/single
question returned Alpha, Gamma and Large exactly. The conversation card and popup
kept selections synchronized. Visual review checked the live popup on port 8081.

Codex default mode returned UNAVAILABLE for request_user_input. Exercised its
installed plan-mode protocol on the dedicated QA thread only: a normal question
frame appeared, Green stayed selected across a QA-supervisor restart and browser
refresh, and the answer produced ANSWER: Green with an ended turn. Restored that
QA thread to default mode with a bounded no-tool turn. Existing composer draft
remained intact. No new unknown-frame categories in these tests.

Claude cancellation of an unanswered question passed: Stop removed the composer
popup and waiting indicator, marked the question No longer waiting, and emitted
Turn interrupted while keeping the provider alive.

Next bounded probe: Claude /compact completed in 21.68s. New unreviewed frames:
system/status compacting (2731), system/compact_boundary (2737), and a synthetic
user summary (2738). Boundary metadata reports 42,638 pre-tokens, 6,648 post-tokens
and 35,990 dropped tokens; includes preserved-message identities. The provider
summary is available in the raw capture. Asked whether to render a composer
compacting status followed by an expandable boundary notice with counts, duration
and the summary when supplied. No compaction mapping changed pending that choice.

Jack chose status + expandable summary. Implemented context/compaction snapshots,
checkpointed Claude boundary/summary correlation and Codex contextCompaction item
mapping. Projection hides starts, retains current status, and places completion
at the boundary; late summaries update without moving the notice. The UI omits
missing counts/summary instead of guessing. Captured replay cases cover duplicates,
checkpoint restoration, unrelated synthetic messages and Codex missing-start
completion; projection covers late updates during another compaction and turn
interruption. Live verification follows deployment.

Live compaction checks passed. Codex emitted a contextCompaction lifecycle and
rendered one duration-only notice (2s); its draft survived refresh. Claude first
reported Not enough messages to compact immediately after an earlier compaction.
After a bounded no-tool exchange, it displayed Compacting context… and completed
with 26,806 → 6,060 tokens in about 17s. The boundary notice expanded to the actual
Markdown summary and survived refresh. Neither run added unknown-frame categories.
Both providers remain on cheap QA models. Server/supervisor deployment preserved
native provider processes.

Also normalized Claude's complete, recognized three-tag slash-command echo to
plain /command arguments in user messages, preserving the raw debug frame and
leaving malformed/unrelated text untouched. Added focused formatting regressions.
Validation: all Go packages (local socket tests required host execution), targeted
adapter/conversation/threads race tests, isolated database conversation tests,
frontend check (0 errors/warnings), 24 frontend test files and production build.

Post-compaction continuation passed on both providers: each replied exactly
POST-COMPACTION-OK and ended idle; the Codex composer draft remained unchanged.

Further recovery QA: killed only the isolated Claude provider with a selected,
unanswered question. Its card became No longer waiting and Turn interrupted;
resuming and requesting the identical question produced a blank form. Answered
Amber and the provider echoed Amber. No unknown-frame categories added.

Reproduced a frontend polling defect with a deterministic request spy: every
settled/cancelled question whose control lookup returns 404 generated another GET
on each poll (3 calls for 3 cycles). Cache absence only after settlement, while
continuing pending questions and uncertain browser-owned write retries. A fresh
resolved projection must still fetch a remotely submitted answer rather than use
an earlier pending-state 404. Regression cases cover all three paths.

Polling fix deployed after frontend check and all 24 frontend test files passed.
The server restarted successfully (2308278); a stale extra PID entry in the local
restart helper then failed its preflight before touching any supervisor. Removed
that stale entry; native providers remained live throughout.

Background kill/resume QA found two fresh shapes: task_notification sequence3202
reports stopped command btn099ydt from the previous run, and result3206 is a
successful empty task-notification receipt (num_turns0, output_tokens0). The old
card had already become Interrupted and emitted its stopped notice at kill.
Asked Jack whether to repeat that notice at the resume position or suppress it.
No resumed-notice mapping changed pending the product choice. Added narrow empty
receipt handling so it cannot create a turn or close an unrelated active turn;
errors still stay inspectable. Captured regression covers idle and active states.

Jack chose to show the stopped notice again at resume. The adapter now carries
minimal known background identity/label metadata across runs, separate from live
tools, and emits a notice in the receiving run. Duplicate delivery stays local;
a later context echo relocates only the new notice. Neither the old card nor the
old notice is rewritten, and no background count is resurrected. Captured tests
cover repeated runs, serialized checkpoints, duplicate delivery, echo relocation,
and mismatched tool identities. Adapter/conversation/hyperharness race tests passed.

Live verification passed: Resume notice QA showed one stopped notice at Kill and
a second after SessionStart:resume, with the original card still Interrupted and
a zero background footer. Refresh retained both positions. The resumed empty
receipt produced no extra turn or Unknown Frame; capture categories were unchanged.
Deployed supervisors: normal2326328, QA2326423; existing provider pipes preserved.

Image-result probe: generated a deterministic 16x16 magenta PNG fixture in both
isolated QA directories. Claude Read succeeded and answered Magenta, but its
image-only tool_result (sequence3359, run95aea1af-b1e5-4af1-85cf-d741a1c47821)
remained Unknown and the still-running Read card was marked Interrupted at turn
end. Captured the native frame in testdata/claude-image-result.json. It supplies
base64 image/png plus original/display dimensions; duplicate image bytes also
appear in tool_use_result metadata. The cheap Codex Spark QA provider reported
view_image unavailable, so no equivalent live image frame was produced and no
model upgrade was made. Existing Codex draft preserved.

Asked Jack whether image tool results should get an expanded-card thumbnail and
click-to-enlarge preview, or metadata only for now. No image mapping/UI changes
made pending that product decision. Both QA threads ended idle.

Jack approved Thumbnail + enlarged preview. Implemented raster image output on
existing tool snapshots, retaining adjacent text and completing image-only Read
results. Captured fixture regressions cover serialization, duplicate delivery,
mixed text/image results and atomic fallback for malformed/unsupported content.
Native duplicate image bytes are not duplicated into the nondebug snapshot.

Live image verification passed on a fresh Claude Read: Completed status, thumbnail,
enlarged preview and refresh. A subsequent one-page PDF Read with pages=1 yielded
mixed text and page-image output; both rendered through the same implementation.
Neither added Unknown Frames. Plan probes on both cheap providers reported the
requested checklist tool unavailable, so no new plan behavior was inferred.
Read-only scan of the two jack threads found only historical unknown categories
(last at 17:51), including already-fixed interruption/approval/diff shapes and the
still-deferred goal event. No user-thread provider was driven.
Server2347542, normal supervisor2347574, QA supervisor2347666; native processes
were preserved. Validation: adapter/conversation race tests, frontend check with
0 errors/warnings, 24 frontend test files, production build.

Whole-PDF variant reproduced a new Unknown: Claude Read without pages returned
text plus a document/base64 application/pdf block (sequence3597, current QA run).
The successful Read was consequently marked Interrupted by turn completion.
Captured in testdata/claude-pdf-result.json. Asked Jack to choose a compact PDF
attachment with Open/Download versus an inline PDF viewer; no document mapping
or UI behavior implemented pending that decision. Both QA providers are idle;
Codex draft remains preserved. Latest capture contains7231 frames, with only this
new unknown category instance since the image fix.

Jack chose PDF attachment with Open/Download. Added whole-PDF mapping with a
basename, byte-preserving browser-local download/open URLs, and the existing
text output. The same card completes normally. Unsupported document formats
stay Unknown; no local file transfer or inline reader was introduced.

Whole-PDF implementation passed captured adapter/conversation race tests,
frontend check (0 errors/warnings), all25 frontend test files and build. Browser
URL tests verify byte identity, PDF MIME and revocation, and reject malformed or
unsupported attachments. Fresh live Read completed, displayed correct filename,
587-byte count and Open/Download links. Browser automation explicitly blocked
navigation to the PDF blob URL; no workaround or alternative browser surface was
used. Native PDF viewer opening/download is therefore not verified end-to-end.
Server2369780, normal supervisor2369812, QA supervisor2369907; native processes
preserved. Continuing with local-command output probes.

Local /status command probe found a delivery bug without adding Unknown Frames:
Claude emits command_lifecycle queued/started, a synthetic assistant response
saying the command is unavailable, and result with user_message_uuid. No user
text echo arrives, leaving Acta's outbox accepted but permanently Sending. Fix:
correlate provider queue/start or explicit result UUID with the exact persisted
Acta send in that run, emitting one completed user snapshot with the submitted
text. Internal commands and wrong-run receipts cannot create a user message;
later echo/duplicate acknowledgements deduplicate by message UUID.

Command acknowledgement fix passed adapter/hyperharness/conversation race tests
and go vet. Deployed supervisors normal2395925 and QA2396031, retaining provider
processes. Created isolated Haiku QA thread5b24dfdc-d229-42e4-9b43-af1f3de85723 in
/tmp/acta-qa-20260908/claude-command-ack because the original recorded /status
cannot be reinterpreted retrospectively. Live /status now completes its user
message, releases Sending, accepts a subsequent ordinary message, and survives
refresh with both acknowledgements. Provider echoed the correct subsequent
prompt but did not follow its exact-response instruction; this is not evidence
of a delivery bug. All QA turns are idle. Latest capture7647 frames has no new
unknown categories beyond the original pre-fix PDF example.

Pending presentation decision: Claude explicitly marks local-command replies
as synthetic/is_meta with local_command_source, but we currently render them
as normal assistant messages. Asked Jack whether to use a compact command-result
notice instead. No presentation change yet.
