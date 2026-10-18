---
title: "Git Push Notifications"
description: >-
  Push notification identities, provider attribution, and displayed
  repository results.
---

`vcs/push` records a provider-reported push with `branch` and `cwd` strings
and a nullable `turn_id`. It is a chronological event, not the current branch
or working directory singleton. The UI shows a compact repository/branch
notice. Claude's `system/vcs_state_changed` with `kind: push` maps here; other
event kinds remain unreviewed. Tool execution heartbeats update the existing
`tool/call` when needed and otherwise remain `debug/local`.
