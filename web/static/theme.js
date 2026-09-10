// Runs before the page paints. Owns theme persistence and system preference;
// the Svelte toggle requests changes through acta:toggle-theme.
(() => {
  const key = 'acta.theme';
  const root = document.documentElement;
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  const valid = value => value === 'dark' || value === 'light' ? value : null;
  let preference = null;
  try { preference = valid(localStorage.getItem(key)); } catch { /* Storage can be unavailable. */ }

  function apply() {
    root.dataset.theme = preference ?? (system.matches ? 'dark' : 'light');
    window.dispatchEvent(new Event('acta:theme-change'));
  }
  apply();
  system.addEventListener('change', () => { if (preference === null) apply(); });
  window.addEventListener('storage', event => {
    if (event.key === key || event.key === null) {
      preference = valid(event.newValue);
      apply();
    }
  });
  window.addEventListener('acta:toggle-theme', () => {
    preference = root.dataset.theme === 'dark' ? 'light' : 'dark';
    try { localStorage.setItem(key, preference); } catch { /* Still works for this page. */ }
    apply();
  });
})();
