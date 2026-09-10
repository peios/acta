import test from "node:test";
import assert from "node:assert/strict";
import { resizeSidebar, restoreSidebarSize } from "../src/lib/sidebar-size.js";

test("collapse preserves the width from before the gesture, with distinct return threshold", () => {
  let state = { width: 340, collapsed: false };
  state = resizeSidebar(state, 180, 340);
  assert.deepEqual(state, { width: 220, collapsed: false });
  state = resizeSidebar(state, 159, 340);
  assert.deepEqual(state, { width: 340, collapsed: true });
  state = resizeSidebar(state, 180, 340);
  assert.deepEqual(state, { width: 340, collapsed: true });
  state = resizeSidebar(state, 190, 340);
  assert.deepEqual(state, { width: 220, collapsed: false });
  state = resizeSidebar(state, 180, 340);
  assert.equal(state.collapsed, false);
});

test("dragging out of the rail expands and enforces the upper bound", () => {
  let state = { width: 300, collapsed: true };
  state = resizeSidebar(state, 189, 300);
  assert.equal(state.collapsed, true);
  state = resizeSidebar(state, 280, 300);
  assert.deepEqual(state, { width: 280, collapsed: false });
  assert.deepEqual(resizeSidebar(state, 900, 300), {
    width: 400,
    collapsed: false,
  });
});

test("remembered preferences are bounded and malformed values fall back safely", () => {
  for (const raw of [
    null,
    "",
    "{",
    "null",
    '{"width":"300","collapsed":false}',
    '{"width":300}',
    '{"width":1e400,"collapsed":false}',
  ]) {
    assert.deepEqual(restoreSidebarSize(raw), { width: 264, collapsed: false });
  }
  assert.deepEqual(restoreSidebarSize('{"width":350,"collapsed":true}'), {
    width: 350,
    collapsed: true,
  });
  assert.deepEqual(restoreSidebarSize('{"width":9999,"collapsed":false}'), {
    width: 400,
    collapsed: false,
  });
  assert.deepEqual(restoreSidebarSize('{"width":-1,"collapsed":true}'), {
    width: 220,
    collapsed: true,
  });
});
