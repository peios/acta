---
title: "Tool Calls"
description: >-
  Provider tool lifecycle, exact arguments, result reconciliation,
  permission denial, and presentation.
---

`tool/call` provides an authoritative snapshot of one operation, identified by
`tool_id` and `turn_id` within a provider run. It includes the provider tool
name, a presentation category (`command`, `read`, `write`, `edit`, or
`generic`), label, arguments, status, optional output/streams, working
directory, exit code, duration and lifecycle timestamps. Unknown values are
null. `tool/arguments/delta` and `tool/output/delta` carry incremental text
with the same IDs. Snapshots replace corresponding accumulated text when
supplied; a null output does not erase streamed output. Replayed frame
identities are deduplicated and terminal calls ignore stale deltas.

Supported mappings are Codex `commandExecution` and Claude tool-use envelopes
with any nonempty tool name. Unknown tool names get the generic presentation;
Read, Write, Edit, and Bash have optional label/icon decoration. Codex's
explicit `commandActions` read annotation supplies a readable file label;
arbitrary shell commands are not parsed to infer their effects. Claude
tool-use starts are Preparing; streamed JSON is assembled locally and
reconciled with the authoritative assistant tool-use record. Finished
arguments are Pending, not proof of execution. Claude content-block stop
closes argument generation; a matching tool_result completes foreground
operations; a background launch receipt keeps the operation running. Tool
results are not human messages. Claude's is_error and interrupted flags
determine failure or interruption; Codex supplies execution state, exit code,
and duration. The adapter does not infer Claude execution timing from
argument-generation timestamps. Non-text results, unknown identities, and
mixed/unreviewed envelopes remain Unknown Frames when no mapping accepts them.

The UI keeps one compact row at the first call frame, collapsed by default,
with an icon, description, lifecycle state and duration when available.
Expanding it shows exact arguments and output as text, plus separate
stdout/stderr when useful. File reads use the same renderer. A turn ending
marks unfinished calls interrupted; a disconnected or replaced provider run
displays unfinished calls as unavailable. Generic Claude tool calls can
include MCP tools when they use the same text-result shape. Reviewed
tool-result mappings support text and raster images; unhandled rich content
remains inspectable as Unknown Frames. Background commands use the lifecycle
below. Approval interaction is described in [Thread permissions and
approvals](~acta/technical-reference/execution/thread-approvals).

Claude's structured `system/permission_denied` and matching `tool_result_meta`
`user-rejected` records establish the terminal `permission_denied` state. The
`permission_denial` object preserves the provider message, reason and reason
type (null when unavailable). A denial and its subsequent result update the
same row; a late denial can refine a failure without losing output. The UI
labels it Permission denied and exposes its reason separately from the exact
result. Ordinary `is_error` results remain Failed; error prose is not parsed
to guess permission decisions. Denials do not offer approval controls: the
call is not waiting. New mappings apply to new captures; historical debug
frames stay unchanged.
