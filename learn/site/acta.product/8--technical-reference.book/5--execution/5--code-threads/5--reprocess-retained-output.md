---
title: "Reprocess Retained Output"
description: >-
  Pure replay of retained Code host output, optimistic revisions, and
  browser reset handling.
---

The thread toolbar's **Reprocess** button rebuilds the actual CAT thread in
place. It is available when the host is connected and the thread is not
starting or busy; finished/failed threads can also be reprocessed. No provider
is started, contacted, or sent input, and replay never calls the live
lifecycle/acknowledgment parser.

The host keeps a separate chronological source journal containing raw provider
lines plus the original input, acknowledgment and submission-error records.
Each source gets a stable `source_seq` and receipt timestamp. Live raw output
and replayed raw output both pass through `Adapter.NewConverter()` /
`Converter.Convert` in `internal/codeagents/convert.go`. A converter gets
fresh state at replay start and can emit zero, one or multiple CAT payloads
per source. Its final state becomes the converter for subsequent live output.
Original non-debug records are copied unchanged, preserving input IDs and
acknowledgment links within retained history.

Both adapters map recognized stderr diagnostics to `debug.log`. Codex drops
its known initialization success acknowledgment on stdout, matching its
structure rather than a particular request ID, version or filesystem path.
Errors, malformed acknowledgments, and responses with unknown fields remain
inspectable. Codex also drops valid stdout events whose top-level `method`
starts with `remoteControl/`, including future methods under that prefix.
Matching is case-sensitive and does not inspect text inside params. Both
adapters also map recognized hook lifecycle snapshots and the host
reconstructs their supersession chains. Raw sources remain retained. Other
events remain unknown; additional schema mappings belong in those pure
converters. Converters use retained input, without process, network,
filesystem, or live-session access. Their bounded state handles incomplete
retained history.

Publication is atomic with respect to reads, incoming output and message
sending. Each successful replay increments `thread.revision`, replaces the CAT
projection, and restarts its sequence numbers. `frame.source_seq` and
timestamps continue to identify the original source observation, including
when a source emits multiple frames. Repeated replay never consumes previously
converted output. A canceled rebuild leaves the previous projection and
revision intact.

`threads.read` includes the caller's `revision`. A mismatch returns
`reset:true` and starts at the first retained frame, even if the old cursor is
beyond the new sequence length. All clients discard their old frames on reset,
including clients that did not press Reprocess. `threads.reprocess` requires
the expected revision, so a retried old request cannot silently replay a
second time.

The source journal and CAT projection each retain up to 1 MiB / 1,000 records.
After source eviction, `thread.source_truncated` stays true and the UI
explicitly warns that replay covers retained history only. Source records
remain in host memory; they are not persisted remotely or to disk. Converter
changes require rebuilding/restarting the host, which clears this history.
Reusing captures across code changes will need a later persistence or import
facility.
