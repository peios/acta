import { createServer } from "node:http";
import { readFileSync, mkdtempSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import assert from "node:assert/strict";
const { chromium } = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const root = fileURLToPath(new URL("../static/", import.meta.url));
const subscription = randomUUID(),
  thread = randomUUID();
let status = 200;
let taskMode = false;
const task = randomUUID();
let hold = false,
  release;
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://localhost");
  if (url.pathname === "/api/push/notification") {
    const respond = () => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(
        JSON.stringify({
          id: url.searchParams.get("id"),
          revision: Number(url.searchParams.get("revision")),
          title: "Approval needed",
          thread_name: "Private review",
          thread_id: thread,
          lane_id: "child/lane",
          ...(taskMode
            ? {
                task_id: task,
                workspace_slug: "review",
                task_reference: "ACT-97",
                task_title: "Task notification",
              }
            : {}),
        }),
      );
    };
    if (hold) release = respond;
    else respond();
    return;
  }
  if (
    url.pathname === "/service-worker.js" ||
    url.pathname === "/manifest.webmanifest" ||
    url.pathname.startsWith("/icons/")
  ) {
    res.writeHead(200, {
      "Content-Type": url.pathname.endsWith(".js")
        ? "text/javascript"
        : url.pathname.endsWith(".webmanifest")
          ? "application/manifest+json"
          : "image/png",
      "Cache-Control": "no-cache",
    });
    res.end(readFileSync(join(root, url.pathname)));
    return;
  }
  res.writeHead(200, { "Content-Type": "text/html" });
  res.end(
    '<!doctype html><html><title>Acta PWA test</title><link rel="manifest" href="/manifest.webmanifest"><h1>Acta PWA test</h1></html>',
  );
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const profile = mkdtempSync(join(tmpdir(), "acta-pwa-"));
const context = await chromium.launchPersistentContext(profile, {
  headless: true,
  channel: "chromium",
  executablePath: process.env.ACTA_CHROMIUM_EXECUTABLE,
  permissions: ["notifications"],
});
try {
  await context.grantPermissions(["notifications"], { origin });
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  let registrationId;
  const errors = [];
  cdp.on("ServiceWorker.workerRegistrationUpdated", ({ registrations }) => {
    for (const r of registrations)
      if (r.scopeURL === origin + "/") registrationId = r.registrationId;
  });
  cdp.on("ServiceWorker.workerErrorReported", (e) =>
    errors.push(e.errorMessage),
  );
  await cdp.send("ServiceWorker.enable");
  await page.goto(origin);
  await page.evaluate(async () => {
    await navigator.serviceWorker.register("/service-worker.js");
    await navigator.serviceWorker.ready;
  });
  const config = async () =>
    page.evaluate(async (id) => {
      const reg = await navigator.serviceWorker.ready;
      await new Promise((resolve) => {
        const c = new MessageChannel();
        c.port1.onmessage = resolve;
        reg.active.postMessage({ type: "push-config", id }, [c.port2]);
      });
    }, subscription);
  await config();
  await page.waitForFunction(() => !!navigator.serviceWorker.controller);
  assert.ok(registrationId, "missing service-worker registration");
  const manifest = await (
    await page.request.get(origin + "/manifest.webmanifest")
  ).json();
  assert.equal(manifest.display, "standalone");
  for (const icon of manifest.icons) {
    assert.equal((await page.request.get(origin + icon.src)).status(), 200);
  }
  // Navigate the only page away: no Acta tab remains open.
  await page.goto("about:blank");
  const push = async (id = randomUUID(), revision = 1) => {
    await cdp.send("ServiceWorker.deliverPushMessage", {
      origin,
      registrationId,
      data: JSON.stringify({ id, revision, subscription }),
    });
    return id;
  };
  const worker = () => context.serviceWorkers()[0];
  const notices = () =>
    worker().evaluate(async () =>
      (await self.registration.getNotifications()).map((n) => ({
        title: n.title,
        body: n.body,
        data: n.data,
        tag: n.tag,
      })),
    );

  const until = async (fn, label) => {
    for (let n = 0; n < 80; n++) {
      if (await fn()) return;
      await new Promise((r) => setTimeout(r, 50));
    }
    console.log(
      "workerErrors",
      errors,
      "workers",
      context.serviceWorkers().length,
    );
    throw new Error(label);
  };
  const first = await push();
  await until(
    async () => (await notices()).length === 1,
    "background push was not displayed",
  );
  let notifications = await notices();
  assert.equal(notifications[0].title, "Approval needed");
  assert.equal(
    notifications[0].data.url,
    `/my-agents/${thread}?lane=child%2Flane`,
  );
  await push(first);
  await new Promise((r) => setTimeout(r, 100));
  assert.equal((await notices()).length, 1, "duplicate push displayed");
  // Restart worker and replay: the receipt survives process lifetime.
  await cdp.send("ServiceWorker.stopAllWorkers");
  await push(first);
  await until(
    async () => context.serviceWorkers().length > 0,
    "worker failed to restart",
  );
  await new Promise((r) => setTimeout(r, 150));
  assert.equal((await notices()).length, 1);
  status = 404;
  await push();
  await new Promise((r) => setTimeout(r, 150));
  assert.equal((await notices()).length, 1, "resolved notice displayed");
  status = 503;
  await push();
  await until(
    async () => (await notices()).length === 2,
    "missing offline fallback",
  );
  notifications = await notices();
  assert.ok(
    notifications.some(
      (n) => n.title === "Acta" && !n.body.includes("Private"),
    ),
    "offline fallback leaked content",
  );
  status = 401;
  await push();
  await new Promise((r) => setTimeout(r, 150));
  assert.equal(
    (await notices()).length,
    2,
    "revoked session displayed content",
  );
  status = 200;
  await push();
  await new Promise((r) => setTimeout(r, 150));
  assert.equal(
    (await notices()).length,
    2,
    "revoked local subscription survived",
  );
  await page.goto(origin + `/my-agents/${thread}`);
  await config();
  await page.bringToFront();
  await push();
  await new Promise((r) => setTimeout(r, 150));
  assert.equal(
    (await notices()).length,
    2,
    "focused conversation was interrupted",
  );
  taskMode = true;
  await page.goto(origin + `/workspaces/review?task=${task}`);
  await push();
  await new Promise((r) => setTimeout(r, 150));
  assert.equal((await notices()).length, 2, "focused task was interrupted");
  await page.goto("about:blank");
  await push();
  await until(async () => (await notices()).length === 3, "task push missing");
  const taskNotice = (await notices()).find((n) => n.body.includes("ACT-97"));
  assert.ok(taskNotice, "task title missing from push");
  assert.equal(taskNotice.data.url, `/workspaces/review?task=${task}`);
  taskMode = false;
  // Clear while an authenticated fetch is in flight: its late response must
  // not display an alert after logout.
  await page.goto(origin + "/my-agents");
  hold = true;
  await push();
  await until(() => !!release, "push fetch did not start");
  await page.evaluate(async () => {
    const reg = await navigator.serviceWorker.ready;
    await new Promise((resolve) => {
      const c = new MessageChannel();
      c.port1.onmessage = resolve;
      reg.active.postMessage({ type: "clear-push" }, [c.port2]);
    });
  });
  release();
  hold = false;
  await new Promise((r) => setTimeout(r, 150));
  assert.equal(
    (await notices()).length,
    0,
    "signout left notifications visible",
  );
  await context.setOffline(true);
  await page.reload();
  await page.getByRole("heading", { name: "You're offline" }).waitFor();
  assert.deepEqual(await page.evaluate(() => caches.keys()), []);
  assert.deepEqual(errors, []);
  console.log(
    "PASS: real service worker installation, manifest/icons, push without an open Acta tab, durable duplicate suppression, resolved/revoked filtering, generic offline fallback, focused-thread suppression, logout cleanup, offline navigation",
  );
} finally {
  await context.close();
  rmSync(profile, { recursive: true, force: true });
  await new Promise((resolve) => server.close(resolve));
}
