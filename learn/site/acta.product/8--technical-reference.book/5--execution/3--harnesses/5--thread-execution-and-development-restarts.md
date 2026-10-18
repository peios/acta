---
title: "Thread Execution and Development Restarts"
description: >-
  Provider lifetime under embedded and separate pipes, with restart and
  reattachment boundaries.
---

My Agents can create Codex and Claude Code threads on a connection. Thread
identities and history are durable; the harness connection remains ephemeral.
An established thread is available when a current authorized connection
advertises it. Disconnect means unreachable, not necessarily that its provider
has exited.

Normally the provider pipe runs within `acta harness`, so exiting that command
terminates its provider processes. For development:

```sh
acta harness --separate-pipe
```

This starts or attaches to the same generic pipe engine in a detached helper.
Provider processes and raw capture survive hyperharness restarts. The
replaceable hyperharness retains the provider adapters; the helper has no Acta
credential and no provider-specific interpretation. It serves a private
Unix-domain socket, not a network port. See [Provider
threads](~acta/technical-reference/execution/threads) for ownership, shutdown,
durability, and failure boundaries.
