---
title: "Context Compaction"
description: >-
  Compaction lifecycle, correlated summaries, optional usage, and
  conversation history.
---

`context/compaction` is a full snapshot keyed by run and `compaction_id`, with
`turn_id`, `state` (`in_progress` or `completed`), nullable `started_at`,
`completed_at`, `duration_ms`, `before_tokens`, `after_tokens`, and `summary`.
The running snapshot feeds the composer status. Completion places one notice
at the completion frame; a later summary updates that stored notice without
moving it. Missing counts, timing, and summaries remain null. Turn termination
clears an unfinished status; connection loss suppresses it in the UI.

Claude `system/status: compacting` starts the snapshot; `compact_boundary`
closes it with provider token counts and duration. Its explicit
preserved-message anchor UUID correlates a subsequent synthetic summary.
Unrelated synthetic user messages remain Unknown. Codex `contextCompaction`
items supply start/end identity only in the installed protocol: elapsed
receipt time is used when both ends were observed, and no token counts or
summary are invented. Raw captures remain in debug frames.

For submitted Claude user-message echoes, the complete recognized
command-name/command-message/command-args wrapper is normalized to plain slash
command text. Mismatched or incomplete wrappers are preserved literally.

A successful Claude `result` with task-notification origin, zero model turns,
zero output tokens and empty text is a dropped debug receipt. It does not
create or complete a conversation turn. Errors and nonempty results retain
their normal handling.

Resumed background notices retain the original known task/tool identity and
label in the local adapter checkpoint, without restoring that tool into the
new run's live tool map. They emit only `tool/notification`, scoped to the
receiving run; the old card and old notice stay intact. Unknown task/tool
pairs remain Unknown.

Tool results can carry `images`, an array of `{media_type, base64}` raster
images (or null). Claude base64 PNG/JPEG/GIF/WebP tool-result blocks populate
this field; text in the same result remains in `output`. Image-only results
complete the tool normally. Unsupported content or invalid base64 remains
Unknown atomically. Native duplicate image metadata is retained only in raw
debug data. Expanded tool cards show thumbnails, with a keyboard-accessible
enlarged preview closed by Escape or its close button. This does not fetch
files from development machines.

Whole-PDF results use `attachments`, an array of `{name, media_type, base64}`
(or null). Only `application/pdf` is mapped. The filename comes from the read
operation's basename, defaulting to document.pdf; source bytes and MIME are
provided by the result. The card retains neighboring text and shows compact
Open/Download actions with the decoded byte count. Open uses the browser's PDF
handler in a separate tab; there is no embedded Acta PDF reader. Browser-local
object URLs are revoked when the snapshot changes or the card unmounts. No
local provider file is fetched by the server.
