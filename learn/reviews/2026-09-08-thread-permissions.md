# Thread permission controls — 8 September 2026

Implemented in ACT-63: three provider-confirmed permission presets beside the
model selector, and one shared approval interaction rendered both inline and in
an automatically opened composer popup. See [the behaviour contract](../thread-approvals.md).

## Validation

- Full Go suite with the race detector and PostgreSQL integration database passed;
  `go vet ./...` passed.
- All 23 frontend test files passed; Svelte reported zero errors and warnings;
  formatting and production build passed.
- Adapter tests cover exact numeric request IDs, stable per-run approval identity,
  native reply shapes, foreign-thread rejection and resolved request identity.
- Controller tests cover immutable decisions, stale/resolved requests, restart
  replay, and interruption after the native write but before its result is saved.
  The latter reuses the persisted reply and write ID after native resolution.
- Browser-state tests cover shared decisions, command-history hydration and an
  unsent decision surviving refresh without changing its answer.

Live checks on localhost:8081 used Codex 0.153.4 with gpt-5.4-mini and Claude Code
2.1.263 with claude-haiku-4-5-20251001, in existing scratch threads only:

- Claude Write produced a real host approval. Dismissing and reopening the
  composer popup preserved the request. Approving created the requested scratch
  file. A second Write was denied inline; the popup resolved and that file did
  not exist.
- Codex confirmed manual review, then requested approval for a harmless printf
  command. Acta server and hyperharness were restarted while the request waited;
  the detached pipe and provider stayed alive. A fresh browser recovered the
  actionable popup. Approving completed the command and updated the inline item.
- Codex was restored to its previous automatic mode. Both scratch provider
  processes were returned to exited after the checks.
- All three mode mappings were accepted by native protocol probes without model
  turns. The real Claude Haiku session rejected automatic review as unavailable
  for that model; Acta retained its confirmed mode and displayed the rejection.

## Boundaries

Already-running Claude processes need a Kill/Resume once to acquire the new stdio
approval callback flags. Mode changes detect missing flags and explain this.
Existing user provider processes were preserved during deployment.

Approvals do not introduce persistent allow rules. Codex explicit permission
grants use its turn-only scope and say so in the request title. Native input
questions and MCP elicitation remain separate, unsupported contracts. Rejected
tool calls are terminal outcomes, not requests that can be retroactively approved.

## Automatic-review follow-up

Added `approval/review` snapshots for Codex automatic permission reviews, and a
compact expandable item with the provider's decision, rationale, risk and user
authorization assessment. Its link opens the related tool operation. Start and
completion share one row at the first observed position; multiple reviews of a
single tool remain separate. Interrupted/disconnected reviews do not retain a
running spinner. Standalone warnings and goals remain Unknown pending their own
mapping decisions.

Validation used sanitized captures from Jack's actual review, with fixtures for
approved, denied, timed-out and aborted results, unknown states/sources and foreign
thread rejection. Feed tests cover duplicate/replayed starts, completed-only
history, run isolation, multiple reviews and turn interruption. All 24 frontend
test files passed. Adapter, hyperharness and thread race tests, adapter vet,
Svelte check, formatting and production build passed. The running/approved/denied
UI states were inspected on 8081 using a temporary preview removed afterwards;
no additional paid provider turn was needed. Provider processes were preserved.
