---
title: "Provider Discovery"
description: >-
  Bounded read-only provider probes, sign-in observations, sanitized
  reports, and refresh timing.
---

Each connected machine in User Settings → Harnesses lists **Codex** and
**Claude Code**. Both remain visible when not installed. Each row separates
the installed version from sign-in state. Initial checks show Checking;
failures and unrecognized output show Unknown with a short explanation on
hover.

The harness finds executables on its own `PATH` and runs read-only probes:

- Codex: `codex --version`, `codex login status --help`, `codex login status`.
- Claude Code: `claude --version`, `claude auth status --help`, `claude auth
  status`.

Discovery invokes a status command only after its help preflight identifies
the expected command. An unsupported CLI is not probed by guessing a command,
because older versions can interpret unrecognized commands as conversational
prompts. An unrecognized version skips the sign-in probe. Versions are
reported, not certified compatible with future execution adapters.

Sign-in is the local CLI's report of stored authentication. It does **not**
verify network access, subscription entitlement, model availability,
credential freshness, or remaining usage. No sign-in/sign-out/install
operation, prompt, or model turn is performed. The harness never reads
provider credential files directly.

Each provider is checked immediately, then 30 seconds after its previous check
finishes. Independent serial loops prevent overlapping checks and allow one
provider to update while the other is slow. Each complete check has a
five-second deadline; command output is limited to 32 KiB per stream. stdin is
closed, no shell is used, and `ACTA_TOKEN` is removed from the child
environment. Unix cancellation kills the probe process group, including
children that retain its output pipes. Provider discovery is independent of
the server reconnect loop and heartbeat.

Only adapter ID, installation state, normalized version, sign-in state, and
fixed issue codes are advertised. Native status output can contain personal
information; it is decoded locally and is never sent to Acta or logged. The
server validates these fields and retains only the current connection's
snapshot. A provider change updates the same connection entry; reconnecting
sends the latest complete snapshot. No durable provider inventory or machine
identity is introduced.

The CLI and server use `acta-harness-v7`. Existing processes retain the
protocol and assets they loaded until restarted or reloaded. Provider
installation or sign-in changes are detected on the next check.

Official command references: [Codex
CLI](https://developers.openai.com/codex/cli/reference/) and [Claude Code
CLI](https://code.claude.com/docs/en/cli-usage).
