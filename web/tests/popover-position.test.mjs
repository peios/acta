import test from "node:test";
import assert from "node:assert/strict";
import { popoverPosition } from "../src/lib/popover-position.js";
test("menus align to their anchor and flip above a low control", () => {
  const anchor = { left: 300, right: 400, top: 550, bottom: 580, width: 100 };
  const p = popoverPosition(
    anchor,
    { width: 700, height: 650 },
    { width: 320, height: 300, align: "end" },
  );
  assert.equal(p.flip, true);
  assert.equal(p.left, 80);
  assert.equal(p.top + p.maxHeight, 542);
});
test("narrow visual viewports and keyboards keep the whole popup within bounds", () => {
  for (const viewport of [
    { width: 320, height: 240 },
    { width: 280, height: 180, left: 30, top: 150 },
  ]) {
    for (const top of [0, 100, 500]) {
      const p = popoverPosition(
        { left: 600, right: 640, top, bottom: top + 32, width: 40 },
        viewport,
        { width: 340, height: 500 },
      );
      assert.ok(p.left >= (viewport.left ?? 0) + 12);
      assert.ok(p.left + p.width <= (viewport.left ?? 0) + viewport.width - 12);
      assert.ok(p.top >= (viewport.top ?? 0) + 12);
      assert.ok(
        p.top + p.maxHeight <= (viewport.top ?? 0) + viewport.height - 12,
      );
    }
  }
});
