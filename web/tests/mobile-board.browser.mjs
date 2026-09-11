import { checkAccessibility } from "./mobile-accessibility.mjs";
import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium, firefox, webkit } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-mobile-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: {
    alias: {
      $lib: join(root, "src/lib"),
      "$app/navigation": join(root, "tests/fixtures/navigation-stub.js"),
    },
  },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/mobile-board-entry.js"),
      name: "MobileFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const engine = process.env.ACTA_MOBILE_BROWSER || "chromium";
const browser = await { chromium, firefox, webkit }[engine].launch({
  headless: true,
  executablePath: process.env.ACTA_MOBILE_EXECUTABLE || undefined,
});
try {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    ...(engine !== "firefox" ? { isMobile: true } : {}),
  });
  page.setDefaultTimeout(8000);
  const errors = [],
    writes = [];
  let fail = false;
  let tasks = Array.from({ length: 14 }, (_, i) => ({
    id: `task-${i}`,
    workspace_id: "mobile",
    reference: `QA-${i + 1}`,
    number: i + 1,
    title:
      i === 0
        ? "Make mobile boards a pleasure to use"
        : `Check mobile interaction ${i + 1}`,
    status_id: "todo",
    parent_id: "",
    priority: "high",
    type: "task",
    size: "s",
    children: 0,
    assignees: [],
    descendant_assignees: [],
    ancestors: [],
    description: "",
    versions: { status_id: 1, priority: 1, assignees: 1 },
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }));
  page.on("pageerror", (e) => errors.push(e.message));
  const routeFixture = async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    if (url.pathname.startsWith("/api/")) {
      if (req.method() === "POST") {
        const data = req.postDataJSON();
        writes.push(data);
        if (fail)
          return route.fulfill({
            status: 409,
            json: {
              error: {
                code: "conflict",
                message: "This task changed. Refresh and try again.",
              },
            },
          });
        const task = tasks.find((t) => url.pathname.endsWith("/" + t.id));
        assert.ok(task);
        assert.equal(data.version, task.versions[data.field]);
        task[data.field] = data.value;
        task.versions[data.field]++;
        return route.fulfill({ json: task });
      }
      if (url.pathname.endsWith("/tasks")) {
        const status = url.searchParams.get("status"),
          group = url.searchParams.get("group");
        const rows = tasks.filter(
          (t) =>
            (!status || t.status_id === status) &&
            (!group || t[group] === url.searchParams.get("group_id")),
        );
        return route.fulfill({
          json: { tasks: rows, total: rows.length, more: false, cursor: "" },
        });
      }
      if (url.pathname.endsWith("/task-people"))
        return route.fulfill({ json: { people: [] } });
      throw Error("Unexpected API " + url.pathname);
    }
    if (url.pathname === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript; charset=utf-8",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (url.pathname === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    return route.fulfill({
      contentType: "text/html",
      body: '<html lang="en"><head><title>Acta mobile audit</title><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  };
  await page.route("http://localhost:8081/**", routeFixture);
  await page.goto("http://localhost:8081");
  const card = () =>
    page.getByRole("button", {
      name: "Open QA-1: Make mobile boards a pleasure to use",
      exact: true,
    });
  await card().waitFor();
  assert.equal(
    await page
      .locator(".board")
      .evaluate((e) => getComputedStyle(e).scrollSnapType),
    "x mandatory",
  );
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  // Synthetic events verify cancellation and lifecycle also in Firefox. Chromium
  // additionally uses real browser touch input for pan arbitration and edge drag.
  async function dispatch(type, x, y, selector = ".shell", cancelled = false) {
    return page.evaluate(
      ({ type, x, y, selector, cancelled }) => {
        const el = window.touchTestTarget || document.querySelector(selector);
        if (type === "touchstart") window.touchTestTarget = el;
        const touch = {
          identifier: 42,
          target: el,
          clientX: x,
          clientY: y,
        };
        const event = new Event(type, {
          bubbles: true,
          cancelable: !cancelled,
        });
        Object.defineProperties(event, {
          touches: {
            value: type === "touchend" || type === "touchcancel" ? [] : [touch],
          },
          changedTouches: { value: [touch] },
        });
        el.dispatchEvent(event);
        if (type === "touchend" || type === "touchcancel")
          delete window.touchTestTarget;
        return event.defaultPrevented;
      },
      { type, x, y, selector, cancelled },
    );
  }
  assert.equal(
    await page.evaluate(
      () => getComputedStyle(document.documentElement).overscrollBehaviorX,
    ),
    "none",
  );
  // The event itself must be cancelled before movement, even with no pageX.
  assert.equal(await dispatch("touchstart", 8, 220), true);
  await dispatch("touchmove", 110, 220);
  await dispatch("touchend", 110, 220);
  await page
    .getByRole("dialog", { name: "Navigation" })
    .waitFor({ state: "visible" });
  assert.equal(await dispatch("touchstart", 180, 260, "dialog"), false);
  await dispatch("touchmove", 70, 262);
  await dispatch("touchend", 70, 262);
  await page
    .getByRole("dialog", { name: "Navigation" })
    .waitFor({ state: "hidden" });
  assert.equal(await dispatch("touchstart", 29, 220), false);
  await dispatch("touchcancel", 29, 220);
  assert.equal(await dispatch("touchstart", 8, 220, ".shell", true), false);
  await dispatch("touchcancel", 8, 220);
  // Edge taps on interactive content and multi-touch are not swallowed.
  for (const tag of ["button", "a", "input", "label"]) {
    await page.evaluate((tag) => {
      const element = document.createElement(tag);
      element.id = "edge-control";
      document.querySelector(".shell").append(element);
    }, tag);
    assert.equal(
      await dispatch("touchstart", 8, 220, "#edge-control"),
      false,
      tag,
    );
    await dispatch("touchcancel", 8, 220);
    await page.locator("#edge-control").evaluate((el) => el.remove());
  }
  assert.equal(
    await page.evaluate(() => {
      const event = new Event("touchstart", {
        bubbles: true,
        cancelable: true,
      });
      Object.defineProperty(event, "touches", {
        value: [
          { identifier: 1, clientX: 8, clientY: 220 },
          { identifier: 2, clientX: 40, clientY: 220 },
        ],
      });
      document.querySelector(".shell").dispatchEvent(event);
      return event.defaultPrevented;
    }),
    false,
  );
  await dispatch("touchstart", 8, 220);
  await dispatch("touchmove", 12, 310);
  await dispatch("touchend", 12, 310);
  assert.equal(
    await page
      .locator('dialog[aria-label="Navigation"]')
      .evaluate((e) => e.open),
    false,
  );
  // Repeated open/close gestures must not leave the modal or gesture locked.
  for (let i = 0; i < 10; i++) {
    await dispatch("touchstart", 8, 220);
    await dispatch("touchmove", 110, 220);
    await dispatch("touchend", 110, 220);
    await page.getByRole("dialog", { name: "Navigation" }).waitFor();
    await dispatch("touchstart", 180, 260, "dialog");
    await dispatch("touchmove", 70, 262);
    await dispatch("touchend", 70, 262);
    await page
      .getByRole("dialog", { name: "Navigation" })
      .waitFor({ state: "hidden" });
  }
  await dispatch("touchstart", 8, 220);
  await dispatch("touchmove", 110, 220);
  await dispatch("touchcancel", 110, 220);
  assert.equal(
    await page
      .locator('dialog[aria-label="Navigation"]')
      .evaluate((e) => e.open),
    false,
  );
  await card().tap();
  assert.equal(await page.locator("output").innerText(), "QA-1");
  await page.locator("output").evaluate((e) => (e.textContent = ""));
  let b = await card().boundingBox();
  await dispatch("touchstart", b.x + 60, b.y + 30, ".board-card");
  await dispatch("touchmove", b.x + 60, b.y - 10);
  await page.waitForTimeout(400);
  assert.equal(await page.locator(".drag-preview").count(), 0);
  await dispatch("touchend", b.x + 60, b.y - 10);
  await dispatch("touchstart", b.x + 60, b.y + 30, ".board-card");
  await page.waitForTimeout(400);
  await page.locator(".drag-preview").waitFor();
  await dispatch("touchcancel", b.x + 60, b.y + 30);
  assert.equal(writes.length, 0);
  // A card picked up near the navigation edge must own the gesture exclusively.
  await dispatch("touchstart", 8, b.y + 30, ".board-card");
  await page.waitForTimeout(400);
  await dispatch("touchmove", 110, b.y + 30);
  await dispatch("touchend", 110, b.y + 30);
  assert.equal(
    await page
      .locator('dialog[aria-label="Navigation"]')
      .evaluate((e) => e.open),
    false,
  );
  assert.equal(writes.length, 0);
  // Rotation and a second finger cancel a held card without submitting a move.
  for (const reason of ["resize", "multi-touch"]) {
    await dispatch("touchstart", b.x + 60, b.y + 30, ".board-card");
    await page.waitForTimeout(400);
    await page.locator(".drag-preview").waitFor();
    await page.evaluate((reason) => {
      if (reason === "resize") window.dispatchEvent(new Event("resize"));
      else {
        const target = document.querySelector(".shell");
        const touches = [1, 2].map((identifier) => ({
          identifier,
          target,
          clientX: 150,
          clientY: 250,
        }));
        const event = new Event("touchstart", { bubbles: true });
        Object.defineProperties(event, {
          touches: { value: touches },
          changedTouches: { value: [touches[1]] },
        });
        target.dispatchEvent(event);
      }
    }, reason);
    await dispatch("touchend", b.x + 60, b.y + 30);
    assert.equal(await page.locator(".drag-preview").count(), 0);
    assert.equal(writes.length, 0);
  }
  if (engine === "chromium") {
    console.log("Native touch checks starting");
    const cdp = await page.context().newCDPSession(page);
    const touch = async (type, x = 0, y = 0) =>
      cdp.send("Input.dispatchTouchEvent", {
        type,
        touchPoints:
          type === "touchEnd" || type === "touchCancel" ? [] : [{ x, y }],
      });
    await touch("touchStart", 8, 220);
    for (const x of [20, 40, 70, 110]) await touch("touchMove", x, 220);
    await touch("touchEnd");
    await page
      .getByRole("dialog", { name: "Navigation" })
      .waitFor({ state: "visible" });
    await touch("touchStart", 180, 260);
    for (const x of [160, 140, 110, 70]) await touch("touchMove", x, 260);
    await touch("touchEnd");
    await page
      .getByRole("dialog", { name: "Navigation" })
      .waitFor({ state: "hidden" });
    // Whole-card long press works with real browser input as well as the handle.
    b = await card().boundingBox();
    await touch("touchStart", b.x + 90, b.y + 45);
    await page.waitForTimeout(400);
    await page.locator(".drag-preview").waitFor();
    await touch("touchMove", b.x + 120, b.y + 45);
    await touch("touchCancel");
    assert.equal(await page.locator(".drag-preview").count(), 0);
    assert.equal(writes.length, 0);
    console.log("Native long press passed");
    // Scroll a long column vertically without accidentally initiating a drag.
    await touch("touchStart", 170, 560);
    for (let y = 540; y >= 320; y -= 20) {
      await touch("touchMove", 170, y);
      await page.waitForTimeout(20);
    }
    await touch("touchEnd");
    await page.waitForFunction(
      () => document.querySelector("main").scrollTop > 50,
    );
    assert.equal(await page.locator(".drag-preview").count(), 0);
    await page.waitForTimeout(400);
    await page.locator("main").evaluate((e) => (e.scrollTop = 0));
    await page.waitForTimeout(150);
    console.log("Native vertical scroll passed");
    // Ordinary touch pan remains native and snaps to the next column.
    await touch("touchStart", 280, 220);
    for (let x = 260; x >= 70; x -= 20) {
      await touch("touchMove", x, 220);
      await page.waitForTimeout(20);
    }
    await touch("touchEnd");
    await page.waitForFunction(() => {
      const board = document.querySelector(".board"),
        lane = document.querySelector('[data-board-lane="doing"]');
      return (
        Math.abs(
          lane.getBoundingClientRect().left -
            board.getBoundingClientRect().left -
            2,
        ) < 3
      );
    });
    await page.locator(".board").evaluate((e) => (e.scrollLeft = 0));
    await page.waitForTimeout(150);
    console.log("Native column snap passed");
    // Handle immediately owns touch; edge holding moves board, then drops once.
    b = await page
      .getByRole("button", { name: "Drag QA-1 to another column", exact: true })
      .boundingBox();
    await touch("touchStart", b.x + 22, b.y + 22);
    await page.locator(".drag-preview").waitFor();
    await touch("touchMove", 370, b.y + 22);
    await page.waitForFunction(
      () => document.querySelector(".board").scrollLeft > 260,
    );
    await touch("touchMove", 190, b.y + 22);
    await page.waitForFunction(() =>
      document
        .querySelector('[data-board-lane="doing"]')
        ?.classList.contains("drop-target"),
    );
    await page.screenshot({ path: "/tmp/acta-mobile-drag.png" });
    await touch("touchEnd");
    await page.waitForFunction(() => !document.querySelector(".drag-preview"));
    await page.waitForTimeout(200);
    assert.equal(writes.length, 1);
    assert.equal(writes[0].value, "doing");
    assert.equal(await page.locator("output").innerText(), "");
    // Failed save keeps card in its original group and exposes a useful error.
    await page.locator(".board").evaluate((e) => (e.scrollLeft = 0));
    await page.waitForTimeout(150);
    fail = true;
    b = await page
      .getByRole("button", { name: "Drag QA-2 to another column", exact: true })
      .boundingBox();
    await touch("touchStart", b.x + 22, b.y + 22);
    await touch("touchMove", 370, b.y + 22);
    await page.waitForFunction(
      () => document.querySelector(".board").scrollLeft > 260,
    );
    await touch("touchMove", 190, b.y + 22);
    await touch("touchEnd");
    await page.getByRole("alert").waitFor();
    assert.equal(tasks[1].status_id, "todo");
    fail = false;
    await cdp.detach();
  }
  await page.getByLabel("Allow editing").uncheck();
  assert.equal(await page.locator(".touch-handle").count(), 0);
  await page.getByLabel("Allow editing").check();
  await page.getByLabel("Group", { exact: true }).selectOption("none");
  assert.equal(await page.locator(".touch-handle").count(), 0);
  await page.getByLabel("Group", { exact: true }).selectOption("status");
  for (const width of [320, 390, 720, 1050]) {
    await page.setViewportSize({ width, height: 844 });
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    const snap = await page
      .locator(".board")
      .evaluate((e) => getComputedStyle(e).scrollSnapType);
    assert.equal(snap, width <= 720 ? "x mandatory" : "none");
  }
  // The menu reaches distant columns without a drag and uses versioned writes.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator("main").evaluate((e) => (e.scrollTop = 0));
  await page.locator(".board").evaluate((e) => (e.scrollLeft = 0));
  const menu = page.getByRole("button", { name: "Move QA-3 to…", exact: true });
  await page.waitForTimeout(400);
  await menu.tap();
  const dialog = page.getByRole("dialog", { name: "Move task", exact: true });
  await dialog.waitFor();
  assert.equal(
    await dialog.getByRole("button", { name: "To do Current" }).isDisabled(),
    true,
  );
  await dialog.getByLabel("Find destination column").fill("done");
  const beforeMove = writes.length;
  await dialog.getByRole("button", { name: "Done", exact: true }).tap();
  await page.waitForFunction(
    () => !document.querySelector('dialog[aria-label="Move task"]').open,
  );
  await page.waitForTimeout(200);
  assert.equal(writes.length, beforeMove + 1);
  assert.equal(tasks[2].status_id, "done");
  assert.equal(await page.locator(".drag-preview").count(), 0);
  await page.getByRole("button", { name: "Move QA-4 to…", exact: true }).tap();
  fail = true;
  await dialog.getByRole("button", { name: "Done", exact: true }).tap();
  await page.getByRole("alert").waitFor();
  assert.equal(tasks[3].status_id, "todo");
  fail = false;
  await page.getByLabel("Group", { exact: true }).selectOption("priority");
  await page.getByRole("button", { name: "Move QA-4 to…", exact: true }).tap();
  await dialog.getByRole("button", { name: "Low", exact: true }).tap();
  await page.waitForTimeout(200);
  assert.equal(tasks[3].priority, "low");
  await page.getByLabel("Group", { exact: true }).selectOption("status");
  await page.getByLabel("Allow editing").uncheck();
  assert.equal(await page.locator(".move-button").count(), 0);
  await page.getByLabel("Allow editing").check();
  console.log("Touch checks passed; testing desktop drag");
  // Desktop HTML drag/drop needs a mouse desktop context, not mobile emulation.
  const desktop = await browser.newPage({
    viewport: { width: 1100, height: 844 },
  });
  desktop.setDefaultTimeout(8000);
  desktop.on("pageerror", (e) => errors.push(e.message));
  await desktop.route("http://localhost:8081/**", routeFixture);
  await desktop.goto("http://localhost:8081");
  await desktop
    .locator('[data-board-lane="todo"] .board-card')
    .first()
    .waitFor();
  const previous = writes.length;
  await desktop
    .locator('[data-board-lane="todo"] .board-card')
    .first()
    .dragTo(desktop.locator('[data-board-lane="doing"] header'));
  await desktop.waitForTimeout(250);
  assert.equal(writes.length, previous + 1);
  await desktop.close();
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: `/tmp/acta-mobile-${engine}.png` });
  await checkAccessibility(page, "mobile-board");
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: mobile fit, snapping, navigation swipe, vertical intent, cancellation, tap, hold, read-only/ungrouped, ${engine === "chromium" ? "native touch pan, cross-column drag, versioned save, conflict recovery" : ""}`,
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
