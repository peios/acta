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
