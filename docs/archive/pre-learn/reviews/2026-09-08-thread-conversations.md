# Persisted conversation and reverse-scroll review

ACT-63, 8 September 2026.

Implemented server-side conversation assembly, current state, atomic ingestion,
startup backfill and newest-first paginated loading. Lifecycle starts/completions
remain attached to their persisted item. Existing rendering and the agreed
hook/MCP placement rules are preserved.

## Evidence

- Full Go suite with race detection and isolated PostgreSQL integration schemas
  passed; `go vet ./...` passed.
- New database integration test covers newest/older page boundaries, current
  configuration independent of history, mutation of an older offscreen item,
  replay safety, owner isolation, debug pagination, forced projection-write
  rollback, and startup replay equivalence.
- Frontend: 24 test files passed, Svelte check reported zero errors/warnings,
  formatting check and production build passed.
- Offline replay of 11,877 normalized captures across six existing threads:
  all 403 visible items matched the previous assembler's ordering and content.
  Comparison ignores replacement IDs and envelope metadata, and compares JSON
  arguments semantically. No provider prompts were sent for this validation.
- On local deployment, all eight stored threads reached conversation version 1.
  The database backup is `/tmp/acta-before-conversation-migration.dump`.
- In-app browser on 8081: Codex opened with 50 items, scrolled to 100 then all
  124; older loading preserved the viewport position. Debug mode opened its own
  50-item page and retained Unknown Frames. Claude opened with 50 items and
  independent current model, context and Fable usage controls. Model popup
  populated successfully. No browser warnings/errors were reported.
- Initial Markdown resizing exposed a 112.5px bottom gap. A content resize
  observer corrected this; measured bottom gap was within half a CSS pixel in
  both providers and debug mode.
- Server and hyperharness deployment preserved the detached pipe and providers.
  The subsequent scroll-only deployment restarted only the server.

## Limits

History pages are bounded by item count, not raw payload size. Loaded items are
retained for the current route; DOM virtualization is not part of this slice.
This migrates existing normalized behaviour, not historical Unknown frames into
newly recognized provider mappings. Live provider prompting was not repeated;
stream/update/replay correctness was tested through stored captures, reducer and
client tests.
