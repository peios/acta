---
title: "Maintaining Build Inputs"
type: how-to
description: >-
  Update generated Go targets, locked frontend dependencies, pinned
  toolchains, and versioned exports.
---

Dependency and toolchain changes update both the declared inputs and the files
that lock their resolution. Build actions do not install dependencies.

## Update Go targets

1. Change the Go source or dependency declarations.
2. Run `./bazel run //:gazelle`.
3. Run `./bazel mod tidy`.
4. Review generated BUILD files and `MODULE.bazel.lock`, then run relevant
  checks.

The web BUILD file is maintained explicitly because it embeds a generated
directory. Preserve explicit test fixtures when reviewing generated changes.
Go dependencies are declared through `go.mod` and `go.sum`.

## Update frontend dependencies

Update `web/package.json` and `web/package-lock.json` together. Bazel fetches
the locked dependencies again when either changes. Repository fetching uses
`npm ci --ignore-scripts`; verify that a changed dependency does not require
an unprovided lifecycle step.

Toolchain pins live in `MODULE.bazel`, `build/repositories.bzl`,
`build/style/tools.bzl`, `.bazelversion`, and `build/bazel.sha256`. Update
the appropriate version and
checksum together.

## Export a versioned build

For a build labeled `0.1.0`, run:

```sh
./bazel run --config=release --define ACTA_VERSION=0.1.0 //:export
```

The version reaches both the frontend and the Go executable. The release
profile turns off CGO for static Go binaries. The Docker build uses the same
exports. A versioned build does not sign, publish, or deploy a release.

## Maintain documentation

Edit the Trail product in `learn/site/acta.product`. Keep user concepts and
procedures in topics and implementation details in the technical manual. The
agent guide is embedded from its canonical Learn file.
Changes to those files also change the application inputs.

Run the documentation checks from the repository root:

```sh
trail lint learn/site
trail build learn/site --strict
```

Use a Trail build with attachment support. Inspect the rendered pages and
links as well as the diagnostics. Historical investigations belong in
`docs/archive`, which is excluded from the published source tree.
Work-specific validation and decisions belong on the corresponding Acta task.
