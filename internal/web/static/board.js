// board.js — interactivity for the board: drag-and-drop, inline create, and the
// item modal (opened by ?item=<id>). The page is fully server-rendered; this
// only enhances it. All mutations go through the JSON API. Loaded after
// sortable.min.js.
(() => {
  const board = document.getElementById('board');
  if (!board) return;
  const wrap = document.querySelector('.board-wrap');
  const base = '/' + wrap.dataset.slug;
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

  const myClient = () => (window.actaClientId ? window.actaClientId() : '');

  async function api(path, body) {
    const res = await fetch(base + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf, 'X-Acta-Client': myClient() },
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

  // --- assignee avatars ---
  // Mirrors the server's initials + colour hash (see initials()/avatarStyle() in
  // board.go) so a card repaints on assign to match a full render. Keep in sync.
  const AVATAR_PALETTE = [['#5b6cf0', '#4d7cfe'], ['#23c3b3', '#16b8a6'], ['#a78bff', '#8b6cf0'], ['#f2628c', '#e0517b'], ['#e6a04b', '#d98a2b'], ['#3ecf8e', '#2bb673'], ['#3fc7d4', '#2ba8b8'], ['#ff8a5b', '#f26d3d']];
  function avatarHash(s) { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193); } return h >>> 0; }
  function avatarStyle(id) { const p = AVATAR_PALETTE[avatarHash(id) % AVATAR_PALETTE.length]; return 'background:linear-gradient(145deg,' + p[0] + ',' + p[1] + ')'; }
  function avatarInitials(name) { const f = name.trim().split(/\s+/).filter(Boolean); if (!f.length) return '?'; if (f.length === 1) return f[0].slice(0, 2).toUpperCase(); return (f[0][0] + f[f.length - 1][0]).toUpperCase(); }

  // setCardAvatar repaints a card's assignee avatar from the modal's selected
  // option (text "Name" or "Name · agent"). An empty id clears it.
  function setCardAvatar(card, assigneeId, optText) {
    let meta = card.querySelector('.item-meta');
    const existing = card.querySelector('.avatar.sm');
    if (!assigneeId) {
      if (existing) existing.remove();
      // The meta row always carries the item's ref id now, so keep it.
      return;
    }
    const raw = (optText || '').trim();
    const dot = raw.indexOf('·');
    const agent = dot >= 0 && raw.slice(dot).includes('agent');
    const name = (dot >= 0 ? raw.slice(0, dot) : raw).trim();
    if (!meta) {
      meta = document.createElement('div');
      meta.className = 'item-meta';
      const sp = document.createElement('span'); sp.className = 'meta-spacer'; meta.appendChild(sp);
      card.appendChild(meta);
    } else if (!meta.querySelector('.meta-spacer')) {
      const sp = document.createElement('span'); sp.className = 'meta-spacer'; meta.appendChild(sp);
    }
    const av = existing || meta.appendChild(document.createElement('span'));
    av.className = 'avatar sm' + (agent ? ' bot' : '');
    av.setAttribute('style', avatarStyle(assigneeId));
    av.title = name;
    av.textContent = avatarInitials(name);
  }

  // --- @-mention autocomplete (comment box) ---
  // Suggests directable principals (humans + your own agents) from
  // /mentionables and inserts the canonical @handle. The panel is fixed-
  // positioned on <body> below the textarea so the modal's overflow:hidden
  // can't clip it. Other agents stay mentionable by typing a full @owner/name.
  let mentionablesP = null;
  function loadMentionables() {
    if (!mentionablesP) {
      mentionablesP = fetch(base + '/mentionables', { headers: { 'X-CSRF-Token': csrf } })
        .then((r) => (r.ok ? r.json() : []))
        .catch(() => []);
    }
    return mentionablesP;
  }
  function filterMentions(all, q) {
    q = q.toLowerCase();
    if (!q) return all.slice(0, 8);
    const starts = [], has = [];
    for (const c of all) {
      const u = c.username.toLowerCase(), d = c.display.toLowerCase();
      if (u.startsWith(q) || d.startsWith(q)) starts.push(c);
      else if (u.includes(q) || d.includes(q)) has.push(c);
    }
    return starts.concat(has).slice(0, 8);
  }
  let mPop = null;
  function mentionPop() {
    if (!mPop) {
      mPop = document.createElement('div');
      mPop.className = 'mention-pop';
      mPop.hidden = true;
      document.body.appendChild(mPop);
    }
    return mPop;
  }
  const hideMentions = () => { if (mPop) mPop.hidden = true; };

  function wireMention(input) {
    const pop = mentionPop();
    let cands = [], sel = 0;
    const isOpen = () => !pop.hidden;
    const place = () => {
      const r = input.getBoundingClientRect();
      pop.style.left = r.left + 'px';
      pop.style.top = (r.bottom + 4) + 'px';
      pop.style.width = Math.max(r.width, 220) + 'px';
    };
    // The @token under the caret: its '@' index, the caret, and the query text.
    const context = () => {
      const pos = input.selectionStart;
      const m = /(^|\s)@([A-Za-z0-9._/-]*)$/.exec(input.value.slice(0, pos));
      return m ? { at: pos - m[2].length - 1, end: pos, query: m[2] } : null;
    };
    const choose = (c) => {
      const ctx = context();
      if (ctx) {
        const before = input.value.slice(0, ctx.at);
        const after = input.value.slice(ctx.end);
        const ins = '@' + c.username + ' ';
        input.value = before + ins + after;
        const caret = before.length + ins.length;
        input.setSelectionRange(caret, caret);
      }
      hideMentions();
      input.focus();
    };
    const render = () => {
      pop.textContent = '';
      cands.forEach((c, i) => {
        const row = document.createElement('div');
        row.className = 'mention-row' + (i === sel ? ' active' : '');
        const av = document.createElement('span');
        av.className = 'avatar sm' + (c.agent ? ' bot' : '');
        av.setAttribute('style', avatarStyle(c.username));
        av.textContent = avatarInitials(c.display);
        const name = document.createElement('span');
        name.className = 'mention-name';
        name.textContent = c.display;
        const handle = document.createElement('span');
        handle.className = 'mention-handle';
        handle.textContent = '@' + c.username;
        row.append(av, name, handle);
        row.addEventListener('mousedown', (e) => { e.preventDefault(); choose(c); });
        pop.appendChild(row);
      });
    };
    const refresh = async () => {
      const ctx = context();
      if (!ctx) return hideMentions();
      cands = filterMentions(await loadMentionables(), ctx.query);
      sel = 0;
      if (!cands.length) return hideMentions();
      pop.hidden = false;
      render();
      place();
    };
    input.addEventListener('input', refresh);
    input.addEventListener('keydown', (e) => {
      if (!isOpen()) return;
      switch (e.key) {
        case 'ArrowDown': e.preventDefault(); e.stopPropagation(); sel = (sel + 1) % cands.length; render(); break;
        case 'ArrowUp': e.preventDefault(); e.stopPropagation(); sel = (sel - 1 + cands.length) % cands.length; render(); break;
        case 'Enter': if (e.metaKey || e.ctrlKey) return; e.preventDefault(); e.stopPropagation(); choose(cands[sel]); break;
        case 'Tab': e.preventDefault(); e.stopPropagation(); choose(cands[sel]); break;
        case 'Escape': e.preventDefault(); e.stopPropagation(); hideMentions(); break;
      }
    });
    input.addEventListener('blur', () => setTimeout(hideMentions, 120));
  }

  // --- board items ---

  function newItem(it) {
    const el = document.getElementById('item-tmpl').content.firstElementChild.cloneNode(true);
    el.dataset.itemId = it.id;
    el.dataset.statusId = it.status_id || '';
    el.dataset.assigneeId = '';
    el.querySelector('.item-title').textContent = it.title;
    if (it.ref) {
      const meta = document.createElement('div');
      meta.className = 'item-meta';
      const ref = document.createElement('span');
      ref.className = 'item-ref';
      ref.textContent = it.ref;
      const sp = document.createElement('span');
      sp.className = 'meta-spacer';
      meta.append(ref, sp);
      el.appendChild(meta);
    }
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
    hideMentions();
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
      const assigneeId = e.target.value;
      try {
        await api('/items/' + id + '/assignee', { assignee_id: assigneeId });
        const c = cardOf(id);
        if (c) {
          c.dataset.assigneeId = assigneeId;
          const opt = e.target.options[e.target.selectedIndex];
          setCardAvatar(c, assigneeId, opt ? opt.textContent : '');
          reapplyFilters();
        }
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
        hideMentions();
      } catch (err2) { fail(err2); }
    });
    wireMention(commentInput);

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
    const badge = document.querySelector('[data-filter-badge]');
    if (badge) { const n = statuses.length + assignees.length; badge.textContent = n; badge.hidden = n === 0; }
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
  }

  // --- header popovers (Filter / Display) ---
  // closePops is module-scoped so Escape can reach it.
  let closePops = () => {};
  function wirePopovers() {
    const anchors = [...document.querySelectorAll('[data-pop]')];
    if (!anchors.length) return;
    closePops = () => anchors.forEach((a) => {
      a.querySelector('[data-pop-menu]').hidden = true;
      a.querySelector('[data-pop-btn]').classList.remove('active');
    });
    anchors.forEach((a) => {
      const btn = a.querySelector('[data-pop-btn]');
      const menu = a.querySelector('[data-pop-menu]');
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const willOpen = menu.hidden;
        closePops();
        if (willOpen) { menu.hidden = false; btn.classList.add('active'); }
      });
      // Clicks inside a menu must not bubble out and close it.
      menu.addEventListener('click', (e) => e.stopPropagation());
    });
    document.addEventListener('click', () => closePops());
  }

  // --- display properties (which fields/empty lanes show; per-workspace pref) ---
  const DISP_KEY = 'acta:disp:' + wrap.dataset.slug;
  const DISP_KEYS = ['empty', 'assignee', 'sub', 'milestone'];
  const loadDisp = () => { try { return JSON.parse(localStorage.getItem(DISP_KEY)) || {}; } catch (_) { return {}; } };
  const saveDisp = (d) => { try { localStorage.setItem(DISP_KEY, JSON.stringify(d)); } catch (_) {} };
  function applyDisp() {
    const d = loadDisp();
    DISP_KEYS.forEach((k) => {
      const on = d[k] !== false; // shown unless explicitly turned off
      wrap.classList.toggle('hide-' + k, !on);
      const ctrl = document.querySelector('[data-display="' + k + '"]');
      if (!ctrl) return;
      ctrl.setAttribute(ctrl.classList.contains('toggle') ? 'aria-checked' : 'aria-pressed', on ? 'true' : 'false');
    });
  }
  function wireDisplay() {
    document.querySelectorAll('[data-display]').forEach((ctrl) => {
      ctrl.addEventListener('click', () => {
        const d = loadDisp();
        d[ctrl.dataset.display] = d[ctrl.dataset.display] === false; // flip, default-on
        saveDisp(d);
        applyDisp();
      });
    });
    const reset = document.querySelector('[data-display-reset]');
    if (reset) reset.addEventListener('click', () => { saveDisp({}); applyDisp(); });
    applyDisp();
  }

  // --- wire the server-rendered board ---

  board.querySelectorAll('.item').forEach(wireItem);
  wireFilters();
  wirePopovers();
  wireDisplay();

  if (board.dataset.mode === 'milestone') {
    board.querySelectorAll('.mcol').forEach(wireColumn);

    // Reorder milestone columns by dragging their headers. The Backlog stays
    // pinned first (no grip, and nothing may drop ahead of it).
    const backlog = board.querySelector('.mcol[data-parent-id=""]');
    new Sortable(board, {
      draggable: '.mcol',
      handle: '.mcol-grip',
      animation: 150,
      onMove: (evt) => !(evt.related === backlog && !evt.willInsertAfter),
      onEnd: () => {
        const ids = [...board.querySelectorAll('.mcol')].map((c) => c.dataset.parentId).filter(Boolean);
        api('/milestones/reorder', { ids }).catch((e) => { if (boardErr) boardErr.textContent = msg(e); });
      },
    });
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
    closePops();
    if (modalEl) closeModal();
  });
  window.addEventListener('popstate', () => {
    const item = new URLSearchParams(location.search).get('item');
    if (item && !modalEl) openModal(item, false);
    else if (!item && modalEl) closeModal(false);
  });

  // --- live updates (SSE) ---
  // live.js opens the stream and re-dispatches each event as `acta:live`, having
  // already dropped this tab's own echoes (origin match). So everything reaching
  // here is a remote change to apply to the board or the open modal.

  function liveCardRef(card, ref) {
    let meta = card.querySelector('.item-meta');
    if (!meta) { meta = document.createElement('div'); meta.className = 'item-meta'; card.appendChild(meta); }
    let el = meta.querySelector('.item-ref');
    if (!el) { el = document.createElement('span'); el.className = 'item-ref'; meta.insertBefore(el, meta.firstChild); }
    el.textContent = ref || '';
    if (!meta.querySelector('.meta-spacer')) {
      const sp = document.createElement('span'); sp.className = 'meta-spacer'; meta.appendChild(sp);
    }
  }

  function liveCardMilestone(card, on) {
    const ms = card.querySelector('.item-ms');
    if (on && !ms) {
      const m = document.createElement('span');
      m.className = 'item-ms'; m.title = 'Milestone'; m.textContent = '◆';
      card.querySelector('.item-title').insertAdjacentElement('afterend', m);
    } else if (!on && ms) {
      ms.remove();
    }
  }

  function liveCardAvatar(card, a) {
    const meta = card.querySelector('.item-meta');
    const existing = card.querySelector('.avatar.sm');
    if (!a) { if (existing) existing.remove(); return; }
    if (!meta) return;
    if (!meta.querySelector('.meta-spacer')) {
      const sp = document.createElement('span'); sp.className = 'meta-spacer'; meta.appendChild(sp);
    }
    const av = existing || meta.appendChild(document.createElement('span'));
    av.className = 'avatar sm' + (a.agent ? ' bot' : '');
    av.setAttribute('style', avatarStyle(a.id));
    av.title = a.name || '';
    av.textContent = a.initials || avatarInitials(a.name || '');
  }

  function applyUpsert(msg) {
    let card = cardOf(msg.id);
    // A non-root item (e.g. just reparented under another) has no board card; if
    // it had one, drop it.
    if (msg.parent_id) { if (card) card.remove(); return; }

    const lane = board.querySelector('.lane[data-status-id="' + CSS.escape(msg.status_id || '') + '"]');
    const itemsEl = lane ? lane.querySelector('.lane-items') : null;

    if (!card) {
      if (!itemsEl) return; // its lane isn't on this view (e.g. milestone mode)
      card = newItem({ id: msg.id, title: msg.title, status_id: msg.status_id, ref: msg.ref });
    } else {
      card.querySelector('.item-title').textContent = msg.title || '';
    }
    card.dataset.statusId = msg.status_id || '';
    liveCardRef(card, msg.ref);
    liveCardMilestone(card, !!msg.milestone);
    card.dataset.assigneeId = msg.assignee ? msg.assignee.id : '';
    liveCardAvatar(card, msg.assignee);
    if (msg.color) card.style.setProperty('--lane-color', msg.color);
    // Only re-home the card when it isn't already in the target lane, so a field
    // change never yanks it to the bottom of its column.
    if (itemsEl && card.parentElement !== itemsEl) itemsEl.appendChild(card);
    reapplyFilters();
  }

  function applyRemove(msg) {
    const card = cardOf(msg.id);
    if (card) card.remove();
    if (modalEl && modalEl.dataset.itemId === msg.id) closeModal();
  }

  function applyComment(msg) {
    if (!modalEl || modalEl.dataset.itemId !== msg.item) return;
    const list = modalEl.querySelector('[data-comment-list]');
    if (!list) return;
    const div = document.createElement('div');
    div.className = 'comment';
    const meta = document.createElement('div');
    meta.className = 'comment-meta';
    meta.textContent = (msg.author || '') + ' · ' + (msg.at || '');
    const text = document.createElement('div');
    text.className = 'comment-body';
    text.textContent = msg.body || '';
    div.append(meta, text);
    list.append(div);
  }

  function applySubtaskAdd(msg) {
    if (!modalEl || modalEl.dataset.itemId !== msg.parent) return;
    const list = modalEl.querySelector('[data-subtask-list]');
    if (!list || list.querySelector('.subtask[data-item-id="' + CSS.escape(msg.id) + '"]')) return;
    const row = document.createElement('div');
    row.className = 'subtask';
    row.dataset.itemId = msg.id;
    const grip = document.createElement('span'); grip.className = 'subtask-grip'; grip.title = 'Drag to reorder'; grip.textContent = '⠿';
    const open = document.createElement('button'); open.type = 'button'; open.className = 'subtask-open'; open.textContent = msg.title || '';
    const status = document.createElement('span'); status.className = 'subtask-status';
    row.append(grip, open, status);
    open.addEventListener('click', () => openModal(msg.id));
    list.append(row);
  }

  document.addEventListener('acta:live', (e) => {
    const msg = e.detail || {};
    try {
      switch (msg.kind) {
        case 'item.upsert': applyUpsert(msg); break;
        case 'item.remove': applyRemove(msg); break;
        case 'comment.add': applyComment(msg); break;
        case 'subtask.add': applySubtaskAdd(msg); break;
      }
    } catch (_) { /* a live update must never break the page */ }
  });
})();
