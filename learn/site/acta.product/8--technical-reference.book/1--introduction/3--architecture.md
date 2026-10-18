---
title: "Architecture"
description: >-
  Locate domain policy, persistence, transports, frontend state, and
  provider mapping ownership.
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
| `internal/client` | Shared command-line HTTP transport and response handling. |
| `internal/threadadapter` | Provider captures converted into normalized frame bundles. |
| `internal/hyperharness` | Local lifecycle orchestration and durable command delivery. |
| `web/src/lib` | Shared forms, typed clients, conversation projections, and browser state. |

Migrations are embedded, ordered, and checksummed. Startup applies them in a
transaction under a database advisory lock. An altered applied migration or an
unknown migration fails startup. Ordinary startup does not drop and recreate
data.

## Provider mapping

The adapter clones its checkpoint before interpreting one captured JSON
object. It validates the resulting bundle before returning the next
checkpoint. Unsupported or rejected input does not retain speculative
normalized output or state. Every capture retains a debug record.

The harness persists mapped bundles and their checkpoint before publishing. A
late receipt is correlated with its persisted command and provider run, not
with a bare command identifier. The generic pipe records process output
without interpreting provider methods or holding Acta credentials.

## Browser ownership

The conversation session owns polling and cancellation for one page. Reads and
control-result polls do not overlap themselves. Navigation disposes the
session. The message outbox owns pending submissions separately from
presentation.

Frame projections associate captures with runs, turns, lanes, and messages.
Unknown frames remain visible with debug display turned off. Presentation
components render these projections without owning transport or durable state.
