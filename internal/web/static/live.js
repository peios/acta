// live.js — the Server-Sent Events client behind Acta's live updates. Loaded on
// every signed-in page (the notification bell is global); the board layers its
// own card/modal handling on top by listening for the `acta:live` event this
// dispatches. No framework, vanilla ES.
(() => {
  'use strict';

  // A per-tab client id, stamped onto every mutating request as X-Acta-Client
  // and echoed back on the events it triggers, so this tab can ignore its own
  // updates (it already applied them locally). sessionStorage keeps it stable
  // across reloads but distinct per tab.
  function clientId() {
    let v = sessionStorage.getItem('acta.cid');
    if (!v) {
      v = (crypto.randomUUID && crypto.randomUUID()) || String(Math.random()).slice(2);
      sessionStorage.setItem('acta.cid', v);
    }
    return v;
  }
  window.actaClientId = clientId;
  const me = clientId();

  // --- notification bell ---

  const bell = () => document.querySelector('.bell');

  function setBellCount(n) {
    const b = bell();
    if (!b) return;
    const btn = b.querySelector('.bell-btn');
    if (!btn) return;
    let dot = btn.querySelector('.bell-dot');
    if (!n || n < 1) {
      if (dot) dot.remove();
      return;
    }
    if (!dot) {
      dot = document.createElement('span');
      dot.className = 'bell-dot';
      btn.appendChild(dot);
    }
    dot.textContent = n < 10 ? String(n) : '9+';
  }

  function prependNotif(msg) {
    const b = bell();
    if (!b) return;
    const list = b.querySelector('.notifs-list');
    if (!list) return;
    const empty = list.querySelector('.notif-empty');
    if (empty) empty.remove();

    const a = document.createElement('a');
    a.className = 'notif unread';
    a.href = msg.url || '#';

    const dot = document.createElement('span');
    dot.className = 'notif-dot';

    const main = document.createElement('span');
    main.className = 'notif-main';

    const line = document.createElement('span');
    line.className = 'notif-line';
    const actor = document.createElement('b');
    actor.textContent = msg.actor || 'Someone';
    const title = document.createElement('b');
    title.textContent = msg.title || '';
    line.append(actor, document.createTextNode(' mentioned you on '), title);
    main.appendChild(line);

    if (msg.excerpt) {
      const ex = document.createElement('span');
      ex.className = 'notif-ex';
      ex.textContent = msg.excerpt;
      main.appendChild(ex);
    }
    const when = document.createElement('span');
    when.className = 'notif-when';
    when.textContent = msg.when || 'just now';
    main.appendChild(when);

    a.append(dot, main);
    list.insertBefore(a, list.firstChild);
  }

  // --- event stream ---

  function handle(msg) {
    if (!msg || typeof msg.kind !== 'string') return;
    if (msg.origin && msg.origin === me) return; // ignore our own echo

    if (msg.kind === 'notif.add') {
      if (typeof msg.count === 'number') setBellCount(msg.count);
      prependNotif(msg);
      return;
    }
    // Board + modal events are applied by board.js when it's present.
    document.dispatchEvent(new CustomEvent('acta:live', { detail: msg }));
  }

  function connect() {
    const wrap = document.querySelector('.board-wrap');
    const slug = wrap ? wrap.dataset.slug : '';
    const url = '/events' + (slug ? '?workspace=' + encodeURIComponent(slug) : '');
    const es = new EventSource(url);
    es.onmessage = (e) => {
      let msg;
      try { msg = JSON.parse(e.data); } catch (_) { return; }
      handle(msg);
    };
    // EventSource reconnects on its own after a transient error; nothing to do.
  }

  document.addEventListener('DOMContentLoaded', connect);
})();
