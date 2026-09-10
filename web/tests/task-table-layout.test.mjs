import test from "node:test";
import assert from "node:assert/strict";
import {
  taskColumns,
  defaultTableLayout,
  visibleColumns,
  columnPercentages,
  resizeColumns,
  reorderColumn,
  parseTableLayout,
  readTableLayout,
  saveTableLayout,
  removeTableLayout,
} from "../src/lib/task-table-layout.js";
const near = (a, b) => assert.ok(Math.abs(a - b) < 1e-8, `${a} != ${b}`);
const sum = (widths) => Object.values(widths).reduce((a, b) => a + b, 0);

test("defaults use size classes, normalise visible properties and enforce minimums", () => {
  const layout = defaultTableLayout();
  near(sum(layout.widths), 100);
  assert.equal(layout.widths.status, layout.widths.assignees);
  assert.ok(
    layout.widths.title > layout.widths.status &&
      layout.widths.status > layout.widths.number,
  );
  for (const properties of [
    [],
    ["status"],
    ["assignees"],
    ["status", "assignees"],
  ]) {
    const columns = visibleColumns(layout, properties);
    for (const available of [100, 464, 600, 1200, 2000]) {
      const actual = Math.max(
        available,
        columns.reduce((s, c) => s + c.minimum, 0),
      );
      const widths = columnPercentages(layout, columns, available);
      near(sum(widths), 100);
      for (const c of columns)
        assert.ok((widths[c.id] * actual) / 100 >= c.minimum - 1e-8);
    }
  }
  for (const c of taskColumns)
    near(
      columnPercentages(layout, taskColumns, 1600)[c.id],
      layout.widths[c.id],
    );
});
test("resizing a boundary clamps both neighbours and retains hidden widths", () => {
  const layout = defaultTableLayout();
  const columns = visibleColumns(layout, ["status"]);
  const rendered = columnPercentages(layout, columns, 1000);
  for (const delta of [-1000, -10, 10, 1000]) {
    const changed = resizeColumns(layout, columns, rendered, 0, delta, 1000);
    near(sum(changed.widths), 100);
    near(changed.widths.assignees, layout.widths.assignees);
    const next = columnPercentages(changed, columns, 1000);
    near(next.number + next.title, rendered.number + rendered.title);
    near(next.status, rendered.status);
    assert.ok(next.number >= 10.4 - 1e-8);
    assert.ok(next.title >= 16 - 1e-8);
  }
});
test("reordering visible columns preserves widths and hidden positions", () => {
  const original = defaultTableLayout();
  const result = reorderColumn(
    original,
    ["number", "title", "assignees"],
    "assignees",
    0,
  );
  assert.deepEqual(result.order, [
    "assignees",
    "number",
    "priority",
    "type",
    "size",
    "status",
    "title",
  ]);
  assert.deepEqual(result.widths, original.widths);
  assert.deepEqual(
    original.order,
    taskColumns.map((c) => c.id),
  );
  assert.deepEqual(
    visibleColumns(result, ["status", "assignees"]).map((c) => c.id),
    result.order.filter((id) =>
      ["number", "title", "status", "assignees"].includes(id),
    ),
  );
});
test("invalid storage and added properties get safe complete defaults", () => {
  for (const value of [
    null,
    [],
    {},
    { order: "bad" },
    { order: [], widths: null },
  ])
    assert.deepEqual(parseTableLayout(value), defaultTableLayout());
  const parsed = parseTableLayout({
    order: ["title", "title", "unknown"],
    widths: { title: NaN, number: -1, status: Infinity, assignees: "20" },
  });
  assert.deepEqual(parsed.order, [
    "title",
    ...taskColumns.filter((c) => c.id !== "title").map((c) => c.id),
  ]);
  for (const c of taskColumns)
    near(parsed.widths[c.id], defaultTableLayout().widths[c.id]);
});
test("layouts are browser-local per preset; copied layouts are independent and missing layouts use defaults", () => {
  const stored = new Map();
  const previous = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key) => stored.get(key) ?? null,
      setItem: (key, value) => stored.set(key, value),
      removeItem: (key) => stored.delete(key),
    },
  });
  try {
    const initial = defaultTableLayout();
    initial.order.reverse();
    saveTableLayout("workspace", "original", initial);
    saveTableLayout(
      "workspace",
      "copy",
      readTableLayout("workspace", "original"),
    );
    const copy = readTableLayout("workspace", "copy");
    copy.order.reverse();
    saveTableLayout("workspace", "copy", copy);
    assert.deepEqual(
      readTableLayout("workspace", "original").order,
      initial.order,
    );
    for (const c of taskColumns)
      near(
        readTableLayout("workspace", "original").widths[c.id],
        initial.widths[c.id],
      );
    assert.deepEqual(
      readTableLayout("workspace", "elsewhere"),
      defaultTableLayout(),
    );
    assert.deepEqual(
      readTableLayout("another-workspace", "original"),
      defaultTableLayout(),
    );
    removeTableLayout("workspace", "copy");
    assert.deepEqual(
      readTableLayout("workspace", "copy"),
      defaultTableLayout(),
    );
    assert.deepEqual(
      readTableLayout("workspace", "original").order,
      initial.order,
    );
    for (const c of taskColumns)
      near(
        readTableLayout("workspace", "original").widths[c.id],
        initial.widths[c.id],
      );
  } finally {
    if (previous) Object.defineProperty(globalThis, "localStorage", previous);
    else delete globalThis.localStorage;
  }
});
