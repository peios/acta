---
title: "Building and Checking Acta"
type: how-to
description: >-
  Run builds, formatting, frontend checks, and database integration tests
  with Bazel.
---

Bazel tracks Go packages, generated frontend assets, embedded migrations, and
documentation inputs. Use it for builds so the server cannot embed stale
assets from a previous checkout build.

## Build and check

From the repository root, run the checks needed for the change:

```sh
./bazel build //:binaries
./bazel build //:frontend
./bazel run //:export
./bazel test //...
./bazel run //:format-check
./bazel run //:style-check
./bazel run //:gazelle -- -mode=diff
```

The export target writes `acta-server`, `acta`, `acta-backup`, and
`acta-update` to `bin/`. Set `ACTA_OUTPUT_DIR` to choose
another destination. The frontend target writes assets under
`bazel-bin/web/build`.

The test suite includes Go unit tests, standard Go vet checks, frontend
formatting, ESLint, Svelte/TypeScript checks, Node interaction tests, and the
application entrypoint regression test.
Database-backed cases skip without their database configuration. Browser
workflows remain explicit checks; a passing default suite does not establish
browser coverage.

## Follow Peios coding style

Acta follows
[Peios coding style](https://learn.peios.org/psc/scope-and-precedence/).
Go uses `gofmt`. The frontend uses four spaces, a 100-column formatting
target, double quotes, and semicolons. Svelte-aware lint checks preserve the
semantics of reactive props and derived values. Braces follow the common Peios
rule; short single-line clauses can omit them.

`//:style-check` checks maintained Python, shell, and Starlark sources with
pinned Ruff, shfmt, ShellCheck, and buildifier. It also checks maintained
JSON, YAML, and web manifests with the frontend's pinned Prettier. The
repository formatter settings are shared with the frontend; npm's generated
lockfile is excluded. Keep workflow Python in `scripts/` to include it in
these checks. Pass `-- --fix` to apply formatters; lint findings still require
review. `//:format-check` checks Go. Frontend formatting and lint run as part
of `./bazel test //...`. With the locked frontend dependencies installed, `npm
run format` in `web/` applies frontend formatting and `npm run lint` checks
the result.

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
ACTA_TEST_DATABASE_URL='postgres://acta:acta-development-only@127.0.0.1:5433/acta?sslmode=disable'   ./bazel run //:integration
```

The integration target refuses a missing database URL, disables cached
results, and runs the suite with the Go race detector. Each test creates a
randomly named schema, applies migrations, exercises real transactions, and
drops its own schema. It does not wipe the database's `public` schema.

Backup recovery, update recovery, and device passkeys need their own
qualification. Do not infer those results from ordinary unit tests.

## Check the result

Inspect failures and skipped cases, including which database configuration was
used. Record the tests actually run on the work's Acta task. Keep local
validation, remote CI, release publication, and deployment as separate claims.
