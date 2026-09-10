import test from "node:test";
import assert from "node:assert/strict";
import { mergeTask } from "../src/lib/task-revisions.js";
test("out-of-order independent saves preserve both edits", () => {
  const current = {
    id: "a",
    title: "new title",
    description: "old",
    versions: { title: 2, description: 1 },
  };
  const incoming = {
    id: "a",
    title: "old title",
    description: "new description",
    versions: { title: 1, description: 2 },
  };
  assert.deepEqual(mergeTask(current, incoming), {
    id: "a",
    title: "new title",
    description: "new description",
    versions: { title: 2, description: 2 },
  });
});

test("stale responses cannot regress board membership after a status move", () => {
  const current = {
    id: "a",
    board: "backlog",
    status_id: "backlog-status",
    versions: { status_id: 2, title: 1 },
    title: "old",
  };
  const incoming = {
    id: "a",
    board: "tasks",
    status_id: "todo",
    versions: { status_id: 1, title: 2 },
    title: "new",
  };
  const saved = mergeTask(current, incoming);
  assert.equal(saved.board, "backlog");
  assert.equal(saved.status_id, "backlog-status");
  assert.equal(saved.title, "new");
});
