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
const root = fileURLToPath(new URL("../", import.meta.url)),
  out = mkdtempSync(join(tmpdir(), "acta-search-"));
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
      entry: join(root, "tests/fixtures/task-archive-entry.js"),
      name: "SearchReview",
      formats: ["iife"],
      fileName: () => "fixture.js",
      cssFileName: "fixture",
    },
    cssCodeSplit: false,
  },
});
const browser = await (
  process.env.ACTA_SEARCH_BROWSER === "firefox" ? firefox : chromium
).launch({ headless: true });
try {
  const page = await browser.newPage({
      viewport: { width: 1050, height: 760 },
    }),
    errors = [],
    queries = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const view = {
    id: "first",
    board: "tasks",
    name: "All tasks",
    version: 1,
    filters: {
      priorities: [],
      types: [],
      sizes: [],
      statuses: [],
      assignees: [],
      unassigned: false,
    },
    display: {
      mode: "table",
      group: "none",
      sort: "number",
      direction: "desc",
      density: "comfortable",
      columns: ["status", "assignees"],
    },
  };
  await page.route("http://localhost:8081/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (v) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(v),
      });
    if (url.pathname.endsWith("/task-views")) return json({ views: [view] });
    if (url.pathname.endsWith("/task-people")) return json({ people: [] });
    if (url.pathname.endsWith("/tasks")) {
      queries.push(url.searchParams);
      if (queries.length > 30) throw Error("Runaway task reads");
      return json({ tasks: [], total: 0, more: false, cursor: "" });
    }
    if (url.pathname === "/fixture.js")
      return route.fulfill({
        contentType: "text/javascript",
        body: readFileSync(join(out, "fixture.js")),
      });
    if (url.pathname === "/fixture.css")
      return route.fulfill({
        contentType: "text/css",
        body: readFileSync(join(out, "fixture.css")),
      });
    if (url.pathname.startsWith("/api/"))
      throw Error("Unexpected request " + url.pathname);
    return route.fulfill({
      contentType: "text/html",
      body: '<html><head><meta charset="utf-8"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  page.setDefaultTimeout(10000);
  await page.goto("http://localhost:8081");
  await page.getByRole("tab", { name: "All tasks" }).waitFor();
  await page.getByRole("button", { name: /^Filter(?: [0-9]+)?$/ }).click();
  await page.getByRole("checkbox", { name: "To do", exact: true }).check();
  await page.getByRole("button", { name: /^Filter(?: [0-9]+)?$/ }).click();
  await page.getByRole("button", { name: "Save All tasks" }).waitFor();
  await page
    .getByRole("button", { name: "Archived tasks", exact: true })
    .click();
  await page.getByRole("heading", { name: "Archived tasks" }).waitFor();
  await page.waitForFunction(
    () =>
      JSON.parse(document.querySelector("[data-testid=state]").textContent)
        .view === "__archived",
  );
  assert.equal(await page.getByRole("tab").count(), 0);
  assert.equal(queries.at(-1).get("archived"), "true");
  assert.equal(queries.at(-1).getAll("status").length, 0);
  await page.getByRole("button", { name: /^Filter(?: [0-9]+)?$/ }).click();
  await page.getByRole("checkbox", { name: "Done", exact: true }).check();
  await page.getByRole("button", { name: /^Filter(?: [0-9]+)?$/ }).click();
  await page.waitForFunction(() =>
    JSON.parse(
      document.querySelector("[data-testid=state]").textContent,
    ).settings.filters.statuses.includes("done"),
  );
  await page.screenshot({ path: "/tmp/acta99-archived.png" });
  await page.getByRole("button", { name: "Back to tasks" }).click();
  await page.getByRole("tab", { name: "All tasks" }).waitFor();
  await page.getByRole("button", { name: "Save All tasks" }).waitFor();
  const restored = await page.getByTestId("state").textContent();
  assert.deepEqual(JSON.parse(restored).settings.filters.statuses, ["todo"]);
  assert.equal(queries.at(-1).get("archived"), "false");
  await page.setViewportSize({ width: 320, height: 760 });
  const createButton = page.getByRole("button", {
    name: "Create task",
    exact: true,
  });
  const createBounds = await createButton.boundingBox();
  assert.ok(createBounds && createBounds.x >= 240 && createBounds.y >= 680);
  assert.equal(createBounds.width, 56);
  await page.getByRole("button", { name: "Search tasks", exact: true }).click();
  await page.getByRole("searchbox").waitFor({ state: "visible" });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  await page.screenshot({ path: "/tmp/acta-archive-header-mobile.png" });
  await page
    .getByRole("button", { name: "Archived tasks", exact: true })
    .click();
  await page.setViewportSize({ width: 390, height: 760 });
  await page.screenshot({ path: "/tmp/acta99-archived-mobile.png" });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS: archive view, separate filters, preserved active draft, archive queries, back navigation and mobile fit",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
