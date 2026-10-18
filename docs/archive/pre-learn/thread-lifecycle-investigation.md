# Provider thread lifecycle investigation (ACT-63)

This records the provider persistence boundary found while implementing ACT-63.
The implemented behavior is documented in [Provider threads](threads.md).

## Verified on 7 September 2026

Using installed Codex CLI 0.153.4, a dedicated `codex app-server --stdio` was
initialised, then `thread/start` was called with `ephemeral: false`, without any
user input or model turn. Every process was shut down after the check. A second
app-server was initialised and asked to `thread/resume` the returned thread ID.

| Check | Result |
| --- | --- |
| Start, immediately close stdin, then resume in a new process | Start succeeds; resume returns `-32600`, `no rollout found for thread id ...` |
| Start, immediately send SIGINT, then resume | Same failure |
| Start, await a successful `thread/name/set`, close stdin, then resume | Same failure |
| Start with explicit legacy history mode, immediately close stdin, then resume | Same failure |
| Start, allow three seconds of startup activity, close stdin, then resume | Resume succeeds in this observation |

The returned `thread.path` was non-null in every Start response. The failed
cases had no file at that path after shutdown; the delayed successful case did.
The configured default history mode was paginated, but the explicit legacy-mode
check also reproduced the failure. A full-history read on the paginated case
returned `-32601`, `list_turns is not supported yet`.

These observations establish that a successful Start response, a non-null path,
or a successful rename is not a persistence barrier. They do not establish that
waiting three seconds is sufficient under other loads, machines or failures.
Do not implement a fixed sleep as a durability guarantee.

## Agreed decision and implementation

Create is a live hypercontrol request with a fresh Acta UUID. It writes no thread
or pending creation row on the server. The hyperharness advertises the thread
after capturing a successful native Start response; discovery is what creates
the durable server record. The browser watches that UUID for about 30 seconds,
then shows a synthetic notice. Expiring that watch neither cancels creation nor
rejects late discovery. Repeating the same UUID never launches another process.
Distinct explicit creations may produce similar threads with different UUIDs.

A live check with the integrated detached pipe also observed an acknowledged
empty session whose returned rollout path never appeared during a minute of
waiting. File existence therefore cannot be a mandatory discovery barrier for
this slice. There is no fixed startup sleep and no claim that acknowledgement
proves native session durability.

Resume always asks the provider to resume the existing native ID. If Codex has
not saved it, its failure is shown in Acta. The earlier proposal to recreate an
empty native session was not adopted. Acta history and identity remain intact;
there is no automatic replacement provider identity and no hidden model turn to
force persistence.

Official protocol reference: [Codex App Server](https://learn.chatgpt.com/docs/app-server).
The installed protocol schema was also generated and checked for an explicit
empty-session persistence option; none was identified in ThreadStartParams.

## Reopened after user reproduction

The user continued receiving `no rollout found` on Resume. Inspection confirmed
that the existing Acta thread still references its original native ID and that
the advertised native history file does not exist. Successful discovery did not
fix the underlying empty-session persistence problem; describing the entire
lifecycle slice as complete overstated its readiness.

Follow-up checks on 7 September 2026 used fresh disposable sessions, no prompts
or model turns, and explicitly enabled the experimental API with legacy history.
All three Start responses reported legacy mode, but after up to eight seconds
none had a history file and each fresh-process Resume failed. A separate probe
found that `thread/inject_items` with an empty list is rejected (`items must not
be empty`); `thread/archive` also fails when the rollout is missing. No dummy
conversation items or manually fabricated provider files were introduced. All
probe processes were terminated.

The official App Server documentation also says paginated history resume is not
supported. That is a relevant compatibility concern because the installed
provider returns paginated by default, but explicit legacy mode does not by
itself solve unused empty-thread persistence. The earlier isolated delayed
success must not be generalized into a timing guarantee.

An honest product decision remains necessary: expose empty sessions as not yet
resumable while deferring message support, bring real first-message support
forward, or explicitly permit replacement of never-used native sessions. The
last option was not authorized by the discovery-model discussion and has not
been implemented. ACT-63 is reopened while this boundary is resolved.

## Approved first-message fixture and successful resumption

Jack subsequently approved sending exactly Test Message on first creation as a
real user message. The adapter now explicitly starts legacy history, submits the
first turn with its stable run/method input identity, and reconciles captured
responses after interrupted creation. Resume and completed-creation reconnects
do not send it again. Existing native sessions are not replaced or backfilled.

Live verification with Codex 0.153.4: a new thread saved its rollout after the
first message; Kill followed by Resume returned the same native ID and one turn
containing the original Test Message. One turn/start write was recorded across
both runs. The earlier observation was therefore an unused-session boundary;
actual first-message persistence and resumption now have end-to-end evidence.
Tests cover lost acknowledgements after both thread/start and turn/start,
reattachment, Resume, and the exact fixture payload without repeating it.
