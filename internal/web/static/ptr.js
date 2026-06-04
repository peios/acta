// Pull-to-refresh for the installed app. A standalone PWA has no browser
// chrome, so it loses the native pull-to-refresh gesture — this restores it.
// No-ops outside a standalone/touch context.
//
// It stays clear of the board's other touch gestures: a horizontal lane-swipe
// is dx-dominant (ignored), and a drag-to-reorder only begins after Sortable's
// 200ms hold — so a pull is an *immediate*, downward, vertical-dominant move
// from the scrolled-to-top content. preventDefault is called only once a pull is
// committed, so normal scrolling and swiping are untouched.
(function () {
  "use strict";

  var standalone = window.matchMedia('(display-mode: standalone)').matches ||
                   window.navigator.standalone === true;
  if (!standalone || !('ontouchstart' in window)) return;

  var THRESHOLD = 64;   // px of (post-resistance) pull needed to commit
  var MAX = 96;         // visual cap
  var MOVE = 6;         // px before the gesture is classified (matches Sortable)
  var DRAG_DELAY = 200; // Sortable's touch hold; a pull beginning later is a drag

  var ind = document.createElement('div');
  ind.className = 'ptr-ind';
  ind.innerHTML = '<svg viewBox="0 0 24 24" width="15" height="15" fill="none"' +
    ' stroke="currentColor" stroke-width="2.2" stroke-linecap="round"' +
    ' stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36"/>' +
    '<path d="M21 3v5h-5"/></svg>';
  var icon = ind.firstChild;
  document.body.appendChild(ind);

  var startX = 0, startY = 0, startT = 0, container = null;
  var state = 'idle'; // idle -> pending -> pulling | done
  var dist = 0;

  function scrollerOf(node) {
    for (var el = node; el && el !== document.body; el = el.parentElement) {
      var oy = getComputedStyle(el).overflowY;
      if ((oy === 'auto' || oy === 'scroll') && el.scrollHeight > el.clientHeight) {
        return el;
      }
    }
    return document.scrollingElement || document.documentElement;
  }

  function reset() {
    state = 'idle'; dist = 0;
    ind.classList.remove('ptr-armed');
    ind.style.opacity = '';
    ind.style.transform = '';
    icon.style.transform = '';
  }

  document.addEventListener('touchstart', function (e) {
    if (state === 'pulling' || e.touches.length !== 1) return;
    var t = e.touches[0];
    startX = t.clientX; startY = t.clientY; startT = Date.now();
    container = scrollerOf(e.target);
    state = container.scrollTop <= 0 ? 'pending' : 'done';
  }, { passive: true });

  document.addEventListener('touchmove', function (e) {
    if (state === 'idle' || state === 'done') return;
    var t = e.touches[0];
    var dy = t.clientY - startY, dx = t.clientX - startX;

    if (state === 'pending') {
      if (Math.max(Math.abs(dy), Math.abs(dx)) < MOVE) return; // not yet classified
      if (Date.now() - startT >= DRAG_DELAY || dy <= 0 ||
          Math.abs(dx) > Math.abs(dy) || container.scrollTop > 0) {
        state = 'done'; // a drag, a swipe, or a normal scroll — not a pull
        return;
      }
      state = 'pulling';
    }

    if (container.scrollTop > 0) { reset(); return; }
    e.preventDefault();
    dist = Math.max(0, Math.min(dy * 0.5, MAX)); // damped, capped
    var p = Math.min(dist / THRESHOLD, 1);
    ind.style.opacity = String(0.25 + 0.75 * p);
    ind.style.transform = 'translateX(-50%) translateY(' + dist + 'px)';
    icon.style.transform = 'rotate(' + (p * 280) + 'deg)';
    ind.classList.toggle('ptr-armed', dist >= THRESHOLD);
  }, { passive: false });

  document.addEventListener('touchend', function () {
    if (state !== 'pulling') { reset(); return; }
    if (dist >= THRESHOLD) {
      // Commit: show the spinner briefly, then reload.
      icon.style.transform = '';
      ind.classList.add('ptr-refreshing', 'ptr-armed');
      ind.style.opacity = '1';
      ind.style.transform = 'translateX(-50%) translateY(' + THRESHOLD + 'px)';
      window.setTimeout(function () { window.location.reload(); }, 150);
    } else {
      reset();
    }
  }, { passive: true });

  document.addEventListener('touchcancel', reset, { passive: true });
})();
