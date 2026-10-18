---
title: "User Input and Acknowledgments"
description: >-
  User input and acknowledgments in Acta, including state, mapping, and
  failure behavior.
---

`threads.send` accepts
`{id, codebase_id, thread_id, text, delivery:"start_turn"}`. The host
validates the input, marks the thread busy, records `input.message.user` with
`id`, `text`, and `delivery`, then submits asynchronously. The HTTP response
confirms host acceptance, not provider acknowledgment. Input is limited to 4
KiB of UTF-8 text, leaving space for JSON escaping within the existing relay
limit.

Claude receives a `type:"user"` envelope with the input UUID and plain text.
Its replayed top-level user message produces `message.user`. Codex receives
`turn/start` with `clientUserMessageId` and a text input; its `userMessage`
item produces `message.user`. That frame carries `input_id`, a message `id`,
`text`, and a native `turn_id` when supplied. Provider acknowledgment checks a
supplied correlation ID; when absent, exact plain-text matching is allowed
against the single pending input. Tool results and non-text content never
acknowledge a submission. Duplicate item lifecycle events produce only one
acknowledged CAT frame.

Both original and acknowledged frames stay in the live CAT buffer. The UI
hides an `input.message.user` when its corresponding `message.user` is
present. The debug toggle only hides `debug.*`; user frames and
`input.message.error` remain visible. Every provider line is preserved in the
source journal; unconverted events also appear as `debug.unknown`.

The host remembers up to 1,000 input IDs and text digests per thread for its
run, independently of frame eviction. An unchanged retry returns the existing
thread without another write, including after completion or failure. Reusing
an ID for different text fails. No automatic provider resend occurs after
uncertainty. The composer keeps its request ID for an unchanged failed HTTP
attempt.

Writes have a five-second bound and acknowledgment a 60-second bound. A stuck
write or absent acknowledgment cancels the provider process and records
visible uncertainty, rather than allowing an untracked late turn. After
acknowledgment, the turn can run until its provider terminal event or
process/host shutdown. Claude `result` and Codex `turn/completed` release the
busy state; model/tool errors remain in raw debug output. Rejection, missing
acknowledgment or premature process exit produces `input.message.error`,
linked to the input ID.

The host resolves the executable from PATH and accepts only an absolute path.
Provider credentials/configuration are inherited normally, while `ACTA_TOKEN`
is excluded from the child environment. Failures and exits update thread
state. Stopping the host cancels and reaps its children; on Unix it kills
their process groups too. Browser disconnects, HTTP timeouts, and transient
relay reconnects do not own or cancel thread processes.

See the official [CLI reference](https://code.claude.com/docs/en/cli-usage)
and [streaming documentation](https://code.claude.com/docs/en/headless).
