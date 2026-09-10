# Agent notifications

The sidebar bell collects unread task and agent updates for the signed-in user.
See [Task following and notifications](task-notifications.md) for task behavior. My Agents and
individual threads also show an unread dot. Notifications cover:

- Approval requests and questions, in the main conversation or a subagent lane.
- A completed or failed main turn.
- An unexpected provider process exit or an uncertain process state.

Normal tool activity, thinking, hooks, background command completions and subagent
turn completions do not notify. Deliberate Stop and Kill do not notify. A lost
harness connection alone is not evidence that a provider exited.

Click an update to open its thread and, when applicable, its subagent lane.
Opening a conversation marks that lane's loaded updates read while the page is
visible and focused. Other lanes retain their unread state. Answering an approval
or question resolves its attention notification, including when another browser
answers it. A new run clears obsolete permission requests from the previous run.

The popup shows up to 200 recent unread records; counts include all unread
records. “Mark all read” acknowledges the displayed snapshot. When the inbox is
larger, the action says “Mark shown read”; subsequent refreshes expose older
unread records. Read state persists across devices. Deleting a thread removes its
notifications along with its Acta history.

## Browser alerts

“Enable push notifications” in the bell popup requests the browser's permission.
This preference is local to this browser and Acta user. Alerts are suppressed
while the same conversation is visible and focused. Existing unread records
appear in the inbox on startup but are not replayed as desktop alerts.

Alerts use Web Push and a service worker, so an Acta tab does not need to stay
open. Delivery still depends on the browser, operating system, connectivity and
notification permission; the server inbox remains authoritative. Fully quitting
a browser or disabling its background operation can delay delivery.

A subscription belongs to this browser and its signed-in session. Logging out or
revoking that session removes it on the server. Browser logout also unsubscribes
locally and closes Acta notifications. Returning to Acta renews an enabled
subscription. Disabling alerts leaves the inbox and unread dots enabled.

Push carries only encrypted notification identity and revision. The worker loads
current details using the browser session, discarding read, resolved or revoked
records. If Acta is temporarily unreachable it displays a generic update without
thread names or content. Receipts survive worker restarts and duplicate pushes
reuse a notification tag. Browsers may impose a generic notification when a push
is suppressed; Web Push is not a silent background synchronization mechanism.

See [Installing Acta and push delivery](pwa.md) for installation and deployment.

## Storage and API contract

Notification intents are derived from normalized provider-independent frames and
written in the same transaction as frame capture and conversation projection.
Provider lifecycle notifications are written alongside discovery updates. Debug
frames and projection rebuilds do not create notifications. Installation does not
backfill historical notifications.

Logical uniqueness is `(thread, run, lane, event key)`, not frame delivery sequence.
Replayed events cannot recreate read notifications. A completed-to-failed
correction updates that record with a new revision. Resolution and accepted
approval/answer controls clear obsolete attention under the thread row lock.

`GET /api/notifications` returns the current user's unread items, counts
by task or thread UUID and total. `POST /api/notifications/read` accepts
`{"items":[{"id":"UUID","revision":1}]}` (1–200 entries). Acknowledgements
match both identity and revision and are owner-scoped: a stale browser cannot
acknowledge a newer failure or a notification that arrived concurrently. Both
endpoints require a human browser session, following the other My Agents APIs.

Tests: `TestThreadNotifications*` in `internal/integration` covers replay,
transaction rollback, owner isolation, accepted approvals, lane boundaries,
revision-bound reads, exit/kill transitions, deletion and HTTP validation.
`web/tests/notifications.browser.mjs` covers the real popup, opt-in, lane
navigation and acknowledgement failures using mocked subscription APIs, without
sending provider turns. `web/tests/pwa.browser.mjs` exercises the real worker.
