import { test } from "node:test";
import assert from "node:assert/strict";
import { mergeCodeFrames } from "../src/lib/code-frame-projection.js";

test("hook supersession updates an old frame without moving the completion", () => {
  const start = { seq: 1, change_seq: 1, id: "start", type: "hook.started" };
  const message = { seq: 2, change_seq: 2, id: "message" };
  const updated = { ...start, change_seq: 3, superseded_by: "done" };
  const done = { seq: 3, change_seq: 4, id: "done", supersedes: "start" };
  const result = mergeCodeFrames([start, message], [updated, done], 1);
  assert.deepEqual(result.frames, [updated, message, done]);
  assert.equal(result.dropped, false);
  assert.deepEqual(
    mergeCodeFrames(result.frames, [start, done], 1).frames,
    result.frames,
  );
  // A page containing only the initiating record still carries its hidden state.
  assert.equal(
    mergeCodeFrames([], [updated], 1).frames[0].superseded_by,
    "done",
  );
});

test("change-order pagination merges into chronological order", () => {
  const message = { seq: 2, change_seq: 2 };
  const start = { seq: 1, change_seq: 3, superseded_by: "done" };
  const done = { seq: 3, change_seq: 4 };
  const page = mergeCodeFrames([], [message, start], 1);
  assert.deepEqual(mergeCodeFrames(page.frames, [done], 1).frames, [
    start,
    message,
    done,
  ]);
});

test("host eviction removes stale running frames even on an empty update page", () => {
  const result = mergeCodeFrames(
    [
      { seq: 1, change_seq: 1 },
      { seq: 5, change_seq: 5 },
    ],
    [],
    5,
  );
  assert.deepEqual(result.frames, [{ seq: 5, change_seq: 5 }]);
  assert.equal(result.dropped, true);
});

test("browser retention counts UTF-8 bytes and trims oldest positions", () => {
  const records = Array.from({ length: 8 }, (_, i) => ({
    seq: i + 1,
    change_seq: i + 1,
    raw: "界".repeat(60000),
  }));
  const result = mergeCodeFrames([], records, 1);
  assert.equal(result.dropped, true);
  assert.equal(result.frames.at(-1).seq, 8);
  assert.ok(
    result.frames.reduce(
      (sum, f) => sum + new TextEncoder().encode(JSON.stringify(f)).length,
      0,
    ) <=
      1024 * 1024,
  );
});
