---
title: "Code Preview"
description: >-
  The Code surface, host and codebase selection, live conversation output,
  and the sample editor boundary.
---

The Code scope opens `/code` using the existing Acta sidebar, theme tokens and
navigation. The editor is a rough UI mockup; host discovery, codebases,
directory browsing, and [Claude Code and Codex thread
output](~acta/technical-reference/execution/code-threads/overview) use the
connected Code host.

The Code sidebar header contains a host picker in the same position and style
as the Workspaces scope picker. It lists the signed-in human's registered
hosts with live presence; see [Code host
registration](~acta/technical-reference/execution/code-host).

The codebase presents agent thread output on the left and a sample editor on
the right, side by side when the content area exceeds 850px. Narrower views
use Editor / Conversation buttons. The Code sidebar uses Agents / Files tabs:
Agents lists real host-owned threads with a start button; Files shows the
[directory explorer](~acta/technical-reference/execution/codebases). Selecting
a file only highlights it; selecting a thread opens its CAT output. Mobile
thread selection closes the navigation drawer. The editor's example diff and
Terminal toggle remain illustrative.

Sample source can be edited in the page. Sample files and the selected sidebar
tab are held in the authenticated app layout and reset on reload or sign-out.
A live codebase selector sits directly above Agents / Files inside a shared
bordered control, with an Add codebase button. The old sample conversation and
draft composer have been replaced by a live CAT thread view with basic message
sending. The explorer reads directory names/types, not file contents. The
editor never writes source files or executes its sample terminal commands.

The editor has no durable buffers, language services, or filesystem writes.

## Live host presence

The host picker lists the signed-in human user's real registered hosts, with
online/offline presence. See [Code host
registration](~acta/technical-reference/execution/code-host). Codebase
registration, directory browsing, and Claude/Codex thread output are live; the
editor remains sample content.
