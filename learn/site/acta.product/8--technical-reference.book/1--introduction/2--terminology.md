---
title: "Terminology and Compatibility"
description: >-
  Distinguish accounts, sessions, threads, runs, lanes, and host
  connections.
---

Acta uses separate identities for durable records and live execution. Reusing
a display name does not establish identity or ownership.

The terms in this manual have these meanings:

| Term | Meaning |
| --- | --- |
| Account | A stable human or human-owned agent identity. |
| Session | Authorized access with expiry and revocation state. |
| Workspace | A boundary for work records and workspace permissions. |
| Thread | A human-owned record of a provider conversation. |
| Run | One provider process lifetime within a thread. |
| Lane | The main conversation or a native subagent conversation. |
| Harness | An outbound connection and local controller for provider execution. |
| Pipe | The local process and raw-output journal engine. |
| Code host | A separate private connection for the Code surface and codebase catalog. |

## Compatibility boundaries

The Bazel build supports x86_64 Linux and pins its Go and frontend toolchains.
The production Compose configuration uses PostgreSQL 17. The harness and
server use `acta-harness-v7`; protocol compatibility requires matching
implementations. Discovery reports provider versions without certifying every
reported version for execution.

The My Agents harness and Code host are separate execution paths. Their
framing, retention, controls, and recovery behavior differ. The Code surface's
live CAT frames are not the durable normalized frame stream described in the
thread-frame chapter.

Provider settings and local native-session storage remain provider-owned.
Resumption cannot reconstruct a native session from the server transcript
alone. Multi-replica routing for live harnesses and Code hosts is not
implemented.
