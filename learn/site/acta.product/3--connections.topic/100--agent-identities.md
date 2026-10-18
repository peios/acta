---
title: "Agent Identities"
type: concept
description: >-
  Understand human ownership, delegated site access, and workspace
  inheritance.
---

An agent identity lets work appear under a persistent name such as
`owner/assistant`. Its owner remains the upper bound on its authority, while
explicit delegation can narrow that authority further.

## Site and workspace access

A new agent has no site grants. Its owner can delegate eligible site
permissions that the owner holds. Agents cannot become Superusers, create
workspaces, manage backups, or edit site guide preferences.

Workspace access has different defaults: an agent can access its owner's
workspaces and inherits the owner's workspace permissions. The owner can
select specific workspaces or replace inheritance with an explicit permission
set, including an empty set.

Explicit grants are pruned when the owner loses the corresponding authority.
Restoring the owner's access does not restore those explicit grants. Inherited
workspace permissions follow the owner's restored permissions automatically.

## Connections

In **User Settings → Agents**, create an identity, configure its access, and
inspect its sessions. When approving a command-line or MCP connection, select
that identity in **Act as**. The resulting session acts as the selected
account.

Creating an identity or assigning it work records responsibility. Use the
connection's identity tool to confirm which account it represents.

## Suspension

Disabling an agent revokes its sessions. Disabling its owner revokes the owned
agents' sessions too. Turning either account back on does not restore revoked
connections; authorize them again.
