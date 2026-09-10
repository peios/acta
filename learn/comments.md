# Task comments and replies

Comments share the task's Activity tab with task changes. Post explicitly with
**Post** or Ctrl/Cmd+Enter. The composer sits at the newest end: below the feed
when oldest first, above it when newest first. Switching order preserves the
composer draft and keeps keyboard traversal in the same order as the page.
Click outside a comment or reply composer to collapse its editing controls into
a quiet text preview. Click the preview to resume editing the same draft.
Post/Reply sits at the bottom right of the expanded composer, sized to its label
with the standard 46px button height.

Unsent posts are kept in this browser, separately for each signed-in account,
task and reply target. A lost response retains the original request until retry
resolves it; it cannot silently create a second comment. Drafts are not synced
between devices. Comment edits use explicit Save and retain unsaved text in the
open editor on failure; unlike new-post drafts, edits are not persisted on reload.

## Conversations

Every top-level comment can have one reply thread. Replying to a reply stays in
that thread and links to the particular comment being answered. Threads always
read oldest first. A reply-count toggle expands them; Reply or navigation to an
unread reply expands the relevant thread automatically. Replies are paginated
in batches of 50, with older replies available on demand.

Edits retain the author's identity, original position and creation time, and show
an Edited marker. Conflicting edits show the latest content alongside your text;
reconcile before saving against the newer version. Deletion removes the text and
leaves a placeholder; replies and their targets remain valid. Deleted comments
cannot be restored through Acta. Full comment revisions are not retained.

## Permissions and read state

Workspace **Comment** (`tasks.comment`) allows posting/replying and editing or
deleting your own comments. **Manage others’ comments** (`tasks.comments.manage`)
allows editing or deleting another account's comments. It does not independently
grant posting or editing your own comments. Actions are hidden when unavailable,
and every mutation rechecks current authority in its transaction. Agent accounts
use their existing workspace inheritance/ceiling; ownership does not make a
human and their agents the same comment author.

New workspaces grant their creator all workspace permissions. Existing explicit
grants are not widened by this addition; superusers and agents inheriting from
them receive the new capabilities through the usual permission resolver.

Each comment and reply has its own read position. Merely viewing a parent comment
never acknowledges a collapsed thread. Visible focused activity is acknowledged;
CLI and MCP reads remain inert. Someone else's edit makes that comment unread
again, including when a moderator edits your own comment. New replies and edits
keep the parent in place. The feed carries the latest event's root identity so
new-activity navigation can find an updated thread beyond the first loaded page.

## API and agent workflow

All endpoints are relative to `/api/tasks/{task}`, where task is a UUID or reference:

- `POST /comments`: `{body, request_id, reply_to?}`. `request_id` is a caller-created
  UUID reused only for retries of this exact logical post. `reply_to` is a comment
  UUID from the same task; the service derives the root thread.
- `GET /comments/{id}`: current comment, author, version and action capabilities.
- `POST /comments/{id}`: `{body, version}` to edit or `{delete:true, version}` to
  delete. Stale mutations return the shared 409 conflict envelope with current
  version/content. Repeating an already-applied value or deletion is a no-op.
- `GET /comments/{root}/replies?cursor=...`: newest-first API pagination; render
  oldest first when presenting a conversation.
- `GET /activity`: top-level comments mixed with changes, plus reply counts,
  thread unread state and latest-event metadata. Replies are fetched separately.

MCP exposes `comment_create`, `comment_get`, `comment_replies`, `comment_update`
and `comment_delete`, using the existing Tasks read/write OAuth grants. The tools
use the same typed success/problem envelopes and authorization as the web app.
Call `task_activity`, then `comment_replies` when a thread matters. Discover UUIDs;
do not infer authors or targets from display names. After an uncertain post,
retry the same `request_id` and inputs. On conflict, read/reconcile before editing.

CLI examples:

```sh
acta2 task comment add ACT-151 --body 'Review complete.'
acta2 task comment add ACT-151 --reply-to <comment-uuid> --body-file notes.md
acta2 task comment get ACT-151 <comment-uuid>
acta2 task comment replies ACT-151 <root-comment-uuid>
acta2 task comment edit ACT-151 <comment-uuid> --version 1 --body 'Updated review.'
acta2 task comment delete ACT-151 <comment-uuid> --version 2
```

`--body-file -` reads stdin. `--json` uses the normal structured CLI output. The
CLI prints a generated post request UUID on stderr before submission; retry with
`--request-id <that-uuid>` and the same inputs if the outcome was uncertain.

## Persistence and validation

`task_comments` stores current Markdown, author, version, reply target and the
original request's hash, unique per author/request UUID. Activity entries carry
root-thread relationships, while immutable activity events record the actor and
kind of each mutation without retaining comment bodies. The database transaction
commits content, activity and workspace refresh revision together. Write
serialization also protects duplicate-post and expected-version decisions.

Grouping follows the latest raw event, so posting or editing a comment—even an
older one—breaks adjacent task-edit grouping. Comment edits update their entry's
latest event while retaining its first event. Unread attribution uses the latest
event actor, not the original author. Read acknowledgements stay per entry.

Integration coverage includes retry deduplication, concurrent posts/edits,
permissions and revocation, cross-task/outsider denial, rollback on event failure,
reply pagination, deletion with surviving replies, grouping barriers, moderator
edit unread state, sparse acknowledgements and real CLI/MCP SDK round trips.
