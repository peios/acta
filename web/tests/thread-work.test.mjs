import test from "node:test";
import assert from "node:assert/strict";
import { groupOlderWork } from "../src/lib/thread-work.js";
const frame = (turn, extra = {}) => ({
  thread_id: "thread",
  run_id: "run",
  lane_id: "",
  data: { turn_id: turn },
  ...extra,
});
const tool = (id, turn = "old", status = "completed") => ({
  kind: "tool-call",
  id,
  turn,
  status,
  interruptedByTurn: false,
  frame: frame(turn),
  data: {
    started_at: "2026-09-09T10:00:00Z",
    completed_at: "2026-09-09T10:00:10Z",
  },
});
const thinking = (id, turn = "old") => ({
  kind: "thinking",
  id,
  turn,
  frame: frame(turn),
  completed: true,
  interrupted: false,
  started: "2026-09-09T10:00:05Z",
  ended: "2026-09-09T10:00:12Z",
});
test("old contiguous tools and thoughts collapse with overlapping elapsed time counted once", () => {
  const items = [tool("a"), thinking("b"), tool("current", "new")];
  const groups = groupOlderWork(items, frame("new"));
  assert.equal(groups.length, 2);
  assert.equal(groups[0].kind, "worked");
  assert.equal(groups[0].durationMs, 12000);
  assert.deepEqual(groups[0].memberIds, ["worked:a", "a", "worked:b", "b"]);
  assert.deepEqual(groups[0].items, items.slice(0, 2));
  assert.equal(groups[1], items[2]);
  assert.equal(items[0].kind, "tool-call");
});
test("new turn singleton collapses prior activity even before new history items arrive", () => {
  const items = [tool("a"), thinking("b")];
  assert.deepEqual(groupOlderWork(items, frame("old")), items);
  assert.equal(groupOlderWork(items, frame("new"))[0].kind, "worked");
});
test("messages, approvals, questions, errors and unfinished background tools break groups", () => {
  for (const between of [
    { kind: "user-message", id: "between", frame: frame("old") },
    { kind: "assistant-message", id: "between", frame: frame("old") },
    {
      kind: "frame",
      id: "between",
      frame: { ...frame("old"), kind: "approval/request" },
    },
    {
      kind: "frame",
      id: "between",
      frame: { ...frame("old"), kind: "question/request" },
    },
    {
      kind: "frame",
      id: "between",
      frame: { ...frame("old"), kind: "debug/unknown" },
    },
    ...[
      "failed",
      "interrupted",
      "permission_denied",
      "declined",
      "running",
      "background",
      "pending",
    ].map((status) => tool("between", "old", status)),
    { ...thinking("between"), completed: false, interrupted: true },
  ]) {
    const groups = groupOlderWork(
      [tool("a"), between, thinking("b")],
      frame("new"),
    );
    assert.equal(groups.length, 3);
    assert.equal(groups[1], between);
  }
});
test("groups respect run/lane/turn boundaries and missing timing is not guessed", () => {
  const a = tool("a");
  for (const b of [
    { ...thinking("b"), turn: "different" },
    { ...thinking("b"), frame: frame("old", { run_id: "other" }) },
    { ...thinking("b"), frame: frame("old", { lane_id: "child" }) },
  ]) {
    assert.equal(groupOlderWork([a, b], frame("new")).length, 2);
  }
  const b = { ...thinking("b"), started: null, memberIds: ["b", "c"] };
  const group = groupOlderWork([a, b], frame("new"))[0];
  assert.equal(group.durationMs, null);
  assert.deepEqual(group.memberIds, ["worked:a", "a", "worked:b", "b", "c"]);
});
test("completed background work can join a group and prior anchors survive older paging", () => {
  const a = tool("a"),
    b = tool("b");
  const recent = groupOlderWork([b], frame("new"))[0];
  const older = groupOlderWork([a, b], frame("new"))[0];
  assert.ok(older.memberIds.includes(recent.id));
});

test("Claude missing provider start uses the persisted observed lifecycle interval", () => {
  const a = {
    ...tool("a"),
    started_at: "2026-09-09T11:27:39.955Z",
    completed_at: "2026-09-09T11:27:43.110Z",
    data: { started_at: null, completed_at: "2026-09-09T11:27:43.109Z" },
  };
  const b = {
    ...tool("b"),
    started_at: "2026-09-09T11:27:43.076Z",
    completed_at: "2026-09-09T11:27:44.182Z",
    data: { started_at: null, completed_at: "2026-09-09T11:27:44.181Z" },
  };
  assert.equal(groupOlderWork([a, b], frame("new"))[0].durationMs, 4227);
  assert.equal(
    groupOlderWork(
      [{ ...a, data: { started_at: "invalid", completed_at: null } }],
      frame("new"),
    )[0].durationMs,
    3155,
  );
});
test("complete provider timing takes precedence over observed lifecycle timing", () => {
  const a = {
    ...tool("a"),
    started_at: "2026-09-09T09:00:00Z",
    completed_at: "2026-09-09T11:00:00Z",
  };
  assert.equal(groupOlderWork([a], frame("new"))[0].durationMs, 10000);
});

test("combined thinking retains its whole observed lifecycle for duration fallback", async () => {
  const { groupThinking } = await import("../src/lib/thread-thinking.js");
  const a = {
    ...thinking("a"),
    text: "a",
    started: null,
    ended: null,
    started_at: "2026-09-09T10:00:00Z",
    completed_at: "2026-09-09T10:00:02Z",
  };
  const b = {
    ...a,
    id: "b",
    text: "b",
    started_at: "2026-09-09T10:00:02Z",
    completed_at: "2026-09-09T10:00:08Z",
  };
  assert.equal(
    groupOlderWork(groupThinking([a, b]), frame("new"))[0].durationMs,
    8000,
  );
});
