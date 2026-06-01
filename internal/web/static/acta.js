// Acta passkeys — the minimum WebAuthn glue htmx can't do. No framework, no
// build step, vanilla ES. Wires two optional buttons if present on the page:
//   #passkey-login[data-return-to]      -> usernameless sign-in
//   #passkey-add[data-success]          -> register a passkey (optional #passkey-name)
(function () {
  "use strict";

  function csrfToken() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? m.getAttribute("content") : "";
  }

  function b64urlToBuf(s) {
    s = s.replace(/-/g, "+").replace(/_/g, "/");
    var pad = s.length % 4;
    if (pad) s += "====".slice(pad);
    var bin = atob(s);
    var buf = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i);
    return buf.buffer;
  }

  function bufToB64url(buf) {
    var bytes = new Uint8Array(buf);
    var bin = "";
    for (var i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function postJSON(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken() },
      body: body ? JSON.stringify(body) : null,
      credentials: "same-origin",
    });
  }

  async function registerPasskey(name, success) {
    var finishURL = "/settings/passkeys/register/finish";
    if (name) finishURL += "?name=" + encodeURIComponent(name);

    var resp = await postJSON("/settings/passkeys/register/begin", null);
    if (!resp.ok) throw new Error("begin failed: " + resp.status);
    var options = (await resp.json()).publicKey;

    options.challenge = b64urlToBuf(options.challenge);
    options.user.id = b64urlToBuf(options.user.id);
    (options.excludeCredentials || []).forEach(function (c) { c.id = b64urlToBuf(c.id); });

    var cred = await navigator.credentials.create({ publicKey: options });
    var body = {
      id: cred.id,
      rawId: bufToB64url(cred.rawId),
      type: cred.type,
      response: {
        attestationObject: bufToB64url(cred.response.attestationObject),
        clientDataJSON: bufToB64url(cred.response.clientDataJSON),
      },
      clientExtensionResults: cred.getClientExtensionResults ? cred.getClientExtensionResults() : {},
    };
    if (cred.response.getTransports) body.response.transports = cred.response.getTransports();

    var fin = await postJSON(finishURL, body);
    if (!fin.ok) throw new Error("finish failed: " + fin.status);
    window.location = success || "/settings/security";
  }

  async function loginPasskey(returnTo) {
    var resp = await postJSON("/login/passkey/begin", null);
    if (!resp.ok) throw new Error("begin failed: " + resp.status);
    var options = (await resp.json()).publicKey;

    options.challenge = b64urlToBuf(options.challenge);
    (options.allowCredentials || []).forEach(function (c) { c.id = b64urlToBuf(c.id); });

    var assertion = await navigator.credentials.get({ publicKey: options });
    var body = {
      id: assertion.id,
      rawId: bufToB64url(assertion.rawId),
      type: assertion.type,
      response: {
        authenticatorData: bufToB64url(assertion.response.authenticatorData),
        clientDataJSON: bufToB64url(assertion.response.clientDataJSON),
        signature: bufToB64url(assertion.response.signature),
        userHandle: assertion.response.userHandle ? bufToB64url(assertion.response.userHandle) : "",
      },
      clientExtensionResults: assertion.getClientExtensionResults ? assertion.getClientExtensionResults() : {},
    };

    var fin = await postJSON("/login/passkey/finish", body);
    if (!fin.ok) throw new Error("login failed: " + fin.status);
    window.location = returnTo || "/";
  }

  function fail(btn, err) {
    console.error(err);
    var box = document.querySelector("[data-passkey-error]");
    if (box) box.textContent = "Passkey failed — please try again.";
    if (btn) btn.disabled = false;
  }

  document.addEventListener("DOMContentLoaded", function () {
    var login = document.getElementById("passkey-login");
    if (login) {
      login.addEventListener("click", function () {
        login.disabled = true;
        loginPasskey(login.dataset.returnTo).catch(function (e) { fail(login, e); });
      });
    }

    var add = document.getElementById("passkey-add");
    if (add) {
      add.addEventListener("click", function () {
        add.disabled = true;
        var nameEl = document.getElementById("passkey-name");
        registerPasskey(nameEl ? nameEl.value : "", add.dataset.success).catch(function (e) { fail(add, e); });
      });
    }
  });
})();
