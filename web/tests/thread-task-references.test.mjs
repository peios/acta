import test from "node:test";
import assert from "node:assert/strict";
import {
  taskReferences,
  TaskReferenceResolver,
} from "../src/lib/thread-task-references.js";
const a = "a05aee6f-3a39-49e7-b67b-508686beaf62",
  b = "9cd50a8f-e325-49f6-ab34-7c2098618f6f";
const task = (id = a) => ({ id, reference: "PEI-5", title: "Review" });
const call = (output, args = {}, name = "mcp__acta__task_get") => ({
  data: { name, arguments: args },
  output: typeof output === "string" ? output : JSON.stringify(output),
});
test("Claude/Codex aliases, envelopes, lists and deduplication", () => {
  for (const name of [
    "mcp__acta__task_get",
    "my-acta1/task_get",
    "mcp__AcTa__task_get",
  ])
    assert.deepEqual(
      taskReferences(
        call(
          { data: { ...task(), subtasks: { tasks: [task(b)] } } },
          { task: a },
          name,
        ),
      ),
      [task(), task(b)],
    );
  assert.deepEqual(
    taskReferences(
      call({
        content: [{ type: "text", text: JSON.stringify({ data: task() }) }],
        structuredContent: { data: task() },
      }),
    ),
    [task()],
  );
  assert.deepEqual(
    taskReferences(
      call(
        `Human output\n\nStructured result:\n${JSON.stringify({ data: task() })}`,
        {},
        "acta/task_create",
      ),
    ),
    [task()],
  );
});
test("comments and parent edits use UUIDs, never colliding task numbers", () => {
  assert.deepEqual(
    taskReferences(
      call(
        { data: { entries: [{ id: b, task_id: a }] } },
        { task: "PEI-5" },
        "mcp__acta__comment_create",
      ),
    ).map((r) => r.id),
    [a],
  );
  assert.deepEqual(
    taskReferences(call({}, { task: a, field: "parent_id", value: b })).map(
      (r) => r.id,
    ),
    [a, b],
  );
  assert.deepEqual(taskReferences(call({}, { task: "PEI-5" })), []);
});
test("unrelated IDs, prose and non-Acta tools are not links", () => {
  for (const value of [
    { id: a, workspace_id: b, title: "Workspace" },
    { data: { memory: task(), description: JSON.stringify(task()) } },
    `Task ${a}`,
  ])
    assert.deepEqual(taskReferences(call(value)), []);
  for (const name of ["other/task_get", "mcp__other__acta_get"])
    assert.deepEqual(taskReferences(call({ data: task() }, {}, name)), []);
  assert.deepEqual(
    taskReferences(
      call({}, { task: "https://example.com", task_id: "../../api" }),
    ),
    [],
  );
});
test("verification deduplicates reads, rejects mismatches and access failures", async () => {
  let reads = 0;
  const r = new TaskReferenceResolver(async (id) => {
    reads++;
    if (id === b) throw Error("403");
    return task(id);
  });
  assert.deepEqual(
    await Promise.all([
      r.resolve(a),
      r.resolve(a),
      r.resolve(b),
      r.resolve("PEI-5"),
    ]),
    [task(), task(), null, null],
  );
  assert.equal(reads, 2);
  await r.resolve(a);
  assert.equal(reads, 2);
  r.close();
  assert.equal(await r.resolve(a), null);
  const wrong = new TaskReferenceResolver(async () => task(b));
  assert.equal(await wrong.resolve(a), null);
  wrong.close();
});
test("verification bounds concurrency and cancels queued work", async () => {
  let active = 0,
    maximum = 0;
  const r = new TaskReferenceResolver(async (id, signal) => {
    active++;
    maximum = Math.max(maximum, active);
    await new Promise((resolve) =>
      signal.addEventListener("abort", resolve, { once: true }),
    );
    active--;
    return task(id);
  });
  const pending = Array.from({ length: 12 }, (_, i) =>
    r.resolve(`00000000-0000-0000-0000-${String(i).padStart(12, "0")}`),
  );
  assert.equal(maximum, 4);
  r.close();
  assert.deepEqual(await Promise.all(pending), Array(12).fill(null));
});
