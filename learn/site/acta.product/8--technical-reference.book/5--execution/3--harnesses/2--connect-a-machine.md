---
title: "Connect a Machine"
description: >-
  Human CLI authorization, outbound harness connections, ephemeral presence,
  and shutdown behavior.
---

This section describes connect a machine in the Acta implementation.

```sh
acta login localhost:8081
acta harness
acta -p work harness
```

The command uses the normal selected/active profile URL and credentials,
including the normal `ACTA_TOKEN` override. It requires a human CLI session.
Agent sessions cannot run a hyperharness. There is no separate registration,
credential, permanent machine identifier, or added permission.

The command runs in the foreground until Ctrl+C or SIGTERM. It establishes an
outbound WebSocket to Acta and advertises the operating-system hostname. It
does not open a listening port on the development machine. HTTPS is required
except for loopback development; redirects are never followed with
credentials. `--json` emits connection states as JSON lines; ordinary output
reports connection, retry, and shutdown status. No credentials are included in
these messages.

User Settings → Harnesses displays the current user's connections, updated
live. Hostnames are labels, not identities. Multiple connections with the same
hostname remain separate entries. Each transport has an ephemeral UUID used
only for list keys. Disconnecting removes that entry, and reconnecting creates
a new one. There are no offline-machine records. Superusers see only their own
connections. If the browser loses its own connection, it clears the stale list
and shows a reconnecting state instead of pretending its cached list is still
current.
