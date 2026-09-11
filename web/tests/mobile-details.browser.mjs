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
      entry: join(root, "tests/fixtures/mobile-details-entry.js"),
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
  const errors = [];
  const creations = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("http://localhost:8081/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/workspaces/mobile/tasks") {
      creations.push(route.request().postDataJSON());
      return creations.length === 1
        ? route.fulfill({
            status: 503,
            json: { error: { code: "unavailable", message: "Try again" } },
          })
        : route.fulfill({
            json: { id: "created", title: creations.at(-1).title },
          });
    }
    if (path.startsWith("/api/"))
      return route.fulfill({ json: { models: [] } });
    if (path === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript; charset=utf-8",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (path === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    return route.fulfill({
      contentType: "text/html",
      body: '<html lang="en"><head><title>Acta mobile audit</title><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  await page.goto("http://localhost:8081");
  const input = page.getByRole("textbox", { name: "Message", exact: true });
  await input.waitFor();
  assert.equal(
    await input.evaluate((e) => document.activeElement === e),
    false,
    "mobile composer must not steal focus",
  );
  await input.fill("First line");
  await input.press("Enter");
  assert.equal(await input.inputValue(), "First line\n");
  assert.equal(await page.locator("output").textContent(), "");
  await input.press("Control+Enter");
  assert.equal(await page.locator("output").textContent(), "First line\n");
  assert.equal(
    await input.evaluate((e) => getComputedStyle(e).fontSize),
    "16px",
  );
  // Keep both axes of the board intact through full-screen detail navigation.
  await page.locator(".board").evaluate((e) => (e.scrollLeft = 316));
  await page.locator("main").evaluate((e) => (e.scrollTop = 160));
  await page.getByRole("button", { name: "Task 2-3", exact: true }).tap();
  const position = await page.evaluate(() => ({
    x: document.querySelector(".board").scrollLeft,
    y: document.querySelector("main").scrollTop,
  }));
  const detail = page.getByRole("dialog", { name: "QA-1", exact: true });
  await detail.waitFor();
  assert.equal(await detail.evaluate((e) => e.matches(":modal")), true);
  await detail.getByLabel("Comment").fill("A comment");
  await page.getByRole("button", { name: "Back from task", exact: true }).tap();
  await detail.waitFor({ state: "hidden" });
  assert.deepEqual(
    await page.evaluate(() => ({
      x: document.querySelector(".board").scrollLeft,
      y: document.querySelector("main").scrollTop,
    })),
    position,
  );
  await page.getByRole("button", { name: "Task 2-3", exact: true }).tap();
  await detail.waitFor();
  await page.goBack();
  await detail.waitFor({ state: "hidden" });
  // Repeated history traversal must not accumulate dialogs or move the board.
  for (let i = 0; i < 8; i++) {
    await page.goForward();
    await detail.waitFor();
    await page.goBack();
    await detail.waitFor({ state: "hidden" });
    assert.deepEqual(
      await page.evaluate(() => ({
        x: document.querySelector(".board").scrollLeft,
        y: document.querySelector("main").scrollTop,
      })),
      position,
    );
    assert.equal(await page.locator("dialog[open]").count(), 0);
  }
  // New task drafts are scoped and survive reload, including an explicit close.
  await page.getByRole("button", { name: "New task", exact: true }).tap();
  await page
    .getByRole("textbox", { name: "Title", exact: true })
    .fill("Remember this task");
  await page.getByRole("button", { name: "Cancel", exact: true }).tap();
  await page.reload();
  await page.getByRole("button", { name: "New task", exact: true }).tap();
  assert.equal(
    await page
      .getByRole("textbox", { name: "Title", exact: true })
      .inputValue(),
    "Remember this task",
  );
  await page.getByRole("button", { name: "Cancel", exact: true }).tap();
  // Simulate visualViewport resize/offset (desktop engines cannot open an OS keyboard).
  await page.evaluate(() => {
    Object.defineProperty(visualViewport, "height", {
      configurable: true,
      value: 420,
    });
    Object.defineProperty(visualViewport, "offsetTop", {
      configurable: true,
      value: 30,
    });
    visualViewport.dispatchEvent(new Event("resize"));
  });
  await page.waitForFunction(
    () =>
      document.documentElement.style.getPropertyValue(
        "--mobile-viewport-height",
      ) === "420px",
  );
  const composer = await page
    .getByRole("group", { name: "Message composer", exact: true })
    .boundingBox();
  assert.ok(
    composer.y >= 30 && composer.y + composer.height <= 450,
    "composer fits above keyboard",
  );
  await page.getByRole("button", { name: "New task", exact: true }).tap();
  const create = await page
    .getByRole("dialog", { name: "Create task", exact: true })
    .boundingBox();
  assert.ok(
    create.y >= 30 && create.y + create.height <= 450,
    "creation controls fit above keyboard",
  );
  const titleInput = page.getByRole("textbox", { name: "Title", exact: true });
  assert.equal(
    await titleInput.evaluate((e) => e === document.activeElement),
    true,
  );
  const titleBounds = await titleInput.boundingBox();
  assert.ok(titleBounds.y > 290 && titleBounds.y + titleBounds.height < 450);
  await page.getByLabel("Priority", { exact: true }).selectOption("high");
  await page.getByLabel("Type", { exact: true }).selectOption("bug");
  await page.getByLabel("Size", { exact: true }).selectOption("s");
  await page.screenshot({ path: `/tmp/acta-create-keyboard-${engine}.png` });
  await page.getByRole("button", { name: "Create task", exact: true }).tap();
  await page.getByRole("alert").filter({ hasText: "Try again" }).waitFor();
  assert.equal(await titleInput.inputValue(), "Remember this task");
  assert.equal(
    await page.getByLabel("Priority", { exact: true }).inputValue(),
    "high",
  );
  await titleInput.press("Enter");
  await page
    .getByRole("dialog", { name: "Create task", exact: true })
    .waitFor({ state: "hidden" });
  assert.deepEqual(creations[1], {
    title: "Remember this task",
    parent_id: "",
    board: "tasks",
    priority: "high",
    type: "bug",
    size: "s",
  });
  assert.equal(await page.getByTestId("created-opened").textContent(), "0");
  await page.getByRole("button", { name: "New task", exact: true }).tap();
  assert.equal(await titleInput.inputValue(), "");
  await titleInput.fill("Open this one");
  await page
    .getByRole("button", { name: "Create and open", exact: true })
    .tap();
  await page
    .getByRole("dialog", { name: "Create task", exact: true })
    .waitFor({ state: "hidden" });
  assert.equal(await page.getByTestId("created-opened").textContent(), "1");
  for (const width of [320, 390, 720]) {
    await page.setViewportSize({ width, height: 844 });
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
  }
  await page.screenshot({ path: `/tmp/acta-mobile-details-${engine}.png` });
  await checkAccessibility(page, "mobile-details");
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: mobile composer, keyboard viewport, touch sizing, detail back/browser back with both scroll axes retained, task drafts after reload`,
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
