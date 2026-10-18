---
title: "Snapshot Semantics"
description: >-
  Full configuration and message snapshots, unknown values, usage
  observations, and debug dispositions.
---

This section describes snapshot semantics in the Acta implementation.

## Configuration snapshots

The adapter emits a complete snapshot of what it knows for the run: omitted
fields mean unknown, and explicit null means known to be unset. Before
emitting a snapshot from a partial provider observation, the adapter merges
that observation with its known state. The client replaces its snapshot; it
does not try to infer whether incoming properties were a patch. A new run does
not inherit unverified state from an earlier run.

The normalized representation includes model, effort, fast_mode, cwd,
work_mode, and a permissions summary. fast_mode is boolean when known and
omitted when unknown. Provider identity/version remain runtime metadata;
native instruction sources, approval policies, and delegation hints remain in
debug capture. Model IDs are necessarily provider identifiers; supported
effort choices are advertised and cannot be silently approximated.

Work modes are execute, plan, and unknown. Permission modes: ask,
accept_edits, automatic, dont_ask, bypass, custom, and unknown. Automatic
means a provider-supported automatic approval reviewer, not blanket
permission. Don't ask denies anything requiring a new approval. Bypass
describes approval behavior and does not by itself remove access restrictions.

The separate filesystem/network summaries report access boundaries. Filesystem
categories are read_only, workspace_write, unrestricted, custom, and unknown;
network categories are allowed, blocked, restricted, and unknown. Use the
accepted practical mappings in [the permission
mapping](~acta/technical-reference/execution/thread-approvals). Reserve custom
for genuinely different configurations, and unknown for missing information.
These reported summaries are not authorization grants. Permission changes use
durable owner-scoped control commands and await the provider's effective
configuration.

## Message snapshots and streaming

`message/user` and `message/assistant` are full upserts with a lifecycle
`state` of `in_progress` or `completed`. A completed assistant snapshot
replaces the accumulated text. The delta frame appends exact text once. Later
replayed starts or deltas do not regress finalized content. An unknown
start/completion time is null; observing a completion does not invent a start
time.

Message and turn IDs are opaque, scoped to the Acta thread. A live native item
ID can be used for normalized identity, but reconstructed resume history has
demonstrated different item IDs. The live adapter does not import
reconstructed history or correlate it by text alone. Full reconciliation of
missing historical items remains an explicit open design item.

User messages support text and embedded raster images. Assistant text retains
the observed commentary and final-answer phases. Separate question frames
carry supported provider questions. Unsupported content stays in debug capture
instead of being silently discarded. A frame with partly understood content
retains unhandled fields as unresolved input.

User-message role alone does not establish the author as a human or an Acta
account. Native client correlation is adapter-local. The optional
`submission_id` is an Acta command UUID, emitted only when the provider echo
matches a known submission for the current run. It correlates the local
Sending placeholder with the provider message without text matching.
Historical/provider-originated messages without that correlation omit it.

## Usage and MCP status

Usage updates replace snapshots rather than incrementing client counters.
Unavailable values are null rather than zero. ContextUsage separates current
context occupancy from cumulative provider-thread model-call consumption and
the latest model-call consumption. The Codex adapter maps total/last into
consumption snapshots; detailed reset behavior remains open to observation
during use. Occupied context tokens remain null, so no context percentage is
inferred. The synthetic example leaves consumption snapshots null to
illustrate unavailable data. Credit balances remain decimal strings, with no
inferred currency. Individual TokenBreakdown fields still use the shared
schema definition.

Account limits use a generic windows array, not Codex primary/secondary
fields. An empty array means no windows were supplied. Account usage is
observed through a run. A bucket ID does not establish provider account
identity; do not combine different runs' limits based solely on the Acta owner
or provider name. Structured individual-limit fields have not been reviewed
and remain in raw debug capture rather than an invented normalized shape.

MCP startup statuses are keyed by `(thread_id, run_id, server_name)`. A
previous run's ready status does not establish readiness after a new run
begins. Startup status is not a continuous connection-health guarantee.

## Raw debug data and processing failures

The `raw` field is **text**, retaining the captured provider line before
parsing, excluding its framing newline. This avoids precision loss for numbers
and retains exact provider spelling/unknown fields. Stderr is stored as its
original text, not JSON-stringified once before insertion into the debug
field. Normal JSON transport escaping still applies. In the browser display
copy, valid JSON raw payloads are embedded as formatted JSON rather than
escaped strings; this does not change the stored or wire raw text. Formatting
uses raw JSON values on the server to retain large integer precision.

Recognized frames producing non-debug output are Resolved even if also used
locally. Hypercontrol-only handling is Local. Resolved references link to
actual outputs. Unknown covers unsupported methods/types/shapes. Dropped is an
intentional no-value disposition, never a catch-all error path.

A recognized frame whose normalized output fails validation becomes
`debug/local` with a non-null `processing_error` and no derived outputs. Its
adapter state changes are discarded. That classification is persisted once;
reconnect never retries a different interpretation under the same capture
identity. Unsupported input shapes remain `debug/unknown`. Provider RPC
failures also retain their error in Local Frames. Storage failures abort
delivery without acknowledgment.
