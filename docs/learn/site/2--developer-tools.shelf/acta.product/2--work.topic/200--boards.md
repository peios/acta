---
title: "Boards and Views"
type: concept
description: >-
  How Tasks, Backlog, saved views, grouping, and task hierarchy affect what
  you see.
---

Each workspace has **Tasks** and **Backlog**, with separate status workflows.
A board describes where work belongs in the workflow. A saved view describes
how you want to see that board.

## Boards and hierarchy

A task's status determines its board. Moving it to the other board changes its
status and preserves its identity, history, assignments, and parent links. Its
children keep their own statuses. A child on another board appears as a root
on that board and remains visible under its parent.

The Tasks workflow has a configured completed status. Backlog starts with one
status and can be configured with a completed status. Completion means the
task has its own board's configured completed status; a name such as "Done"
alone does not make a status complete.

## Personal views

A view stores your filters and display choices for one workspace and board.
Views belong to your account. Search text is temporary and is not saved with
them.

Changing filters or display choices creates a draft. Save it with the save
icon, or use **Undo** to return to the saved view. The plus action creates a
separate view from the draft when one exists. Switching views retains their
drafts during the visit; reloading restores saved settings.

Table column widths and order belong to the browser. They are not part of the
server-stored view and can differ on another device.

## Filtering and grouping

Selections match any value within one filter category, and all selected
categories together. Ordinary filtered lists show matching roots; they do not
promote every matching descendant. Use [task search](~acta/work/searching) to
find work at any depth.

Grouping by assignee combines a person's assignments with their agents.
Grouping by agents shows your own agent identities and your direct
assignments. A task assigned to several people can appear in several groups.

Board dragging changes the property represented by the destination column.
Assignment moves preserve unrelated assignments. The task's pickers provide
the same changes without dragging. Each change still checks its saved
revision.
