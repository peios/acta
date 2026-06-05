// watch.js — wires every Watch control on a server-rendered page (project page,
// agents page). A control is a [data-watch-control] with a data-subscribe URL: a
// YouTube-style notification button (eye) plus a category dropdown. Clicking the
// button while off subscribes (subject-type default) and opens the dropdown;
// while on it just toggles the dropdown, which carries the category checkboxes
// and an Unsubscribe. Everything posts to data-subscribe and repaints from the
// server's canonical {watching, events} reply. The item modal has its own copy
// in board.js (it's built dynamically per open); this covers the static pages.
(() => {
  const csrf = (document.querySelector('input[name="csrf_token"]') || {}).value || '';
  document.querySelectorAll('[data-watch-control]').forEach(wire);

  function wire(wc) {
    const url = wc.dataset.subscribe;
    if (!url) return;
    const trigger = wc.querySelector('[data-pill-trigger]');
    const menu = wc.querySelector('[data-pill-menu]');
    const hidden = wc.querySelector('.modal-watch-toggle');
    const cats = Array.from(wc.querySelectorAll('[data-watch-cat]'));
    const offBtn = wc.querySelector('[data-watch-off]');

    const paint = (state) => {
      const on = !!state.watching;
      wc.classList.toggle('is-on', on);
      trigger.setAttribute('aria-pressed', on ? 'true' : 'false');
      if (hidden) hidden.checked = on;
      const ev = state.events || [];
      cats.forEach((c) => { c.checked = ev.includes(c.value); });
    };
    const post = async (body) => {
      try {
        const res = await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
          body: JSON.stringify(body),
        });
        if (!res.ok) throw new Error(res.status);
        paint(await res.json());
      } catch (_) { /* best-effort, like the bell elsewhere */ }
    };
    const openMenu = (open) => {
      menu.hidden = !open;
      trigger.classList.toggle('active', open);
    };

    trigger.addEventListener('click', () => {
      const willOpen = menu.hidden;
      if (willOpen && !(hidden && hidden.checked)) post({ watching: true });
      openMenu(willOpen);
    });
    cats.forEach((c) => c.addEventListener('change', () => {
      post({ watching: true, events: cats.filter((x) => x.checked).map((x) => x.value) });
    }));
    if (offBtn) offBtn.addEventListener('click', () => { openMenu(false); post({ watching: false }); });
    document.addEventListener('pointerdown', (e) => { if (!wc.contains(e.target)) openMenu(false); });
  }
})();
