---
title: "Task Notifications"
description: >-
  Following preferences, notification reasons, access checks, and revision-
  bound read acknowledgments.
---

The small bell beside a task's menu follows or unfollows that task. A filled
bell means following. Anyone who can read the task can follow it; editing
rights are not required. The preference belongs to the signed-in human and
persists across devices. Agents cannot change it directly.

Creating a task or receiving a direct assignment automatically follows it for
the human account, including when the creator or assignee is one of their
agents. Human and agent assignments to the same owner produce one follow.
Removing an assignment does not remove following. Explicit unfollowing is
retained and is never overwritten by later assignments. Existing creators and
assignees are seeded on installation without sending historical notifications.

The shared sidebar inbox includes changes, comments and document activity on
followed tasks. Direct assignments, new resolved @mentions, and replies to the
recipient's comments notify that person even without following. Multiple
reasons produce one notice per activity item. A human's actions do not notify
that same human; their agents' actions can, with the agent identified as the
actor. References inside code do not create mentions. Editing unrelated text
does not re-notify a non-follower about an existing mention.

Selecting a notification opens its task and acknowledges that notice. Viewing
activity acknowledges only the revisions actually seen. Rapid changes share
the activity feed's existing grouping window, updating one inbox record. A new
change after reading makes that record unread again. Deleting a comment
resolves its notice; deleting a task removes its notices and following
preferences.

Task alerts use the existing opt-in Web Push subscription. The worker
suppresses an alert when that task is already visible and focused. Reading the
inbox or fetching a push notice rechecks current workspace access, so revoked
members cannot receive task details. See [Push
delivery](~acta/technical-reference/application/pwa) for deployment setup.

## API and storage

- `GET /api/tasks/{task}/following` returns `{"following":true}` or false.
- `POST /api/tasks/{task}/following` accepts the same explicit boolean. Both
  endpoints require an authorized human session and normal workspace access.
- `GET /api/notifications` is the unread task inbox. Task records
  include `task_id`, `workspace_slug`, `task_reference`, `task_title`, and
  `activity_id`. Counts are keyed by the task UUID.
- `POST /api/notifications/read` acknowledges exact notification
  IDs/revisions. The request contains `items`, each with an `id` UUID and a
  positive `revision`. It accepts between 1 and 200 items and requires a human
  session. A newer revision remains unread.

`task_followers` retains explicit true/false preferences per task and human.
`notifications` holds task records, each with a required task UUID.
Task mutations, activity, following, and notification/push intents commit
in one transaction. Task notice uniqueness is `(owner, task, activity entry)`.
Replayed comment requests and rolled-back mutations cannot create extra
notifications. Revision-bound reads also advance the corresponding activity
read position; stale reads never acknowledge newer grouped changes.
