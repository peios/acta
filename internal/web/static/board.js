// board.js — interactivity for the board: drag-and-drop, inline create, and the
// item modal (opened by ?item=<id>). The page is fully server-rendered; this
// only enhances it. All mutations go through the JSON API. Loaded after
// sortable.min.js.
(() => {
  const board = document.getElementById('board');
  if (!board) return;
  const wrap = document.querySelector('.board-wrap');
  const base = '/w/' + wrap.dataset.slug;
  const csrf = document.querySelector('meta[name="csrf-token"]').content;
  const boardErr = document.querySelector('[data-board-error]');

  const MESSAGES = {
    status_not_empty: 'Empty the lane before deleting it.',
    invalid_name: 'Enter a status name (1–40 characters).',
    invalid_title: 'Enter an item title.',
    invalid_comment: 'Enter a comment.',
    invalid_description: 'That description is too long.',
    user_not_found: 'That user no longer exists.',
  };
  const msg = (e) => MESSAGES[e.message] || 'Something went wrong — reload and try again.';

  async function api(path, body) {
    const res = await fetch(base + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      let code = String(res.status);
      try { code = (await res.json()).error || code; } catch (_) {}
      throw new Error(code);
    }
    return res.status === 204 ? null : res.json();
  }

  const cardOf = (id) => board.querySelector('.item[data-item-id="' + CSS.escape(id) + '"]');

  function debounce(fn, ms) {
    let t;
    return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
  }

  // --- board items ---

  function newItem(it) {
    const el = document.getElementById('item-tmpl').content.firstElementChild.cloneNode(true);
    el.dataset.itemId = it.id;
    el.dataset.statusId = it.status_id || '';
    el.dataset.assigneeId = '';
    el.querySelector('.item-title').textContent = it.title;
    wireItem(el);
    return el;
  }

  function wireItem(el) {
    el.querySelector('.item-del').addEventListener('click', async (e) => {
      e.stopPropagation();
      if (boardErr) boardErr.textContent = '';
      try { await api('/items/' + el.dataset.itemId + '/archive'); el.remove(); }
      catch (err) { if (boardErr) boardErr.textContent = msg(err); }
    });
    el.addEventListener('click', (e) => {
      if (e.target.closest('.item-del')) return;
      openModal(el.dataset.itemId);
    });
  }

  // --- lane colour picker ---

  function closePalettes() {
    document.querySelectorAll('.lane-palette').forEach((p) => { p.hidden = true; });
  }

  // setLaneColor repaints a lane's dot and every card's left bar in that lane.
  function setLaneColor(lane, color) {
    lane.dataset.color = color;
    const dot = lane.querySelector('.lane-dot');
    if (dot) dot.style.setProperty('--lane-color', color);
    lane.querySelectorAll('.lane-items .item').forEach((c) => c.style.setProperty('--lane-color', color));
  }

  function wireSwatch(lane) {
    const dot = lane.querySelector('.lane-dot');
    const panel = lane.querySelector('.lane-palette');
    if (!dot || !panel) return;
    dot.addEventListener('click', (e) => {
      e.stopPropagation();
      const willOpen = panel.hidden;
      closePalettes();
      panel.hidden = !willOpen;
    });
    panel.addEventListener('click', (e) => e.stopPropagation());
    panel.querySelectorAll('.swatch').forEach((sw) => {
      sw.addEventListener('click', async () => {
        try {
          await api('/statuses/' + lane.dataset.statusId + '/color', { color: sw.dataset.color });
          setLaneColor(lane, sw.dataset.color);
        } catch (e) { if (boardErr) boardErr.textContent = msg(e); }
        panel.hidden = true;
      });
    });
  }

  function wireLane(lane) {
    const statusId = () => lane.dataset.statusId;
    const nameInput = lane.querySelector('.lane-name');
    let lastName = nameInput.value;
    wireSwatch(lane);

    nameInput.addEventListener('change', async () => {
      const name = nameInput.value.trim();
      if (!name) { nameInput.value = lastName; return; }
      if (name === lastName) return;
      try { await api('/statuses/' + statusId() + '/rename', { name }); lastName = name; }
      catch (e) { if (boardErr) boardErr.textContent = msg(e); nameInput.value = lastName; }
    });

    lane.querySelector('.lane-del').addEventListener('click', async () => {
      if (!confirm('Delete this lane? It must be empty.')) return;
      try { await api('/statuses/' + statusId() + '/delete'); lane.remove(); }
      catch (e) { if (boardErr) boardErr.textContent = msg(e); }
    });

    lane.querySelector('.item-add').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = e.target.querySelector('.item-add-input');
      const title = input.value.trim();
      if (!title) return;
      try {
        const it = await api('/items', { status_id: statusId(), title });
        const card = newItem(it);
        card.style.setProperty('--lane-color', lane.dataset.color || '');
        lane.querySelector('.lane-items').append(card);
        input.value = '';
        input.focus();
      } catch (err) { if (boardErr) boardErr.textContent = msg(err); }
    });

    new Sortable(lane.querySelector('.lane-items'), {
      group: 'items',
      animation: 150,
      draggable: '.item',
      filter: '.item-del',
      ghostClass: 'sortable-ghost',
      chosenClass: 'sortable-chosen',
      onEnd: (evt) => {
        const id = evt.item.dataset.itemId;
        const destLane = evt.to.closest('.lane');
        evt.item.style.setProperty('--lane-color', destLane.dataset.color || '');
        api('/items/' + id + '/move', { status_id: destLane.dataset.statusId, index: evt.newIndex })
          .catch((e) => { if (boardErr) boardErr.textContent = msg(e); location.reload(); });
      },
    });
  }

  // wireColumn handles a Milestone-mode column (the Backlog or a milestone).
  // Cross-column drag reparents (and reloads); dragging within a milestone
  // column reorders its children.
  function wireColumn(col) {
    const parentId = col.dataset.parentId; // "" for Backlog
    const openBtn = col.querySelector('.mcol-title[data-open]');
    if (openBtn) openBtn.addEventListener('click', () => openModal(openBtn.dataset.open));

    col.querySelector('.item-add').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = e.target.querySelector('.item-add-input');
      const title = input.value.trim();
      if (!title) return;
      try {
        // Subtask of a milestone, or a Backlog root (server defaults the status).
        const it = parentId
          ? await api('/items/' + parentId + '/subtasks', { title })
          : await api('/items', { title });
        const card = newItem(it);
        card.style.setProperty('--lane-color', it.color || '');
        col.querySelector('.lane-items').append(card);
        input.value = '';
        input.focus();
      } catch (err) { if (boardErr) boardErr.textContent = msg(err); }
    });

    new Sortable(col.querySelector('.lane-items'), {
      group: 'items',
      animation: 150,
      draggable: '.item',
      filter: '.item-del',
      ghostClass: 'sortable-ghost',
      chosenClass: 'sortable-chosen',
      onEnd: (evt) => {
        const itemId = evt.item.dataset.itemId;
        const toCol = evt.to.closest('.mcol').dataset.parentId;
        const fromCol = evt.from.closest('.mcol').dataset.parentId;
        if (toCol !== fromCol) {
          api('/items/' + itemId + '/parent', { parent_id: toCol })
            .then(() => location.reload())
            .catch((e) => { if (boardErr) boardErr.textContent = msg(e); location.reload(); });
        } else if (toCol !== '') {
          const ids = [...evt.to.querySelectorAll('.item')].map((c) => c.dataset.itemId);
          api('/items/' + toCol + '/subtasks/reorder', { ids })
            .catch((e) => { if (boardErr) boardErr.textContent = msg(e); });
        }
        // Within the Backlog, order isn't persisted (root items keep their lane position).
      },
    });
  }

  // --- the item modal ---

  let modalEl = null;
  let opener = null; // card to restore focus to on close

  // URL helpers that preserve other params (notably ?mode=).
  const urlWithItem = (id) => {
    const p = new URLSearchParams(location.search);
    p.set('item', id);
    return location.pathname + '?' + p.toString();
  };
  const urlWithoutItem = () => {
    const p = new URLSearchParams(location.search);
    p.delete('item');
    const q = p.toString();
    return location.pathname + (q ? '?' + q : '');
  };

  async function openModal(id, push = true) {
    if (modalEl) closeModal(false);
    let html;
    try {
      const res = await fetch(base + '/items/' + id + '/modal', { headers: { 'X-CSRF-Token': csrf } });
      if (!res.ok) throw new Error(res.status);
      html = await res.text();
    } catch (_) {
      if (boardErr) boardErr.textContent = 'Could not open that item.';
      return;
    }
    const holder = document.createElement('div');
    holder.innerHTML = html.trim();
    modalEl = holder.firstElementChild;
    document.body.appendChild(modalEl);
    wireModal(modalEl);
    if (push) history.pushState({ item: id }, '', urlWithItem(id));
    const title = modalEl.querySelector('.modal-title');
    if (title) title.focus();
  }

  function closeModal(push = true) {
    if (modalEl) { modalEl.remove(); modalEl = null; }
    if (push) history.pushState({}, '', urlWithoutItem());
    if (opener) { opener.focus(); opener = null; }
  }

  // saveDescription posts the raw markdown and returns the server-rendered,
  // sanitized view fragment (markdown -> safe HTML) to swap into the modal.
  async function saveDescription(id, text) {
    const res = await fetch(base + '/items/' + id + '/description', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
      body: JSON.stringify({ description: text }),
    });
    if (!res.ok) {
      let code = String(res.status);
      try { code = (await res.json()).error || code; } catch (_) {}
      throw new Error(code);
    }
    return res.text();
  }

  // wireDescription runs the description's read view (rendered markdown + a
  // show-more clamp) and its editor (pencil -> textarea, autosave, Done).
  function wireDescription(el, id, fail) {
    const field = el.querySelector('[data-desc-field]');
    if (!field) return;
    const view = field.querySelector('[data-desc-view]');
    const editor = field.querySelector('[data-desc-editor]');
    const input = field.querySelector('[data-desc-input]');
    const editBtn = field.querySelector('[data-desc-edit]');
    const doneBtn = field.querySelector('[data-desc-done]');
    const hint = field.querySelector('[data-desc-saved]');
    const setHint = (t) => { if (hint) hint.textContent = t; };
    let pending = null; // latest rendered view from a save, applied on Done

    function wireMore() {
      const more = view.querySelector('[data-desc-more]');
      if (!more) return;
      more.addEventListener('click', () => {
        const full = view.querySelector('[data-desc-full]');
        const prev = view.querySelector('[data-desc-preview]');
        const expanded = more.getAttribute('aria-expanded') === 'true';
        if (full) full.hidden = expanded;
        if (prev) prev.hidden = !expanded;
        more.setAttribute('aria-expanded', String(!expanded));
        more.textContent = expanded ? 'Show more' : 'Show less';
      });
    }
    wireMore();

    editBtn.addEventListener('click', () => {
      editor.hidden = false;
      view.hidden = true;
      editBtn.hidden = true;
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
    });

    doneBtn.addEventListener('click', () => {
      if (pending !== null) { view.innerHTML = pending; wireMore(); pending = null; }
      editor.hidden = true;
      view.hidden = false;
      editBtn.hidden = false;
      setHint('');
    });

    const save = debounce(async () => {
      try {
        pending = await saveDescription(id, input.value);
        setHint('saved');
        setTimeout(() => { if (!editor.hidden) setHint(''); }, 1400);
      } catch (e) { fail(e); setHint(''); }
    }, 600);
    input.addEventListener('input', () => { setHint('saving…'); save(); });
  }

  function wireModal(el) {
    const id = el.dataset.itemId;
    opener = cardOf(id);
    const err = el.querySelector('[data-modal-error]');
    const fail = (e) => { if (err) err.textContent = msg(e); };

    el.addEventListener('mousedown', (e) => { if (e.target === el) closeModal(); });
    el.querySelector('[data-modal-close]').addEventListener('click', (e) => { e.preventDefault(); closeModal(); });

    const title = el.querySelector('.modal-title');
    const saveTitle = debounce(async () => {
      const t = title.value.trim();
      if (!t) return;
      try { await api('/items/' + id + '/rename', { title: t }); const c = cardOf(id); if (c) c.querySelector('.item-title').textContent = t; }
      catch (e) { fail(e); }
    }, 500);
    title.addEventListener('input', saveTitle);

    el.querySelector('.modal-status').addEventListener('change', async (e) => {
      const statusId = e.target.value;
      try {
        await api('/items/' + id + '/status', { status_id: statusId });
        const c = cardOf(id);
        const laneEl = board.querySelector('.lane[data-status-id="' + CSS.escape(statusId) + '"]');
        if (c) {
          c.dataset.statusId = statusId;
          if (laneEl) {
            laneEl.querySelector('.lane-items').append(c);
            c.style.setProperty('--lane-color', laneEl.dataset.color || '');
          }
          reapplyFilters();
        }
      } catch (err2) { fail(err2); }
    });

    el.querySelector('.modal-assignee').addEventListener('change', async (e) => {
      try {
        await api('/items/' + id + '/assignee', { assignee_id: e.target.value });
        const c = cardOf(id);
        if (c) { c.dataset.assigneeId = e.target.value; reapplyFilters(); }
      } catch (err2) { fail(err2); }
    });

    // Reparenting (promote to None / demote under an item) restructures the
    // board, so reload to reflect it; the ?item= in the URL reopens the modal.
    el.querySelector('.modal-parent-select').addEventListener('change', async (e) => {
      try { await api('/items/' + id + '/parent', { parent_id: e.target.value }); location.reload(); }
      catch (err2) { fail(err2); }
    });

    wireDescription(el, id, fail);

    // Comments post on Cmd/Ctrl+Enter.
    const commentInput = el.querySelector('[data-comment-input]');
    commentInput.addEventListener('keydown', async (e) => {
      if (!((e.metaKey || e.ctrlKey) && e.key === 'Enter')) return;
      e.preventDefault();
      const body = commentInput.value.trim();
      if (!body) return;
      try {
        const c = await api('/items/' + id + '/comment', { body });
        const div = document.createElement('div');
        div.className = 'comment';
        const meta = document.createElement('div');
        meta.className = 'comment-meta';
        meta.textContent = c.author + ' · ' + c.at;
        const text = document.createElement('div');
        text.className = 'comment-body';
        text.textContent = c.body;
        div.append(meta, text);
        el.querySelector('[data-comment-list]').append(div);
        commentInput.value = '';
      } catch (err2) { fail(err2); }
    });

    const parentLink = el.querySelector('[data-parent-link]');
    if (parentLink) parentLink.addEventListener('click', (e) => { e.preventDefault(); openModal(parentLink.dataset.parentLink); });

    const subList = el.querySelector('[data-subtask-list]');
    const wireSubRow = (row) =>
      row.querySelector('.subtask-open').addEventListener('click', () => openModal(row.dataset.itemId));
    subList.querySelectorAll('.subtask').forEach(wireSubRow);

    new Sortable(subList, {
      handle: '.subtask-grip',
      animation: 150,
      draggable: '.subtask',
      onEnd: () => {
        const ids = [...subList.querySelectorAll('.subtask')].map((r) => r.dataset.itemId);
        api('/items/' + id + '/subtasks/reorder', { ids }).catch(fail);
      },
    });

    el.querySelector('[data-subtask-form]').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = el.querySelector('.subtask-input');
      const title = input.value.trim();
      if (!title) return;
      try {
        const it = await api('/items/' + id + '/subtasks', { title });
        const row = document.createElement('div');
        row.className = 'subtask';
        row.dataset.itemId = it.id;
        const grip = document.createElement('span');
        grip.className = 'subtask-grip';
        grip.title = 'Drag to reorder';
        grip.textContent = '⠿';
        const open = document.createElement('button');
        open.type = 'button';
        open.className = 'subtask-open';
        open.textContent = it.title;
        const status = document.createElement('span');
        status.className = 'subtask-status';
        const firstLane = board.querySelector('.lane .lane-name');
        status.textContent = firstLane ? firstLane.value : '';
        row.append(grip, open, status);
        wireSubRow(row);
        subList.append(row);
        input.value = '';
        input.focus();
      } catch (err2) { fail(err2); }
    });

    const msToggle = el.querySelector('.modal-ms-toggle');
    if (msToggle) msToggle.addEventListener('change', async () => {
      try { await api('/items/' + id + '/milestone', { is_milestone: msToggle.checked }); location.reload(); }
      catch (e) { fail(e); msToggle.checked = !msToggle.checked; }
    });

    const archive = el.querySelector('.modal-archive');
    if (archive) archive.addEventListener('click', async () => {
      try { await api('/items/' + id + '/archive'); const c = cardOf(id); if (c) c.remove(); closeModal(); }
      catch (e) { fail(e); }
    });
    const unarchive = el.querySelector('.modal-unarchive');
    if (unarchive) unarchive.addEventListener('click', async () => {
      try { await api('/items/' + id + '/unarchive'); location.reload(); }
      catch (e) { fail(e); }
    });
  }

  // --- board filters (status + assignee), progressively enhanced ---
  // The server already rendered the correct filtered state from the URL; this
  // adds instant toggling, the parent/agent cascade, "only", and URL sync.

  function facetValues(form, name) {
    return [...form.querySelectorAll('input[name="' + name + '"]:checked')].map((c) => c.value);
  }

  function filterBoard(statuses, assignees) {
    const sSet = new Set(statuses), aSet = new Set(assignees);
    const wrap = document.querySelector('.board-wrap');
    const me = wrap ? (wrap.dataset.me || '') : '';
    const statusOK = (id) => sSet.size === 0 || sSet.has(id);
    const assigneeOK = (aid) => {
      if (aSet.size === 0) return true;
      if (!aid) return aSet.has('unassigned');
      if (aSet.has(aid)) return true;
      return aSet.has('me') && aid === me;
    };
    board.querySelectorAll('.item').forEach((card) => {
      const hide = !statusOK(card.dataset.statusId) || !assigneeOK(card.dataset.assigneeId || '');
      card.classList.toggle('is-filtered', hide);
    });
    board.querySelectorAll('.lane[data-status-id]').forEach((lane) => {
      lane.classList.toggle('is-filtered', !statusOK(lane.dataset.statusId));
    });
  }

  function setFacetCount(form, facet, n, label) {
    const summary = form.querySelector('[data-facet="' + facet + '"] .facet-trigger');
    if (summary) summary.innerHTML = label + (n ? ' <span class="facet-count">' + n + '</span>' : '');
  }

  function syncFilterURL(statuses, assignees) {
    const p = new URLSearchParams(location.search);
    p.delete('status'); p.delete('assignee');
    statuses.forEach((v) => p.append('status', v));
    assignees.forEach((v) => p.append('assignee', v));
    const q = p.toString();
    history.replaceState(null, '', location.pathname + (q ? '?' + q : ''));
  }

  function applyFilters(form) {
    const statuses = facetValues(form, 'status');
    const assignees = facetValues(form, 'assignee');
    filterBoard(statuses, assignees);
    setFacetCount(form, 'status', statuses.length, 'Status');
    setFacetCount(form, 'assignee', assignees.length, 'Assignee');
    syncFilterURL(statuses, assignees);
    if (window.__actaBoardPrefs) window.__actaBoardPrefs.save(); // remember filters per workspace
    const clear = form.querySelector('.facet-clear');
    if (clear) clear.hidden = statuses.length + assignees.length === 0;
  }

  // reapplyFilters re-evaluates visibility after a card's status/assignee changes.
  function reapplyFilters() {
    const form = document.querySelector('[data-filters]');
    if (form) applyFilters(form);
  }

  // refreshParent sets a person row's indeterminate dash when their agents are
  // partially selected (some but not all of {human, agents}).
  function refreshParent(group) {
    const parent = group.querySelector('input[data-parent]');
    const agents = [...group.querySelectorAll('input[data-agent]')];
    if (!parent || !agents.length) return;
    const checked = agents.filter((a) => a.checked).length;
    const all = parent.checked && checked === agents.length;
    const none = !parent.checked && checked === 0;
    parent.indeterminate = !all && !none;
  }

  function clearAll(form) {
    form.querySelectorAll('input[type="checkbox"]').forEach((c) => { c.checked = false; c.indeterminate = false; });
  }

  function wireFilters() {
    const form = document.querySelector('[data-filters]');
    if (!form) return;
    document.documentElement.classList.add('has-js');

    form.addEventListener('submit', (e) => { e.preventDefault(); applyFilters(form); });

    form.querySelectorAll('input[name="status"], input[value="me"], input[value="unassigned"]').forEach((c) => {
      c.addEventListener('change', () => applyFilters(form));
    });

    form.querySelectorAll('.facet-group').forEach((group) => {
      const parent = group.querySelector('input[data-parent]');
      const agents = [...group.querySelectorAll('input[data-agent]')];
      const twist = group.querySelector('.facet-twist');
      if (twist) twist.addEventListener('click', (e) => { e.preventDefault(); group.classList.toggle('collapsed'); });
      if (parent) {
        parent.addEventListener('change', () => {
          agents.forEach((a) => { a.checked = parent.checked; });
          refreshParent(group);
          applyFilters(form);
        });
      }
      agents.forEach((a) => a.addEventListener('change', () => { refreshParent(group); applyFilters(form); }));
      const only = group.querySelector('.facet-only');
      if (only && parent) {
        only.addEventListener('click', (e) => {
          e.preventDefault();
          clearAll(form);
          parent.checked = true;
          applyFilters(form);
        });
      }
      refreshParent(group); // initial dash state from the server-rendered checks
    });

    const clear = form.querySelector('.facet-clear');
    if (clear) {
      clear.addEventListener('click', (e) => { e.preventDefault(); clearAll(form); applyFilters(form); });
    }

    // Close an open facet popover when clicking outside it.
    document.addEventListener('click', (e) => {
      form.querySelectorAll('.facet[open]').forEach((f) => { if (!f.contains(e.target)) f.removeAttribute('open'); });
    });
  }

  // --- wire the server-rendered board ---

  board.querySelectorAll('.item').forEach(wireItem);
  wireFilters();

  if (board.dataset.mode === 'milestone') {
    board.querySelectorAll('.mcol').forEach(wireColumn);
  } else {
    board.querySelectorAll('.lane').forEach(wireLane);

    new Sortable(board, {
      draggable: '.lane',
      handle: '.lane-grip',
      animation: 150,
      onMove: (evt) => !evt.related.classList.contains('lane-add'),
      onEnd: () => {
        const ids = [...board.querySelectorAll('.lane')].map((l) => l.dataset.statusId);
        api('/statuses/reorder', { ids }).catch((e) => { if (boardErr) boardErr.textContent = msg(e); });
      },
    });

    document.querySelector('.lane-add-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = e.target.querySelector('.lane-add-input');
      const name = input.value.trim();
      if (!name) return;
      try {
        const st = await api('/statuses', { name });
        const lane = document.getElementById('lane-tmpl').content.firstElementChild.cloneNode(true);
        lane.dataset.statusId = st.id;
        lane.dataset.color = st.color;
        lane.querySelector('.lane-name').value = st.name;
        lane.querySelector('.lane-dot').style.setProperty('--lane-color', st.color);
        board.insertBefore(lane, document.querySelector('.lane-add'));
        wireLane(lane);
        input.value = '';
        input.focus();
      } catch (err) { if (boardErr) boardErr.textContent = msg(err); }
    });
  }

  // The server may have rendered a modal already (a ?item= deep link); wire it.
  const existing = document.querySelector('[data-modal]');
  if (existing) { modalEl = existing; wireModal(existing); }

  // A click anywhere but a dot/swatch (those stop propagation) closes any open
  // colour picker; Escape closes it too.
  document.addEventListener('click', closePalettes);
  window.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    closePalettes();
    document.querySelectorAll('.facet[open]').forEach((f) => f.removeAttribute('open'));
    if (modalEl) closeModal();
  });
  window.addEventListener('popstate', () => {
    const item = new URLSearchParams(location.search).get('item');
    if (item && !modalEl) openModal(item, false);
    else if (!item && modalEl) closeModal(false);
  });
})();
