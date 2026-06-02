// board-prefs.js — remembers each workspace board's view mode (status /
// milestone) and filters (status + assignee) in localStorage, and restores them
// on a "bare" visit: one with no view params in the URL, e.g. arriving from the
// top-nav, a bookmark, or a fresh load. The whole view is already encoded in the
// URL query, so persistence is just "remember the query per workspace, replay it
// on a bare visit."
//
// Loaded blocking in <head> so a restore redirect fires before the default view
// paints (no flash). board.js calls window.__actaBoardPrefs.save() after the
// filters change so client-side filter toggles get remembered too.
(() => {
  // Only the board page itself (/w/<slug>), not its sub-pages (/archive, …).
  const m = location.pathname.match(/^\/w\/([^/]+)\/?$/);
  if (!m) return;
  const key = 'acta:board:' + decodeURIComponent(m[1]);

  // Canonical view string: mode (only when milestone — status is the default)
  // plus sorted status/assignee values. '' means the default view, which is
  // never worth restoring. Sorting keeps the saved value stable regardless of
  // the order boxes were ticked.
  function canon(params) {
    const parts = [];
    if (params.get('mode') === 'milestone') parts.push('mode=milestone');
    for (const name of ['status', 'assignee']) {
      params.getAll(name).map(encodeURIComponent).sort().forEach((v) => parts.push(name + '=' + v));
    }
    return parts.join('&');
  }

  function save() {
    try { localStorage.setItem(key, canon(new URLSearchParams(location.search))); } catch (_) {}
  }
  window.__actaBoardPrefs = { save };

  const params = new URLSearchParams(location.search);
  const hasView = params.has('mode') || params.has('status') || params.has('assignee');

  if (!hasView) {
    // Bare visit — replay the saved view if there's a non-default one. Any
    // ?item= deep link is preserved so the item still opens, in the saved view.
    let saved = null;
    try { saved = localStorage.getItem(key); } catch (_) {}
    if (saved) {
      const item = params.get('item');
      location.replace(location.pathname + '?' + saved + (item ? '&item=' + encodeURIComponent(item) : ''));
    }
    return; // either navigating away, or a genuine default visit (nothing to save)
  }

  // Explicit view in the URL (mode toggle, restore target, or a filtered link) —
  // remember it as this workspace's view.
  save();
})();
