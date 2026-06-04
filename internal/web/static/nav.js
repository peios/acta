// nav.js — the app sidebar's responsive behaviour. One toggle button (in the
// sidebar header) does three jobs depending on viewport + state:
//   • desktop, full → collapse to a 60px icon rail   (class `nav-rail`, saved)
//   • desktop, rail → expand back to the full sidebar
//   • mobile        → open/close a full-screen menu   (class `nav-open`, transient)
// Both classes live on <html>. Loaded blocking in <head> on every chrome page so
// the saved rail state is applied before the body paints — no flash of the full
// sidebar. No framework, vanilla ES; mirrors board-prefs.js's storage idiom.
(() => {
  'use strict';
  const root = document.documentElement;
  const RAIL_KEY = 'acta:nav:rail';
  const desktop = () => window.matchMedia('(min-width: 769px)').matches;

  // Apply the saved desktop state right away, before first paint.
  try { if (localStorage.getItem(RAIL_KEY) === '1') root.classList.add('nav-rail'); } catch (_) {}

  // Open/close the mobile menu. Closing holds `nav-closing` on (keeping the
  // overlay fixed) while the slide-out animation plays, then drops both classes.
  let closeTimer = null;
  function openNav() {
    clearTimeout(closeTimer);
    // The overlay sits below the top bar; measure its current height so the two
    // meet exactly (the bar's controls fix its height, but be precise anyway).
    const top = document.querySelector('.side-top');
    if (top) root.style.setProperty('--topbar-h', top.offsetHeight + 'px');
    root.classList.remove('nav-closing');
    root.classList.add('nav-open');
  }
  function closeNav(instant) {
    if (!root.classList.contains('nav-open')) return;
    clearTimeout(closeTimer);
    if (instant) { root.classList.remove('nav-open', 'nav-closing'); return; }
    root.classList.add('nav-closing');
    closeTimer = setTimeout(() => root.classList.remove('nav-open', 'nav-closing'), 220);
  }

  function toggle() {
    if (desktop()) {
      const railed = root.classList.toggle('nav-rail');
      try { localStorage.setItem(RAIL_KEY, railed ? '1' : '0'); } catch (_) {}
      closeNav(true); // never carry a mobile state into desktop
    } else if (root.classList.contains('nav-open')) {
      closeNav();
    } else {
      openNav();
    }
  }

  // Edge-swipe the mobile menu: a fast right-flick opens it, but only when the
  // board can't scroll further right (already at its left edge) so it never
  // fights the board's horizontal scroll; a left-flick closes it. We require a
  // quick early movement (`fast`) so a card long-press-drag — which holds still
  // for ~200ms first — and a vertical scroll never qualify.
  function wireSwipe() {
    let sx = 0, sy = 0, st = 0, single = false, fast = false, atEdge = false;
    document.addEventListener('touchstart', (e) => {
      single = e.touches.length === 1;
      if (!single || desktop()) return;
      const t = e.touches[0];
      sx = t.clientX; sy = t.clientY; st = Date.now(); fast = false;
      const b = document.querySelector('.board');
      atEdge = !b || b.scrollLeft <= 0;
    }, { passive: true });
    document.addEventListener('touchmove', (e) => {
      if (!single || fast) return;
      if (Math.abs(e.touches[0].clientX - sx) > 20 && Date.now() - st < 150) fast = true;
    }, { passive: true });
    document.addEventListener('touchend', (e) => {
      if (!single || desktop() || document.querySelector('.modal-backdrop, .ws-modal:target')) return;
      const t = e.changedTouches[0];
      const dx = t.clientX - sx, dy = t.clientY - sy;
      if (!fast || Math.abs(dx) < 55 || Math.abs(dx) < Math.abs(dy) * 1.5) return;
      const open = root.classList.contains('nav-open');
      if (dx > 0 && !open && atEdge) openNav();      // right-flick at the left edge → open the nav menu
      else if (dx < 0 && open) closeNav();           // left-flick while the nav menu is open → close
    }, { passive: true });
  }

  function wire() {
    const btn = document.querySelector('.nav-toggle');
    if (btn) btn.addEventListener('click', toggle);

    // Tapping a nav link closes the mobile menu. The click also navigates (a
    // fresh page loads closed), but closing first keeps the back-button case tidy.
    root.querySelectorAll('.nav .nav-item, .side-foot .nav-item').forEach((a) => {
      a.addEventListener('click', () => closeNav(true));
    });

    // Crossing the breakpoint clears the transient open-state, so you never land
    // in desktop with a stuck full-screen overlay.
    window.matchMedia('(min-width: 769px)').addEventListener('change', () => {
      closeNav(true);
    });

    // Native <details> menus (workspace switcher, bell, account) only toggle via
    // their summary; close any open one when a pointer goes down outside it, or
    // on Escape. pointerdown fires reliably on touch (unlike a bubbled click).
    const openMenus = () => document.querySelectorAll('details.wsmenu[open]');
    document.addEventListener('pointerdown', (e) => {
      openMenus().forEach((d) => { if (!d.contains(e.target)) d.removeAttribute('open'); });
    });
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') openMenus().forEach((d) => d.removeAttribute('open'));
    });

    wireSwipe();
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', wire);
  else wire();
})();
