import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const engines = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-header-"));
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
      entry: join(root, "tests/fixtures/thread-header-entry.js"),
      name: "HeaderReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const engine = process.env.ACTA_MOBILE_BROWSER || "chromium";
const browser = await engines[engine].launch({ headless: true });
try {
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("http://localhost:8081/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/fixture.js" || path === "/fixture.css")
      return route.fulfill({
        contentType: path.endsWith("js") ? "text/javascript" : "text/css",
        body: readFileSync(join(out, path.slice(1))),
      });
    return route.fulfill({
      contentType: "text/html",
      body: '<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  await page.goto("http://localhost:8081");
  const options = page.getByRole("button", {
    name: "Thread options",
    exact: true,
  });
  assert.equal(
    await page.getByRole("button", { name: "Kill", exact: true }).count(),
    0,
  );
  const heading = await page
    .getByRole("heading", { name: "Pekit tooling" })
    .boundingBox();
  const nav = await page
    .getByRole("button", { name: "Open navigation" })
    .boundingBox();
  assert.ok(heading.height < 30 && Math.abs(heading.y - nav.y) < 14);
  async function assertInlineStatus() {
    const title = await page.locator(".title-row h2").boundingBox();
    const status = await page.locator(".title-row [role=status]").boundingBox();
    assert.ok(status.x >= title.x + title.width, "status follows title");
    assert.ok(
      status.y < title.y + title.height && status.y + status.height > title.y,
      "status shares the title line",
    );
  }
  await assertInlineStatus();
  await options.click();
  const menu = page.getByRole("dialog", {
    name: "Thread options",
    exact: true,
  });
  await menu
    .getByRole("button", { name: "Context usage: 27% used", exact: true })
    .click();
  await page
    .getByRole("dialog", { name: "Context usage", exact: true })
    .waitFor();
  await page.keyboard.press("Escape");
  const debug = menu.getByRole("button", { name: "Show debug frames" });
  await debug.click();
  assert.equal(await debug.getAttribute("aria-pressed"), "true");
  assert.equal(await page.getByRole("switch").count(), 0);
  assert.equal(
    await debug.evaluate((el) => el.nextElementSibling.textContent.trim()),
    "Delete thread",
  );
  await page.screenshot({ path: `/tmp/acta-thread-header-${engine}.png` });
  await menu.getByRole("button", { name: "Kill", exact: true }).click();
  assert.equal(await page.locator("output").textContent(), "kill:true");
  await options.click();
  await menu.getByRole("button", { name: "Resume", exact: true }).waitFor();
  await options.click();
  await page.setViewportSize({ width: 320, height: 700 });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.setViewportSize({ width: 1100, height: 800 });
  await page.getByRole("button", { name: "Resume", exact: true }).waitFor();
  await page
    .getByRole("button", { name: "Context usage: 27% used", exact: true })
    .waitFor();
  await assertInlineStatus();
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: compact mobile header, nested usage details, debug toggle/order, power action, desktop controls`,
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
