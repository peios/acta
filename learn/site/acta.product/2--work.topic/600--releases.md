---
title: "Planning Releases"
type: how-to
description: >-
  Create a release, target tasks, and interpret release state and progress.
---

A release gives a planned delivery a name, notes, and a view of its work. It
can include tasks from both boards and at every subtask depth.

## Before you start

Creating or changing a release requires **Manage releases**. Setting a task's
target release requires **Edit tasks**. Choose release names and state changes
according to the project's agreed release process.

## Plan the delivery

1. Open **Releases** in the workspace and select **New release**.
2. Enter a name, optional codename, and starting state.
3. Add notes describing the delivery.
4. Set **Release** on each task that belongs to it.
5. Open the release to inspect its tasks and progress.

Subtasks do not inherit their parent's release. Assign each target
deliberately. A task can target one release in its own workspace.

## Interpret the state

The states communicate how work is handled:

| State | Meaning |
| --- | --- |
| Planned | The delivery is recorded, but work toward it has not started. |
| Open | The release is taking work. |
| Frozen | Existing work finishes; new work does not start. |
| Released | The delivery has shipped. |

These are coordination signals. Acta permits task changes even for frozen or
released releases, so follow the project's policy rather than relying on a
lock. Managers can reverse a state change. Releases cannot be deleted.

## Check the result

The release page lists all directly targeted active tasks. Progress counts a
task as finished only when it holds its own board's completed status. Archived
tasks are excluded. Check both the count and the work itself before declaring
a release shipped; a completed task count does not publish software.
