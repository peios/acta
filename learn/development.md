# Developing Acta

Acta uses Go 1.26, PostgreSQL, and Svelte 5 with SvelteKit's static adapter.
Node 22 (22.12 or newer) and npm are build/development dependencies. Deployment
runs one Go executable with embedded static assets plus PostgreSQL; it does not
run a Node or SvelteKit server.

## Run locally

From the repository root:

```sh
make install
make db
make run
```

Open http://localhost:8081. `make db` starts an isolated PostgreSQL 17 container
with a persistent named volume. Its port is **127.0.0.1:5433**, leaving other
PostgreSQL instances alone. Compose contains development-only credentials.
The Go server remains in the foreground; its terminal prints the setup code
for a new installation. Stop with Ctrl-C. PostgreSQL continues running.

`make run` uses the Compose connection by default and permits the local Vite
origin. Override `DEV_DATABASE_URL` or `ACTA_DATABASE_URL` when needed. The
executable itself requires `ACTA_DATABASE_URL` and has no default password.
Starting the server applies embedded schema migrations before listening.

For interactive review with Jack, rebuild and restart the Go server on **8081**
so the visible app stays current. Do not move review to another port.

For optional frontend hot reload, keep Go running and use `make dev` in another terminal.
Open http://localhost:5173. Vite proxies `/api` to the Go server. Its allowed
origin is `http://localhost:5173`; use that exact hostname. Edits to Go require
a server restart. `make build` rebuilds the frontend before embedding it; using
`go build` alone retains whatever assets were last built.

## Checks

```sh
make check
ACTA_TEST_DATABASE_URL='postgres://acta:acta-development-only@127.0.0.1:5433/acta?sslmode=disable' make test-integration
```

`make check` builds the frontend, checks Prettier formatting, runs Svelte/TypeScript diagnostics, frontend
interaction-rule tests, `go vet`, Go unit tests and Go formatting checks. Run
`npm --prefix web run format` to format frontend sources, tests and configuration.
Prettier and its Svelte plugin are pinned in the lockfile; a global formatter is
not required. PostgreSQL integration tests are skipped
unless `ACTA_TEST_DATABASE_URL` is set. The explicit integration target refuses
to run without it and runs the full Go suite with the race detector.

Use an expendable development database and a role allowed to create schemas.
Each integration test creates a randomly named schema, migrates it, exercises
real SQL transactions, then drops only its own schema. It never wipes `public`,
the development administrator, or another application's database. Tests cover
setup races and rollback, permanent completion, session expiry/revocation,
shared throttling, the HTTP account lifecycle, malformed requests, origin
checks, cookie policy, migration integrity, profile version conflicts, atomic
name claims, reservation reclamation, namespace separation and rename continuity. Security tests additionally cover
MFA activation/replacement, code replay and concurrent consumption, stale flows,
session-bound actions, concurrent policy changes and login issuance, and signed
WebAuthn registration/assertions, user-management authorization, pending
activation, recovery grants, disabling and re-enabling without session revival.

Browser review should cover both setup steps, keyboard/error focus, password
visibility, login failure/success, reload persistence, logout and narrow-screen
layout. Keep fixture accounts in isolated schemas; leave the real first-run
account creation to the operator.

## Configuration and deployment shape

| Setting | Purpose |
| --- | --- |
| `ACTA_DATABASE_URL` | Required PostgreSQL connection URL. Use a dedicated database and role. Configure TLS appropriately for remote PostgreSQL. |
| `ACTA_TRUSTED_PROXIES` | Optional comma-separated IPs/CIDRs trusted to supply `X-Forwarded-For`; empty trusts no forwarding headers. Invalid entries and all-address networks refuse startup. |
| `ACTA_PUBLIC_URL` | Exact external origin, default `http://localhost:8081`. Use `https://acta.peios.org` for that deployment. Non-loopback HTTP is rejected. |
| `ACTA_DEV_ORIGIN` | Optional extra loopback origin for Vite; allowed only with a local HTTP installation. Omit in production. |
| `ACTA_SECURITY_KEY_FILE` | Private, persistent encryption key file; default `.local/security.key` relative to the working directory. Provisioned on first start. All instances sharing a database must use the same key. |
| `-listen` | HTTP bind address, default `:8081`. Behind a local proxy, use `127.0.0.1:8081`. |

For a deployment, build and invoke the binary directly with production
configuration, rather than using the convenience `make run` target:

```sh
make build
export ACTA_DATABASE_URL='postgres://...'
export ACTA_PUBLIC_URL='https://acta.peios.org'
unset ACTA_DEV_ORIGIN
./bin/acta-server -listen 127.0.0.1:8081
```

An HTTPS reverse proxy terminates TLS and must preserve the external Host
header. This checkpoint does not configure a public proxy or deploy a public
site. Forwarded IP headers are currently ignored; see the rate-limit caveat in
[Accounts](accounts.md). The database pool allows at most eight connections per
server. HTTP header/read/write/idle timeouts and bounded password hashing avoid
unbounded per-request resource use.

SvelteKit emits a hash-based content security policy for the compiled bootstrap
script. The server adds frame restrictions and other response headers. See the
[SvelteKit CSP documentation](https://svelte.dev/docs/kit/configuration#csp).
Fingerprint assets receive immutable caching; the HTML shell and API do not.
JavaScript is required, with a noscript explanation when disabled.

Back up PostgreSQL, including schema migration records, **and the security key
file**. Keep the key private (mode 0600) and outside the database backup; loss
of the key makes encrypted security state unreadable. A database fingerprint
rejects a mismatched key at startup. Use an explicit persistent key path in a
deployment; do not put it on ephemeral container storage. See [Security](security.md). Never use Compose's
example password on a public deployment. `docker compose stop` preserves the
volume; deleting the volume deletes all Acta data and is not a normal reset
or upgrade procedure.

Permission integration cases in `internal/integration/permissions_test.go` cover
capability and credential-authority boundaries, concurrent last-Superuser
protection, invitation profile policy and required-MFA flows. Run them with the
same isolated-schema `make test-integration` command above.

## CLI

`make build` produces `bin/acta-server` and the standalone `bin/acta` client.
See [CLI profiles and authentication](cli.md) for configuration and login.

## Thread implementation boundaries

- `internal/threadadapter/adapter.go` owns one captured frame's mapping transaction:
  clone the checkpoint, dispatch to a provider, validate the output bundle, and
  return the next checkpoint. Unsupported or rejected input must not retain
  speculative output or state. Input must be exactly one JSON object. Every
  capture still produces its debug record. `state.go` owns checkpoint encoding;
  `values.go` holds shared decoding helpers.
- Provider dispatch lives in `codex.go` and `claude.go`. Their tool and thinking
  mappings live in provider-specific files; shared tool/thinking state remains in
  `tools.go` and `thinking.go`. Native provider shapes stay inside these adapters.
- `internal/hyperharness/controller.go` owns lifecycle orchestration under the
  controller lock. `rpc.go` owns provider request/reply envelopes, `codex.go`
  owns Codex startup, and `delivery.go` owns capture interpretation, command
  receipt reconciliation and acknowledgements. Delivery saves mapped bundles
  and their checkpoint before publishing them. A late receipt must match its
  persisted command and provider run; a bare command ID is insufficient.
- `web/src/lib/thread-frames.js` owns capture identity, run/turn association and
  transcript visibility. The separate feed projections retain their distinct
  tool, thinking, message and turn lifecycle rules. Unknown Frames remain visible
  when debug is off.
- `web/src/lib/thread-session.js` owns one page's frame/control polling and
  cancellation. It is disposed on navigation; reads and control-result polls
  cannot overlap themselves. `ThreadSender` separately owns the message outbox.
  The route composes these with `ThreadHeader`, `ThreadTranscript` and the
  composer. Presentation components do not own transport or durable state.

Regression tests cover rejected-envelope rollback, malformed captures, exact
command receipt identity, replay deduplication, cross-thread turn isolation,
non-overlapping polling and disposal during requests. Existing history can be
used for browser review without starting a paid provider turn. See the
[September thread refactor report](reviews/2026-09-08-thread-refactor.md).

## Installable app and Web Push

See [Installing Acta and push delivery](pwa.md). Rebuild the frontend before the
Go executable so the manifest, worker and icons are embedded. Use HTTPS for real
devices and keep the installation security key persistent. Push delivery runs
inside the Go server and does not require a separate worker service.


## Independent backups

See [Installation backup and recovery](backups.md) for operator provisioning,
Site Settings, isolated restores, production preparation and the full recovery
test. `ACTA_BACKUP_SOCKET` and `ACTA_BACKUP_TOKEN_FILE` connect the web process to
an independently supervised `acta-backup` process. Neither is configured by
default. Managed PostgreSQL integration remains deferred.

For the integrated Caddy/PostgreSQL production package and its isolated smoke
test, see [production deployment](deployment.md).
