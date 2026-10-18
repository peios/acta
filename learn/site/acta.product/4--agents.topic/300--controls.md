---
title: "Controlling a Conversation"
type: how-to
description: >-
  Send messages and images, handle approvals, and distinguish stopping,
  killing, and deleting.
---

Conversation controls affect different parts of provider execution. Check the
selected thread and lane before sending input or interrupting work.

## Send a message

1. Confirm the connected provider, working directory, and permission mode.
2. Enter the message. Optionally attach up to four PNG, JPEG, GIF, or WebP
  images, totaling at most 4 MiB.
3. Select **Send** and check the delivery state.

An uncertain delivery retains its command identity for retry. It is not proof
that the provider never received the input. A rejected image remains in the
draft so you can remove it or choose an appropriate model; Acta does not
change models automatically.

On touch devices, Enter inserts a line break. Use **Send** or
<kbd>Ctrl</kbd>+<kbd>Enter</kbd> (<kbd>Cmd</kbd>+<kbd>Enter</kbd> on macOS).
Desktop Enter sends, while <kbd>Shift</kbd>+<kbd>Enter</kbd> inserts a line
break.

## Answer an approval

1. Open the pending request and inspect its action, paths, and explanation.
2. Select **Approve** or **Deny** for that request.
3. Check the provider's reported resolution.

Changing the permission mode does not answer a pending request or undo a
running action. **Approve for me** delegates review to the provider's
automatic reviewer, which can reject actions. **Unrestricted** requests the
provider's broad preset; managed restrictions can still apply. The presets do
not promise identical sandbox behavior across providers.

## Interrupt or close work

Choose the action with the intended effect:

| Action | Effect |
| --- | --- |
| Stop turn | Requests interruption of the selected conversation's active turn. |
| Kill | Stops the dedicated provider process. |
| Resume | Starts a process for the same stored native session. |
| Delete thread | Removes the owner's stored Acta transcript and commands. |

Deleting a thread does not kill the provider or delete its local session and
files. Acta cannot restore a deleted transcript. Kill the provider separately
if stopping local execution is part of the intended action.

If resumption fails because native history is unavailable, Acta reports that
failure and retains its stored history. It does not silently replace the
native session with a different conversation.
