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

## Running

### Whole stack in Docker (app + db)

```sh
docker compose up --build
```

Then open http://localhost:8080 and sign in with the bootstrap admin
(`admin` / `admin` by default — set `ACTA_BOOTSTRAP_*` in `docker-compose.yml`
to change). Migrations run automatically on first boot.

### Host dev loop (app on host, db in Docker)

```sh
make db-up                                   # just Postgres
ACTA_SEED_PASSWORD=secret \
  go run ./cmd/acta createuser -username jack -display "Jack"
make run                                     # serve on :8080
```

Either way, visiting http://localhost:8080 logged-out bounces you to the
login page.

## Configuration

| Env var                 | Default                          | Meaning                        |
| ----------------------- | -------------------------------- | ------------------------------ |
| `ACTA_HTTP_ADDR`        | `:8080`                          | listen address                 |
| `ACTA_DATABASE_URL`     | `postgres://acta:acta@localhost:5432/acta?sslmode=disable` | Postgres DSN |
| `ACTA_ENV`              | `dev`                            | `prod` enables Secure cookies  |
| `ACTA_SESSION_IDLE`     | `24h`                            | idle session timeout           |
| `ACTA_SESSION_ABSOLUTE` | `720h`                           | absolute session lifetime      |
