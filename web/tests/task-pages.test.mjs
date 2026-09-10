import assert from "node:assert/strict";
import test from "node:test";
import { readTaskWindow } from "../src/lib/task-pages.js";

const page = (ids, cursor = "", total = 100) => ({
  tasks: ids.map((id) => ({ id })),
  cursor,
  more: !!cursor,
  total,
});

test("live refresh retains the visible window and the next server cursor", async () => {
  const calls = [];
  const pages = {
    "": page(["a", "b"], "second"),
    second: page(["c", "d"], "third"),
  };
  const result = await readTaskWindow(async (cursor) => {
    calls.push(cursor);
    return pages[cursor];
  }, 4);
  assert.deepEqual(calls, ["", "second"]);
  assert.deepEqual(
    result.tasks.map((task) => task.id),
    ["a", "b", "c", "d"],
  );
  assert.equal(result.cursor, "third");
  assert.equal(result.more, true);
});

test("shrinking results stop at the last page and overlapping rows are unique", async () => {
  const pages = {
    "": page(["a", "b"], "second"),
    second: page(["b", "c"], "", 3),
  };
  const result = await readTaskWindow(async (cursor) => pages[cursor], 10);
  assert.deepEqual(
    result.tasks.map((task) => task.id),
    ["a", "b", "c"],
  );
  assert.equal(result.total, 3);
  assert.equal(result.more, false);
});

test("failed later pages never return a partially refreshed window", async () => {
  await assert.rejects(
    readTaskWindow(async (cursor) => {
      if (cursor) throw new Error("Connection lost");
      return page(["a"], "second");
    }, 3),
    /Connection lost/,
  );
});

test("a repeated cursor cannot cause an endless refresh", async () => {
  await assert.rejects(
    readTaskWindow(async () => page(["a"], "same"), 3),
    /refresh the task list/,
  );
});
