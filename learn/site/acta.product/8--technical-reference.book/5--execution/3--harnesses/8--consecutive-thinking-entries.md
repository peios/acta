---
title: "Consecutive Thinking Entries"
description: >-
  Grouping and timing of adjacent thinking records without losing their
  individual contents.
---

Adjacent visible thinking entries in the same thread, provider run, turn and
subagent lane render as one expandable item. Their text is joined in order
with paragraph breaks. The activity indicator stays active while any
constituent is still thinking. Thinking text opens automatically and stays
expanded after the thinking block completes, until its owning turn ends
(including interruption or failure). At turn end it collapses to one line and
can be manually reopened. Its elapsed time spans the known start/end times;
its token estimate is summed only when every constituent supplies an estimate.
Messages, tool calls, Unknown Frames, and other visible entries split groups.
Hidden debug/state frames do not split them; enabling debug reveals those
boundaries.

This is presentation grouping over the independently persisted thinking
snapshots. It works with existing history and incremental updates without
rewriting captures. Loading an older page can extend a group; constituent IDs
remain scroll anchors.

Claude foreground Bash tasks can also emit `task_started` with
`is_backgrounded: false`, followed by `task_notification`. The adapter stores
that task-to-tool identity in its checkpoint and classifies both as local
debug bookkeeping. The ordinary tool result supplies completion, stdout and
stderr; these notices do not add a background indicator or duplicate
completion notice. If the same task later backgrounds, it switches to the
normal background lifecycle. Unrecognized identities or task shapes remain
Unknown Frames.
