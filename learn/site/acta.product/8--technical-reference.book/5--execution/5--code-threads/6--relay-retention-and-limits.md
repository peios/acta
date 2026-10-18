---
title: "Relay, Retention, and Limits"
description: >-
  Code host methods, bounded in-memory buffers, authorization, and reconnect
  behavior.
---

These methods use the existing per-human Code host request relay:

- `threads.start`: `{id, codebase_id, root_id, provider}` returns a thread.
  `provider` accepts `claude` or `codex`. The UI supplies a UUID so a retry on
  the same host run returns the existing thread instead of spawning another
  process. Reusing an ID with different roots or providers is rejected. The
  dialog retains the ID for an unchanged retry and generates a new ID when its
  provider or folder changes after a failed request.
- `threads.list`: `{codebase_id}` returns `{threads}` in newest-first order.
- `threads.send`: the input contract above; returns the thread with
  `busy:true` on initial acceptance. Retried inputs return its current state.
- `threads.reprocess`: `{codebase_id, thread_id, revision}` returns the thread
  with its incremented revision after rebuilding retained output.
- `threads.read`: `{codebase_id, thread_id, after, revision}` returns
  `{thread, frames, first, next, more, reset}`. `after`/`next` are
  per-revision **change** cursors; `first` is the earliest retained
  chronological `seq`, used for pruning and indicating truncated history.
  Frames can update earlier positions.

The browser reads the selected thread every 500 ms while Code is visible,
fetching subsequent pages immediately when `more` is true. Lists refresh every
three seconds. Account/host/codebase changes abort old reads and clear scoped
UI state. All operations retain the relay's caller and native-session checks;
no Workspaces sharing, superuser bypass, or new MCP tools are introduced.

This is intentionally a live, in-memory surface: up to 64 threads and eight
active processes per host run, shared across both providers. Each thread
retains at most 1 MiB of encoded frames and 1,000 entries, evicting the oldest
with cursor gaps. Read pages are bounded to 512 KiB/100 frames; browser
retention is also bounded. Frames remain on the host across browser reloads
and relay reconnects. Restarting the host stops processes and clears this
thread store; durable CAT storage and provider session resumption are not
implemented. The server only relays these reads and does not persist
transcripts. Debug output can include local hook output and paths, and is
visible only to the host's authenticated human owner.
