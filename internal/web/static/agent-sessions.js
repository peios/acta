// agent-sessions.js — the new-session form on the My Agents page: pick a
// harness (the most recently used one is on by default), and choose a
// working directory with suggestions — where the harness runs, where its
// other sessions run, and directories completed from the harness host's
// filesystem as you type. No framework, vanilla ES.
(() => {
  'use strict';
  const form = document.querySelector('[data-new-session]');
  if (!form) return;
  let picks = [];
  try { picks = JSON.parse(form.dataset.picks || '[]') || []; } catch (_) {}
  const idInput = form.querySelector('[data-harness-id]');
  const cwd = form.querySelector('[data-cwd]');
  const menu = form.querySelector('[data-cwd-menu]');
  const hint = form.querySelector('[data-cwd-hint]');
  const backend = form.querySelector('[data-backend]');
  let cur = null;          // the picked harness
  let items = [];          // rows in the menu
  let active = -1;         // highlighted row
  let seq = 0;             // latest completion request
  let timer = null;

  function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; }
  function tilde(p) { return cur && cur.home && p.startsWith(cur.home + '/') ? '~' + p.slice(cur.home.length) : (cur && p === cur.home ? '~' : p); }
  function expand(p) { return cur && cur.home && (p === '~' || p.startsWith('~/')) ? cur.home + p.slice(1) : p; }

  function pick(id, userChosen) {
    const prev = cur;
    cur = picks.find(p => p.id === id) || null;
    idInput.value = cur ? cur.id : '';
    for (const b of form.querySelectorAll('[data-hpick]')) {
      const on = cur && b.dataset.hpick === cur.id;
      b.classList.toggle('is-on', !!on);
      b.setAttribute('aria-checked', on ? 'true' : 'false');
    }
    if (cur) {
      // the directory follows the harness until the user has typed their own
      const untouched = !cwd.value || (prev && cwd.value === tilde(prev.cwd)) || (prev && cwd.value === prev.cwd);
      if (untouched) cwd.value = tilde(cur.cwd || '');
      cwd.placeholder = tilde(cur.cwd || '') || '~/projects';
      if (backend) {
        const have = new Set(cur.backends || []);
        for (const o of backend.options) o.disabled = have.size > 0 && !have.has(o.value);
        if (backend.selectedOptions[0] && backend.selectedOptions[0].disabled) { const ok = [...backend.options].find(o => !o.disabled); if (ok) backend.value = ok.value; }
      }
      if (hint) hint.textContent = 'Runs on ' + cur.label + (cur.cwd ? ', in ' + tilde(cur.cwd) + ' unless you pick a directory.' : '.');
    }
    if (userChosen) close();
  }
  for (const b of form.querySelectorAll('[data-hpick]')) b.addEventListener('click', () => pick(b.dataset.hpick, true));

  // --- suggestions ---
  function close() { menu.hidden = true; cwd.setAttribute('aria-expanded', 'false'); active = -1; items = []; }
  function highlight(i) {
    active = i;
    [...menu.querySelectorAll('.cwd-item')].forEach((n, k) => n.classList.toggle('is-active', k === i));
    const n = menu.querySelectorAll('.cwd-item')[i]; if (n) n.scrollIntoView({ block: 'nearest' });
  }
  function choose(item) {
    // a completed directory keeps the shell feel: fill it and offer what's inside
    cwd.value = item.kind === 'fs' ? tilde(item.path) + '/' : tilde(item.path);
    cwd.focus();
    if (item.kind === 'fs') suggest(); else close();
  }
  function row(item, typed) {
    const b = el('button', 'cwd-item');
    b.type = 'button';
    const shown = tilde(item.path) + (item.kind === 'fs' ? '/' : '');
    const path = el('span', 'cwd-item-path');
    const t = typed && shown.toLowerCase().startsWith(typed.toLowerCase()) ? typed.length : 0;
    // the row reads right-to-left so a long path loses its start; the
    // left-to-right marks at both ends keep the slashes where they belong
    path.appendChild(document.createTextNode('\u200E'));
    if (t) { path.appendChild(el('b', null, shown.slice(0, t))); path.appendChild(document.createTextNode(shown.slice(t))); }
    else path.appendChild(document.createTextNode(shown));
    path.appendChild(document.createTextNode('\u200E'));
    b.appendChild(path);
    b.appendChild(el('span', 'cwd-kind is-' + item.kind, item.kind === 'harness' ? 'harness' : item.kind === 'session' ? 'session' : 'directory'));
    b.addEventListener('mousedown', (e) => e.preventDefault()); // keep focus in the box
    b.addEventListener('click', () => choose(item));
    return b;
  }
  function paint(list, typed) {
    menu.textContent = '';
    items = list;
    if (!list.length) { menu.appendChild(el('div', 'cwd-empty', typed ? 'No matching directory on ' + (cur ? cur.label : 'the harness') : 'Type a path')); }
    for (const it of list) menu.appendChild(row(it, typed));
    menu.hidden = false;
    cwd.setAttribute('aria-expanded', 'true');
    active = -1;
  }
  // known places first (the harness's own directory, its sessions'), then the host's completions
  function known(typed) {
    if (!cur) return [];
    const out = [];
    const seen = new Set();
    const add = (p, kind) => { if (!p || seen.has(p)) return; const shown = tilde(p); if (typed && !shown.toLowerCase().startsWith(typed.toLowerCase()) && !p.toLowerCase().startsWith(expand(typed).toLowerCase())) return; seen.add(p); out.push({ path: p, kind }); };
    add(cur.cwd, 'harness');
    for (const p of cur.cwds || []) add(p, 'session');
    return out;
  }
  async function suggest() {
    if (!cur) return;
    const typed = cwd.value.trim();
    const base = known(typed);
    paint(base, typed);
    const my = ++seq;
    let dirs = [];
    try {
      const r = await fetch('/account/harnesses/' + encodeURIComponent(cur.id) + '/dirs?path=' + encodeURIComponent(expand(typed)), { credentials: 'same-origin' });
      const j = await r.json();
      dirs = j.dirs || [];
    } catch (_) {}
    if (my !== seq) return; // a newer keystroke superseded this one
    const have = new Set(base.map(b => b.path));
    const fs = dirs.filter(d => !have.has(d)).map(d => ({ path: d, kind: 'fs' }));
    paint(base.concat(fs), typed);
  }
  cwd.addEventListener('focus', () => { if (menu.hidden) suggest(); });
  cwd.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(suggest, 120); });
  cwd.addEventListener('blur', () => setTimeout(close, 120));
  cwd.addEventListener('keydown', (e) => {
    if (menu.hidden) { if (e.key === 'ArrowDown') { e.preventDefault(); suggest(); } return; }
    if (e.key === 'ArrowDown') { e.preventDefault(); highlight(Math.min(items.length - 1, active + 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); highlight(Math.max(0, active - 1)); }
    else if (e.key === 'Enter') { if (active >= 0 && items[active]) { e.preventDefault(); choose(items[active]); } else close(); }
    else if (e.key === 'Tab' && items.length) {
      // shell-style: complete the highlighted row, else the first row (known
      // places are directories too, and they sort first); focus stays put
      e.preventDefault();
      choose(items[active >= 0 ? active : 0]);
    }
    else if (e.key === 'Escape') { e.preventDefault(); close(); }
  });
  form.addEventListener('submit', () => { cwd.value = expand(cwd.value.trim()); });

  const first = picks.find(p => p.recent) || picks[0];
  if (first) pick(first.id, false);
})();

// The import picker: the conversations a harness's backend keeps on its host,
// listed for choosing. Nothing is imported wholesale — a machine holds
// hundreds of transcripts and most are throwaway — and a conversation Acta
// already has a session for is shown but cannot be picked twice.
(() => {
  'use strict';
  const form = document.querySelector('[data-import]');
  const open = document.querySelector('[data-import-open]');
  if (!form || !open) return;
  let picks = [];
  try { picks = JSON.parse(form.dataset.picks || '[]') || []; } catch (_) {}
  const harnessInput = form.querySelector('[data-import-harness]');
  const host = form.querySelector('[data-import-host]');
  const list = form.querySelector('[data-import-list]');
  const filter = form.querySelector('[data-import-filter]');
  const hint = form.querySelector('[data-import-hint]');
  const submit = form.querySelector('[data-import-submit]');
  const all = form.querySelector('[data-import-all]');
  const backendInput = form.querySelector('[data-import-backend-input]');
  let cur = null;
  let backend = 'claude-code';
  let items = [];
  let seq = 0;

  function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text != null) n.textContent = text; return n; }
  function tilde(p) { return cur && cur.home && p && p.startsWith(cur.home + '/') ? '~' + p.slice(cur.home.length) : (cur && p === cur.home ? '~' : (p || '')); }
  function ago(iso) {
    const t = new Date(iso).getTime(); if (!t) return '';
    const s = Math.max(0, (Date.now() - t) / 1000);
    if (s < 60) return 'just now';
    if (s < 3600) return Math.floor(s / 60) + ' min ago';
    if (s < 86400) return Math.floor(s / 3600) + ' h ago';
    if (s < 86400 * 14) return Math.floor(s / 86400) + ' d ago';
    const d = new Date(t);
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === new Date().getFullYear() ? undefined : 'numeric' });
  }
  function size(n) { return n < 1024 * 1024 ? Math.max(1, Math.round(n / 1024)) + ' KB' : (n / 1048576).toFixed(1) + ' MB'; }

  function count() {
    const n = form.querySelectorAll('input[name="transcript"]:checked').length;
    submit.disabled = n === 0;
    submit.textContent = n ? 'Import selected (' + n + ')' : 'Import selected';
  }
  function row(it) {
    const lab = el('label', 'import-row' + (it.held ? ' is-held' : ''));
    const cb = el('input'); cb.type = 'checkbox'; cb.name = 'transcript'; cb.value = it.id; cb.disabled = !!it.held;
    cb.addEventListener('change', count);
    lab.appendChild(cb);
    const main = el('div', 'import-main');
    main.appendChild(el('div', 'import-name', it.title || it.first || it.id));
    if (it.title && it.first) main.appendChild(el('div', 'import-first', it.first));
    const meta = el('div', 'import-meta');
    if (it.cwd) { const c = el('span', 'import-cwd', tilde(it.cwd)); c.title = it.cwd; meta.appendChild(c); }
    const when = el('span', null, ago(it.updated)); when.title = new Date(it.updated).toLocaleString(); meta.appendChild(when);
    meta.appendChild(el('span', null, size(it.size || 0)));
    main.appendChild(meta);
    lab.appendChild(main);
    lab.appendChild(el('span', 'import-state', it.held ? 'in Acta' : ''));
    lab.dataset.text = ((it.title || '') + ' ' + (it.first || '') + ' ' + (it.cwd || '')).toLowerCase();
    return lab;
  }
  function paint() {
    list.textContent = '';
    if (!items.length) { list.appendChild(el('div', 'import-empty', 'No conversations found on ' + (cur ? cur.label : 'the harness') + '.')); hint.textContent = ''; return; }
    for (const it of items) list.appendChild(row(it));
    applyFilter();
  }
  function applyFilter() {
    const q = filter.value.trim().toLowerCase();
    let shown = 0;
    for (const r of list.querySelectorAll('.import-row')) { const on = !q || r.dataset.text.includes(q); r.hidden = !on; if (on) shown++; }
    hint.textContent = items.length ? (q ? shown + ' of ' + items.length : items.length) + (items.length === 1 ? ' conversation' : ' conversations') : '';
  }
  async function load() {
    if (!cur) return;
    harnessInput.value = cur.id;
    host.textContent = cur.label;
    list.textContent = '';
    list.appendChild(el('div', 'import-empty', 'Looking on ' + cur.label + '…'));
    hint.textContent = '';
    const my = ++seq;
    let got = [];
    let err = '';
    try {
      const r = await fetch('/account/harnesses/' + encodeURIComponent(cur.id) + '/transcripts?backend=' + encodeURIComponent(backend), { credentials: 'same-origin' });
      const j = await r.json();
      got = j.items || [];
      err = j.error || '';
    } catch (e) { err = 'could not reach Acta'; }
    if (my !== seq) return;
    items = got;
    if (err) { list.textContent = ''; list.appendChild(el('div', 'import-empty', 'Could not list conversations: ' + err)); return; }
    paint();
    count();
  }
  // the backends the picked harness can run; the first is on unless the
  // user chose another it also has
  function paintBackends() {
    const have = new Set(cur && cur.backends && cur.backends.length ? cur.backends : []);
    const btns = [...form.querySelectorAll('[data-import-backend]')];
    for (const b of btns) b.hidden = have.size > 0 && !have.has(b.dataset.importBackend);
    const shown = btns.filter(b => !b.hidden);
    if (!shown.some(b => b.dataset.importBackend === backend) && shown[0]) backend = shown[0].dataset.importBackend;
    for (const b of btns) { const on = b.dataset.importBackend === backend; b.classList.toggle('is-on', on); b.setAttribute('aria-checked', on ? 'true' : 'false'); }
    backendInput.value = backend;
  }
  function pick(id) {
    cur = picks.find(p => p.id === id) || null;
    for (const b of form.querySelectorAll('[data-import-pick]')) {
      const on = cur && b.dataset.importPick === cur.id;
      b.classList.toggle('is-on', !!on);
      b.setAttribute('aria-checked', on ? 'true' : 'false');
    }
    paintBackends();
    load();
  }
  for (const b of form.querySelectorAll('[data-import-pick]')) b.addEventListener('click', () => pick(b.dataset.importPick));
  for (const b of form.querySelectorAll('[data-import-backend]')) b.addEventListener('click', () => { backend = b.dataset.importBackend; paintBackends(); load(); });
  open.addEventListener('click', () => {
    const was = form.hidden;
    form.hidden = !was;
    open.setAttribute('aria-expanded', was ? 'true' : 'false');
    if (was) {
      // the harness the new-session form has on is the natural default
      const onForm = document.querySelector('[data-hpick].is-on');
      pick(onForm ? onForm.dataset.hpick : (picks.find(p => p.recent) || picks[0] || {}).id);
      filter.focus();
    }
  });
  form.querySelector('[data-import-cancel]').addEventListener('click', () => { form.hidden = true; open.setAttribute('aria-expanded', 'false'); });
  filter.addEventListener('input', applyFilter);
  all.addEventListener('click', () => {
    const boxes = [...list.querySelectorAll('.import-row:not([hidden]) input:not(:disabled)')];
    const every = boxes.length && boxes.every(b => b.checked);
    for (const b of boxes) b.checked = !every;
    count();
  });
  form.addEventListener('submit', () => { submit.disabled = true; submit.textContent = 'Importing…'; });
})();
