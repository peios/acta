---
title: "Stream Invariants"
description: >-
  Atomic capture bundles, replay equality, contiguous acknowledgments, and
  monotonic message state.
---

JSON Schema validates individual shapes; these require stream/transaction
checks:

1. Every captured input has exactly one debug frame and contiguous non-debug
  indices `1..N`. `Resolved` has at least one such output; other debug types
  have none. References agree with the actual output kinds and indices.
2. A capture's complete output bundle is durable before its capture sequence
  is acknowledged. The server commits the debug frame and all derived outputs
  in one transaction before acknowledgment. Delivery cursors account for the
  complete bundle.
3. Replayed frame identities have identical payloads. Persisted derived
  bundles prevent reinterpretation of acknowledged captures under the same
  identity during adapter upgrades.
4. Acknowledgment advances only through complete contiguous capture bundles.
  Output filtering in the UI does not change capture ordering/acknowledgment.
5. Source thread/run identities match the adapter's known provider
  association. User/assistant snapshots and deltas resolve to the correct
  message/turn.
6. Snapshot replacement and lifecycle monotonicity are enforced by reducers; a
  late event does not reopen a completed message or duplicate a turn outcome.

The adapter persists its output bundle and mapping state before delivery. The
server checks the schema, complete output references, contiguous captures, and
identical replays in one transaction before acknowledging. Browser pages
include whole captures (up to 64 capture sequences); they never split outputs
at a cursor. Previously captured raw-only history remains raw-only; it is not
silently reinterpreted. New pipe journals additionally retain exact original
text. A detached pipe started before the mapping retains its previous
JSON-normalizing capture behavior until its next restart; its provider
processes need not be interrupted for the mapping.

The thread feed hides optional debug frames by default. Unknown Frames remain
visible so unhandled provider input remains inspectable. Debug visibility does
not change capture, pagination, or stored history. The feed retains the same
available width when debug visibility changes.
