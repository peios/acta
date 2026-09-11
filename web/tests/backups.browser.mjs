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
  out = mkdtempSync(join(tmpdir(), "acta-backups-ui-"));
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
      entry: join(root, "tests/fixtures/backups-entry.js"),
      name: "BackupsFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({
      viewport: { width: 1200, height: 1050 },
    }),
    errors = [],
    writes = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const now = new Date().toISOString();
  let configured = true,
    fail = false;
  const view = {
    configured: true,
    min_retain_full: 2,
    max_retain_full: 30,
    policy: {
      revision: 1,
      enabled: true,
      destination: "offsite",
      mode: "continuous",
      anchor: now,
      backup_minutes: 60,
      full_minutes: 1440,
      drill_minutes: 1440,
      max_age_minutes: 120,
      archive_age_minutes: 5,
      retain_full: 7,
    },
    destinations: [{ id: "offsite", name: "Offsite repository" }],
    can_drill: true,
    warnings: [],
    jobs: [],
    repository: {
      observed_at: now,
      archive_last: now,
      points: [
        {
          label: "20260910-120000F",
          type: "full",
          finished_at: now,
          database_version: "17",
          release: "acta-review",
          complete: true,
          integrity_at: now,
          restored_at: now,
        },
      ],
    },
  };
  await page.route("http://localhost:8081/**", async (route) => {
    const req = route.request(),
      path = new URL(req.url()).pathname;
    if (!path.startsWith("/api/"))
      return route.fulfill({
        contentType: "text/html",
        body: '<div id="app"></div>',
      });
    if (req.method() === "POST") {
      const input = req.postDataJSON();
      writes.push({ path, input });
      if (fail)
        return route.fulfill({
          status: 409,
          json: {
            error: { message: "Backup policy changed; reload before saving" },
          },
        });
      if (path.endsWith("/policy")) {
        view.policy = { ...input, revision: input.revision + 1 };
        return route.fulfill({ json: view.policy });
      }
      view.jobs = [
        { id: "job", kind: input.kind, state: "queued", requested_at: now },
      ];
      return route.fulfill({ status: 202, json: view.jobs[0] });
    }
    return route.fulfill({ json: configured ? view : { configured: false } });
  });
  await page.goto("http://localhost:8081/_review/backups-test");
  await page.addStyleTag({
    content: readFileSync(join(out, "acta-web.css"), "utf8"),
  });
  await page.addScriptTag({
    content: readFileSync(join(out, "fixture.js"), "utf8"),
  });
  const interval = page.getByLabel("Backup interval (minutes)", {
    exact: true,
  });
  await interval.waitFor();
  await interval.fill("90");
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  assert.equal(
    await interval.inputValue(),
    "90",
    "refresh overwrote unsaved policy",
  );
  assert.equal(
    await page.getByRole("button", { name: "Back up now" }).isDisabled(),
    true,
  );
  await page.getByRole("button", { name: "Save policy" }).click();
  await page.waitForFunction(
    () => document.querySelector('input[type="number"]').value === "90",
  );
  assert.equal(writes[0].input.backup_minutes, 90);
  fail = true;
  await interval.fill("95");
  await page.getByRole("button", { name: "Save policy" }).click();
  await page.getByRole("alert").waitFor();
  assert.equal(await interval.inputValue(), "95");
  fail = false;
  await page.getByRole("button", { name: "Save policy" }).click();
  await page.getByRole("button", { name: "Back up now" }).waitFor();
  await page.screenshot({
    path: "/tmp/acta100-backups-desktop.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "Back up now" }).click();
  await page.getByText("queued", { exact: true }).waitFor();
  assert.equal(await interval.isDisabled(), true);
  assert.equal(writes.at(-1).input.kind, "full");
  view.jobs = [];
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: "/tmp/acta100-backups-mobile.png",
    fullPage: true,
  });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
    "mobile overflow",
  );
  configured = false;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await page
    .getByRole("heading", { name: "Connect a backup service" })
    .waitFor();
  assert.deepEqual(errors, []);
  console.log(
    "Backup UI: policy editing, refresh, conflict, queue, responsive and unconfigured states passed",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
