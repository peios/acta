# Subagent lanes validation — 9 September 2026

Implementation and provider boundaries are described in [Subagent conversations](subagent-lanes.md).

## Automated checks

- Full Go suite passed, including PostgreSQL integration tests with isolated schemas.
- Focused adapter, conversation reducer and hyperharness tests passed with the race detector.
- Frontend type/Svelte checks: zero errors and warnings; all 25 frontend test files passed; production build passed.
- Captured Claude and Codex subagent events replay through serialized adapter checkpoints.
- Database coverage verifies independent lane pagination, colliding native IDs across lanes, singleton state isolation and projection rebuild equivalence.
- Regression coverage includes nested lane attribution, child interruption, pending child approvals surviving the parent's completion, notification relocation, child background commands and provider interruption-marker suppression.
- Frontend coverage verifies lane switching while a page request is in flight and restoring cached history without mixing lanes.

## Live checks

QA used separate scratch conversations and inexpensive models: Claude Haiku and Codex Spark.

- Claude: foreground and background child attribution; forwarded messages/tools; approval popup visible from Main and child; denial; native stop while awaiting permission; interrupted child history; parent remaining usable; background completion waking a new parent turn.
- Codex: child history and model metadata; reopen completed child from its parent card; pill removal on leaving; native child-turn interruption while preserving the parent and its draft.
- Both providers: rendered lane navigation, compact parent cards and completion notices inspected in the browser.

Nested delegation was verified with deterministic protocol tests; the live Codex probe did not actually spawn its requested nested child. Do not treat that probe as end-to-end nested-delegation coverage.

## Observed limitations

Codex multi-agent v2 rejects direct child input. Claude's tested SDK does not expose direct child messaging. Child message and settings controls therefore remain read-only. Claude forwards completed child content blocks rather than token-by-token text. Missing child configuration remains unknown.

Old Unknown Frames and old projections from the exploratory QA runs remain as recorded; new adapter mappings apply to new captures. Existing provider processes require resumption to acquire the new launch flags. Server/supervisor redeployment preserves detached provider processes.

## Extended regression pass

A second pass exercised concurrent Codex children and repeated follow-ups to the
same completed child, plus Claude child approvals across a supervisor reconnect.
A pending request was denied from a different child lane after reconnect; a later
child write was approved and completed. Both QA providers were killed/resumed to
check recovery of drafts and historical child conversations. User work was kept
separate from the scratch QA conversations.

The pass reproduced and fixed:

- Claude child background wake-ups without a repeated Agent tool ID: these now
  reopen the known lane with a distinct turn and completion notice, preserving the
  original task prompt. Replayed starts do not duplicate turns.
- Queued Claude background replies: a new provider message after a completed turn
  establishes its own turn. Notification timing no longer leaves Working stuck.
- Codex `subAgentActivity` follow-up receipts with `kind: interacted`: recognized
  as local coordination; child turn events remain authoritative for lifecycle.
- Prior-run child identities in approval routing: only current-run children can
  receive requests.
- Multiple Codex children described by one coordination call: each has a separate
  stored parent card. Projection version 5 rebuilds cards from normalized frames;
  the client invalidates all lane caches when the projection version changes.
- Old unfinished lanes appearing in the running pill bar after provider resume;
  they remain accessible through history and show their recorded configuration.
- Per-turn child timing: cumulative native task duration no longer counts earlier
  activities again when a background child wakes.

Added deterministic coverage also verifies question request/resolution routing for
both providers across serialized checkpoints, and late older-page responses during
lane/debug switches. All frontend tests and checks passed. The complete Go suite,
including isolated PostgreSQL integration tests, passed; targeted adapter,
conversation and hyperharness race tests passed again after the final timing fix.

The final live regression capture windows contained 561 Claude frames and 110
Codex frames with no new Unknown Frames or normalization errors. Claude's child
launch and background wake displayed separate completed turns; both parent replies
ended cleanly. This does not claim every possible provider frame is covered.

### Remaining native-provider observations

The Codex child follow-up lifecycle ran twice and produced distinct completed
turns. However, the child answered the injected recommended-plugin list instead
of the requested SIBLING-SECOND / SIBLING-THIRD prompt. The same assistant text is
present in Codex's own child rollout, so this is not an Acta rendering mismatch.
Do not count this as successful semantic follow-up delivery; the cause within the
native provider has not been established. Initial child instructions did work.

Codex's resume also emitted the already-known `thread/goal/cleared` event, which
remains an Unknown Frame because goal UI/mapping is outside this subagent slice.
The zero-error capture windows above concern the final subagent regression turns,
not every historical or resume-time frame.
