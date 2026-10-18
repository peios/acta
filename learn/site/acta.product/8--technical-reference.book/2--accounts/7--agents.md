---
title: "Agent Identities"
description: >-
  Human-owned agent identities, delegated permission ceilings, name
  reservations, and connection authorization.
---

An agent is a persistent account owned by one human. Its UUID and owner are
immutable; its effective access is bounded by the owner on every request.

## Managing an agent

Create agent asks for a username and optional display name, then opens the
shared permission editor on its detail page. A new agent starts with no site
grants. The editor only lists capabilities the owner holds and excludes
Superuser, Create workspaces, Manage backups, and Edit site guide preferences.
Changes are staged until saved. The owner can edit names, inspect and revoke
individual or all sessions, and disable or re-enable the identity. There is no
delete action; UUIDs and historical references are retained.

The owner's own name-changing permissions do not limit their ability to name
agents. Segment validation is shared with human usernames. Renaming either the
agent or its owner reserves previous full handles against the same agent UUID.
Owner renames also advance the agent's profile revision, so stale profile
forms receive a conflict. Historical handles with a previous owner prefix
remain visible but cannot be selected as a new agent segment under the current
prefix.

## Delegated access

Effective agent site access is the intersection of its explicit grants and its
owner's current effective permissions. `accounts.ResolvePermissions` is the
common resolution point for browser API, CLI and MCP account checks. A missing
owner projection grants no access. Group membership is resolved on the owner;
agents do not inherit all owner site permissions automatically.

[Workspace access](~acta/technical-reference/accounts/workspaces#agent-access)
has separate defaults: all owner workspaces and inherited workspace
permissions, with optional selection and explicit per-workspace overrides.
Workspace creation remains human-only.

When a human loses access through direct grants, group edits, group deletion
or membership changes, the same transaction prunes affected grants from their
agents and advances those agents' permissions revisions. Another remaining
source preserves a grant. Restoring the owner's access later does not restore
previous delegation: restoration requires an explicit grant by the owner.

Disabling an agent revokes all its sessions. Disabling its owner also revokes
all owned-agent sessions and prevents new authorization. Re-enabling either
account does not revive old connections. A unmet owner MFA requirement blocks
agent use and authorization until the human completes enrollment.

## CLI and MCP authorization

Both browser authorization pages use the same **Act as** selector. It defaults
to the signed-in human and offers their active agents. The human completes the
existing password/passkey/MFA flow and approves the connection. The resulting
session belongs to the selected UUID and records the human who authorized it.
CLI status and MCP `whoami` return that selected identity, including its full
username. An agent cannot authorize another connection.

Human connections remain under User Settings → Security. Agent connections
appear on that agent's detail page. Session expiry stays seven days idle and
thirty days absolute. MCP tool grants remain an additional ceiling; selecting
an agent does not grant extra tools. [Task
tools](~acta/technical-reference/work-records/tasks) add workspace-scoped
reading, creation and editing, subject to separately approved MCP tool grants.

## API and storage contract

- `GET /api/agents` lists the caller's agents, including disabled ones.
- `POST /api/agents` creates an agent with `username` and `display_name`.
- `GET /api/agents/permissions` returns the owner's delegable catalog.
- `GET /api/agents/{uuid}` returns one owned account.
- `POST /api/agents/{uuid}/profile` accepts the same fields and
  `profile_version` as human profile editing.
- `POST /api/agents/{uuid}/permissions` accepts the complete `permissions`
  array and expected `permissions_version`; no MFA requirement is accepted.
- `POST /api/agents/{uuid}/disabled` accepts a `disabled` boolean.
- `GET /api/agents/{uuid}/sessions` lists active connections.
- `POST /api/agents/{uuid}/revoke` accepts a session `id`, or an empty ID for
  all.
- Device and OAuth approval accept an optional `account_id`. Empty or the
  caller's UUID means self; another UUID is accepted only for an active owned
  agent.
- Account responses add `username_segment`, `owner_id`, and `owner_username`;
  `username` remains the full handle.

`AgentStore` extends management persistence; `Security.connectionIdentity`
owns shared CLI/OAuth subject selection. Transactions recheck ownership,
status, session, and owner policy. Account-family mutations lock the human
before an agent, including authentication, naming and permission pruning.
Management retains the installation lock for existing administrative
invariants. Session issuance, authorization records, and events commit
atomically.

Migration 010 adds durable full-handle reservations and session authorizers,
and database guards against agent credentials, groups, browser sessions and
account requirements. The storage boundary includes transactional grant
pruning and revocation.
