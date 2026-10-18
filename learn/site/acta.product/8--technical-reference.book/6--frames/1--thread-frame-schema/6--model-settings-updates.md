---
title: "Model Settings Updates"
description: >-
  Provider-confirmed configuration, Claude usage semantics, and local
  diagnostic redaction.
---

Codex `thread/settings/updated` uses the same `thread/configuration`
normalization as start/resume snapshots (mapping `effort` and `sandboxPolicy`
to the existing configuration inputs). Duplicate snapshots remain local debug
frames. Model catalog responses and settings RPC acknowledgments are local
debug frames; they belong to hypercontrol command results, not a new
conversation frame type. The UI uses the provider-confirmed configuration for
its model controls.

## Claude adapter observations

Claude reuses the existing kinds. Its message UUIDs correlate real Acta sends;
streaming text deltas append to a stable assistant message, and full snapshots
replace accumulated text before completion. Claude does not report commentary
versus final-answer phase, so phase is null. Provider local-command receipts
are not user messages. Unsupported content remains in Unknown Frames.

`context.estimated` is an optional boolean on context occupancy; true labels a
provider estimate in the gauge. Claude's read-only summary supplies estimated
occupancy/capacity, while model stream usage supplies per-request counters.
Thread-lifetime cumulative consumption and separate reasoning tokens remain
null when their semantics cannot be established. Native per-process and
per-turn counters are not labeled as a thread-lifetime total.

`Result.settings` optionally carries the provider-confirmed model settings;
`ModelOption.resolved_model` resolves advertised aliases. The original
immutable control is retained separately from readback. A model with no effort
choices uses an empty effort string for control and null in observed
configuration.

## Local configuration redaction

Debug data can include `redacted: true`. Claude initialization/settings/MCP
status responses have server-bound diagnostic copies minimized to reviewed
fields so local environment variables, source settings and transport headers
are not forwarded. The raw field is therefore a redacted copy for these
responses; the original provider bytes remain in the private local pipe. Every
input still has one debug frame and unchanged capture identity. Ordinary
unknown conversation frames remain exact captures. This is not general
message/stderr secret detection.
