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

**Slice 7 — promote / demote.** A "Parent" dropdown in every item's modal
reparents it: `None` promotes it to the board (top-level), picking an item
demotes it under that item. The picker excludes the item and its own
descendants, and the server re-checks, so a reparent can never form a cycle.
The item keeps its status and lands at the end of its new container.

**Slice 8 — milestones.** Any item can be flagged a milestone (a toggle in its
modal). The board gains a **mode** toggle — **Status | Milestone** (via
`?mode=`). In Milestone mode the columns are: a **Backlog** of root
non-milestones, then one column per root milestone holding its children as
cards (a nested milestone shows as a card in its parent's column, not its own).
Dragging a card between columns **reparents** it (to that milestone, or to the
root for Backlog) — reusing slice 7 — and dragging within a milestone column
reorders its children. Cards show a status chip since the status isn't the
column anymore.

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
ACTA_SEED_PASSWORD=secret go run ./cmd/acta-server createuser -username jack -display "Jack"
make run
```

Visiting http://localhost:8080 logged-out bounces you to the login page.

## Configuration

| Env var                 | Default                          | Meaning                        |
| ----------------------- | -------------------------------- | ------------------------------ |
| `ACTA_HTTP_ADDR`        | `:8080`                          | listen address                 |
| `ACTA_DATABASE_URL`     | `postgres://acta:acta@localhost:5432/acta?sslmode=disable` | Postgres DSN |
| `ACTA_ENV`              | `dev`                            | `prod` enables Secure cookies + HSTS |
| `ACTA_SESSION_IDLE`     | `24h`                            | idle session timeout           |
| `ACTA_SESSION_ABSOLUTE` | `720h`                           | absolute session lifetime      |
| `ACTA_RP_ID`            | `localhost`                      | WebAuthn relying-party id (domain, no scheme/port) |
| `ACTA_RP_ORIGIN`        | `http://localhost:8080`          | WebAuthn origin the browser sends |
| `ACTA_RP_NAME`          | `Acta`                           | WebAuthn relying-party display name |
| `ACTA_TRUSTED_PROXIES`  | (empty)                          | reverse-proxy CIDRs trusted for `X-Forwarded-For`; empty = use the socket peer |
| `ACTA_LOGIN_WINDOW`     | `15m`                            | how long a failed login is remembered |
| `ACTA_LOGIN_IP_MAX`     | `20`                             | failed logins per IP in that window before it's blocked |
| `ACTA_LOGIN_BACKOFF_STEP` | `1s`                           | delay added per consecutive failure against a username |
| `ACTA_LOGIN_BACKOFF_MAX`  | `10s`                          | cap on that per-username backoff |

## Self-hosting

Acta is built to sit behind a TLS-terminating reverse proxy. The included
`docker-compose.prod.yml` runs the app (a prebuilt image pulled from GHCR —
the box never compiles code), Postgres, and **Caddy** (which obtains a Let's
Encrypt certificate automatically). The origin is built to be safe when reached
directly — a CDN like Cloudflare in front is a bonus layer, not a dependency.
A 1 GB host is plenty. The full tick-through is in [DEPLOY.md](DEPLOY.md).

1. **Provision a host** (any Linux box with Docker + Compose) and point a DNS
   `A`/`AAAA` record for your domain at it. Leave it unproxied (DNS-only) for
   now so Caddy can complete the ACME challenge.
2. **Configure.** Grab the deploy bundle (no source needed):
   ```sh
   curl -fsSL https://github.com/peios/acta/releases/latest/download/acta-deploy.tar.gz | tar xz --strip-components=1
   ```
   Then `cp .env.example .env` and fill it in: set `ACTA_DOMAIN`, and generate
   strong secrets — `openssl rand -base64 32` for `POSTGRES_PASSWORD`, and a real
   `ACTA_BOOTSTRAP_PASSWORD`.
3. **Launch.**
   ```sh
   docker compose -f docker-compose.prod.yml up -d
   ```
   This pulls the image and starts everything; Caddy provisions the certificate
   on first request. Visit `https://acta.example.com` and sign in as the
   bootstrap admin. Migrations run on boot.
4. **Rotate the admin password** off the bootstrap value (it prompts, so the
   secret never lands in your shell history):
   ```sh
   docker compose -f docker-compose.prod.yml exec app /acta-server setpassword -username admin
   ```
   The same command is the recovery path for a forgotten password or lockout; it
   also revokes the account's existing sessions.
5. **Firewall** the host to `80`, `443`, and SSH only — Postgres and the app
   port are never published (Caddy reaches the app over the internal network).
6. **Back up** Postgres on a schedule, and test a restore:
   ```sh
   docker compose -f docker-compose.prod.yml exec -T db pg_dump -U acta acta | gzip > acta-$(date +%F).sql.gz
   ```
7. **Monitor** by pointing an uptime check at
   `https://acta.example.com/healthz` (liveness); `/readyz` additionally
   verifies the database.

### Putting Cloudflare in front

Once the certificate is issued you can enable Cloudflare's proxy (orange cloud).
Set the SSL/TLS mode to **Full (strict)** so it validates the real origin
certificate. Two changes keep per-client features (like the login throttle)
seeing real client IPs: remove the `header_up -CF-Connecting-IP` line from the
`Caddyfile`, and add [Cloudflare's published IP ranges](https://www.cloudflare.com/ips/)
to `ACTA_TRUSTED_PROXIES`. For the strongest posture, also restrict the host
firewall to accept `443` only from those ranges.

## MCP

Acta speaks the [Model Context Protocol](https://modelcontextprotocol.io) at
`/mcp` (Streamable HTTP). It is a sibling of the JSON API — an agent-shaped
presentation of the same board — authenticated the same way: a personal access
token as a `Bearer` header, no cookies, no CSRF.

### Install (Claude Code or Codex)

```sh
acta login <host>     # once, if you haven't
acta mcp install
```

`acta mcp install` rides your login: it lets you pick or create the principal to
act as, mints that principal's token, and writes the selected client's MCP
config. Pick **Claude Code** or **Codex** when prompted. It uses the server and
credentials from `acta login`, so log in first.

For Claude Code, the installer runs `claude mcp add` under the hood and stores
the bearer token in Claude's config. Re-running replaces the `acta` entry.

For Codex, the installer runs `codex mcp add` under the hood. Codex stores the
command `acta mcp proxy codex`, which Codex starts automatically when it needs
the MCP server. The proxy reads the Acta URL and token from Acta's own config
and forwards to `/mcp`, so no separate proxy process or token environment
variable is needed.

**Act as an agent.** When the installer offers a principal, pick or create an
**agent** (a `you/agentname` principal) rather than yourself. Every write the
agent makes is attributed to it, so `created_by` and comment authorship read
`you/agentname` — the board shows who (well, what) did the work. Acting as
yourself works too; it just attributes to `you`.

### Manual wiring

If you'd rather not use the installer, mint a token (Settings → Tokens, or an
agent's token under Settings → Agents) and add the server yourself:

```sh
claude mcp add --transport http --scope user acta http://localhost:8080/mcp \
  --header "Authorization: Bearer acta_pat_…"
```

For Codex:

```sh
codex mcp add acta -- acta mcp proxy codex
```

For the manual Codex proxy form, first create a `codex` MCP profile in Acta's
config by running `acta mcp install`, or use Codex's direct HTTP mode with your
own token environment variable.

or, as a committed `.mcp.json` (keep the secret in an env var, not the file):

```jsonc
{
  "mcpServers": {
    "acta": {
      "type": "http",
      "url": "${ACTA_URL:-http://localhost:8080}/mcp",
      "headers": { "Authorization": "Bearer ${ACTA_TOKEN}" }
    }
  }
}
```

### Tools

`whoami`, `list_principals`, `list_workspaces`, `list_items` (filter by status /
assignee / `mine` / parent), `get_item` (deep — description, subtasks,
comments), `create_item`, `set_item_title`, `claim_item`, `set_item_status`,
`set_item_assignee`, `set_item_description`, `set_item_milestone`,
`set_item_parent` (reparent / promote), `add_comment`, `archive_item`,
`unarchive_item`. Statuses and principals are referred to by name; items by id.
Item payloads carry a `url` permalink that opens the item on the board.
