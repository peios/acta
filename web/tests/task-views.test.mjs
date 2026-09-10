import test from "node:test";
import assert from "node:assert/strict";
import { copyViewFilters, sameViewFilters } from "../src/lib/task-views.js";
test("view drafts are independent of saved filters and each other", () => {
  const saved = {
    priorities: [],
    types: [],
    sizes: [],
    statuses: ["todo"],
    assignees: ["jack"],
    unassigned: false,
  };
  const first = copyViewFilters(saved),
    second = copyViewFilters(saved);
  first.statuses.push("done");
  first.assignees.splice(0);
  first.unassigned = true;
  assert.deepEqual(second, saved);
  assert.equal(sameViewFilters(first, saved), false);
  assert.equal(sameViewFilters(copyViewFilters(saved), saved), true);
});
test("selection order and duplicates do not make a view dirty", () => {
  assert.equal(
    sameViewFilters(
      { statuses: ["b", "a", "a"], assignees: ["y", "x"], unassigned: false },
      { statuses: ["a", "b"], assignees: ["x", "y"], unassigned: false },
    ),
    true,
  );
  assert.equal(
    sameViewFilters(
      { statuses: [], assignees: [], unassigned: true },
      { statuses: [], assignees: [], unassigned: false },
    ),
    false,
  );
});

test("display changes have independent drafts and participate in save and undo", async () => {
  const { defaultViewSettings, copyViewSettings, sameViewSettings } =
    await import("../src/lib/task-views.js");
  const saved = defaultViewSettings();
  const draft = copyViewSettings(saved);
  draft.display.mode = "board";
  assert.equal(sameViewSettings(saved, draft), false);
  draft.display.columns.pop();
  draft.display.sort = "title";
  draft.display.group = "status";
  draft.display.density = "compact";
  assert.equal(sameViewSettings(saved, draft), false);
  assert.deepEqual(saved.display.columns, ["status", "assignees"]);
  assert.equal(saved.display.mode, "table");
  const newTab = copyViewSettings(draft);
  const restored = copyViewSettings(saved);
  assert.equal(sameViewSettings(newTab, draft), true);
  assert.equal(newTab.display.mode, "board");
  assert.equal(sameViewSettings(restored, saved), true);
  newTab.display.columns.push("assignees");
  assert.deepEqual(draft.display.columns, ["status"]);
});

test("metadata filters survive draft copies and participate in dirty state", () => {
  const saved = {
    statuses: [],
    assignees: [],
    unassigned: false,
    priorities: ["high"],
    types: ["bug"],
    sizes: ["s"],
  };
  const draft = copyViewFilters(saved);
  draft.priorities.push("urgent");
  assert.deepEqual(saved.priorities, ["high"]);
  assert.equal(sameViewFilters(saved, draft), false);
  assert.equal(
    sameViewFilters(saved, { ...saved, priorities: ["high", "high"] }),
    true,
  );
  for (const key of ["priorities", "types", "sizes"])
    assert.equal(sameViewFilters(saved, { ...saved, [key]: [] }), false);
});
