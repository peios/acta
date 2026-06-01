// board.js — drag-and-drop and inline editing for the board. The page is fully
// server-rendered; this only layers on interactivity. All mutations go through
// the same JSON API that automation uses. Loaded after sortable.min.js.
(() => {
  const board = document.getElementById('board');
  if (!board) return;
  const wrap = document.querySelector('.board-wrap');
  const base = '/w/' + wrap.dataset.slug;
  const csrf = document.querySelector('meta[name="csrf-token"]').content;
  const errEl = document.querySelector('[data-board-error]');

  const MESSAGES = {
    status_not_empty: 'Empty the lane before deleting it.',
    invalid_name: 'Enter a status name (1–40 characters).',
    invalid_title: 'Enter an item title.',
  };

  function showErr(e) {
    if (errEl) errEl.textContent = MESSAGES[e.message] || 'Something went wrong — reload and try again.';
  }
  function clearErr() {
    if (errEl) errEl.textContent = '';
  }

  // api POSTs JSON and returns the parsed body (or null for 204). Throws an
  // Error whose message is the server's error code on failure.
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

  // --- elements ---

  function newItem(it) {
    const el = document.getElementById('item-tmpl').content.firstElementChild.cloneNode(true);
    el.dataset.itemId = it.id;
    el.querySelector('.item-title').textContent = it.title;
    wireItem(el);
    return el;
  }

  function wireItem(el) {
    el.querySelector('.item-del').addEventListener('click', async () => {
      clearErr();
      try { await api('/items/' + el.dataset.itemId + '/delete'); el.remove(); }
      catch (e) { showErr(e); }
    });
    el.querySelector('.item-title').addEventListener('click', (ev) => editTitle(ev.target));
  }

  // editTitle swaps the title span for an input; Enter/blur saves, Escape cancels.
  function editTitle(span) {
    const id = span.closest('.item').dataset.itemId;
    const input = document.createElement('input');
    input.className = 'item-edit';
    input.maxLength = 200;
    input.value = span.textContent;
    span.replaceWith(input);
    input.focus();
    input.select();

    let done = false;
    const finish = async (save) => {
      if (done) return;
      done = true;
      const title = input.value.trim();
      if (save && title && title !== span.textContent) {
        clearErr();
        try { await api('/items/' + id + '/rename', { title }); span.textContent = title; }
        catch (e) { showErr(e); }
      }
      input.replaceWith(span);
    };
    input.addEventListener('blur', () => finish(true));
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); finish(true); }
      else if (e.key === 'Escape') { e.preventDefault(); finish(false); }
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
      clearErr();
      try { await api('/statuses/' + statusId() + '/rename', { name }); lastName = name; }
      catch (e) { showErr(e); nameInput.value = lastName; }
    });

    lane.querySelector('.lane-del').addEventListener('click', async () => {
      clearErr();
      if (!confirm('Delete this lane? It must be empty.')) return;
      try { await api('/statuses/' + statusId() + '/delete'); lane.remove(); }
      catch (e) { showErr(e); }
    });

    lane.querySelector('.item-add').addEventListener('submit', async (e) => {
      e.preventDefault();
      const input = e.target.querySelector('.item-add-input');
      const title = input.value.trim();
      if (!title) return;
      clearErr();
      try {
        const it = await api('/items', { status_id: statusId(), title });
        lane.querySelector('.lane-items').append(newItem(it));
        input.value = '';
        input.focus();
      } catch (err) { showErr(err); }
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
        clearErr();
        api('/items/' + id + '/move', { status_id: toStatus, index: evt.newIndex })
          .catch((e) => { showErr(e); location.reload(); });
      },
    });
  }

  // --- wire the server-rendered board ---

  board.querySelectorAll('.lane').forEach(wireLane);
  board.querySelectorAll('.item').forEach(wireItem);

  // Reorder lanes by dragging their grip; the add-a-lane column stays put.
  new Sortable(board, {
    draggable: '.lane',
    handle: '.lane-grip',
    animation: 150,
    onMove: (evt) => !evt.related.classList.contains('lane-add'),
    onEnd: () => {
      const ids = [...board.querySelectorAll('.lane')].map((l) => l.dataset.statusId);
      clearErr();
      api('/statuses/reorder', { ids }).catch(showErr);
    },
  });

  // Add a lane.
  document.querySelector('.lane-add-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = e.target.querySelector('.lane-add-input');
    const name = input.value.trim();
    if (!name) return;
    clearErr();
    try {
      const st = await api('/statuses', { name });
      const lane = document.getElementById('lane-tmpl').content.firstElementChild.cloneNode(true);
      lane.dataset.statusId = st.id;
      lane.querySelector('.lane-name').value = st.name;
      board.insertBefore(lane, document.querySelector('.lane-add'));
      wireLane(lane);
      input.value = '';
      input.focus();
    } catch (err) { showErr(err); }
  });
})();
