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

  // --- board items ---

  function newItem(it) {
    const el = document.getElementById('item-tmpl').content.firstElementChild.cloneNode(true);
    el.dataset.itemId = it.id;
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

  function wireLane(lane) {
    const statusId = () => lane.dataset.statusId;
    const nameInput = lane.querySelector('.lane-name');
    let lastName = nameInput.value;

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
        lane.querySelector('.lane-items').append(newItem(it));
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
        const toStatus = evt.to.closest('.lane').dataset.statusId;
        api('/items/' + id + '/move', { status_id: toStatus, index: evt.newIndex })
          .catch((e) => { if (boardErr) boardErr.textContent = msg(e); location.reload(); });
      },
    });
  }

  // --- the item modal ---

  let modalEl = null;
  let opener = null; // card to restore focus to on close

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
    if (push) history.pushState({ item: id }, '', '?item=' + id);
    const title = modalEl.querySelector('.modal-title');
    if (title) title.focus();
  }

  function closeModal(push = true) {
    if (modalEl) { modalEl.remove(); modalEl = null; }
    if (push) history.pushState({}, '', location.pathname);
    if (opener) { opener.focus(); opener = null; }
  }

  function wireModal(el) {
    const id = el.dataset.itemId;
    opener = cardOf(id);
    const err = el.querySelector('[data-modal-error]');
    const fail = (e) => { if (err) err.textContent = msg(e); };

    el.addEventListener('mousedown', (e) => { if (e.target === el) closeModal(); });
    el.querySelector('[data-modal-close]').addEventListener('click', (e) => { e.preventDefault(); closeModal(); });

    const title = el.querySelector('.modal-title');
    title.addEventListener('change', async () => {
      const t = title.value.trim();
      if (!t) return;
      try { await api('/items/' + id + '/rename', { title: t }); const c = cardOf(id); if (c) c.querySelector('.item-title').textContent = t; }
      catch (e) { fail(e); }
    });

    el.querySelector('.modal-status').addEventListener('change', async (e) => {
      const statusId = e.target.value;
      try {
        await api('/items/' + id + '/status', { status_id: statusId });
        const c = cardOf(id);
        const lane = board.querySelector('.lane[data-status-id="' + CSS.escape(statusId) + '"] .lane-items');
        if (c && lane) lane.append(c);
      } catch (err2) { fail(err2); }
    });

    el.querySelector('.modal-assignee').addEventListener('change', async (e) => {
      try { await api('/items/' + id + '/assignee', { assignee_id: e.target.value }); }
      catch (err2) { fail(err2); }
    });

    // Reparenting (promote to None / demote under an item) restructures the
    // board, so reload to reflect it; the ?item= in the URL reopens the modal.
    el.querySelector('.modal-parent-select').addEventListener('change', async (e) => {
      try { await api('/items/' + id + '/parent', { parent_id: e.target.value }); location.reload(); }
      catch (err2) { fail(err2); }
    });

    el.querySelector('.modal-desc-save').addEventListener('click', async () => {
      const description = el.querySelector('.modal-desc').value;
      try { await api('/items/' + id + '/description', { description }); }
      catch (e) { fail(e); }
    });

    el.querySelector('[data-comment-form]').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = el.querySelector('.comment-input');
      const body = input.value.trim();
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
        input.value = '';
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

  // --- wire the server-rendered board ---

  board.querySelectorAll('.lane').forEach(wireLane);
  board.querySelectorAll('.item').forEach(wireItem);

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
      lane.querySelector('.lane-name').value = st.name;
      board.insertBefore(lane, document.querySelector('.lane-add'));
      wireLane(lane);
      input.value = '';
      input.focus();
    } catch (err) { if (boardErr) boardErr.textContent = msg(err); }
  });

  // The server may have rendered a modal already (a ?item= deep link); wire it.
  const existing = document.querySelector('[data-modal]');
  if (existing) { modalEl = existing; wireModal(existing); }

  window.addEventListener('keydown', (e) => { if (e.key === 'Escape' && modalEl) closeModal(); });
  window.addEventListener('popstate', () => {
    const item = new URLSearchParams(location.search).get('item');
    if (item && !modalEl) openModal(item, false);
    else if (!item && modalEl) closeModal(false);
  });
})();
