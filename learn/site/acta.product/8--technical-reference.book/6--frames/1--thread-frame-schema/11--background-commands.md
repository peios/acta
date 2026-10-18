---
title: "Background Commands"
description: >-
  Long-running tool identity, completion notices, context echoes, and
  resumed-session attribution.
---

A tool snapshot can have status `background` and a `background_task_id`. The
task identity survives the launching turn. Turn completion or interruption
does not imply that a background process exited. The original tool item
changes to Completed, Failed, or Interrupted only on a correlated terminal
task update.

Claude local_bash task lifecycle records and Bash backgroundTaskId launch
receipts establish this association. Inventory without a tool identity is
consumed locally; unreviewed task types and uncorrelated notifications remain
Unknown Frames. A correlated replayed task-notification user envelope produces
one `tool/notification` with task_id, tool_id, originating turn_id, label,
terminal status, summary, and context_entry. It first appears on system
notification delivery; a subsequent context echo updates and relocates the
same stored item. An idle provider can deliver system/task_notification
directly and start an autonomous reply without a user echo; that path emits
the notice on delivery and assigns the new response a distinct turn identity
from its first provider message. It is not a human message. context_entry is
false on initial delivery and true on a later context echo. Duplicate
notifications do not create duplicate notices or move an already echoed
notice.

For Codex, a commandExecution still in progress with a processId when its turn
ends becomes a background tool snapshot before the turn/completed frame. Its
run-scoped identity is `command/<tool_id>`. Subsequent output deltas continue
to update the original card; item/completed ends it and emits one
tool/notification with context_entry false. Checkpoints preserve process
identity and notice delivery state. A missing processId is insufficient
evidence for this mapping.

Current conversation state stores active background tasks independently of
history pagination. The composer footer shows the live count alongside
Working, or by itself after the turn ends. Unavailable/replaced provider runs
do not claim tasks are still running. The original card remains at its first
position and completion notices have a compact expandable summary.

Codex MCP calls with text or structured JSON results also use the generic tool
card, named server/tool. Text blocks retain their order. Structured data is
shown as formatted JSON when it adds information; a JSON text block that
already contains the same value is not repeated. Provider errors produce
Failed with the reported error text. Rich content blocks (images/resources/and
so on) remain Unknown Frames until their presentation is designed.
