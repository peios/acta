---
title: "Hosted Artifacts"
description: >-
  Claude-hosted artifact publication, native permission checks, launch
  configuration, and hosting boundaries.
---

Acta's Claude harness opts into the provider's native `Artifact` tool. The
provider publishes interactive HTML or Markdown pages on claude.ai; Acta
retains its ordinary tool call, approval and assistant-message history. Open
the returned link to use the artifact. Acta does not host, proxy, or embed the
artifact content.

The opt-in is passed as session-local `--settings` environment configuration:
`CLAUDE_CODE_ARTIFACT=1`. Claude Code 2.1.267 otherwise excludes the tool from
print/SDK sessions, even with `enableArtifact: true`. This opt-in was
confirmed against that installed runtime; it is not a guarantee for all future
versions. The harness respects an explicit inherited `CLAUDE_CODE_ARTIFACT`
value. `CLAUDE_CODE_ARTIFACT_AUTO_OPEN=0` prevents opening a browser on the
development machine; use the returned link in the client instead.

Native authentication, account eligibility, organization policy, permission
mode, file-read restrictions, and disable settings still apply. In Ask mode,
publishing presents the existing Acta approval popup with the provider's
explanation that the file will be uploaded to Anthropic. The harness never
approves a publication automatically. Native automatic approval modes remain
under the user's selected provider policy. An explicit native disable such as
`enableArtifact: false` or `CLAUDE_CODE_DISABLE_ARTIFACT=1` is not bypassed.

Artifacts are private initially. Sharing is managed on claude.ai. To update,
republish the same source path in the same conversation or pass the existing
artifact URL. Reading the file is required even when the only requested action
is publication; disabling the native Read tool prevents publishing.

This applies to newly spawned Claude processes, including resumed sessions.
Already-running processes keep their launch configuration. When convenient,
Kill then Resume the thread to enable the tool; Stop only interrupts a turn
and does not reload the launch configuration. Updating the hyperharness
supervisor requires no detached-pipe or running-provider restart.

The tested print session reported that its artifact comment/live-subscription
watch was skipped. Publishing and republishing work; automatic ingestion of
artifact comments into the session is not provided by this integration. Codex
artifact support and custom Acta-hosted artifacts are not implemented.
