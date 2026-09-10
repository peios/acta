import { test } from "node:test";
import assert from "node:assert/strict";
import { loadActivityWindow } from "../src/lib/activity-window.js";
const entry = (first) => ({ id: first, first_event: first, last_event: first });
test("activity refresh keeps the loaded window when new entries shift pages", async () => {
  const calls = [];
  const pages = {
    "": {
      entries: [entry("103"), entry("102")],
      more: true,
      cursor: "102",
      unread: true,
    },
    102: {
      entries: [entry("101"), entry("100")],
      more: true,
      cursor: "100",
      unread: true,
    },
    100: {
      entries: [entry("99"), entry("98")],
      more: true,
      cursor: "98",
      unread: true,
    },
  };
  const result = await loadActivityWindow(
    async (c) => {
      calls.push(c);
      return pages[c];
    },
    { oldest: "99" },
  );
  assert.deepEqual(calls, ["", "102", "100"]);
  assert.deepEqual(
    result.entries.map((e) => e.id),
    ["103", "102", "101", "100", "99", "98"],
  );
  assert.equal(result.cursor, "98");
});
test("older activity requests one page and retains precise large positions", async () => {
  const result = await loadActivityWindow(
    async (cursor) => {
      assert.equal(cursor, "9007199254740993");
      return {
        entries: [entry("9007199254740992")],
        more: true,
        cursor: "9007199254740992",
        unread: false,
      };
    },
    { cursor: "9007199254740993" },
  );
  assert.equal(result.cursor, "9007199254740992");
});
test("activity paging detects broken cursors rather than looping forever", async () => {
  await assert.rejects(
    loadActivityWindow(
      async () => ({
        entries: [entry("10")],
        more: true,
        cursor: "10",
        unread: false,
      }),
      { oldest: "1" },
    ),
    /repeated/,
  );
  await assert.rejects(
    loadActivityWindow(
      async () => ({ entries: [], more: true, cursor: "", unread: false }),
      { oldest: "1" },
    ),
    /incomplete/,
  );
});
