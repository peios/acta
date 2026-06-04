// Acta Web Push opt-in. Drives the account page's "Notifications" toggle: it
// registers the service worker, subscribes/unsubscribes the browser's push
// manager, and tells the server. No framework, vanilla ES, no build step — and
// it no-ops on any page without the [data-push] section.
(function () {
  "use strict";

  var root = document.querySelector("[data-push]");
  if (!root) return;

  var btn = document.getElementById("push-toggle");
  var statusEl = root.querySelector("[data-push-status]");
  var errEl = document.querySelector("[data-push-error]");
  var vapidKey = root.getAttribute("data-vapid-key") || "";

  var supported =
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window;

  function csrfToken() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.getAttribute("content") : "";
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
      body: body ? JSON.stringify(body) : null,
      credentials: "same-origin",
    });
  }

  // VAPID keys travel as base64url; PushManager wants the raw bytes.
  function urlB64ToBytes(s) {
    s = s.replace(/-/g, "+").replace(/_/g, "/");
    var pad = s.length % 4;
    if (pad) s += "====".slice(pad);
    var bin = atob(s);
    var out = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  }

  function setStatus(text) {
    if (statusEl) statusEl.textContent = text;
  }

  function showError(text) {
    if (!errEl) return;
    errEl.textContent = text;
    errEl.hidden = !text;
  }

  function render(on) {
    showError("");
    if (Notification.permission === "denied") {
      btn.hidden = true;
      setStatus("Blocked in your browser settings for this site.");
      return;
    }
    btn.hidden = false;
    btn.disabled = false;
    btn.textContent = on ? "Disable on this device" : "Enable on this device";
    setStatus(on ? "On for this device." : "Off on this device.");
  }

  var reg = null;

  async function enable() {
    btn.disabled = true;
    var perm = await Notification.requestPermission();
    if (perm !== "granted") {
      render(false);
      showError("Notifications weren't allowed.");
      return;
    }
    try {
      var sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlB64ToBytes(vapidKey),
      });
      var resp = await postJSON("/account/push/subscribe", sub.toJSON());
      if (!resp.ok) throw new Error("subscribe failed: " + resp.status);
      render(true);
    } catch (e) {
      showError("Couldn't enable notifications. " + (e && e.message ? e.message : ""));
      render(false);
    }
  }

  async function disable() {
    btn.disabled = true;
    try {
      var sub = await reg.pushManager.getSubscription();
      if (sub) {
        var endpoint = sub.endpoint;
        await sub.unsubscribe();
        await postJSON("/account/push/unsubscribe", { endpoint: endpoint });
      }
    } catch (e) {
      // Best-effort: even if the network call fails, reflect the local state.
    }
    render(false);
  }

  async function init() {
    if (!supported) {
      setStatus("This browser doesn't support push notifications.");
      return;
    }
    if (!vapidKey) {
      setStatus("Push isn't configured on the server.");
      return;
    }
    try {
      reg = await navigator.serviceWorker.register("/sw.js");
      var sub = await reg.pushManager.getSubscription();
      render(!!sub);
      btn.addEventListener("click", function () {
        var on = btn.textContent.indexOf("Disable") === 0;
        if (on) disable();
        else enable();
      });
    } catch (e) {
      setStatus("Couldn't start the notifications worker.");
    }
  }

  init();
})();
