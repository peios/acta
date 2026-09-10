import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url)),
  out = mkdtempSync(join(tmpdir(), "acta-migration-"));
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
      entry: join(root, "tests/fixtures/migration-access-entry.js"),
      name: "MigrationReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({
      viewport: { width: 1000, height: 820 },
    }),
    errors = [],
    writes = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("http://localhost:8081/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (path === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    if (path === "/app.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(root, "src/lib/styles.css")),
      });
    if (path === "/api/security/sessions/migration-session/grants") {
      writes.push(route.request().postDataJSON());
      return route.fulfill({ contentType: "application/json", body: "{}" });
    }
    return route.fulfill({
      contentType: "text/html",
      body: '<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/app.css"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  const edit = () =>
    page
      .getByRole("button", { name: "Edit access for Migration review client" })
      .click();
  await page.goto("http://localhost:8081/_review/migration");
  await edit();
  const grant = page.getByRole("checkbox", {
    name: "Migration Assistant · full content impersonation",
  });
  assert.equal(await grant.isChecked(), false);
  await grant.check();
  await page.screenshot({ path: "/tmp/acta103-migration-access.png" });
  await page.getByRole("button", { name: "Save access" }).click();
  await page.getByRole("status").waitFor();
  assert.deepEqual(writes[0].previous_grants, ["identity.read"]);
  assert.ok(writes[0].tool_grants.includes("migration.assistant"));
  await page.goto("http://localhost:8081/_review/migration?superuser=false");
  await edit();
  assert.equal(await grant.count(), 0);
  await page.goto(
    "http://localhost:8081/_review/migration?superuser=false&existing=1",
  );
  await edit();
  await grant.uncheck();
  assert.equal(await grant.isDisabled(), true);
  await page.getByRole("button", { name: "Save access" }).click();
  await page.getByRole("status").waitFor();
  assert.ok(!writes[1].tool_grants.includes("migration.assistant"));
  await page.goto("http://localhost:8081/_review/migration");
  await page.setViewportSize({ width: 390, height: 844 });
  await edit();
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.screenshot({ path: "/tmp/acta103-migration-mobile.png" });
  assert.deepEqual(errors, []);
  console.log(
    "PASS: explicit opt-in, save payload, non-superuser exclusion, demoted revocation, mobile fit",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
