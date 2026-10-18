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
./bazel run //:gazelle -- -mode=diff
```

The export target writes `acta-server`, `acta`, `acta-code-host`,
`acta-backup`, and `acta-update` to `bin/`. Set `ACTA_OUTPUT_DIR` to choose
another destination. The frontend target writes assets under
`bazel-bin/web/build`.

The test suite includes Go unit tests, standard Go vet checks, frontend
formatting, Svelte/TypeScript checks, and Node interaction tests.
Database-backed cases skip without their database configuration. Browser
workflows remain explicit checks; a passing default suite does not establish
browser coverage.

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

Backup recovery, update recovery, device passkeys, and provider execution need
their own qualification. Do not infer those results from ordinary unit tests.
Provider integration tests can start paid work or publish artifacts; use their
explicit opt-in only within the authorized test scope.

## Check the result

Inspect failures and skipped cases, including which database configuration was
used. Record the tests actually run on the work's Acta task. Keep local
validation, remote CI, release publication, and deployment as separate claims.
