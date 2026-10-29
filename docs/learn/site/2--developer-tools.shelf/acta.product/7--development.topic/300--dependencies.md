---
title: "Maintaining Build Inputs"
type: how-to
description: >-
  Update Go modules, locked frontend dependencies, pinned toolchains, and
  versioned exports.
---

Dependency and toolchain changes update both the declared inputs and the files
that lock their resolution. Build actions do not install dependencies.

## Update Go modules

1. Change the Go source or dependency declarations.
2. Run `go mod tidy` to update `go.mod` and `go.sum`.
3. Run `pekit run pin_modules` to write `go.pins` again.
4. Review the changes to `go.sum` and `go.pins` together, then run relevant
  checks.

pekit plans each package's build from the Go sources, so new packages and
imports need no build file changes. A new package with tests joins the test
suite through `test_packages` in `build.gcsl`.

## Update frontend dependencies

1. Update `web/package.json` and `web/package-lock.json` together.
2. Run `pekit run pin_packages` to write `web/npm.pins` again.
3. Review the changes to the lockfile and the pins together.

The pins record the SHA-256 of each package tarball. pekit fetches each
tarball by its pin and checks it against the lockfile's integrity. Package
install scripts never run; verify that a changed dependency does not require
an unprovided lifecycle step.

Toolchain pins live in the gcslib libraries that `workspace.gcsl` imports.
`pekit.lock` pins those libraries' commits. Run `pekit lock` to update them.

## Export a versioned build

For a build labeled `0.1.0`, run:

```sh
printf '0.1.0\n' > build/release-version
tools/export
```

The version reaches the Go executables and the frontend's `package.json`.
Without `build/release-version`, both report `0.0.0`. Every build is pure
Go, with CGO off, so the executables are static. The container images copy
the programs from `bin/`, so run `tools/export` before a Docker build. A
versioned build does not sign, publish, or deploy a release.

The frontend's version is a hash of its sources, so two builds of the same
sources produce the same assets.

## Maintain documentation

Edit the Trail product in
`docs/learn/site/2--developer-tools.shelf/acta.product`. Keep user concepts
and procedures in topics and implementation details in the technical manual.
The agent guide is embedded from its canonical Learn file. Changes to those
files also change the application inputs.

Run the documentation checks from the repository root:

```sh
trail lint docs/learn/site
trail build docs/learn/site --strict
```

Use a Trail build with attachment support. Inspect the rendered pages and
links as well as the diagnostics. Historical documentation remains available
in Git history.
Work-specific validation and decisions belong on the corresponding Acta task.
