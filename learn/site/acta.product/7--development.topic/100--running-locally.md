---
title: "Running Acta Locally"
type: how-to
description: >-
  Build Acta with the pinned Bazel toolchains and run a development server
  on port 8081.
---

The development setup runs a Go server with embedded web assets and a separate
PostgreSQL database. Bazel supplies the Go and frontend toolchains.

## Before you start

Use an x86_64 Linux build host with these prerequisites:

- Bash, Python 3, curl, and Git
- A C/C++ compiler for race tests
- Docker and Docker Compose for the supplied development database
- Network access for the first dependency fetch

The wrapper pins Bazel 8.4.2. The build pins Go 1.26.8 and Node 22.22.1 with
npm 10.9.4. Host Go, Node, and npm installations are not required. The build
depends on host Linux utilities and does not claim a fully hermetic host
environment.

## Start the application

1. From the repository root, run `./bazel run //:db`.
2. Run `./bazel run //:run`.
3. Open [the local Acta server](http://localhost:8081).
4. For an empty installation, use the setup code printed in the server
  terminal to create its first administrator. Keep that terminal private.
5. Sign in and check that the application loads.

The development database listens on `127.0.0.1:5433` and keeps a persistent
named volume. Its credentials are development-only. The server runs on port
8081; keep this port for interactive review. Starting the server applies
embedded migrations. Stopping it with <kbd>Ctrl</kbd>+<kbd>C</kbd> leaves
PostgreSQL running.

The run target supplies the development database URL and permits the local
Vite origin. Override `ACTA_DATABASE_URL` or `ACTA_DEV_ORIGIN` when using
another setup. The server executable itself requires a database URL and has no
default password.

## Use frontend hot reload

Keep the Go server running, then run `./bazel run //:dev` in another terminal.
Open [the Vite development server](http://localhost:5173) using that hostname.
Vite proxies API and WebSocket requests to Go. Restart Go after backend
changes.

This target installs locked dependencies in `web/node_modules` using the
pinned toolchain. Ordinary builds and tests use scratch directories and keep
generated frontend output out of the checkout.

## Preserve development data

Stopping Compose preserves its database volume. Removing the volume deletes
Acta data; it is not an upgrade procedure. Test fixtures use isolated schemas
and do not initialize the operator's development account.
