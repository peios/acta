/* Acta's worker deliberately does not cache account pages or API responses. */
const DB = "acta-push-v1";
let queue = Promise.resolve();
let cleared = 0;
function database() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB, 1);
    request.onupgradeneeded = () => request.result.createObjectStore("state");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}
async function read(key) {
  const db = await database();
  try {
    return await new Promise((resolve, reject) => {
      const tx = db.transaction("state", "readonly"),
        req = tx.objectStore("state").get(key);
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  } finally {
    db.close();
  }
}
async function write(key, value) {
  const db = await database();
  try {
    await new Promise((resolve, reject) => {
      const tx = db.transaction("state", "readwrite");
      tx.objectStore("state").put(value, key);
      tx.oncomplete = resolve;
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  } finally {
    db.close();
  }
}
self.addEventListener("install", (event) =>
  event.waitUntil(self.skipWaiting()),
);
self.addEventListener("activate", (event) =>
  event.waitUntil(self.clients.claim()),
);
self.addEventListener("message", (event) => {
  if (
    !event.source?.url ||
    new URL(event.source.url).origin !== self.location.origin
  )
    return;
  const message = event.data;
  if (message?.type === "push-config") {
    event.waitUntil(
      write("subscription", message.id || "").then(() =>
        event.ports[0]?.postMessage({ ok: true }),
      ),
    );
  }
  if (message?.type === "clear-push") {
    cleared++;
    event.waitUntil(
      (async () => {
        await write("subscription", "");
        for (const notification of await self.registration.getNotifications())
          notification.close();
        event.ports[0]?.postMessage({ ok: true });
      })(),
    );
  }
});
self.addEventListener("fetch", (event) => {
  if (event.request.mode !== "navigate") return;
  event.respondWith(
    fetch(event.request).catch(
      () =>
        new Response(
          `<!doctype html><html lang="en"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Offline · Acta</title><style>body{background:#202020;color:#e6e6e6;font:16px system-ui;display:grid;place-content:center;min-height:90vh;padding:24px;text-align:center}p{color:#aaa;max-width:36rem;line-height:1.6}a{color:#bccadd}</style><h1>You're offline</h1><p>Reconnect to open Acta. Your agents may still be running on their development machines.</p><a href="/my-agents">Try again</a></html>`,
          {
            status: 503,
            headers: {
              "Content-Type": "text/html; charset=utf-8",
              "Cache-Control": "no-store",
            },
          },
        ),
    ),
  );
});
const uuid = (value) =>
  typeof value === "string" && /^[0-9a-f-]{36}$/i.test(value);
async function receive(data) {
  const generation = cleared;
  if (
    !uuid(data?.id) ||
    !uuid(data?.subscription) ||
    !Number.isSafeInteger(data.revision) ||
    data.revision < 1
  )
    return;
  if ((await read("subscription")) !== data.subscription) return;
  const receipt = `${data.id}:${data.revision}`;
  const seen = (await read("receipts")) || [];
  if (seen.includes(receipt)) return;
  const query = new URLSearchParams({
    id: data.id,
    subscription: data.subscription,
    revision: String(data.revision),
  });
  let n;
  try {
    const response = await fetch(`/api/push/notification?${query}`, {
      credentials: "include",
      cache: "no-store",
      signal: AbortSignal.timeout(10000),
    });
    if (response.status === 401 || response.status === 403) {
      await write("subscription", "");
      return;
    }
    if (response.status === 404) return; // Read, resolved, expired or revoked.
    if (!response.ok) throw new Error("Acta unavailable");
    n = await response.json();
  } catch {
    // No task content or account information is carried in the push payload.
    // If Acta cannot be reached, show only a generic update, requiring sign-in
    // when opened. The durable inbox remains available after reconnecting.
    n = {
      title: "Acta",
      thread_name: "There is an update. Open Acta to check.",
    };
  }
  let url = "/my-agents";
  if (uuid(n.task_id) && typeof n.workspace_slug === "string") {
    url = `/workspaces/${encodeURIComponent(n.workspace_slug)}?task=${n.task_id}`;
    const clients = await self.clients.matchAll({
      type: "window",
      includeUncontrolled: true,
    });
    if (
      clients.some(
        (c) =>
          c.focused &&
          c.visibilityState === "visible" &&
          new URL(c.url).searchParams.get("task") === n.task_id,
      )
    )
      return;
  } else if (uuid(n.thread_id)) {
    const path = `/my-agents/${n.thread_id}`;
    const clients = await self.clients.matchAll({
      type: "window",
      includeUncontrolled: true,
    });
    if (
      clients.some(
        (c) =>
          c.focused &&
          c.visibilityState === "visible" &&
          new URL(c.url).pathname === path,
      )
    )
      return;
    url = path + (n.lane_id ? `?lane=${encodeURIComponent(n.lane_id)}` : "");
  }
  if (
    generation !== cleared ||
    (await read("subscription")) !== data.subscription
  )
    return;
  // Keep the same tag for retries or a corrected notification revision.
  await self.registration.showNotification(n.title, {
    body: n.task_id ? `${n.task_reference} · ${n.task_title}` : n.thread_name,
    tag: `acta:${data.id}`,
    icon: "/icons/acta-192.png",
    badge: "/icons/acta-192.png",
    renotify: false,
    data: {
      url,
      id: data.id,
      revision: data.revision,
      subscription: data.subscription,
    },
  });
  if (generation !== cleared) {
    for (const n of await self.registration.getNotifications({
      tag: `acta:${data.id}`,
    }))
      n.close();
    return;
  }
  await write("receipts", [...seen, receipt].slice(-1000));
}
self.addEventListener("push", (event) => {
  let data;
  try {
    data = event.data?.json();
  } catch {
    return;
  }
  queue = queue.catch(() => {}).then(() => receive(data));
  event.waitUntil(queue);
});
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    (async () => {
      const data = event.notification.data || {};
      const url = new URL(data.url || "/my-agents", self.location.origin);
      if (
        url.origin !== self.location.origin ||
        !/^\/my-agents(?:\/[0-9a-f-]{36})?$/.test(url.pathname)
      )
        return;
      const clients = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const client = clients.find(
        (c) => new URL(c.url).origin === self.location.origin,
      );
      if (client) {
        await client.focus();
        await client.navigate(url.href);
      } else await self.clients.openWindow(url.href);
      // The page acknowledges after authenticated loading. Clicking a stale alert
      // must never acknowledge another user's record after an account switch.
    })(),
  );
});
