# Installing Acta and push delivery

Acta provides a web app manifest, application icons and a root-scoped service
worker. Install it from the browser's app menu, or use **Install Acta** in the
notification popup when the browser offers that action. On iPhone/iPad, use
**Share → Add to Home Screen**, open that installed app and enable browser alerts
from its bell menu. Installation does not automatically grant notification
permission. Desktop browser support and installation menus differ.

The app remains server-backed: account pages, API data, threads and tool results
are not cached for offline browsing. Offline navigation shows a simple reconnect
screen. This avoids retaining authenticated content in a shared browser cache.
Agent execution remains independent of whether the app is open.

## Deployment

Use the HTTPS origin in `ACTA_PUBLIC_URL` for real devices. Loopback HTTP works
for local development; a phone's localhost refers to the phone, not the Acta
server. This feature does not configure a reverse proxy or publish the server.

The server derives a separate VAPID signing key (the server identity used by Web
Push) from the existing protected installation key. Keep `ACTA_SECURITY_KEY_FILE`
persistent and consistent across all instances. No additional push API key or
Firebase project is required. Production VAPID contact uses the public HTTPS
origin. Local development supplies its HTTPS-form origin for the signing claim;
Apple's push service does not accept a localhost contact.

Allow outbound DNS and HTTPS to browser-selected public push services. Acta
rejects private/reserved addresses, checks DNS at connection time, pins resolved
addresses for the connection and does not follow redirects or environment HTTP
proxies. The push service receives an encrypted identifier payload; it does not
receive plaintext conversation content. The device retrieves current notification
details from Acta using its existing browser session.

## Delivery guarantees

Subscription registration is user- and session-bound, with at most 16 browser
subscriptions per user. Session deletion cascades to subscriptions and queued
work. Disabled accounts and expired/idle sessions cannot dispatch.

New or corrected notification records enqueue a delivery per subscription in
the same database transaction. Existing inbox records are not backfilled when
alerts are enabled. A three-second grace period lets focused pages acknowledge
updates before dispatch. Read/resolved records and superseded revisions are
canceled before delivery. These checks also run on the device before rendering.

Workers claim a one-minute lease with `SKIP LOCKED`. Retries use exponential
backoff capped at one hour; queued work expires after 24 hours. A process crash
leaves the lease reclaimable. Stale workers cannot acknowledge a newer lease.
Push-service acceptance ends the server attempt; it is not proof that the user
saw an alert. Push-service delivery has a one-hour TTL. Endpoint 404/410 removes
the subscription. Other permanent 4xx responses are not retried; 408, 429,
network failures and 5xx responses are retried. Failed attempts log only delivery
identity/status, not secret endpoint URLs or response bodies.

The worker persists a bounded receipt history and uses stable notification tags
for replay suppression. Browser storage loss or operating-system behavior may
still affect delivery. Logout cancels alerts even when the detail request is
already in flight. The durable inbox is always the source of unread state.

## API and validation

Human browser sessions use `GET /api/push` for the public signing key,
`POST /api/push/subscribe` with the browser's endpoint and keys, and
`POST /api/push/unsubscribe` with that endpoint. The worker uses
`GET /api/push/notification?subscription=UUID&id=UUID&revision=N`; it only returns
an unread, unresolved, matching revision belonging to that session/user.
Subscription endpoints and keys must be treated as secrets in logs and exports.

`internal/push` tests decrypt a generated RFC 8291 payload and check outbound
address restrictions. `TestPushQueueLifecycle` exercises real PostgreSQL leases,
retry, replay, owner isolation, read cancellation, endpoint expiry and logout.
`web/tests/pwa.browser.mjs` uses a temporary regular Chromium profile and the
actual service worker: install, closed-tab push, restart/deduplication, revoked
sessions, generic offline fallback, focused-thread suppression, logout races and
offline navigation. It requires full Chromium, not the stripped headless shell;
set `ACTA_CHROMIUM_EXECUTABLE` if the full binary is outside Playwright's install.

The automated browser test injects a push event through Chromium's testing
protocol. Real push-service transport, operating-system tray appearance and
Apple Home Screen delivery still require device testing with permission granted.
