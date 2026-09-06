// status.js — a session's status lives in its title as a marker ("[TODO]
// Fix the build", "[IN PROGRESS] …", "[DONE] …") so it shows wherever the
// title does, Claude Code's and Codex's own pickers included. This is the
// browser side of internal/agentsession/status.go: read a marker at either
// end, write it at the start, and paint the mark and pickers for a session
// after a rename. Vanilla ES, one global.
window.sessionStatus = (() => {
  'use strict';
  const lead = /^\s*\[\s*(todo|to do|in[ _-]?progress|wip|done)\s*\]\s*/i;
  const tail = /\s*\[\s*(todo|to do|in[ _-]?progress|wip|done)\s*\]\s*$/i;
  function norm(w) {
    w = w.toLowerCase().replace(/[ _-]/g, '');
    return w === 'todo' ? 'todo' : (w === 'inprogress' || w === 'wip') ? 'in_progress' : w === 'done' ? 'done' : '';
  }
  function split(title) {
    title = title || '';
    let m = lead.exec(title);
    if (m) return { status: norm(m[1]), bare: title.slice(m[0].length).trim() };
    m = tail.exec(title);
    if (m) return { status: norm(m[1]), bare: title.slice(0, title.length - m[0].length).trim() };
    return { status: '', bare: title.trim() };
  }
  const marker = { todo: '[TODO]', in_progress: '[IN PROGRESS]', done: '[DONE]' };
  function compose(status, bare) {
    bare = (bare || '').trim();
    const m = marker[status] || '';
    return m ? (bare ? m + ' ' + bare : m) : bare;
  }
  const label = { todo: 'To do', in_progress: 'In progress', done: 'Done', '': 'No status' };
  // apply rings every provider mark and sets every picker for a session
  // from its full title, and returns the split for the caller to set the
  // bare text. The ring is a status--* class on the mark (see base.html).
  function apply(id, title) {
    const parts = split(title);
    document.querySelectorAll('[data-session-dot="' + id + '"]').forEach((dot) => {
      for (const c of [...dot.classList]) if (c.startsWith('status--')) dot.classList.remove(c);
      if (parts.status) dot.classList.add('status--' + parts.status);
    });
    document.querySelectorAll('[data-status-pick="' + id + '"]').forEach((el) => { el.value = parts.status; });
    return parts;
  }
  return { split, compose, label, apply };
})();
