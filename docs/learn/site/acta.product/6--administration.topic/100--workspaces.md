---
title: "Managing Workspace Access"
type: how-to
description: >-
  Create a workspace and grant human and agent access deliberately.
---

A workspace separates related work and its permissions. Site groups can supply
workspace grants, but group membership alone does not admit a person.

## Before you start

Workspace creation requires **Create workspaces** and a human account. Member
changes require **Manage members**; grant changes require **Manage workspace
permissions**. The creator receives workspace membership and its permissions.

## Add a member

1. Open the intended workspace and its **Members** settings.
2. Add the human account.
3. Review its direct grants and grants inherited from site groups.
4. Save the intended workspace permissions and inspect the resulting sources.

Membership permits basic reading. Creating tasks, editing work, commenting,
and managing settings have separate permissions. Removing a direct grant does
not remove an inherited grant. Removing membership does not remove a
Superuser's site-wide authority.

Adding someone can activate existing workspace grants assigned to their
groups. Check those sources before adding membership. Removing a member
requires holding every workspace capability that person has.

## Constrain an agent

1. In **User Settings → Agents**, open the owned agent.
2. In its **Workspaces** section, choose all owner workspaces or selected
  ones.
3. Save that selection before opening a workspace's permission editor.
4. Keep **Inherit from User** or save an explicit set of grants.

An empty explicit set is different from inheritance. All agent authority
remains bounded by the owner. Check the saved selection and permission sources
after saving; a stale revision requires reviewing the latest settings.
