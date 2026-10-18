---
title: "Thinking Lifecycle"
description: >-
  Stable thinking identities, streamed text, completion, redaction, and
  usage-independent timing.
---

`thinking/started`, `thinking/delta`, and `thinking/completed` identify one
thinking item using `thinking_id` and `turn_id`, scoped to its provider run.
Start and completion carry full `text`; deltas carry text fragments with
`channel` (`summary` or `content`) and zero-based `section_index`. Sections
are joined with paragraph breaks. The UI prefers readable summaries when
provided, otherwise content. Completion is authoritative and replaces streamed
text. Token-only updates have an empty text fragment. `estimated_tokens` is a
nullable absolute estimate, never a delta or a billed usage counter.
Start/completion timestamps are nullable, using provider item timestamps where
supplied and capture times for observed Claude lifecycle boundaries. A
completion without a known start does not invent a duration.

Codex maps reasoning item lifecycle and its indexed summary/content deltas. It
does not expose per-item token estimates. Claude maps thinking block start,
text deltas, and block stop. Its final assistant thinking record reconciles
with the streamed block using the opaque signature (or the sole unambiguous
block); it never creates an empty assistant bubble. Signatures remain local
adapter and debug data, never readable UI text. Ambiguous or unsupported
records remain Unknown Frames. Every captured input retains its usual debug
classification.

Claude's cumulative thinking-token estimates are attributed only when the turn
has one thinking item. The system and stream paths update the same maximum, so
repeated values do not add together; null estimates do not erase a known
estimate. If another thinking item starts in that turn, per-item estimates are
cleared rather than dividing a cumulative total by guesswork.

The thinking row stays at its first frame's position. It reads `Thinking…`,
then `Thinking for Ns · ~N tokens` when estimates exist, and `Thought for Ns`
on completion. The local timer runs without requiring deltas. Available text
expands automatically during thinking and collapses on completion, with a
manual toggle for reading it afterward. Empty thinking has no disclosure. A
terminal turn freezes an unfinished item as interrupted; an unavailable or
replaced provider run never keeps ticking or claims successful completion.
Historical debug frames are not reinterpreted by adapter upgrades; these
mappings apply to new captures.
