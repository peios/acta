// project.js — single-project page extras. The Watch control is wired generically
// by watch.js; this just handles the Edit toggle: the pencil pill shows/hides the
// edit form (which the server renders open when a save errors).
(() => {
  const editBtn = document.querySelector('[data-edit-toggle]');
  const editPanel = document.querySelector('[data-edit-panel]');
  if (!editBtn || !editPanel) return;
  const sync = () => {
    const open = !editPanel.hidden;
    editBtn.classList.toggle('is-on', open);
    editBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
  };
  editBtn.addEventListener('click', () => { editPanel.hidden = !editPanel.hidden; sync(); });
  sync();
})();
