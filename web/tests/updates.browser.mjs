import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import assert from "node:assert/strict";
const { chromium, firefox } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../", import.meta.url));
const output = mkdtempSync(join(tmpdir(), "acta-updates-ui-"));
await build({
  configFile: false,
  root,
  logLevel: "error",
  plugins: [svelte({ configFile: false })],
  resolve: { alias: { $lib: join(root, "src/lib") } },
  build: {
    outDir: output,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/updates-entry.js"),
      name: "UpdateReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
for (const engine of [chromium, firefox]) {
  const browser = await engine.launch({ headless: true });
  try {
    const page = await browser.newPage({
      viewport: { width: 1100, height: 820 },
    });
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    let unavailable = false;
    let view = {
      configured: true,
      checked_at: new Date().toISOString(),
      repository: "peios/acta2",
      current: { version: "v0.1.0-preview.1" },
      available: {
        id: "signed-release",
        release: {
          version: "v0.1.0-preview.2",
          notes: "Improved task tracking and more reliable agent recovery.",
        },
        blocked: "",
      },
      jobs: [],
    };
    let installs = 0;
    let retries = 0;
    await page.route("http://localhost:8081/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/api/updates")
        return route.fulfill(
          unavailable ? { status: 503, body: "Maintenance" } : { json: view },
        );
      if (url.pathname === "/api/updates/install") {
        assert.equal(route.request().postDataJSON().id, "signed-release");
        installs++;
        view.jobs = [
          {
            id: "job",
            version: "v0.1.0-preview.2",
            phase: "preparing",
            updated_at: new Date().toISOString(),
          },
        ];
        return route.fulfill({ status: 202, json: { id: "job" } });
      }
      if (url.pathname === "/api/updates/retry") {
        retries++;
        view.jobs[0].paused = false;
        return route.fulfill({ status: 202, json: { queued: true } });
      }
      if (url.pathname === "/fixture.js")
        return route.fulfill({
          contentType: "text/javascript",
          body: readFileSync(join(output, "fixture.js")),
        });
      if (url.pathname === "/fixture.css")
        return route.fulfill({
          contentType: "text/css",
          body: readFileSync(join(output, "fixture.css")),
        });
      return route.fulfill({
        contentType: "text/html",
        body: '<html data-theme="dark"><meta charset="utf-8"><link rel="stylesheet" href="/fixture.css"><div id="app" style="max-width:900px;margin:auto;padding:24px"></div><script src="/fixture.js"></script></html>',
      });
    });
    await page.goto("http://localhost:8081/_review/updates");
    await page
      .getByRole("button", { name: "Update Acta", exact: true })
      .click();
    assert.equal(installs, 0);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await page.screenshot({ path: "/tmp/acta-updates-desktop.png" });
    await page
      .getByRole("button", { name: "Update Acta", exact: true })
      .click();
    await page
      .getByRole("button", { name: "Install update", exact: true })
      .click();
    await page
      .getByRole("heading", { name: "Verifying images and recovery" })
      .waitFor();
    assert.equal(installs, 1);
    unavailable = true;
    await page.getByText(/temporarily unavailable/).waitFor({ timeout: 10000 });
    unavailable = false;
    view.jobs[0].phase = "restoring";
    view.jobs[0].error = "Candidate failed validation";
    await page
      .getByRole("heading", { name: "Recovering the previous release" })
      .waitFor({ timeout: 10000 });
    assert.equal(
      await page.getByRole("button", { name: "Continue recovery" }).count(),
      0,
    );
    view.jobs[0].paused = true;
    await page
      .getByRole("button", { name: "Continue recovery" })
      .click({ timeout: 10000 });
    assert.equal(retries, 1);
    view.jobs[0].phase = "succeeded";
    view.current = view.available.release;
    view.available = null;
    await page
      .getByText("You’re up to date on this release channel.")
      .waitFor({ timeout: 10000 })
      .catch(async (error) => {
        console.error(await page.locator("body").innerText());
        console.error(view);
        throw error;
      });
    await page.getByText("Update completed").waitFor({ timeout: 10000 });
    assert.equal(installs, 1);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({ path: "/tmp/acta-updates-mobile.png" });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      true,
    );
    assert.deepEqual(errors, []);
    console.log(
      "PASS:",
      engine.name(),
      "update confirmation, durable status, reconnect and narrow layout",
    );
  } finally {
    await browser.close();
  }
}
rmSync(output, { recursive: true, force: true });
