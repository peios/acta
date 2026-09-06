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
    if (msg.nkind === 'activity' || msg.nkind === 'session') {
      // "<actor> moved to Doing" — the phrase is already rendered server-side.
      line.append(actor, document.createTextNode(' ' + (msg.summary || '')));
    } else {
      const title = document.createElement('b');
      title.textContent = msg.title || '';
      line.append(actor, document.createTextNode(' mentioned you on '), title);
    }
    main.appendChild(line);

    // Sub-line: the item title for an activity row, the comment excerpt for a mention.
    const sub = msg.nkind === 'activity' || msg.nkind === 'session' ? msg.title : msg.excerpt;
    if (sub) {
      const ex = document.createElement('span');
      ex.className = 'notif-ex';
      ex.textContent = sub;
      main.appendChild(ex);
    }
    const when = document.createElement('span');
    when.className = 'notif-when';
    when.textContent = msg.when || 'just now';
    main.appendChild(when);

    a.append(dot, main);
    list.insertBefore(a, list.firstChild);
  }

  // --- agent session presence ---
  //
  // Sidebar dots and list badges for agent sessions: grey = no harness holds
  // it, blue = held (resumable) but no process, green = running.

  function applyPresence(msg) {
    const state = msg.running ? 'running' : msg.held ? 'held' : 'off';
    document.querySelectorAll('[data-session-dot="' + msg.id + '"]').forEach((dot) => {
      dot.classList.toggle('is-running', state === 'running');
      dot.classList.toggle('is-held', state === 'held');
      dot.title = state === 'running' ? 'running' : state === 'held' ? 'idle (harness connected)' : 'offline';
    });
    document.querySelectorAll('[data-session-badge="' + msg.id + '"]').forEach((b) => {
      b.className = state === 'running' ? 'badge-live' : state === 'held' ? 'badge-held' : 'badge-off';
      b.textContent = state === 'running' ? 'running' : state === 'held' ? 'idle' : 'offline';
    });
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
    if (msg.kind === 'session.presence') {
      applyPresence(msg);
      return;
    }
    if (msg.kind === 'notif.count') {
      // the unread set changed without a new row (a session's rows read by opening it)
      setBellCount(msg.count);
      return;
    }
    if (msg.kind === 'session.renamed') {
      // the status marker in the title paints as a mark; the text shows the rest
      const parts = window.sessionStatus ? window.sessionStatus.apply(msg.id, msg.title) : { status: '', bare: msg.title };
      document.querySelectorAll('[data-session-name="' + msg.id + '"]').forEach((el) => {
        if (el.matches('[data-rename-input]')) return;
        el.textContent = parts.bare;
        if (el.title) el.title = parts.bare;
      });
      const stage = document.querySelector('.chat-stage[data-session="' + msg.id + '"]');
      if (stage) { stage.dataset.status = parts.status; document.title = parts.bare + ' · Acta'; }
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
