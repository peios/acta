---
title: "Provider Threads"
type: concept
description: >-
  Separate provider execution, harness connections, Acta history, and agent
  accounts.
---

A provider thread is a conversation with Codex or Claude Code running on your
development machine. Acta supplies the browser interface and stores
conversation history; the local provider performs the work.

## The connection and the process

A harness makes an outbound connection from the development machine. It
reports available providers and relays commands and captured output. Closing
the browser does not stop provider work.

A lost connection means Acta cannot reach the provider. It does not prove that
the provider exited. The normal harness owns its provider processes and stops
them on orderly shutdown. A separate-pipe harness can leave those processes
running through a supervisor restart.

## History and identity

Acta stores normalized frames and retained debug output. Native provider
history and local files remain on the development machine. A server backup
does not back up those files or provider sessions.

Threads belong to their human owner; Superuser does not bypass this privacy
boundary. The provider's own MCP configuration determines its Acta identity. A
thread is not a separate Acta agent account merely because it runs a model.

## Subagent conversations

Native subagents appear as lanes inside the parent thread. Each lane has its
own history, state, and browser draft, while the parent provider owns
execution. Approval requests identify their lane and can be answered without
losing the conversation you are reading.

Stopping a selected lane interrupts that lane. Killing the provider affects
the whole process. A completed child remains accessible from its card in
parent history even after its navigation pill disappears.
