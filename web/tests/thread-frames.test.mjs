import test from "node:test";
import assert from "node:assert/strict";
import {
  visibleFeed,
  uniqueFrames,
  frameId,
} from "../src/lib/thread-frames.js";
import { threadFeed } from "./reference/thread-feed.js";
const f = (thread_id, kind, data, sequence) => ({
  thread_id,
  run_id: "same-run",
  sequence,
  output_index: 1,
  kind,
  data,
  received_at: "2026-09-08T12:00:00Z",
});
test("captures keep sub-output identity while exact replays deduplicate", () => {
  const a = f("a", "message/assistant", {}, 1),
    b = { ...a, output_index: 2 };
  assert.equal(uniqueFrames([a, b, a]).length, 2);
  assert.notEqual(frameId(a), frameId(b));
});
test("turn completion cannot interrupt another thread sharing native identities", () => {
  const frames = [
    f(
      "a",
      "tool/call",
      { tool_id: "tool", turn_id: "t", status: "running" },
      1,
    ),
    f(
      "a",
      "thinking/started",
      { thinking_id: "think", turn_id: "t", text: "" },
      2,
    ),
    f("b", "turn/completed", { turn_id: "t", outcome: "completed" }, 3),
  ];
  const items = threadFeed(frames);
  assert.equal(items.find((i) => i.kind === "tool-call").status, "running");
  assert.equal(items.find((i) => i.kind === "thinking").interrupted, false);
});
test("debug visibility keeps Unknown visible and state-only frames out of the transcript", () => {
  const items = [
    "debug/unknown",
    "debug/local",
    "thread/status",
    "usage/context",
  ].map((kind, i) => ({
    kind: "frame",
    frame: f("a", kind, {}, i),
    id: String(i),
  }));
  assert.deepEqual(
    visibleFeed(items, false).map((i) => i.frame.kind),
    ["debug/unknown"],
  );
  assert.deepEqual(
    visibleFeed(items, true).map((i) => i.frame.kind),
    ["debug/unknown", "debug/local"],
  );
});

function thought(id, overrides = {}) {
  return {
    kind: "thinking",
    id,
    frame: f("a", "thinking/completed", {}, Number(id)),
    turn: "turn",
    text: `Thought ${id}`,
    tokens: 10,
    started: "2026-09-08T12:00:00Z",
    ended: "2026-09-08T12:00:01Z",
    completed: true,
    interrupted: false,
    ...overrides,
  };
}
test("consecutive thinking combines snapshots without mutating persisted entries", () => {
  const a = thought("1"),
    b = thought("2", { ended: "2026-09-08T12:00:04Z" });
  const [group] = visibleFeed([a, b], false);
  assert.equal(group.id, "1");
  assert.deepEqual(group.memberIds, ["1", "2"]);
  assert.equal(group.text, "Thought 1\n\nThought 2");
  assert.equal(group.tokens, 20);
  assert.equal(group.ended, b.ended);
  assert.equal(group.completed, true);
  assert.equal(a.text, "Thought 1");
  assert.equal(a.memberIds, undefined);
  const live = { ...b, completed: false, ended: null, tokens: null };
  const [streaming] = visibleFeed([a, live], false);
  assert.equal(streaming.id, group.id);
  assert.equal(streaming.completed, false);
  assert.equal(streaming.interrupted, false);
  assert.equal(streaming.ended, null);
  assert.equal(streaming.tokens, null);
  assert.equal(visibleFeed([a, b], false)[0].completed, true);
});
test("visible items and turn/run/lane boundaries separate thinking groups", () => {
  const a = thought("1"),
    b = thought("2");
  for (const kind of [
    "debug/unknown",
    "message/assistant",
    "tool/call",
    "turn/completed",
  ]) {
    const between = {
      id: "between",
      kind: "frame",
      frame: f("a", kind, {}, 2),
    };
    assert.equal(visibleFeed([a, between, b], false).length, 3);
  }
  for (const other of [
    { ...b, turn: "other" },
    { ...b, frame: { ...b.frame, run_id: "other" } },
    { ...b, frame: { ...b.frame, thread_id: "other" } },
    { ...b, frame: { ...b.frame, lane_id: "child" } },
  ])
    assert.equal(visibleFeed([a, other], false).length, 2);
  const debug = {
    id: "debug",
    kind: "frame",
    frame: f("a", "debug/local", {}, 2),
  };
  assert.equal(visibleFeed([a, debug, b], false).length, 1);
  assert.equal(visibleFeed([a, debug, b], true).length, 3);
});
test("older pages extend thinking groups while retaining member anchors", () => {
  const a = thought("1"),
    b = thought("2"),
    c = thought("3");
  const recent = visibleFeed([b, c], false)[0];
  const complete = visibleFeed([a, b, c], false)[0];
  assert.ok(complete.memberIds.includes(recent.id));
  assert.equal(complete.text, "Thought 1\n\nThought 2\n\nThought 3");
});

test("thinking text follows its own turn ending, not block completion or other lanes", async () => {
  const { finishedThinkingTurns, thinkingTurnKey } =
    await import("../src/lib/thread-thinking.js");
  const a = thought("1");
  const key = thinkingTurnKey(a.frame, a.turn);
  assert.equal(finishedThinkingTurns([a]).has(key), false);
  const ending = {
    kind: "turn-ended",
    id: "end",
    frame: { ...a.frame, data: { turn_id: a.turn, outcome: "interrupted" } },
  };
  for (const frame of [
    { ...ending.frame, run_id: "other" },
    { ...ending.frame, lane_id: "child" },
    { ...ending.frame, thread_id: "other" },
    { ...ending.frame, data: { turn_id: "other" } },
  ])
    assert.equal(
      finishedThinkingTurns([a, { ...ending, frame }]).has(key),
      false,
    );
  assert.equal(finishedThinkingTurns([a, ending]).has(key), true);
});
