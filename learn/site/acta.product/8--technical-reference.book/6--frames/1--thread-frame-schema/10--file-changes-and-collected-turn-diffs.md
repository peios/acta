---
title: "File Changes and Collected Turn Diffs"
description: >-
  Operation patches, combined turn snapshots, result authority, and
  incomplete-diff handling.
---

Codex `fileChange` items use `tool/call` snapshots with a `changes` array
(null for tools without supplied changes). Each entry preserves the path,
operation kind, optional move destination, diff text, and format. Add/delete
content is rendered as whole-file additions/removals; updates are unified
patches. One operation can affect multiple files. Its changes stay attached to
its tool ID, with the provider's lifecycle state. Changes on unfinished or
unsuccessful operations are labeled Requested changes, not presented as
successful writes. The shared expandable tool UI shows colored diffs and full
paths. Patch-looking file contents are not misinterpreted as diff headers, and
source text is escaped.

Codex `turn/diff/updated` maps to `turn/diff` with `{turn_id, diff}`. This is
the provider's combined turn snapshot, not an append-only patch and not a
delta for the most recent tool. Every capture retains its debug frame. The UI
associates the latest snapshot by thread, provider run and turn with the
existing Turn ended divider; expanding it shows Turn changes. Late snapshots
enrich the same divider, replays do not roll it back, and an empty snapshot
clears previous changes. The Codex adapter uses the provider's combined diff
rather than concatenating operation patches. Providers that do not supply one
have no collected-diff section. Historical Unknown Frames remain unchanged;
these mappings apply to newly captured provider output.
