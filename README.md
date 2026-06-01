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
