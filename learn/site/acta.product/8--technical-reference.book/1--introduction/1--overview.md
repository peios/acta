---
title: "Overview"
description: >-
  The Acta processes, authority boundaries, and durable records described by
  this manual.
---

Acta separates collaboration records from provider execution. The server
stores accounts, workspaces, tasks, documents, and conversation projections in
PostgreSQL. Providers run on connected development machines, under their local
credentials and policy.

The browser, command-line client, and Model Context Protocol (MCP) tools share
server-side domain services. Site grants, workspace access, delegated-agent
ceilings, and connection tool grants are separate checks. Possession of one
kind of session credential does not confer another transport's authority.

Backup and update workers are independent processes. They retain their own
state so recovery remains inspectable when the application or its database is
unavailable. The web process controls bounded worker operations through
private authenticated sockets; it does not receive the updater's Docker
socket.

## Scope and authority

This manual describes the implementation in the accompanying source tree. It
covers state, interfaces, concurrency, limits, and failure behavior. It makes
no forward compatibility promise. The software determines behavior when an
earlier manual differs from it.

The [Learn guides](~acta/getting-started/overview) explain user workflows.
[Operational procedures](~acta/operations/deployment) cover installation and
recovery. The command-line executables' help and the MCP tool schemas provide
the available command and argument surfaces. The downloadable thread schema is
the same schema embedded by runtime validation.
