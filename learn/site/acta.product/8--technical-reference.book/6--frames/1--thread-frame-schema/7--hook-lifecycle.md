---
title: "Hook Lifecycle"
description: >-
  Correlated provider hook starts and completions, outcomes, timestamps, and
  untrusted output.
---

Both adapters emit `hook/started` and `hook/completed`, correlated by
`hook_id` within the thread's provider run. Both carry the hook name,
triggering event, and an optional turn ID. Completion adds an outcome,
optional exit code/duration, status message, separate output/stdout/stderr
fields, and labeled output entries. Claude streams remain separate and
unchanged; Codex entries retain their labels. Only an explicit `context` entry
is described as added to context. Hook output is untrusted display text and
cannot execute HTML or instructions in Acta.

The feed removes the matching running row when completion arrives and inserts
an expandable result at the completion frame's position. Completion does not
move back to the start. A completion without a captured start still renders;
repeated observations do not duplicate rows, and IDs from separate runs do not
collide. JSON output is pretty-printed for reading; other output preserves
whitespace. Identical combined output and stdout are shown once as Response ·
stdout; both original fields remain in the frame. The response's position
records when the provider reported it, not proof that all its output was
inserted into model context. Hook progress events remain Unknown Frames
pending their own design. Historical debug classifications are not rewritten
by this adapter change.

Explicit hook context is rendered with the shared Markdown viewer: Claude
`hookSpecificOutput.additionalContext` and Codex `context` entries. Claude's
full structured response remains expandable under Raw hook response. Other
output remains plain text or formatted JSON; stored frames are unchanged.
