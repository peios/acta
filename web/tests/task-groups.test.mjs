import test from "node:test";
import assert from "node:assert/strict";
import { moveAssignments } from "../src/lib/task-groups.js";
const people = [
  { id: "jack", owner_id: "" },
  { id: "reviewer", owner_id: "jack" },
  { id: "builder", owner_id: "jack" },
  { id: "sam", owner_id: "" },
];
test("human roll-up moves remove the owner and every owned agent, preserving other users", () => {
  assert.deepEqual(moveAssignments(people, "assignee", "jack", ""), ["sam"]);
  assert.deepEqual(moveAssignments(people, "assignee", "jack", "sam"), ["sam"]);
  assert.deepEqual(moveAssignments(people, "assignee", "jack", "lee"), [
    "sam",
    "lee",
  ]);
});
test("agent moves affect only the source assignment and deduplicate destinations", () => {
  assert.deepEqual(moveAssignments(people, "agents", "reviewer", ""), [
    "jack",
    "builder",
    "sam",
  ]);
  assert.deepEqual(moveAssignments(people, "agents", "reviewer", "builder"), [
    "jack",
    "builder",
    "sam",
  ]);
  assert.deepEqual(moveAssignments(people, "agents", "jack", "reviewer"), [
    "reviewer",
    "builder",
    "sam",
  ]);
});
test("unassigned adds a direct destination without losing intervening assignments", () => {
  assert.deepEqual(moveAssignments([], "assignee", "unassigned", "jack"), [
    "jack",
  ]);
  assert.deepEqual(
    moveAssignments(
      [{ id: "sam", owner_id: "" }],
      "agents",
      "unassigned",
      "reviewer",
    ),
    ["sam", "reviewer"],
  );
});
