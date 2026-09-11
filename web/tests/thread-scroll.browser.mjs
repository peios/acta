import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
const { chromium, firefox } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-scroll-test-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: { alias: { $lib: join(root, "src/lib") } },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/thread-scroll-entry.js"),
      name: "ScrollFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
const engine =
  process.env.ACTA_SCROLL_BROWSER === "firefox" ? firefox : chromium;
const browser = await engine.launch({
  headless: true,
  executablePath: process.env.ACTA_SCROLL_EXECUTABLE || undefined,
  ...(engine === firefox
    ? {
        firefoxUserPrefs: {
          "general.smoothScroll": true,
          "general.smoothScroll.mouseWheel": true,
        },
      }
    : {}),
});
try {
  const page = await browser.newPage({ viewport: { width: 900, height: 720 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.setContent('<div id="app"></div>');
  await page.addStyleTag({
    content: readFileSync(join(out, "acta-web.css"), "utf8"),
  });
  await page.evaluate(() => {
    window.wheelListeners = [];
    const add = EventTarget.prototype.addEventListener;
    EventTarget.prototype.addEventListener = function (
      type,
      listener,
      options,
    ) {
      if (type === "wheel")
        window.wheelListeners.push(options?.passive === true);
      return add.call(this, type, listener, options);
    };
  });
  await page.addScriptTag({
    content: readFileSync(join(out, "fixture.js"), "utf8"),
  });
  await page.waitForFunction(() => window.scrollFixture);
  assert.deepEqual(
    await page.evaluate(() => window.wheelListeners),
    [true],
    "wheel observation must be passive",
  );
  const settle = () =>
    page.evaluate(
      () =>
        new Promise((r) =>
          requestAnimationFrame(() => requestAnimationFrame(r)),
        ),
    );
  const read = () =>
    page.evaluate(() => {
      const n = document.querySelector(".frame-feed");
      const t = n.getBoundingClientRect().top;
      const a = [...n.querySelectorAll("[data-feed-id]")].find(
        (el) => el.getBoundingClientRect().bottom > t,
      );
      return {
        top: n.scrollTop,
        max: n.scrollHeight - n.clientHeight,
        anchor: a?.dataset.feedId,
        offset: a?.getBoundingClientRect().top - t,
      };
    });
  if (process.env.ACTA_SCROLL_ITEMS) {
    const data = JSON.parse(
      readFileSync(process.env.ACTA_SCROLL_ITEMS, "utf8"),
    );
    await page.evaluate((data) => window.scrollFixture.replace(data), data);
  }
  await settle();
  let p = await read();
  assert.equal(p.top, p.max, "initial end");
  await page.locator(".frame-feed").hover();
  // Repeated idle responses must not write offsets during native wheel motion.
  await page.evaluate(() => {
    const node = document.querySelector(".frame-feed");
    const descriptor = Object.getOwnPropertyDescriptor(
      Element.prototype,
      "scrollTop",
    );
    window.scrollWrites = [];
    Object.defineProperty(node, "scrollTop", {
      configurable: true,
      get() {
        return descriptor.get.call(this);
      },
      set(value) {
        window.scrollWrites.push(value);
        descriptor.set.call(this, value);
      },
    });
  });
  let last = p.top;
  for (let i = 0; i < 30; i++) {
    await page.mouse.wheel(0, -22);
    await page.evaluate(() => window.scrollFixture.poll());
    await page.waitForTimeout(20);
    const position = await read();
    assert.ok(position.top <= last + 1, "idle upward wheel jumped down");
    last = position.top;
  }
  assert.ok(last < p.max - 300, "upward wheel stalled");
  assert.deepEqual(
    await page.evaluate(() => window.scrollWrites),
    [],
    "idle poll interrupted native scrolling",
  );
  await page.evaluate(() => {
    delete document.querySelector(".frame-feed").scrollTop;
  });
  await page.mouse.wheel(0, 10000);
  await page.waitForTimeout(150);
  await page.mouse.wheel(0, -420);
  await page.waitForTimeout(80);
  const paused = await read();
  assert.ok(paused.top < paused.max - 200);
  for (let i = 0; i < 8; i++) {
    await page.evaluate(() => window.scrollFixture.append());
    await settle();
  }
  p = await read();
  assert.equal(p.top, paused.top, "append while paused");
  await page.evaluate(() => window.scrollFixture.grow(0));
  await settle();
  p = await read();
  assert.equal(p.anchor, paused.anchor);
  assert.ok(Math.abs(p.offset - paused.offset) < 1, "growth above anchor");
  const beforeOlder = p;
  await page.evaluate(() => window.scrollFixture.older());
  await settle();
  p = await read();
  assert.equal(p.anchor, beforeOlder.anchor);
  assert.ok(Math.abs(p.offset - beforeOlder.offset) < 1, "prepend anchor");
  // An offset delivered after an UP input must not rearm following.
  await page.evaluate(() => {
    const n = document.querySelector(".frame-feed");
    n.scrollTop = n.scrollHeight - n.clientHeight - 10;
  });
  await settle();
  await page.evaluate(() => {
    const n = document.querySelector(".frame-feed");
    n.dispatchEvent(new WheelEvent("wheel", { deltaY: -1, bubbles: true }));
    n.scrollTop = n.scrollHeight;
  });
  await settle();
  const late = await read();
  await page.evaluate(() => window.scrollFixture.append());
  await settle();
  p = await read();
  assert.equal(p.top, late.top, "late scroll incorrectly resumed following");
  assert.ok(p.max > p.top);
  await page.mouse.wheel(0, 10000);
  await page.waitForTimeout(100);
  await page.evaluate(() => window.scrollFixture.append());
  await settle();
  p = await read();
  assert.equal(p.top, p.max, "downward gesture should resume following");
  // High frequency streaming plus native upward wheel events stays paused.
  await page.mouse.wheel(0, -100);
  await page.waitForTimeout(50);
  let previous = (await read()).top;
  for (let i = 0; i < 10; i++) {
    await page.evaluate(() => window.scrollFixture.append());
    await page.mouse.wheel(0, -25);
    await page.waitForTimeout(25);
    p = await read();
    assert.ok(p.top <= previous + 1, `jumped ${previous} to ${p.top}`);
    previous = p.top;
  }
  await page.evaluate(async () => {
    await window.scrollFixture.stress();
    const original = Element.prototype.getBoundingClientRect;
    window.feedMeasurements = 0;
    Element.prototype.getBoundingClientRect = function () {
      if (this.hasAttribute("data-feed-id")) window.feedMeasurements++;
      return original.call(this);
    };
  });
  await settle();
  await page.evaluate(() => {
    window.feedMeasurements = 0;
    const node = document.querySelector(".frame-feed");
    node.scrollTop -= 50;
    node.dispatchEvent(new Event("scroll"));
  });
  const measurements = await page.evaluate(() => window.feedMeasurements);
  assert.ok(
    measurements > 0 && measurements < 30,
    `scroll scanned ${measurements} rows in a 1000-row history`,
  );
  // Whole pages can fold into the same Worked row without moving scrollTop.
  // Pagination must continue without another native scroll event.
  await page.evaluate(async () => {
    await window.scrollFixture.groupedHistory();
    const node = document.querySelector(".frame-feed");
    node.scrollTop = 0;
    node.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(
    () => window.scrollFixture.olderCalls() === 3,
    undefined,
    { timeout: 2500 },
  );
  await page.waitForFunction(
    () => !document.querySelector(".load-history"),
    undefined,
    { timeout: 2500 },
  );
  assert.equal(
    (await read()).top,
    0,
    "collapsed history should not move the reader",
  );
  await page.evaluate(async () => {
    await window.scrollFixture.groupedHistory(true);
    const node = document.querySelector(".frame-feed");
    node.scrollTop = 0;
    node.dispatchEvent(new Event("scroll"));
  });
  await page.waitForFunction(() => window.scrollFixture.olderCalls() === 1);
  await page.waitForTimeout(200);
  assert.equal(
    await page.evaluate(() => window.scrollFixture.olderCalls()),
    1,
    "an unchanged/failed page must not automatically retry",
  );
  // Retry with a wheel gesture while already at the top: no scroll event occurs.
  await page.locator(".frame-feed").hover();
  await page.mouse.wheel(0, -25);
  await page.waitForFunction(() => window.scrollFixture.olderCalls() === 2);
  await page.waitForTimeout(100);
  await page
    .getByRole("button", { name: "Load older messages", exact: true })
    .click();
  await page.waitForFunction(() => window.scrollFixture.olderCalls() === 3);
  // Native phone touch must pause following while new messages arrive.
  if (engine === chromium) {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(() => {
      const meta = document.createElement("meta");
      meta.name = "viewport";
      meta.content = "width=device-width, initial-scale=1";
      document.head.append(meta);
    });
    await page.evaluate(async () => {
      await window.scrollFixture.stress();
      const n = document.querySelector(".frame-feed");
      n.scrollTop = n.scrollHeight;
      n.dispatchEvent(new Event("scroll"));
    });
    await settle();
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: true });
    const b = await page.locator(".frame-feed").boundingBox();
    const start = (await read()).top;
    const x = b.x + b.width / 2,
      y = b.y + 80;
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [{ x, y }],
    });
    for (let i = 1; i <= 8; i++) {
      await cdp.send("Input.dispatchTouchEvent", {
        type: "touchMove",
        touchPoints: [{ x, y: y + i * 25 }],
      });
      await page.evaluate(() => window.scrollFixture.append());
    }
    await cdp.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
    await settle();
    const paused = await read();
    assert.ok(
      paused.top < start - 80,
      "touch scroll should move up while streaming",
    );
    await page.evaluate(() => window.scrollFixture.append());
    await settle();
    assert.ok(
      (await read()).top <= paused.top + 1,
      "new output must not pull mobile reader back down",
    );
    await cdp.detach();
  }
  assert.deepEqual(errors, []);
  console.log(
    "PASS: passive wheel, idle polling, bounded anchor measurements, initial end, live following, upward pause, late-event latch, older-page anchoring, growth above viewport, resume at bottom, repeated wheel during streaming, collapsed-page pagination, no-progress guard, explicit history retries",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
