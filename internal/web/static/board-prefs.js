// board-prefs.js — remembers each workspace board's display state (grouping,
// sub-group, ordering, layout, show-sub-tasks) and filters, and restores it on a
// *fresh* arrival (top-nav, bookmark, cold load). The whole view is encoded in
// the URL query, so persistence is "remember the query per workspace, replay it
// on a fresh bare visit."
//
// The catch: a deliberate reset to the default view (untick the last control,
// pick "All items") also produces a bare URL, and must NOT be replayed over.
// So a toolbar action that changes the view sets a one-shot "explicit nav"
// marker in sessionStorage; board-prefs then treats the landing URL as
// authoritative (even bare) and skips the restore. A bare URL with no marker is
// a genuine fresh arrival and gets the saved view.
//
// Loaded blocking in <head> so the restore redirect fires before paint (no
// flash). board.js calls window.__actaBoardPrefs.save() after client-side filter
// changes so those are remembered too.
(() => {
  // Only the board page itself (/<slug>), not its sub-pages (/archive, …).
  const m = location.pathname.match(/^\/([^/]+)\/?$/);
  if (!m) return;
  const slug = decodeURIComponent(m[1]);
  const key = 'acta:board:' + slug;
  const navKey = 'acta:nav:' + slug; // sessionStorage flag: a toolbar view action

  // The non-default values worth remembering (mirrors the Go whitelists). Defaults
  // — status grouping, manual order, board layout, no sub-group/sub-tasks — are
  // dropped by canon, so a reset persists as "" and is never replayed.
  const GROUP_MODES = ['milestone', 'release', 'priority', 'type', 'size', 'due', 'assignee', 'project'];
  const SUBGROUP_MODES = ['status', 'priority', 'type', 'size', 'due', 'assignee', 'project'];
  const ORDER_MODES = ['priority', 'due', 'title', 'created'];
  const VIEW_KEYS = ['mode', 'subgroup', 'order', 'subtasks', 'layout',
    'status', 'assignee', 'project', 'release', 'priority', 'type', 'size', 'due'];
  // Toolbar controls whose click means "I'm explicitly choosing a view" — so a
  // reset to default isn't auto-restored. Filters apply client-side (no nav), so
  // they're not here; the sidebar top-nav is outside .board-bar, so it still
  // restores.
  const NAV_SEL = 'a[data-mode], a[data-layout-opt], a[data-subgroup], a[data-order], a[data-subtasks-toggle], a.view-tab';

  // Canonical view string: non-default grouping/sub-group/order/layout/sub-tasks
  // plus sorted facet values. '' is the default view (never worth restoring).
  function canon(params) {
    const parts = [];
    const mode = params.get('mode');
    if (GROUP_MODES.includes(mode)) parts.push('mode=' + mode);
    const sub = params.get('subgroup');
    if (SUBGROUP_MODES.includes(sub) && sub !== (mode || 'status')) parts.push('subgroup=' + sub);
    const ord = params.get('order');
    if (ORDER_MODES.includes(ord)) parts.push('order=' + ord);
    if (params.get('subtasks') === '1') parts.push('subtasks=1');
    if (params.get('layout') === 'list') parts.push('layout=list');
    for (const name of ['status', 'assignee', 'project', 'release', 'priority', 'type', 'size', 'due']) {
      params.getAll(name).map(encodeURIComponent).sort().forEach((v) => parts.push(name + '=' + v));
    }
    return parts.join('&');
  }

  function save() {
    try { localStorage.setItem(key, canon(new URLSearchParams(location.search))); } catch (_) {}
  }
  window.__actaBoardPrefs = { save };

  const params = new URLSearchParams(location.search);
  const hasView = VIEW_KEYS.some((k) => params.has(k));

  // Consume the explicit-navigation marker. Set means a toolbar control brought
  // us here, so the URL — bare or not — is authoritative and won't be replayed.
  let explicitNav = false;
  try {
    explicitNav = sessionStorage.getItem(navKey) === '1';
    sessionStorage.removeItem(navKey);
  } catch (_) {}

  if (!hasView && !explicitNav) {
    // Fresh, unspecified arrival — restore the saved view if there is a non-default
    // one. A ?item= deep link is preserved so the item still opens, in that view.
    let saved = null;
    try { saved = localStorage.getItem(key); } catch (_) {}
    if (saved) {
      const item = params.get('item');
      location.replace(location.pathname + '?' + saved + (item ? '&item=' + encodeURIComponent(item) : ''));
      return;
    }
  }

  // The URL is authoritative: remember it. canon drops defaults, so a reset to the
  // default view persists as "" and won't be replayed next time.
  save();

  // Mark future toolbar view-changes as explicit. Capture phase, so it fires even
  // though the Display popover stops click propagation on its menu.
  const markNav = (e) => { if (e.target.closest(NAV_SEL)) { try { sessionStorage.setItem(navKey, '1'); } catch (_) {} } };
  const attach = () => {
    const bar = document.querySelector('.board-bar');
    if (bar) bar.addEventListener('click', markNav, true);
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', attach);
  else attach();
})();
