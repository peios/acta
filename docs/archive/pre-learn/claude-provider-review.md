# Claude provider integration review

ACT-63 · 2026-09-08

Claude Code now runs through the same local hyperharness, detached process pipe,
server discovery and existing My Agents UI as Codex. This review compares the
features that Acta already renders. Tools, subagents and other features deferred
for both providers are not counted as missing Claude core features.

## Implemented

| Existing Acta surface | Claude implementation |
| --- | --- |
| Create, discover, Kill, Resume | Dedicated streaming-JSON CLI process; stable native session UUID; local stored-user-history check establishes commitment. Neither creation nor resumption sends an automatic message. |
| Sending / delivery confirmation | Durable pipe write identity and echoed user UUID. A completed replay acknowledgement removes the local Sending bubble. Local model-command receipts are not user submissions. |
| Assistant text streaming | Text content blocks and deltas assemble one message. Completion finalizes it. Provider block snapshots do not duplicate streamed text or overwrite earlier blocks. |
| Working state and turn-end rule | Command lifecycle and session state map to thread status. Result supplies terminal outcome and elapsed time. |
| Model list, effort slider, fast button | Provider catalogue; aliases resolve to actual model IDs; duplicate aliases are collapsed. Unsupported effort shows Not applicable; Default leaves the choice to Claude. Apply settings, then read back effective values without a model turn. |
| MCP startup / error rows | Observed init/status snapshots. Pending status is polled until settled. Authentication-required and failed connections produce error rows. No invented startup history. |
| Context gauge | Read-only context summary, visibly marked estimated; capacity changes with the applied model. |
| Account gauges | Five-hour, weekly and model-scoped allowance windows. Partial rate-limit events retain other observed windows. |
| Turn token details | Last model request's input, cache and output counters. Unknown counters remain absent. |
| Debug feed | Every captured provider line retains its debug frame. Unknown Frames remain visible with debug disabled. |

The provider's own local authentication, MCP configuration and hooks are used.
The Acta server does not launch Claude or hold its login credentials. The generic
pipe has no provider interpretation. Normal and detached modes use the same
engine. The wire protocol is `acta-harness-v7`; update server and CLI together.

## Where existing UI cannot reproduce Codex behavior exactly

| Area | Evidence / limitation | Current behavior |
| --- | --- | --- |
| Commentary versus final answer | Claude assistant frames have no equivalent phase discriminator. Being completed does not establish whether text was commentary. | `phase: null`; normal assistant text, with no invented classification. |
| User in-progress transition | The observed Claude echo is a completed replay, rather than separate in-progress/completed user snapshots. | Sending remains local until the correlated echo; then the message becomes completed. A grey intermediate provider phase may never be visible. |
| Effort | The catalogue can advertise no effort settings and does not advertise a per-model default level. | No fake Low/High settings for unsupported models. A provider-default choice is separate from explicitly selecting Low. |
| Permission boundaries | Approval mode is reported; that mode alone does not prove OS filesystem or network confinement. | Approval/work mode maps where known. Filesystem and network remain unknown. This is a summary, not an authorization grant. |
| Exact context occupancy | The cheap summary is an estimate, not an exact recount of the next model request. | The gauge says estimated. No additional model/token-count request is made. |
| Thread-lifetime / reasoning usage | `result.usage` is per-turn main-loop usage. `modelUsage` is cumulative for the query process and resets on resume or clear. Separate thinking-token events contain estimates, not the same measured counter. | Do not relabel either as thread-lifetime usage. Last-request counters are available; lifetime and separate measured reasoning counters remain null. |
| Every MCP startup transition | Init and status queries can first observe a server already ready or failed. A short-lived starting state can be missed. | Render observed state honestly; no fake loading delay or backdated transition. |

## Claude-specific information that needs UI decisions

These are genuine representation differences, rather than the shared backlog:

1. **Fast mode has more than two states.** Claude reports on, off and cooldown,
   plus availability reasons. The lightning button represents actual on/off and
   reports a failed enable attempt, but cannot show an enabled preference that is
   temporarily cooling down, a cooldown reset, or persistent eligibility detail.
   A future small status/popover extension would fit the current design. The live
   account rejected fast mode because extra usage was disabled; Acta preserved
   off and reported that reason. Billing settings were not changed to test it.
2. **Estimated monetary cost and paid extra usage.** Claude reports
   `total_cost_usd` and a currency-aware spend/limit/balance structure, including
   paid-overage state. Current gauges describe token occupancy and percentage
   allowance; their generic credit field is not a currency-aware billing UI.
   A cost detail area could represent this, with explicit scope and estimate
   labels. Do not sum cumulative result costs: they cover the current query
   process, reset on resume/clear, and are not an invoice. The reviewed Codex
   app-server surfaces supply token/rate-limit information rather than this same
   monetary estimate.

Model-scoped allowance windows already fit the current arbitrary-window gauge
model, so they are not a UI gap. Model aliases and models without effort required
small capability-aware adjustments, which are implemented above.

The Fable allowance uses **Fable** as its visible gauge label to distinguish it
from the general **7d** allowance. Its seven-day window remains in the popover.
The additional fast-mode states are deferred for the medium term, as Jack does
not use Claude fast mode.

## Intentionally excluded from the core-gap list

Tool execution, approvals, subagents, reasoning content, compaction UX,
attachments, richer command handling and other capabilities not yet represented
for either provider remain future work. Their unreviewed provider frames remain
Unknown Frames; they have not been silently dropped or auto-approved.

The local account has SessionStart hooks and several claude.ai connectors.
Live review therefore showed hook/thinking Unknown Frames and legitimate
connector authentication errors. Those do not mean text sending failed. The
first review thread also retains one pre-fix model-receipt bubble and two empty
background-task Unknown Frames: captured history is deliberately not rewritten
when an adapter is corrected. Later model receipts are consumed locally and
empty background-task lists are recognized as irrelevant.

## Reliability and validation

- Real Claude Code **2.1.263**: initialization, settings/catalogue/version/status
  controls, a text turn and read-only usage summaries.
- Explicit opt-in native integration: start, initial message, stored-history
  commitment, catalogue/settings readback, Kill, Resume and a message afterward.
  Two small Haiku turns; no tool calls. All completed successfully.
- Browser on **localhost:8081**: Claude discovery, model changes, no-effort model,
  fast-mode rejection, MCP ready/authentication-error rows, gauges and a streamed
  reply. Browser Kill and Resume returned the same thread to running without
  another Test Message. Restarting the hyperharness preserved the provider process.
- PostgreSQL integration checks both Codex and Claude discovery/frame persistence.
  This caught a remaining Codex-only advertisement check, now centralized.
- Fake-provider tests cover immutable commands, reattachment without duplicate
  sending, resolved-model readback and stored-history validation. Adapter tests
  cover observed text frames, multiple text blocks, numeric usage preservation,
  unknown shapes, session isolation and provider-local receipts.
- Full Go suite including isolated PostgreSQL schemas passed. Targeted adapter /
  controller race tests passed. Frontend checks, tests and production build
  passed. The browser outbox fix was verified with a fresh send: its
  completed-only acknowledgement released the composer, and another draft could
  be typed immediately without reloading.

Limits of validation: fast mode becoming genuinely active and later entering
cooldown was not forced; the account was ineligible. MCP startup/error mapping is
based on native snapshots and observed authentication failures, not an exhaustive
network-fault campaign. New provider versions may add or change wire shapes;
unsupported data stays visible rather than being guessed into the schema.

## Sources

- [Claude Agent SDK TypeScript reference](https://code.claude.com/docs/en/agent-sdk/typescript)
- [Claude streaming output](https://code.claude.com/docs/en/agent-sdk/streaming-output)
- [Claude CLI reference](https://code.claude.com/docs/en/cli-reference)
- Official published `@anthropic-ai/claude-agent-sdk` **0.3.263** declarations,
  inspected without installing or running package scripts. These document
  settings controls, model capabilities, fast states and counter semantics.
- [Codex app-server reference](https://learn.chatgpt.com/docs/app-server), compared
  with the installed native protocol declarations.
- Sanitized observed text-turn fixture:
  `internal/threadadapter/testdata/claude-text-turn.json`. Private hook output and account identity fields are not copied into the fixture or this report.


## Local configuration diagnostics

Native settings and MCP-status queries can include environment variables and
transport headers. The review found authorization headers in the latter.
Server-bound debug copies now explicitly set `redacted: true`: settings retain
only applied model/effort, initialization retains reviewed capability/status
fields, and MCP status excludes transport configuration. Original captures remain
in the private local pipe. A regression test injects credential values and proves
they do not occur in any emitted frame from those configuration responses.

The local review database's 70 pre-fix configuration debug copies were minimized
as well. Conversation frames and lifecycle identities were preserved. This is an
explicit exception to byte-exact remote raw diagnostics, not a change to the
original local capture. It is not a general secret scrubber for arbitrary model
messages or provider stderr.
