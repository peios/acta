# Thread runtime and UI refactor — 8 September 2026

Scope: the current provider adapters, frame contract/projections, thread UI and
hyperharness delivery/lifecycle. Preserve the existing product behaviour and
provider processes; no new tools, approvals, compaction UI or provider features.
Baseline source snapshot: `/tmp/acta2-refactor-baseline.tgz` (local review aid).

## Findings and work

1. The adapter entry point and shared state/value decoding live in `codex.go`,
   despite owning both providers. Checkpoint copying ignores errors. Parsing
   accepts a valid first JSON object with trailing garbage. A handler that emits
   data and then rejects the envelope can have those partial outputs promoted
   to Resolved. Separate provider-neutral transaction/bundle construction from
   provider dispatch and make unsuccessful mapping leave checkpoint state intact.
2. Tool, thinking and turn projections each repeat capture identity/deduplication
   and run/turn association logic. Consolidate these rules without merging their
   different lifecycle semantics. Visibility policy belongs beside projection,
   not in the route component.
3. The thread route mixes sender lifetime, frame/control polling, asynchronous
   navigation state, header rendering and every feed renderer. Extract a
   disposable per-thread session controller and presentation components. Guard
   results from previous routes and prevent overlapping polling requests.
4. The hyperharness controller mixes process lifecycle, Codex startup, native RPC
   parsing and durable frame delivery. Preserve its single controller lock and
   persist-before-publish ordering; separate those responsibilities. Late
   configuration receipts must match the complete expected run/request identity.

## Outcomes

The four boundaries above are implemented. Checkpoint and frame storage schemas
remain unchanged. No migrations or history rewriting were needed.

Confirmed defects fixed, with regression tests:

- A Claude handler could emit provisional configuration before rejecting an
  envelope; the mapper then labelled the partial result Resolved. Unknown mapping
  now restores the incoming checkpoint and discards speculative outputs.
- JSON captures with trailing garbage or multiple objects were accepted. They
  now remain Unknown. Checkpoint cloning errors are returned, and JSON number
  lexemes remain intact through the checkpoint round trip.
- Late configuration reconciliation could accept a bare command UUID because
  `TrimPrefix` does not require the prefix to exist. Receipts must now match
  the complete run/request prefix and persisted command.
- Tool/thinking interruption matching omitted the thread identity. Shared
  run/turn matching now includes it, so reused provider IDs cannot interrupt
  another thread's projected items.

Async safeguards now have explicit tests: disposed route sessions cannot publish
late frame/control results; wrong-thread batches cannot partially advance the
read cursor; repeated pages deduplicate; frame reads, control-result polls and
message-result polls do not overlap themselves. Thread inventory refreshes also
ignore superseded responses. Timers are disposed with their owning route.

## Validation

- Baseline: 20 frontend test files and the Go suite passed before edits.
- Refactor: 22 frontend test files pass; Svelte check reports zero errors and
  warnings; production frontend build passes.
- `go test -race ./...` and `go vet ./...` pass. The PostgreSQL integration
  package also passes with the race detector and an explicit development
  database URL, using isolated temporary schemas.
- Frontend formatting and Go formatting checks pass. One pre-existing wrapping
  issue in `thread-usage.js` was formatted without changing its behavior.
- Built both Go binaries and restarted the server on 8081 plus the hyperharness.
  Detached provider pipe PID 12075 was preserved.
- Browser review of existing Codex history: user/assistant messages, tool edits,
  inline diff, collected turn diff, debug toggle and Unknown Frame visibility.
- Browser review after navigating to Claude history: hooks, thinking, Read,
  Write permission denial, usage gauges and composer. No paid provider turn
  was submitted for this refactor.

## Limits

This is a refactor of the current thread slice, not a claim that every runtime
failure has been exhausted. The transport/checkpoint protocol and detached-pipe
ownership model remain as before. Existing captured frames are replayed as
stored; previously Unknown frames are not retroactively remapped. New approval,
compaction and subagent UI remain separate feature work.
