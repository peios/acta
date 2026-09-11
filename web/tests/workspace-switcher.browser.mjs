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
  resolve: {
    alias: {
      $lib: join(root, "src/lib"),
      "$app/navigation": join(
        root,
        "tests/fixtures/workspace-navigation-stub.js",
      ),
    },
  },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/workspace-switcher-entry.js"),
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
  const workspace = (slug) => ({
    id: slug,
    name: slug === "peios" ? "Peios" : "Acta",
    slug,
    description: "",
    version: 1,
    permissions: [],
    previous_slugs: [],
  });
  let failure = false;
  await page.route("http://localhost:8081/**", (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path === "/api/workspaces") {
      if (failure)
        return route.fulfill({
          status: 500,
          json: { error: { code: "internal", message: "Try again later" } },
        });
      const q = url.searchParams.get("q") || "";
      const offset = Number(url.searchParams.get("offset"));
      const all = [workspace("peios"), workspace("acta")].filter((w) =>
        w.name.toLowerCase().includes(q.toLowerCase()),
      );
      return route.fulfill({
        json: {
          workspaces: all.slice(offset, offset + 1),
          more: all.length > offset + 1,
        },
      });
    }
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
  page.setDefaultTimeout(5000);
  const trigger = page.getByRole("button", {
    name: "Switch workspace",
    exact: true,
  });
  await trigger.tap();
  const mobile = page.locator("dialog.workspace-search");
  const input = mobile.getByRole("combobox");
  await input.waitFor();
  assert.equal(await input.evaluate((e) => document.activeElement === e), true);
  await mobile.getByRole("option", { name: /Peios/ }).waitFor();
  await mobile.getByRole("button", { name: "Load more" }).click();
  await mobile.getByRole("option", { name: /Acta/ }).waitFor();
  await input.fill("act");
  await mobile.getByRole("option", { name: /Acta/ }).waitFor();
  assert.equal(await mobile.getByRole("option").count(), 1);
  await input.fill("missing");
  await mobile.getByText("No accessible workspaces found.").waitFor();
  await page.evaluate(() => {
    document.documentElement.style.setProperty(
      "--mobile-viewport-height",
      "450px",
    );
    document.documentElement.style.setProperty("--mobile-viewport-top", "20px");
  });
  let box = await mobile.boundingBox();
  assert.equal(Math.round(box.y + box.height), 470);
  const field = await input.boundingBox();
  assert.ok(field.y > 380 && field.y + field.height <= 470);
  await page.mouse.click(370, 300);
  assert.equal(await page.locator("output").textContent(), "0");
  await input.fill("");
  await mobile.getByRole("option", { name: /Peios/ }).waitFor();
  await page.screenshot({ path: `/tmp/acta-workspace-picker-${engine}.png` });
  await mobile.getByRole("button", { name: "Close workspace selector" }).tap();
  assert.equal(await trigger.getAttribute("aria-expanded"), "false");
  await page.setViewportSize({ width: 1100, height: 800 });
  await page.evaluate(() => {
    document.documentElement.style.removeProperty("--mobile-viewport-height");
    document.documentElement.style.removeProperty("--mobile-viewport-top");
  });
  await trigger.click();
  const desktop = page.locator(".workspace-dropdown");
  await desktop.getByRole("option", { name: /Peios/ }).waitFor();
  box = await desktop.boundingBox();
  const buttonBox = await trigger.boundingBox();
  assert.ok(box.y >= buttonBox.y + buttonBox.height && box.width <= 340);
  await trigger.click();
  assert.equal(await trigger.getAttribute("aria-expanded"), "false");
  failure = true;
  await trigger.click();
  await desktop.getByRole("alert").waitFor();
  failure = false;
  await desktop.getByRole("button", { name: "Try again" }).click();
  await desktop.getByRole("option", { name: /Peios/ }).waitFor();
  await desktop.getByRole("button", { name: "Load more" }).click();
  await desktop.getByRole("option", { name: /Acta/ }).waitFor();
  await desktop.getByRole("combobox").focus();
  await page.keyboard.press("ArrowDown");
  assert.equal(
    await desktop
      .getByRole("option", { name: /Acta/ })
      .getAttribute("aria-selected"),
    "true",
  );
  await page.keyboard.press("Enter");
  await page.waitForURL("**/workspaces/acta");
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: mobile search, keyboard viewport, pagination, filtering, no click-through, desktop dropdown, retry and keyboard selection`,
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
