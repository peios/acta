import test from "node:test";
import assert from "node:assert/strict";
import { memoryReferences } from "../src/lib/thread-memory-references.js";
import { ReferenceResolver } from "../src/lib/thread-reference-resolver.js";
const a = "a05aee6f-3a39-49e7-b67b-508686beaf62",
  b = "9cd50a8f-e325-49f6-ab34-7c2098618f6f";
const memory = (id = a) => ({
  id,
  key: "build-conventions",
  scope: "workspace",
  summary: "Build guidance",
});
const call = (output, args = {}, name = "mcp__acta2__memory_get") => ({
  data: { name, arguments: args },
  output: typeof output === "string" ? output : JSON.stringify(output),
});
test("memory results from both providers, aliases, recall lists and structured output", () => {
  for (const name of [
    "mcp__acta2__memory_get",
    "my-acta1/memory_get",
    "mcp__AcTa__memory_save",
  ]) {
    assert.deepEqual(
      memoryReferences(call({ data: memory() }, { id: a }, name)),
      [memory()],
    );
  }
  assert.deepEqual(
    memoryReferences(
      call(
        { data: { memories: [memory(), memory(b)] } },
        {},
        "acta/memory_recall",
      ),
    ),
    [memory(), memory(b)],
  );
  assert.deepEqual(
    memoryReferences(
      call({
        content: [{ type: "text", text: JSON.stringify({ data: memory() }) }],
        structuredContent: { data: memory() },
      }),
    ),
    [memory()],
  );
  assert.deepEqual(
    memoryReferences(
      call(
        `plain\n\nStructured result:\n${JSON.stringify({ data: memory() })}`,
      ),
    ),
    [memory()],
  );
});
test("known memory arguments can be checked, but arbitrary IDs, prose and servers cannot", () => {
  assert.deepEqual(
    memoryReferences(call({}, { id: a })).map((r) => r.id),
    [a],
  );
  assert.deepEqual(
    memoryReferences(
      call({ deleted: true }, { id: a }, "acta/memory_delete"),
    ).map((r) => r.id),
    [a],
  );
  for (const name of [
    "other/memory_get",
    "mcp__other__acta_memory_get",
    "acta/task_get",
    "acta/memory_future",
  ])
    assert.deepEqual(
      memoryReferences(call({ data: memory() }, { id: a }, name)),
      [],
    );
  for (const value of [
    `remember ${a}`,
    { id: a, key: "secret" },
    { data: { id: a, scope_id: b } },
    { content: JSON.stringify(memory()) },
  ])
    assert.deepEqual(memoryReferences(call(value)), []);
  assert.deepEqual(
    memoryReferences(
      call({}, { workspace: a, agent_id: b, id: a }, "acta/memory_recall"),
    ),
    [],
  );
  assert.deepEqual(memoryReferences(call({}, { id: "../../secrets" })), []);
  assert.deepEqual(
    memoryReferences(call({ data: { ...memory(), scope: "invalid" } })),
    [],
  );
});
test("memory links use current API metadata and fail closed", async () => {
  let reads = 0;
  const resolver = new ReferenceResolver(async (id) => {
    reads++;
    if (id === b) throw Error("not found");
    return { ...memory(id), key: "renamed" };
  });
  const found = await Promise.all([
    resolver.resolve(a),
    resolver.resolve(a),
    resolver.resolve(b),
  ]);
  assert.equal(reads, 2);
  assert.equal(found[0].key, "renamed");
  assert.equal(found[2], null);
  resolver.close();
  assert.equal(await resolver.resolve(a), null);
});
test("memory result traversal has a fixed link bound", () => {
  const memories = Array.from({ length: 500 }, (_, i) =>
    memory(`00000000-0000-0000-0000-${String(i).padStart(12, "0")}`),
  );
  assert.equal(
    memoryReferences(call({ data: { memories } }, {}, "acta/memory_recall"))
      .length,
    128,
  );
});
