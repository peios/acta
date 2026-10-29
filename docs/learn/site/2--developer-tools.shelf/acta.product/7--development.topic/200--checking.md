---
title: "Building and Checking Acta"
type: how-to
description: >-
  Run builds, formatting, frontend checks, and database integration tests
  with pekit.
---

[pekit](https://learn.peios.org/pekit/) tracks Go packages, generated frontend
assets, embedded migrations, and documentation inputs. Use it for builds so
the server cannot embed stale assets from a previous checkout build.

## Build and check

From the repository root, run the checks needed for the change:

```sh
pekit build programs
pekit build frontend
tools/export
pekit test
pekit run style
```

`tools/check` runs the style check and then the tests. The `programs` target
holds `acta-server`, `acta`, `acta-backup`, and `acta-update`; pekit prints
where it is, under `pekit-out/`. `tools/export` copies them to `bin/`. Set
`ACTA_OUTPUT_DIR` to choose another destination. The `frontend` target holds
the built web assets.

The test suite includes Go unit tests, standard Go vet checks, the Go race
detector, Go formatting, frontend formatting, ESLint, Svelte/TypeScript
checks, Node interaction tests, and the application entrypoint regression
test. Each test runs in a sandbox without network access. A test whose inputs
did not change is found in pekit's store and not run again.
Database-backed cases skip without their database configuration. Browser
workflows remain explicit checks; a passing default suite does not establish
browser coverage.

GitHub runs the style check, the tests, and the build on every push and pull
request. A second job runs the database integration tests against a
PostgreSQL service.

## Follow Peios coding style

Acta follows
[Peios coding style](https://learn.peios.org/psc/common-coding-rules/scope-and-precedence/).
Go uses `gofmt`. The frontend uses four spaces, a 100-column formatting
target, double quotes, and semicolons. Svelte-aware lint checks preserve the
semantics of reactive props and derived values. Braces follow the common Peios
rule; short single-line clauses can omit them.

`pekit run style` checks maintained Python and shell sources with pinned Ruff,
shfmt, and ShellCheck. It also checks maintained JSON, YAML, and web manifests
with the frontend's pinned Prettier. The repository formatter settings are
shared with the frontend; npm's generated lockfile is excluded. Keep workflow
Python in `scripts/` to include it in these checks. The `gofmt` test checks
Go. Frontend formatting and lint run as part of `pekit test`. With the locked
frontend dependencies installed, `npm run format` in `web/` applies frontend
formatting and `npm run lint` checks the result.

These checks do not enforce hard function-length or complexity limits. Review
names, units, error handling, ownership, import initialization order, and
functions beyond the Peios soft thresholds separately. Preserve published
field names and protocol contracts during style changes.

Generated outputs and dependencies are outside the style rewrite scope. Do not
reformat applied SQL migrations: Acta verifies their checksums on startup.

## Test database behavior

Use an expendable database and a role allowed to create schemas. With the
supplied development database running, execute:

```sh
ACTA_TEST_DATABASE_URL='postgres://acta:acta-development-only@127.0.0.1:5433/acta?sslmode=disable' pekit run integration
```

The integration command refuses a missing database URL and runs every
package's tests with the Go race detector, outside pekit's sandboxes, so that
they reach the database. It runs them every time. Each test creates a randomly
named schema, applies migrations, exercises real transactions, and drops its
own schema. It does not wipe the database's `public` schema.

Backup recovery, update recovery, and device passkeys need their own
qualification. Do not infer those results from ordinary unit tests.

## Check the result

Inspect failures and skipped cases, including which database configuration was
used. Record the tests actually run on the work's Acta task. Keep local
validation, remote CI, release publication, and deployment as separate claims.
