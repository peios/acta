---
title: "Overview"
description: >-
  The Acta processes, authority boundaries, and durable records described by
  this manual.
---

Acta stores accounts, workspaces, tasks, documents, and project history in
PostgreSQL. People use the browser or CLI; external tools use authenticated
CLI and MCP connections.

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
[Operational procedures](~acta/deployment/deployment) cover installation and
recovery. The command-line executables' help and the MCP tool schemas provide
the available command and argument surfaces.
