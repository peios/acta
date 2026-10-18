---
title: "Common Envelope"
description: >-
  Normalized frame identity, timestamps, capture sequences, and output
  indices.
---

This section describes common envelope in the Acta implementation.

```json
{
  "schema_version": 1,
  "thread_id": "11111111-1111-4111-8111-111111111111",
  "run_id": "22222222-2222-4222-8222-222222222222",
  "sequence": 42,
  "output_index": 1,
  "provider": "codex",
  "received_at": "2026-09-07T17:00:31.670Z",
  "occurred_at": "2026-09-07T17:00:31.668Z",
  "kind": "message/assistant/delta",
  "data": {
    "message_id": "example-message",
    "turn_id": "example-turn",
    "text": " received."
  }
}
```

Every field in this envelope is required. `occurred_at` is null when the
provider supplies no event timestamp. `received_at` is always the original
capture timestamp, not the time of a retry. Lifecycle timestamps inside `data`
are distinct from the provider envelope's event timestamp.

`sequence` retains the durable capture sequence already used by Acta,
increasing across provider runs in one thread. The `output_index` allows one
captured input to produce several output frames:

- Index `0`: exactly one debug frame.
- Indices `1..N`: non-debug outputs, in deterministic order.
- Frame identity: `(thread_id, sequence, output_index)`. The matching `run_id`
  and provider are immutable attributes checked on replay.
- A `debug/resolved` frame lists its outputs' indices and kinds. The
  references inherit its thread, run, and capture sequence.

The server supports multiple outputs per capture. The debug frame carries the
raw input, and derived outputs link back through the envelope.
