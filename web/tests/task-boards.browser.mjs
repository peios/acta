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
  out = mkdtempSync(join(tmpdir(), "acta-boards-"));
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
      entry: join(root, "tests/fixtures/task-boards-entry.js"),
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
    if (url.pathname.endsWith("/task-views")) {
      const board = url.searchParams.get("board") || "tasks";
      return json({
        views: [
          {
            ...view,
            board,
            id: board + "-view",
            name: board === "tasks" ? "All tasks" : "All backlog",
          },
        ],
      });
    }
    if (url.pathname.endsWith("/task-people")) return json({ people: [] });
    if (url.pathname.endsWith("/tasks")) {
      if (route.request().method() === "POST") {
        const payload = route.request().postDataJSON();
        queries.push(payload);
        return json({ id: "created", ...payload });
      }
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
  await page
    .getByRole("button", { name: "Backlog board", exact: true })
    .click();
  await page.getByRole("tab", { name: "All backlog" }).waitFor();
  await page.waitForFunction(
    () => document.querySelector("h1")?.textContent === "Backlog",
  );
  assert.equal(
    queries
      .filter((q) => q instanceof URLSearchParams)
      .at(-1)
      .get("board"),
    "backlog",
  );
  await page.getByRole("button", { name: /^Filter/ }).click();
  await page.getByRole("checkbox", { name: "Ideas", exact: true }).waitFor();
  assert.equal(
    await page.getByRole("checkbox", { name: "To do", exact: true }).count(),
    0,
  );
  await page.getByRole("button", { name: /^Filter/ }).click();
  await page
    .getByRole("button", { name: "Status: To do", exact: true })
    .click();
  const list = page.getByRole("listbox", { name: "Task status" });
  await list.getByText("Tasks", { exact: true }).waitFor();
  await list.getByText("Backlog", { exact: true }).waitFor();
  await page.screenshot({ path: "/tmp/acta105-board-status.png" });
  await page.keyboard.press("End");
  await page.keyboard.press("Enter");
  await page
    .getByRole("button", { name: "Status: Ideas", exact: true })
    .waitFor();
  await page.getByRole("button", { name: "Create task", exact: true }).click();
  await page
    .getByRole("textbox", { name: "Title", exact: true })
    .fill("Backlog creation");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Create task", exact: true })
    .click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(
    queries.find((q) => q.title === "Backlog creation").board,
    "backlog",
  );
  await page.getByRole("button", { name: "Tasks board", exact: true }).click();
  await page.getByRole("tab", { name: "All tasks" }).waitFor();
  await page.setViewportSize({ width: 390, height: 760 });
  await page
    .getByRole("button", { name: "Backlog board", exact: true })
    .click();
  await page.getByRole("tab", { name: "All backlog" }).waitFor();
  await page.screenshot({ path: "/tmp/acta105-boards-mobile.png" });
  assert.ok(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS: board presets and filters, scoped queries and creation, grouped keyboard status picker, mobile fit",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
