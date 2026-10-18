# Old Acta feature comparison before cutover

Date: 2026-09-10. Tracking: ACT-104.

This is a read-only source comparison of `acta/` and `acta/`, not a runtime certification or an audit of which features contain production data. Projects and releases are explicitly deferred by the user. Their data must remain recoverable for later migration.

## Substantive gaps

| Capability | Old Acta | Acta | Cutover implication |
| --- | --- | --- | --- |
| Status checklists | Workspace facts, recorded confirmations, required facts for status entry, pending transitions and confirm/cancel/force operations. | Configurable statuses, but no equivalent facts or transition gates. Markdown checkboxes do not enforce a transition. | A real workflow gap if checklists encode completion policy. Preserve both requirements and historical confirmations during migration. |
| Due dates | Date-only task targets, overdue calculation and due-date grouping/filtering. | No due-date task field or filter. | Useful core metadata parity; populated dates must not silently disappear. No claim that old Acta had deadline reminders or recurring tasks. |
| Separate boards | Distinct Tasks and Backlog boards, each with its own statuses. | One workspace status set; table/board are display modes and personal presets are filtered presentations. | Agree an explicit mapping. A Backlog status plus preset may suffice for the default setup; this does not generally reproduce multiple independent workflows. |
| Agent inbox and comment waiting | MCP notification listing/acknowledgement and a comment cursor with a bounded long wait. | Human task following/notifications and ordinary task activity/comment reads, but no equivalent agent MCP inbox or comment-wait tools. | Matters if agents rely on task notifications as a coordination loop. Normal task/comment operations already work. |
| Workspace activity | Workspace/board/item activity views and MCP activity queries. | Per-task activity, without the workspace-wide feed. | Useful visibility feature, probably deferrable; task history already has a storage foundation. |

Source pointers:

- Old facts/checklists: `acta/internal/store/store.go` (`Fact`, `FactTick`, `PendingStatusID`), `acta/internal/board/checklists.go` (`StatusFacts`, `SetStatusFacts`, `ConfirmStatus`), `acta/internal/web/mcp.go` (`set_item_status`). These establish capabilities, not a claim that every old transition is transactionally atomic.
- Old dates: `acta/internal/store/store.go` (`Item.DueDate`), `acta/internal/board/attributes.go` (`ParseDue`, `DueBucket`, `SetDue`), `acta/internal/web/mcp.go` (`set_item_due`).
- Old boards: `acta/internal/store/store.go` (`Board`, `Status.BoardID`), `acta/internal/board/board.go` (`SeedDefaults`, `DefaultBacklogStatuses`).
- New task fields/statuses: `acta/internal/tasks/task.go`; supported display grouping and sorting: `acta/internal/tasks/view_display.go`; filters: `acta/internal/tasks/views.go`.
- Old agent coordination: `acta/internal/web/mcp.go` (`watch_comments`, `list_notifications`, `mark_notification_read`); subscriptions: `acta/internal/board/subscriptions.go`. New reads: `acta/internal/httpapi/task_tools.go`, `acta/internal/httpapi/comment_tools.go`; human-follow semantics: `acta/learn/task-notifications.md`, `acta/internal/auth/task_following.go`.
- Old activity: `acta/internal/board/board.go` (`WorkspaceActivity`, `BoardActivity`), `acta/internal/web/server.go`, `acta/internal/web/mcp.go` (`list_activity`). New per-task activity: `acta/internal/postgres/activity.go`.

## Genuine differences that can reasonably wait

- **Human inline Markdown document authoring.** Old task dialogs contain title/body editors. New documents support uploads, replacements, previews, downloads and versions, and MCP text saving, but the human UI lacks an equivalent body editor. Document storage itself is covered. Sources: old `internal/web/templates/item_modal.html`; new `web/src/lib/components/tasks/TaskDocuments.svelte` and `learn/task-documents.md`.
- **Manual task/subtask ordering.** Old items have positions and reorder operations. New lists use property-based sorting. Table column ordering is a different feature. Sources: old `internal/store/store.go`, `internal/board/board.go` (`MoveItem`, `ReorderSubtasks`); new task and display models.
- **Shared saved views.** Old saved views belong to boards; new view collections belong to an account and workspace. Sources: old `BoardView` model; new `internal/postgres/migrations/013_task_views.sql`.
- **Permanent task deletion.** Old archived tasks can be deleted; new tasks have archive/restore. This need not block ordinary use. Sources: old `internal/web/templates/archive.html`; new `learn/tasks.md`.
- **Milestone designation/grouping, status colours, and richer subscription choices.** These are additive conveniences rather than prerequisites for the core task model. Preserve associated old metadata in the retained export.
- **Combined claim helper.** Old `claim_item` bundles assignment, an optional status change/checklist and a comment. New versioned task updates plus comment creation can express the work. The old helper is not evidence of an atomic claim lock.

## Covered baseline

Both codebases provide task creation/editing, nesting, assignments, readable references, configurable statuses, priority/type/size, archive/restore, task comments/history, search, task views, memories, documents, CLI/MCP access and account management. Acta additionally has its own permission model, threaded replies, multiple assignments, document versions and human push notifications. This statement concerns implemented capabilities, not a fresh end-to-end test of each.

## Migration requirements separate from feature parity

1. **Preserve old links.** Old routes use `/{workspace}?item=<old-id>` (and legacy `/w/...` forms); new routes use `/workspaces/{workspace}?task=<uuid>`. Preserving `PEI-123` alone does not preserve these URLs. Keep an old-ID/new-ID mapping, rewrite imported internal links where appropriate, and decide how external bookmarks and published links will redirect. Migration Assistant currently generates new UUIDs; it is not an old-link alias service.
2. **Map board membership deliberately.** Include every board in the inventory. Repeated status names across boards must not accidentally merge distinct workflow meanings.
3. **Retain data for deferred features.** Keep a complete old export/backup, including projects, releases, milestone metadata, due dates, checklist configuration and confirmations. A feature being deferred is not permission to discard its data.
4. **Check actual production usage before declaring a gap a blocker.** This review did not query the production database. Count populated due dates, configured gates and non-default boards, and identify agent workflows using notification/comment-wait tools.

## Recommendation

Status checklists and due dates are the strongest candidates for core feature parity before adopting Acta as the primary tracker. Decide the board mapping and whether agent inbox/comment-wait behaviour is required for existing automation. Workspace activity and the smaller convenience gaps can follow deployment. Resolve old-link preservation as part of cutover, independently of which additional features are implemented.

No old Acta files, deployed services, release channels or production data were changed by this review.
