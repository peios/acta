// Cmd-K quick-switcher: a global overlay that searches items as you type and
// jumps to the chosen one. Results come from /{slug}/search as an HTML fragment.
(function () {
  var root = document.querySelector('[data-cmdk]');
  if (!root) return;
  var input = root.querySelector('[data-cmdk-input]');
  var results = root.querySelector('[data-cmdk-results]');
  var url = root.getAttribute('data-search-url');
  var open = false, seq = 0, timer = null, active = -1;

  function show() {
    if (open) return;
    open = true;
    root.hidden = false;
    document.body.classList.add('cmdk-on');
    input.value = '';
    results.innerHTML = '';
    active = -1;
    requestAnimationFrame(function () { input.focus(); });
  }
  function hide() {
    if (!open) return;
    open = false;
    root.hidden = true;
    document.body.classList.remove('cmdk-on');
  }
  function hits() { return results.querySelectorAll('[data-hit]'); }
  function highlight(i) {
    var hs = hits();
    if (!hs.length) { active = -1; return; }
    active = (i + hs.length) % hs.length;
    for (var n = 0; n < hs.length; n++) {
      var on = n === active;
      hs[n].classList.toggle('active', on);
      if (on) hs[n].scrollIntoView({ block: 'nearest' });
    }
  }
  function run() {
    var q = input.value.trim();
    if (!q) { results.innerHTML = ''; active = -1; return; }
    var mine = ++seq;
    fetch(url + '?q=' + encodeURIComponent(q), { headers: { 'Accept': 'text/html' } })
      .then(function (r) { return r.ok ? r.text() : ''; })
      .then(function (html) {
        if (mine !== seq) return; // a newer keystroke already fired
        results.innerHTML = html;
        highlight(0);
      })
      .catch(function () {});
  }

  document.addEventListener('keydown', function (e) {
    if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
      e.preventDefault();
      open ? hide() : show();
      return;
    }
    if (!open) return;
    if (e.key === 'Escape') { e.preventDefault(); hide(); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); highlight(active + 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); highlight(active - 1); }
    else if (e.key === 'Enter') {
      var hs = hits();
      if (active >= 0 && hs[active]) { e.preventDefault(); window.location.assign(hs[active].getAttribute('href')); }
    }
  });

  var openers = document.querySelectorAll('[data-cmdk-open]');
  for (var i = 0; i < openers.length; i++) {
    openers[i].addEventListener('click', function (e) { e.preventDefault(); show(); });
  }
  var closers = root.querySelectorAll('[data-cmdk-close]');
  for (var j = 0; j < closers.length; j++) {
    closers[j].addEventListener('click', hide);
  }
  input.addEventListener('input', function () {
    clearTimeout(timer);
    timer = setTimeout(run, 120);
  });
  results.addEventListener('mousemove', function (e) {
    var a = e.target.closest('[data-hit]');
    if (!a) return;
    var hs = hits();
    for (var k = 0; k < hs.length; k++) {
      if (hs[k] === a) { if (k !== active) highlight(k); return; }
    }
  });
})();
