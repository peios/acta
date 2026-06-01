# Acta

A deliberate, slow rebuild of Acta — built in small horizontal slices, each
one feature-complete rather than a stub.

Stack: Go + htmx + Postgres. Designed to run on both Debian and Peios, so
authentication is pluggable (see `internal/authn`): the `local` provider owns
its own user accounts and verifies passwords; a future Peios provider would
defer identity to the kernel without touching the session or web layers.

## Status

**Slice 1 — login / logout.** Multi-user accounts, argon2id passwords,
server-side sessions (real logout), CSRF protection, secure cookies,
return-to redirects.

**Slice 2 — passkeys.** WebAuthn (usernameless / discoverable login),
registration from Settings → Security, and a post-login interstitial that
offers to add one. Passkeys are a second method of the same pluggable `local`
provider.

**Slice 3 — workspaces.** Top-level work containers, URL-scoped at `/w/{slug}`
(immutable slug, so a rename never breaks a link); `/` redirects to your last
workspace. A top-bar switcher and a Settings → Workspaces page to create,
rename, and delete them. Shared/global for now — every signed-in user sees all
workspaces; membership is a later slice.

**Slice 4 — board.** Each workspace's `/w/{slug}` is a board of user-defined
**statuses** (lanes) holding **items** (title + status). Drag items to reorder
within a lane or transition between lanes, and drag lanes to reorder; create /
rename / delete both; a lane can't be deleted while it holds items. New
workspaces seed *To do / Doing / Done*. The page is fully server-rendered and
works without JavaScript; `board.js` layers on drag-and-drop and inline editing
through a JSON API (the same surface automation will use). Drag-and-drop uses a
vendored, self-hosted copy of [SortableJS](https://sortablejs.github.io/Sortable/)
(`internal/web/static/sortable.min.js`, MIT) — no CDN, so it stays CSP-clean and
runs offline.

**Slice 5 — deeper items.** Items gain a description, an optional assignee (any
user), comments, and archiving. They open in a **modal** via `?item=<id>` on the
board — deep-linkable and refresh-safe because the server renders it when the
param is present; `board.js` opens it without a reload (card click) and closes
on Esc / back / backdrop. Archiving (the card ×, or the modal) soft-deletes —
items drop off the board but are restorable from the **archive view**
(`/w/{slug}/archive`), where they can also be permanently deleted. Every edit
flows through the JSON API the MCP will share.

**Slice 6 — subtasks.** An item can nest under a parent (`parent_id`), to any
depth — a subtask is a *full item* (own status, assignee, description,
comments) that lives in its parent's modal rather than on the board. The board
shows only top-level items, with a `done/total` badge on parents (done = the
last lane). Modals carry a Subtasks section (add / open / drag-reorder) and a
link back to the parent. Archiving a parent archives its whole subtree and
restoring brings it back; the archive view lists subtree roots, and a permanent
delete cascades via the FK.

## Running

### Live-reload dev (recommended)

```sh
docker compose up        # app + Postgres; just leave it running
```

The dev override mounts the source and runs the app under
[air](https://github.com/air-verse/air), so any edit to Go, templates, or
static assets rebuilds and restarts in ~1s — keep it up and refresh the
browser. Open http://localhost:8080 and sign in with the bootstrap admin
(`admin` / `admin` by default; set `ACTA_BOOTSTRAP_*` in `docker-compose.yml`).
Migrations run on boot.

### Plain build (no live reload)

```sh
docker compose -f docker-compose.yml up --build
```

### Host binary (Postgres in Docker)

```sh
make db-up
ACTA_SEED_PASSWORD=secret go run ./cmd/acta createuser -username jack -display "Jack"
make run
```

Visiting http://localhost:8080 logged-out bounces you to the login page.

## Configuration

| Env var                 | Default                          | Meaning                        |
| ----------------------- | -------------------------------- | ------------------------------ |
| `ACTA_HTTP_ADDR`        | `:8080`                          | listen address                 |
| `ACTA_DATABASE_URL`     | `postgres://acta:acta@localhost:5432/acta?sslmode=disable` | Postgres DSN |
| `ACTA_ENV`              | `dev`                            | `prod` enables Secure cookies  |
| `ACTA_SESSION_IDLE`     | `24h`                            | idle session timeout           |
| `ACTA_SESSION_ABSOLUTE` | `720h`                           | absolute session lifetime      |
| `ACTA_RP_ID`            | `localhost`                      | WebAuthn relying-party id (domain, no scheme/port) |
| `ACTA_RP_ORIGIN`        | `http://localhost:8080`          | WebAuthn origin the browser sends |
| `ACTA_RP_NAME`          | `Acta`                           | WebAuthn relying-party display name |
