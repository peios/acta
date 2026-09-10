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
const root = fileURLToPath(new URL("../", import.meta.url));
const out = mkdtempSync(join(tmpdir(), "acta-properties-test-"));
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
      entry: join(root, "tests/fixtures/task-properties-entry.js"),
      name: "PropertiesFixture",
      formats: ["iife"],
      fileName: () => "fixture.js",
    },
    cssCodeSplit: false,
  },
});
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({
    viewport: { width: 1200, height: 800 },
  });
  const errors = [],
    requests = [],
    writes = [];
  page.on("pageerror", (e) => errors.push(e.message));
  let tasks = [
    {
      id: "one",
      workspace_id: "review",
      number: 1,
      reference: "QA-1",
      title: "Metadata review",
      description: "",
      status_id: "todo",
      parent_id: "",
      priority: "high",
      type: "none",
      size: "s",
      assignees: [],
      descendant_assignees: [],
      ancestors: [],
      children: 0,
      versions: { priority: 1, type: 1, size: 1 },
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  ];
  await page.route("http://localhost:8081/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (!url.pathname.startsWith("/api/"))
      return route.fulfill({
        contentType: "text/html",
        body: '<div id="app"></div>',
      });
    requests.push(url.search);
    if (request.method() === "POST") {
      const input = request.postDataJSON();
      writes.push(input);
      if (url.pathname === "/api/tasks/one") {
        tasks[0] = {
          ...tasks[0],
          [input.field]: input.value,
          versions: {
            ...tasks[0].versions,
            [input.field]: tasks[0].versions[input.field] + 1,
          },
        };
        return route.fulfill({ json: tasks[0] });
      }
      tasks.push({
        ...tasks[0],
        priority: "none",
        type: "none",
        size: "none",
        ...input,
        id: "two",
        reference: "QA-2",
        number: 2,
      });
      return route.fulfill({ json: tasks[1] });
    }
    if (url.pathname.endsWith("/task-people"))
      return route.fulfill({ json: { people: [] } });
    let rows = tasks.filter(
      (t) =>
        !url.searchParams.get("group") ||
        t[url.searchParams.get("group")] === url.searchParams.get("group_id"),
    );
    for (const field of ["priority", "type", "size"]) {
      const selected = url.searchParams.getAll(field);
      if (selected.length)
        rows = rows.filter((t) => selected.includes(t[field]));
    }
    return route.fulfill({
      json: {
        tasks: rows,
        total: rows.length,
        more: false,
        cursor: "",
        next: 0,
      },
    });
  });
  await page.goto("http://localhost:8081/_review/task-properties-test");
  await page.addStyleTag({
    content: readFileSync(join(out, "acta2-web.css"), "utf8"),
  });
  await page.addScriptTag({
    content: readFileSync(join(out, "fixture.js"), "utf8"),
  });
  await page
    .getByRole("button", { name: "QA-1 Metadata review", exact: true })
    .waitFor();
  // Switching between groups sharing the None key must fetch a new window.
  const statusTrigger = page.getByRole("button", {
    name: "Status: To do",
    exact: true,
  });
  await statusTrigger.click();
  await page.getByRole("listbox", { name: "Task status" }).waitFor();
  await statusTrigger.click();
  await page
    .getByRole("listbox", { name: "Task status" })
    .waitFor({ state: "hidden", timeout: 2000 });
  await statusTrigger.focus();
  await statusTrigger.press("ArrowDown");
  await page.getByRole("listbox", { name: "Task status" }).press("Escape");
  assert.ok(
    await statusTrigger.evaluate((el) => document.activeElement === el),
    "Escape returns focus to status trigger",
  );
  await page.getByRole("button", { name: "Display", exact: true }).click();
  const groupTrigger = page.getByRole("button", {
    name: "Group by: Priority",
    exact: true,
  });
  await groupTrigger.click();
  await page.getByRole("listbox", { name: "Group by", exact: true }).waitFor();
  await groupTrigger.click();
  await page
    .getByRole("listbox", { name: "Group by", exact: true })
    .waitFor({ state: "hidden", timeout: 2000 });
  assert.ok(
    await page.getByRole("button", { name: "Board", exact: true }).isVisible(),
    "closing the nested picker keeps Display open",
  );
  await page
    .getByRole("button", { name: "Group by: Priority", exact: true })
    .click();
  await page.getByRole("option", { name: "Type", exact: true }).click();
  await page.getByRole("button", { name: "Display", exact: true }).click();
  await page.waitForFunction(
    () =>
      document.querySelector(".task-open")?.textContent === "Metadata review",
  );
  assert.ok(
    requests.some(
      (q) => q.includes("group=type") && q.includes("group_id=none"),
    ),
  );
  await page
    .getByRole("button", { name: "Sort by Priority", exact: true })
    .first()
    .click();
  await page.waitForTimeout(80);
  assert.ok(
    requests.some(
      (q) => q.includes("sort=priority") && q.includes("direction=asc"),
    ),
  );
  await page.getByRole("button", { name: "Filter", exact: true }).click();
  await page.getByRole("button", { name: "Urgent", exact: true }).click();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await page
    .getByRole("button", { name: "QA-1 Metadata review", exact: true })
    .waitFor({ state: "hidden" });
  await page.getByRole("button", { name: "Filter 1", exact: true }).click();
  await page.getByRole("button", { name: "Clear", exact: true }).click();
  await page.getByRole("button", { name: "Done", exact: true }).click();
  await page.getByRole("button", { name: "Display", exact: true }).click();
  await page.getByRole("button", { name: "Board", exact: true }).click();
  await page.getByRole("button", { name: "Display", exact: true }).click();
  const card = page.getByRole("button", {
    name: "Open QA-1: Metadata review",
    exact: true,
  });
  await card.waitFor();
  await card.dragTo(
    page
      .locator(".lane")
      .filter({ has: page.getByRole("heading", { name: "Bug", exact: true }) }),
  );
  await page.waitForTimeout(120);
  assert.ok(
    writes.some(
      (w) => w.field === "type" && w.value === "bug" && w.version === 1,
    ),
    "property drag",
  );
  await page
    .getByRole("button", { name: "Create new task in Feature", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "New task title in Feature", exact: true })
    .fill("New feature");
  await page
    .getByRole("textbox", { name: "New task title in Feature", exact: true })
    .press("Enter");
  await page
    .getByRole("button", { name: "Open QA-2: New feature", exact: true })
    .waitFor();
  assert.ok(
    writes.some((w) => w.title === "New feature" && w.type === "feature"),
    "column creation",
  );
  await page.screenshot({
    path: join(tmpdir(), "acta88-board.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 780 });
  await page.locator(".board").evaluate((node) => {
    const bug = node.querySelector('[aria-label="Bug"]');
    node.scrollLeft = bug.offsetLeft - node.offsetLeft;
  });
  await page.screenshot({
    path: join(tmpdir(), "acta88-board-mobile.png"),
    fullPage: true,
  });
  assert.deepEqual(errors, []);
  console.log(
    "PASS: metadata grouping refresh, sort, filtering, board drag, inline creation and mobile render",
  );
} finally {
  await browser.close();
  rmSync(out, { recursive: true, force: true });
}
