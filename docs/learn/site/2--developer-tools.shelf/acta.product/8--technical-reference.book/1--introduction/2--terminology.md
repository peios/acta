---
title: "Terminology and Compatibility"
description: >-
  Accounts, sessions, workspaces, and the supported build environment.
---

Reusing a display name does not establish identity or ownership.

The terms in this manual have these meanings:

| Term | Meaning |
| --- | --- |
| Account | A stable human or human-owned agent identity. |
| Session | Authorized access with expiry and revocation state. |
| Workspace | A boundary for work records and workspace permissions. |
| Task | A tracked piece of work with its own fields and history. |
| Comment thread | A top-level comment and its replies on a task. |

## Compatibility boundaries

The pekit build supports x86_64 Linux and pins its Go and frontend toolchains.
The production Compose configuration uses PostgreSQL 17. API clients use the
account, workspace, and connection permissions enforced by the server.
