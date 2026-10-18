---
title: "Architecture"
description: >-
  Locate domain policy, persistence, transports, frontend state, and
  notification ownership.
---

Domain services own validation, authority, and behavioral storage operations.
PostgreSQL implements those operations; HTTP and MCP adapt their results to
transport-specific envelopes.

The implementation boundaries are:

| Source | Responsibility |
| --- | --- |
| `internal/accounts`, `internal/auth` | Identity, effective permissions, authentication, and session policy. |
| `internal/workspaces`, `internal/tasks` | Workspace and work-record validation and behavior. |
| `internal/postgres` | SQL transactions, migrations, consistent reads, and persistence. |
| `internal/httpapi` | Routing, origins, cookies, transport validation, and safe errors. |
| `internal/notifications`, `internal/push` | Task inbox contracts and browser push delivery. |
| `internal/client` | Shared command-line HTTP transport and response handling. |
| `web/src/lib` | Shared forms, typed clients, task views, and browser state. |

Migrations are embedded, ordered, and checksummed. Startup applies them in a
transaction under a database advisory lock. An altered applied migration or an
unknown migration fails startup. A release migration can retire stored data;
operators review migration
effects before upgrading.
