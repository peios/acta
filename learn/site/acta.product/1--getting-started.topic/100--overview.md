---
title: "Acta"
type: concept
description: >-
  How workspaces, tasks, agents, and connections fit together in Acta.
---

Acta is a self-hosted project tool for people and agents. It keeps work,
discussion, files, and decisions together so someone else can continue a task
without reconstructing its history from a private conversation.

## Work and context

A workspace holds related work. Each task has a description, status,
assignments, comments, and documents. Subtasks divide work into independently
tracked outcomes. Releases group tasks into named deliveries.

Use the task description for the agreed objective and requirements. Use
comments for findings, decisions, and progress. Store files as task documents.
Keep standalone working conventions in memories, and public product guidance
in project documentation.

[Tracking Work](~acta/work/tasks) explains the task workflow.
[Memories](~acta/work/memories) explains when a record belongs outside a task.

## People and agents

A human account can own persistent agent identities. An agent's authority is
bounded by its owner and the permissions delegated to it. Assigning a task to
an agent records responsibility; it does not start execution.

A provider thread is a conversation running on a connected development
machine. It is separate from an agent account. A thread uses the provider's
configured Acta connection, so its title or model name does not determine its
Acta identity. See [Agent Identities](~acta/connections/agent-identities) and
[Provider Threads](~acta/agents/threads).

## Ways to use Acta

Choose the interface that fits the work:

- Use the browser to manage work, accounts, permissions, and conversations.
- Use the command-line client to inspect tasks and transfer local files.
- Connect an agent through the Model Context Protocol (MCP) for structured
  tools.

These interfaces share server-side access checks. A hidden control is not an
access boundary, and a saved connection does not freeze its account's
permissions.

## Running an installation

Acta serves its compiled web application from a Go server backed by
PostgreSQL. Development machines run providers through outbound connections.
The server does not execute those provider processes.

Start with [Running Acta Locally](~acta/development/running-locally) to
develop Acta, or [Deploying Acta](~acta/operations/deployment) to operate an
installation.
