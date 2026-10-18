---
title: "Adapter and Process"
description: >-
  Code host provider handshakes, readiness, process lifetime, and preview-
  only controls.
---

`internal/codeagents.Adapter` accepts a provider-neutral launch directory and
an output callback, returning a session with `Wait()` and `Send(ctx, input)`.
`Send` waits for the turn's terminal event; user acknowledgment is a separate
CAT frame. The host owns thread identity, lifetime, state, and frame ordering;
the adapter owns the provider process and converts its output into CAT
payloads. The host routes the required `provider` field through an explicit
adapter registry; the browser cannot name an executable.

## Claude Code

```sh
claude --print --input-format stream-json --output-format stream-json --verbose --replay-user-messages
```

`--print` selects the noninteractive protocol. The adapter starts reading both
output streams immediately and writes one initialization control request:

```json
{"type":"control_request","request_id":"<unique UUID>","request":{"subtype":"initialize"}}
```

The thread stays `starting` until stdout delivers a `control_response` whose
nested `response.request_id` matches and whose `response.subtype` is
`success`. Then it becomes `ready`. The acknowledgment is still emitted as a
`debug.unknown` frame before readiness changes, as are all early hook events.
Unrelated responses and stderr cannot acknowledge startup. A matching error or
malformed response fails startup; a missing acknowledgment times out after 30
seconds. Failed startup cancels and reaps the process. Provider exit before
acknowledgment is also a failure, even with a successful process exit code.

Stdin stays open after the handshake for explicit user messages. No permission
reply or stop button exists. Native startup hooks/configuration still apply;
the host does not bypass or override Claude permission settings. Startup hooks
can run before the client handshake completes. `ready` confirms
initialization, not a model turn or a successful authenticated model request.

## Codex

Each Codex thread gets its own private stdio process:

```sh
codex app-server --listen stdio://
```

The adapter sends a uniquely identified `initialize` request with
`clientInfo: {name: "acta_code", title: "Acta Code", version: "0.1.0"}`. After
a matching successful response it sends the `initialized` notification, then a
separate `thread/start` request with `params: {cwd: <selected root>}`. Only a
successful matching response containing a nonempty `result.thread.id` makes
the Acta thread `ready`. The adapter retains that native ID internally. All
output, including RPC responses and notifications understood during startup,
is retained as source output. The converter drops the known successful
initialization response (`id` plus `result` containing `userAgent`,
`codexHome`, `platformFamily`, and `platformOs`) from the CAT view. Hook
lifecycle events become `hook.*`, and recognized stderr diagnostics become
`debug.log`; unmapped output remains `debug.unknown`. Stderr, unrelated IDs,
and server requests cannot acknowledge startup. Errors, malformed matching
responses, early exit, and the 30-second total startup deadline fail the
thread and reap the process.

Startup sends no `turn/start`; explicit user submission does. Model,
permissions, auth, and configuration use the local Codex defaults; this UI
does not change them. A ready thread does not establish that an authenticated
model request would succeed. Codex can create its own local native history
under its normal configuration; Acta does not resume that history after a host
restart. See the official [app-server
protocol](https://learn.chatgpt.com/docs/app-server). The installed CLI's
generated schema also validates `initialize` and `thread/start` inputs.

## Shared lifecycle and UI

The thread view includes a Codex-inspired composer: rounded draft area,
attachment and approval controls, model/effort, microphone and circular send
button. Enter or the send button submits plain text; Shift+Enter inserts a
newline. Attachment, approval, model/effort, and microphone controls remain
preview-only. The temporary draft resets when switching threads or reloading.
One turn can run at a time: the send button is disabled until the provider
reports completion. Permissions are not answered or bypassed; a provider
waiting for permission can remain busy. There is no steering, queue,
interrupt, or permission UI.
