// Acta service worker. Two jobs only: show a notification when a Web Push
// arrives, and route a click on it to the item it points at. Registered by the
// account page's notification toggle (see push.js). Deliberately tiny — Acta is
// server-driven, so there's no offline cache to maintain.

self.addEventListener("push", function (event) {
  var data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (e) {
    data = {};
  }
  event.waitUntil(
    self.registration.showNotification(data.title || "Acta", {
      body: data.body || "",
      icon: "/static/icon-192.png",
      badge: "/static/icon-192.png",
      tag: data.tag || undefined, // collapse repeated pings about one item
      data: { url: data.url || "/" },
    })
  );
});

self.addEventListener("notificationclick", function (event) {
  event.notification.close();
  var url = (event.notification.data && event.notification.data.url) || "/";
  event.waitUntil(
    self.clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then(function (windows) {
        // Prefer steering an already-open Acta tab to the item over opening a
        // duplicate window.
        for (var i = 0; i < windows.length; i++) {
          var w = windows[i];
          if ("focus" in w) {
            if ("navigate" in w) {
              try {
                w.navigate(url);
              } catch (e) {
                /* cross-origin or detached — fall through to focus */
              }
            }
            return w.focus();
          }
        }
        if (self.clients.openWindow) return self.clients.openWindow(url);
      })
  );
});
