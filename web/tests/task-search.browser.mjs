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
  resolve: { alias: { $lib: join(root, "src/lib") } },
  build: {
    outDir: out,
    emptyOutDir: true,
    lib: {
      entry: join(root, "tests/fixtures/task-search-entry.js"),
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
  const context = await browser.newContext({
    viewport: { width: 1100, height: 800 },
  });
  let fail = false,
    queries = [];
  const errors = [];
  const result = (id, source = "title") => ({
    id,
    reference: "ACT-" + id,
    title: "Improve task discovery",
    status: { id: "status", name: "In progress" },
    workspace_id: "acta",
    workspace_name: "Acta",
    workspace_slug: "acta",
    ancestors: [{ id: "p", reference: "ACT-1", title: "Production readiness" }],
    source,
    comment_id: source === "comment" ? "comment-id" : "",
    excerpt: [
      { text: "A <script>window.bad=true</script> " },
      { text: "search", match: true },
      { text: " should find nested tasks." },
    ],
  });
  await context.route("http://localhost:8081/**", async (route) => {
    const url = new URL(route.request().url());
    const json = (v, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(v),
      });
    if (url.pathname === "/api/workspaces")
      return json({
        workspaces: [
          { id: "acta", name: "Acta" },
          { id: "peios", name: "Peios" },
        ],
        more: false,
      });
    if (url.pathname === "/api/tasks/search") {
      queries.push(url.searchParams);
      if (fail)
        return json(
          { error: { message: "Search is temporarily unavailable" } },
          503,
        );
      if (url.searchParams.get("q") === "empty")
        return json({ tasks: [], more: false, cursor: "" });
      return json(
        url.searchParams.get("cursor")
          ? { tasks: [result("3")], more: false, cursor: "" }
          : {
              tasks: [result("1"), result("2", "comment")],
              more: true,
              cursor: "next",
            },
      );
    }
    if (url.pathname === "/api/tasks/task/comments/root/replies") {
      const older = !!url.searchParams.get("cursor");
      await new Promise((resolve) => setTimeout(resolve, 40));
      return json({
        entries: [
          {
            id: older ? "old-reply" : "new-reply",
            first_event: older ? "2" : "3",
            last_event: older ? "2" : "3",
            actor: { id: "user", username: "Reviewer", display_name: null },
            kind: "comment",
            before: {},
            after: {},
            count: 1,
            unread: false,
            started_at: "2026-09-01T00:00:00Z",
            updated_at: "2026-09-01T00:00:00Z",
            comment: {
              body: older ? "The matching older reply" : "Newest reply",
              version: 1,
              deleted: false,
              edited: false,
              can_edit: false,
              can_delete: false,
            },
          },
        ],
        more: !older,
        cursor: older ? "" : "older",
        unread: false,
      });
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
    return route.fulfill({
      contentType: "text/html",
      body: '<html><head><meta charset="utf-8"><link rel="stylesheet" href="/fixture.css"></head><body><div id="app"></div><script src="/fixture.js"></script></body></html>',
    });
  });
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto("http://localhost:8081");
  await page.getByRole("button", { name: "Search tasks", exact: true }).click();
  const input = page.getByRole("combobox", { name: "Search tasks" });
  await input.fill("search");
  await page.getByRole("option").nth(1).waitFor();
  assert.equal(await page.getByRole("option").count(), 2);
  assert.equal(await page.evaluate(() => window.bad), undefined);
  await page.screenshot({ path: "/tmp/acta98-search.png" });
  const selectedIndex = async () =>
    (await input.getAttribute("aria-activedescendant")).split("-").at(-1);
  await input.press("Control+p");
  assert.equal(await selectedIndex(), "1");
  await input.press("Control+n");
  assert.equal(await selectedIndex(), "0");
  assert.equal(await input.inputValue(), "search");
  await input.press("ArrowDown");
  await input.press("Enter");
  await page.waitForFunction(
    () =>
      document.querySelector("[data-testid=selected]").textContent ===
      "2:comment-id",
  );
  assert.equal(await page.getByRole("dialog").count(), 0);
  await page.getByRole("button", { name: "Search tasks", exact: true }).click();
  await page.getByRole("option").nth(1).waitFor();
  await page.getByRole("button", { name: "Load more results" }).click();
  await page.getByRole("option").nth(2).waitFor();
  assert.equal(queries.at(-1).get("cursor"), "next");
  await page
    .getByRole("button", { name: "Search workspace: All workspaces" })
    .click();
  await page.getByRole("option", { name: "Peios", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelectorAll(".results>[role=option]").length === 2,
  );
  assert.equal(queries.at(-1).get("workspace"), "peios");
  assert.equal(queries.at(-1).get("cursor"), "");
  await input.fill("empty");
  await page
    .getByText("No matching tasks. Try different words or another workspace.")
    .waitFor();
  fail = true;
  await input.fill("failure");
  await page.getByRole("alert").waitFor();
  fail = false;
  await page.getByRole("button", { name: "Try again" }).click();
  await page.getByRole("option").nth(1).waitFor();
  await page.setViewportSize({ width: 390, height: 780 });
  await page.screenshot({ path: "/tmp/acta98-search-mobile.png" });
  assert.ok(
    await page.evaluate(
      () =>
        document.querySelector("dialog").getBoundingClientRect().right <=
        innerWidth,
    ),
  );
  await input.press("Escape");
  assert.equal(await page.getByRole("dialog").count(), 0);
  await page.getByRole("button", { name: "Open matching reply" }).click();
  await page.getByText("The matching older reply", { exact: true }).waitFor();
  await page.waitForFunction(
    () => document.activeElement?.getAttribute("data-read-id") === "old-reply",
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS: search results, escaped highlights, keyboard comment selection, pagination, workspace reset, empty/error recovery, mobile fit, Escape and older reply focus",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
