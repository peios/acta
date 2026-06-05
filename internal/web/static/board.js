// board.js — interactivity for the board: drag-and-drop, inline create, and the
// item modal (opened by ?item=<id>). The page is fully server-rendered; this
// only enhances it. All mutations go through the JSON API. Loaded after
// sortable.min.js.
(() => {
  const board = document.getElementById('board');
  if (!board) return;
  const wrap = document.querySelector('.board-wrap');
  const base = '/' + wrap.dataset.slug;
  const boardId = wrap.dataset.boardId || ''; // which board new lanes join
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

  // --- comment composer (shared by the new-comment box and inline edit) ---
  // One behaviour for both: auto-grow, a send that enables once there's text,
  // ⌘/Ctrl+Enter (and the button) to submit, Escape/Cancel to back out, and
  // @-mention autocomplete. onSubmit(text) may be async; after a successful
  // submit the box clears (unless resetOnSubmit:false, e.g. for inline edit,
  // where the caller tears the composer down itself).
  function autogrow(ta) {
    ta.style.height = 'auto';
    ta.style.height = ta.scrollHeight + 'px';
  }

  function wireComposer(box, opts) {
    const ta = box.querySelector('.composer-input');
    const send = box.querySelector('.composer-send');
    const cancel = box.querySelector('.composer-cancel');
    let busy = false;
    const sync = () => { send.disabled = busy || !ta.value.trim(); autogrow(ta); };
    const submit = async () => {
      const text = ta.value.trim();
      if (!text || busy) return;
      busy = true; sync();
      try {
        await opts.onSubmit(text);
        if (opts.resetOnSubmit !== false) ta.value = '';
        hideMentions();
      } catch (e) { if (opts.onError) opts.onError(e); }
      finally { busy = false; sync(); }
    };
    ta.addEventListener('input', sync);
    ta.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); submit(); }
      else if (e.key === 'Escape' && opts.onCancel) { e.preventDefault(); e.stopPropagation(); opts.onCancel(); }
    });
    send.addEventListener('click', submit);
    if (cancel && opts.onCancel) cancel.addEventListener('click', opts.onCancel);
    wireMention(ta);
    requestAnimationFrame(() => autogrow(ta)); // size correctly once laid out
    sync();
    return { textarea: ta, focus: () => ta.focus() };
  }

  // buildComposer mints a .composer matching the server-rendered one, for inline
  // edit. Mirror the markup in item_modal.html if you change either.
  function buildComposer({ value = '', placeholder = '', withCancel = false } = {}) {
    const box = document.createElement('div');
    box.className = 'composer';
    box.dataset.composer = '';
    const ta = document.createElement('textarea');
    ta.className = 'composer-input';
    ta.rows = 1;
    ta.maxLength = 5000;
    ta.placeholder = placeholder;
    ta.value = value;
    const foot = document.createElement('div');
    foot.className = 'composer-foot';
    const hint = document.createElement('span');
    hint.className = 'composer-hint';
    hint.textContent = 'Markdown supported · ⌘↵ to save';
    const actions = document.createElement('div');
    actions.className = 'composer-actions';
    if (withCancel) {
      const cancel = document.createElement('button');
      cancel.type = 'button';
      cancel.className = 'composer-cancel';
      cancel.textContent = 'Cancel';
      actions.appendChild(cancel);
    }
    const send = document.createElement('button');
    send.type = 'button';
    send.className = 'composer-send';
    send.setAttribute('aria-label', 'Save');
    send.disabled = true;
    send.innerHTML = '<svg class="ico" viewBox="0 0 16 16"><path d="M8 13.5V3.5M3.5 8 8 3.5 12.5 8"/></svg>';
    actions.appendChild(send);
    foot.append(hint, actions);
    box.append(ta, foot);
    return box;
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
      // Anchor the panel's bottom just above the input so it overlays upward —
      // clears the on-screen keyboard on mobile, and is consistent on desktop.
      pop.style.top = 'auto';
      pop.style.bottom = (window.innerHeight - r.top + 4) + 'px';
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
    // Open on a tap. We use pointer events rather than `click` because
    // SortableJS's touch drag-delay swallows the first click on touch (it took
    // two taps). A tap is down→up on the same card with negligible movement and,
    // on touch, quicker than the drag delay — so a real drag (moves, or is held
    // past the delay) and a scroll (moves, or fires pointercancel) never open it.
    const DRAG_DELAY = 200; // keep in sync with the card Sortable's `delay`
    let px = 0, py = 0, downT = 0, pmoved = false;
    el.addEventListener('pointerdown', (e) => {
      if (e.button > 0) return;
      px = e.clientX; py = e.clientY; downT = Date.now(); pmoved = false;
    });
    el.addEventListener('pointermove', (e) => {
      if (!pmoved && (Math.abs(e.clientX - px) > 9 || Math.abs(e.clientY - py) > 9)) pmoved = true;
    });
    el.addEventListener('pointercancel', () => { downT = 0; });
    el.addEventListener('pointerup', (e) => {
      const t = downT; downT = 0;
      if (!t || pmoved || e.target.closest('.item-del')) return;
      if (e.pointerType === 'touch' && Date.now() - t >= DRAG_DELAY) return;
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
      // On touch, require a short hold before a card starts dragging so quick
      // swipes still scroll the board; moving past the threshold mid-hold
      // cancels the pending drag (treated as a scroll). Mouse drag stays instant.
      delay: 200,
      delayOnTouchOnly: true,
      touchStartThreshold: 6,
      // On touch the board doesn't auto-scroll (scroll:false); instead onChange
      // snaps the board to whichever lane the placeholder enters, so a card
      // crosses columns one snap at a time. Desktop keeps normal auto-scroll.
      scroll: !touchPaging,
      forceAutoScrollFallback: true,
      scrollSensitivity: 60,
      scrollSpeed: 14,
      onStart: startCardDrag,
      onMove: nestOnMove,
      onChange: onDragChange,
      onEnd: (evt) => {
        endCardDrag();
        if (handleNestDrop(evt)) return;
        if (handleBoardDrop(evt)) return;
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
      // On touch, require a short hold before a card starts dragging so quick
      // swipes still scroll the board; moving past the threshold mid-hold
      // cancels the pending drag (treated as a scroll). Mouse drag stays instant.
      delay: 200,
      delayOnTouchOnly: true,
      touchStartThreshold: 6,
      // On touch the board doesn't auto-scroll (scroll:false); instead onChange
      // snaps the board to whichever lane the placeholder enters, so a card
      // crosses columns one snap at a time. Desktop keeps normal auto-scroll.
      scroll: !touchPaging,
      forceAutoScrollFallback: true,
      scrollSensitivity: 60,
      scrollSpeed: 14,
      onStart: startCardDrag,
      onMove: nestOnMove,
      onChange: onDragChange,
      onEnd: (evt) => {
        endCardDrag();
        if (handleNestDrop(evt)) return;
        if (handleBoardDrop(evt)) return;
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
            reapplyFilters();
          } else if (board.querySelector('.lane')) {
            // Status mode, but no lane here for the new status — the item took a
            // status on another board, so it leaves this one.
            c.remove();
          }
        }
      } catch (err2) { fail(err2); }
    });

    // --- modal pills (status / assignee) ----------------------------------
    // Each pill is a styled trigger + dropdown that drives the matching hidden
    // side <select>, so the change handlers do the real work. A shared manager
    // closes any open pill on an outside tap (pointerdown — iOS-safe). All
    // listeners hang off `el`, so they're torn down when the modal is replaced.
    // A pop manager is one set of mutually-exclusive popovers. The top bar +
    // "more" kebab live in one (modalPops); the side-panel pills live in a
    // separate one (sidePops) so opening a side dropdown only closes its
    // siblings — never the kebab that hosts the whole side panel on mobile.
    const makePops = () => {
      const pops = [];
      const closeAll = () => pops.forEach((p) => { p.menu.hidden = true; p.trigger.classList.remove('active'); });
      const wire = (wrap) => {
        const trigger = wrap.querySelector('[data-pill-trigger]');
        const menu = wrap.querySelector('[data-pill-menu]');
        pops.push({ wrap, trigger, menu });
        trigger.addEventListener('click', (e) => {
          e.stopPropagation();
          const willOpen = menu.hidden;
          closeAll();
          menu.hidden = !willOpen;
          trigger.classList.toggle('active', willOpen);
        });
        return { trigger, menu };
      };
      return { pops, closeAll, wire };
    };
    const top = makePops();
    const side = makePops();
    const modalPops = top.pops, closeModalPops = top.closeAll, wirePill = top.wire;
    el.addEventListener('pointerdown', (e) => {
      if (top.pops.length && !top.pops.some((p) => p.wrap.contains(e.target))) top.closeAll();
      if (side.pops.length && !side.pops.some((p) => p.wrap.contains(e.target))) side.closeAll();
    });

    const statusSelect = el.querySelector('.modal-status');
    const statusPill = el.querySelector('[data-status-pill]');
    if (statusPill && statusSelect) {
      wirePill(statusPill);
      const dot = statusPill.querySelector('[data-status-pill-dot]');
      const nameEl = statusPill.querySelector('[data-status-pill-name]');
      const opts = {};
      statusPill.querySelectorAll('[data-status-opt]').forEach((o) => {
        opts[o.dataset.statusOpt] = { color: o.dataset.color || '', name: o.querySelector('.status-opt-name').textContent, dashed: o.dataset.dashed === '1' };
      });
      const syncStatus = () => {
        const d = opts[statusSelect.value];
        if (!d) return;
        nameEl.textContent = d.name;
        dot.style.setProperty('--lane-color', d.color);
        dot.classList.toggle('dashed', d.dashed); // Backlog statuses render dashed
      };
      statusPill.querySelectorAll('[data-status-opt]').forEach((o) => {
        o.addEventListener('click', () => {
          closeModalPops();
          if (o.dataset.statusOpt !== statusSelect.value) {
            statusSelect.value = o.dataset.statusOpt;
            statusSelect.dispatchEvent(new Event('change'));
          }
          syncStatus();
        });
      });
      syncStatus();
    }

    const assigneeSelect = el.querySelector('.modal-assignee');
    const assigneePill = el.querySelector('[data-assignee-pill]');
    if (assigneePill && assigneeSelect) {
      const { trigger } = wirePill(assigneePill);
      const current = assigneePill.querySelector('[data-assignee-current]');
      const curAvatar = assigneePill.querySelector('[data-assignee-current-avatar]');
      const curName = assigneePill.querySelector('[data-assignee-current-name]');
      const syncAssignee = () => {
        const aid = assigneeSelect.value;
        trigger.classList.toggle('assigned', !!aid);
        if (!aid) { current.hidden = true; return; }
        const opt = assigneeSelect.options[assigneeSelect.selectedIndex];
        const raw = (opt ? opt.textContent : '').trim();
        const name = raw.split('·')[0].trim();
        current.hidden = false;
        curAvatar.className = 'avatar sm' + (raw.includes('agent') ? ' bot' : '');
        curAvatar.setAttribute('style', avatarStyle(aid));
        curAvatar.textContent = avatarInitials(name);
        curName.textContent = name;
      };
      assigneePill.querySelectorAll('[data-assignee-opt]').forEach((o) => {
        o.addEventListener('click', () => {
          closeModalPops();
          if (o.dataset.assigneeOpt !== assigneeSelect.value) {
            assigneeSelect.value = o.dataset.assigneeOpt;
            assigneeSelect.dispatchEvent(new Event('change'));
          }
          syncAssignee();
        });
      });
      syncAssignee();
    }

    // --- side-panel pills (desktop) ---------------------------------------
    // Same dropdowns, driving the same hidden side <select>s. Status/assignee
    // here are desktop-only (hidden on mobile, where the top bar covers them),
    // so they never need to stay in display-sync with the top-bar pills — only
    // one set is ever visible. Each pill resyncs itself off its own select.
    const sideStatusPill = el.querySelector('[data-status-pill-side]');
    if (sideStatusPill && statusSelect) {
      side.wire(sideStatusPill);
      const dot = sideStatusPill.querySelector('[data-side-status-dot]');
      const nameEl = sideStatusPill.querySelector('[data-side-status-name]');
      const opts = {};
      sideStatusPill.querySelectorAll('[data-status-opt]').forEach((o) => {
        opts[o.dataset.statusOpt] = { color: o.dataset.color || '', name: o.querySelector('.status-opt-name').textContent, dashed: o.dataset.dashed === '1' };
      });
      const syncSideStatus = () => {
        const d = opts[statusSelect.value];
        if (!d) return;
        nameEl.textContent = d.name;
        dot.style.setProperty('--lane-color', d.color);
        dot.classList.toggle('dashed', d.dashed);
      };
      sideStatusPill.querySelectorAll('[data-status-opt]').forEach((o) => {
        o.addEventListener('click', () => {
          side.closeAll();
          if (o.dataset.statusOpt !== statusSelect.value) {
            statusSelect.value = o.dataset.statusOpt;
            statusSelect.dispatchEvent(new Event('change'));
          }
          syncSideStatus();
        });
      });
      syncSideStatus();
    }

    const sideAssigneePill = el.querySelector('[data-assignee-pill-side]');
    if (sideAssigneePill && assigneeSelect) {
      const { trigger } = side.wire(sideAssigneePill);
      const avatar = sideAssigneePill.querySelector('[data-side-assignee-avatar]');
      const nameEl = sideAssigneePill.querySelector('[data-side-assignee-name]');
      const syncSideAssignee = () => {
        const aid = assigneeSelect.value;
        trigger.classList.toggle('unset', !aid);
        if (!aid) { nameEl.textContent = 'Unassigned'; return; }
        const opt = assigneeSelect.options[assigneeSelect.selectedIndex];
        const raw = (opt ? opt.textContent : '').trim();
        const name = raw.split('·')[0].trim();
        nameEl.textContent = name;
        avatar.className = 'avatar sm' + (raw.includes('agent') ? ' bot' : '');
        avatar.setAttribute('style', avatarStyle(aid));
        avatar.textContent = avatarInitials(name);
      };
      sideAssigneePill.querySelectorAll('[data-assignee-opt]').forEach((o) => {
        o.addEventListener('click', () => {
          side.closeAll();
          if (o.dataset.assigneeOpt !== assigneeSelect.value) {
            assigneeSelect.value = o.dataset.assigneeOpt;
            assigneeSelect.dispatchEvent(new Event('change'));
          }
          syncSideAssignee();
        });
      });
      syncSideAssignee();
    }

    // Project pill (side panel only — a secondary field like milestone; it rides
    // into the "more" kebab on mobile with the rest of .modal-side). Drives the
    // hidden .modal-project select, whose change handler posts and repaints.
    const projectSelect = el.querySelector('.modal-project');
    const sideProjectPill = el.querySelector('[data-project-pill-side]');
    if (sideProjectPill && projectSelect) {
      const { trigger } = side.wire(sideProjectPill);
      const dot = sideProjectPill.querySelector('[data-side-project-dot]');
      const nameEl = sideProjectPill.querySelector('[data-side-project-name]');
      const opts = {};
      sideProjectPill.querySelectorAll('[data-project-opt]').forEach((o) => {
        opts[o.dataset.projectOpt] = { color: o.dataset.color || '', name: o.querySelector('.status-opt-name').textContent };
      });
      const syncProject = () => {
        const d = opts[projectSelect.value] || { color: '', name: 'No project' };
        const set = !!projectSelect.value;
        nameEl.textContent = d.name;
        trigger.classList.toggle('unset', !set);
        dot.hidden = !set;
        dot.style.setProperty('--lane-color', d.color);
      };
      sideProjectPill.querySelectorAll('[data-project-opt]').forEach((o) => {
        o.addEventListener('click', () => {
          side.closeAll();
          if (o.dataset.projectOpt !== projectSelect.value) {
            projectSelect.value = o.dataset.projectOpt;
            projectSelect.dispatchEvent(new Event('change'));
          }
          syncProject();
        });
      });
      syncProject();
    }
    if (projectSelect) {
      projectSelect.addEventListener('change', async (e) => {
        const pid = e.target.value;
        const opt = e.target.options[e.target.selectedIndex];
        try {
          await api('/items/' + id + '/project', { project_id: pid });
          const c = cardOf(id);
          if (c) {
            c.dataset.projectId = pid;
            setCardProject(c, pid ? (opt ? opt.textContent : '') : '', opt ? (opt.dataset.color || '') : '');
            reapplyFilters(); // the project facet may now hide/show this card
          }
        } catch (err2) { fail(err2); }
      });
    }

    // "More" kebab (mobile): relocate the whole side panel into a dropdown so
    // its remaining fields (parent, milestone, created, archive) live behind the
    // kebab. The hidden status/assignee selects ride along, still pill-driven, so
    // every existing handler keeps working — nothing is re-wired or duplicated.
    const moreWrap = el.querySelector('[data-more-pill]');
    const sideEl = el.querySelector('.modal-side');
    if (moreWrap && sideEl && window.matchMedia('(max-width: 768px)').matches) {
      const moreTrigger = moreWrap.querySelector('[data-pill-trigger]');
      moreWrap.appendChild(sideEl);
      sideEl.hidden = true;
      modalPops.push({ wrap: moreWrap, trigger: moreTrigger, menu: sideEl });
      moreTrigger.addEventListener('click', (e) => {
        e.stopPropagation();
        const willOpen = sideEl.hidden;
        closeModalPops();
        sideEl.hidden = !willOpen;
        moreTrigger.classList.toggle('active', willOpen);
      });
    }

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

    wireDescription(el, id, fail);

    // The new-comment composer: post, append the card optimistically, clear.
    const feed = el.querySelector('[data-feed]');
    const composerBox = el.querySelector('[data-composer]');
    if (composerBox) {
      wireComposer(composerBox, {
        onSubmit: async (body) => {
          const c = await api('/items/' + id + '/comment', { body });
          appendComment(feed, { ...c, mine: true }, id);
        },
        onError: fail,
      });
    }
    // Wire edit/delete on the viewer's own comments already in the feed.
    el.querySelectorAll('.cmt[data-mine]').forEach((card) => wireCommentCard(card, id));

    const parentLink = el.querySelector('[data-parent-link]');
    if (parentLink) parentLink.addEventListener('click', (e) => { e.preventDefault(); openModal(parentLink.dataset.parentLink); });

    const subList = el.querySelector('[data-subtask-list]');
    const wireSubRow = (row) =>
      row.querySelector('.subtask-open').addEventListener('click', () => openModal(row.dataset.itemId));
    subList.querySelectorAll('.subtask').forEach(wireSubRow);

    // Dragging a subtask out onto the promote zone reparents it to root; the
    // shared 'subtasks' group connects the list to that zone. Reparenting
    // restructures the board, so reload (the ?item= reopens this modal) — same
    // as the Parent picker does.
    const promoteZone = el.querySelector('[data-subtask-promote]');
    new Sortable(subList, {
      group: { name: 'subtasks', pull: true, put: true },
      handle: '.subtask-grip',
      animation: 150,
      draggable: '.subtask',
      onStart: () => el.classList.add('subtask-dragging'),
      onEnd: (evt) => {
        el.classList.remove('subtask-dragging');
        if (promoteZone && evt.to === promoteZone) {
          const subId = evt.item.dataset.itemId;
          evt.item.remove();
          api('/items/' + subId + '/parent', { parent_id: '' })
            .then(() => location.reload())
            .catch((e) => fail(e));
          return;
        }
        const ids = [...subList.querySelectorAll('.subtask')].map((r) => r.dataset.itemId);
        api('/items/' + id + '/subtasks/reorder', { ids }).catch(fail);
      },
    });
    if (promoteZone) {
      new Sortable(promoteZone, {
        group: { name: 'subtasks', pull: false, put: true },
        sort: false,
        draggable: '.subtask',
      });
    }

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

    // Milestone: a switch (the visible control) backed by a hidden checkbox
    // (the source of truth the change handler reads).
    const msToggle = el.querySelector('.modal-ms-toggle');
    const msSwitch = el.querySelector('[data-ms-toggle]');
    if (msSwitch && msToggle) msSwitch.addEventListener('click', () => {
      msToggle.checked = !msToggle.checked;
      msSwitch.setAttribute('aria-checked', msToggle.checked ? 'true' : 'false');
      msToggle.dispatchEvent(new Event('change'));
    });
    if (msToggle) msToggle.addEventListener('change', async () => {
      try { await api('/items/' + id + '/milestone', { is_milestone: msToggle.checked }); location.reload(); }
      catch (e) {
        fail(e);
        msToggle.checked = !msToggle.checked;
        if (msSwitch) msSwitch.setAttribute('aria-checked', msToggle.checked ? 'true' : 'false');
      }
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

  function filterBoard(statuses, assignees, projects) {
    const sSet = new Set(statuses), aSet = new Set(assignees), pSet = new Set(projects);
    const wrap = document.querySelector('.board-wrap');
    const me = wrap ? (wrap.dataset.me || '') : '';
    const statusOK = (id) => sSet.size === 0 || sSet.has(id);
    const assigneeOK = (aid) => {
      if (aSet.size === 0) return true;
      if (!aid) return aSet.has('unassigned');
      if (aSet.has(aid)) return true;
      return aSet.has('me') && aid === me;
    };
    const projectOK = (pid) => {
      if (pSet.size === 0) return true;
      if (!pid) return pSet.has('none');
      return pSet.has(pid);
    };
    board.querySelectorAll('.item').forEach((card) => {
      const hide = !statusOK(card.dataset.statusId) || !assigneeOK(card.dataset.assigneeId || '') ||
        !projectOK(card.dataset.projectId || '');
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

  function syncFilterURL(statuses, assignees, projects) {
    const p = new URLSearchParams(location.search);
    p.delete('status'); p.delete('assignee'); p.delete('project');
    statuses.forEach((v) => p.append('status', v));
    assignees.forEach((v) => p.append('assignee', v));
    projects.forEach((v) => p.append('project', v));
    const q = p.toString();
    history.replaceState(null, '', location.pathname + (q ? '?' + q : ''));
  }

  function applyFilters(form) {
    const statuses = facetValues(form, 'status');
    const assignees = facetValues(form, 'assignee');
    const projects = facetValues(form, 'project');
    filterBoard(statuses, assignees, projects);
    setFacetCount(form, 'status', statuses.length, 'Status');
    setFacetCount(form, 'assignee', assignees.length, 'Assignee');
    setFacetCount(form, 'project', projects.length, 'Project');
    syncFilterURL(statuses, assignees, projects);
    if (window.__actaBoardPrefs) window.__actaBoardPrefs.save(); // remember filters per workspace
    const total = statuses.length + assignees.length + projects.length;
    const clear = form.querySelector('.facet-clear');
    if (clear) clear.hidden = total === 0;
    const badge = document.querySelector('[data-filter-badge]');
    if (badge) { badge.textContent = total; badge.hidden = total === 0; }
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

    form.querySelectorAll('input[name="status"], input[name="project"], input[value="me"], input[value="unassigned"]').forEach((c) => {
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
      // Taps inside the trigger or menu must not reach the document dismiss
      // handlers. We dismiss on `pointerdown` as well as `click` because iOS
      // doesn't reliably bubble `click` from the (non-interactive) board
      // background, so an outside tap was being missed; guarding pointerdown on
      // the trigger keeps tap-to-close from closing-then-reopening.
      btn.addEventListener('pointerdown', (e) => e.stopPropagation());
      menu.addEventListener('click', (e) => e.stopPropagation());
      menu.addEventListener('pointerdown', (e) => e.stopPropagation());
    });
    document.addEventListener('click', () => closePops());
    document.addEventListener('pointerdown', () => closePops());
  }

  // --- display properties (which fields/empty lanes show; per-workspace pref) ---
  const DISP_KEY = 'acta:disp:' + wrap.dataset.slug;
  const DISP_KEYS = ['empty', 'assignee', 'sub', 'milestone', 'project'];
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

  // --- view-tab strip: fade whichever edge still has tabs off-screen ---
  function wireViewScroll() {
    const views = document.querySelector('.views');
    if (!views) return;
    const update = () => {
      const max = views.scrollWidth - views.clientWidth;
      views.classList.toggle('fade-start', views.scrollLeft > 1);
      views.classList.toggle('fade-end', max > 1 && views.scrollLeft < max - 1);
    };
    views.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    update();
  }

  // --- card drag: snap the board to the lane the ghost enters (mobile) -------
  // On touch the board does NOT auto-scroll (scroll:false below). Instead, when
  // SortableJS moves the drag placeholder into a different lane (onChange fires),
  // we snap-scroll that lane to centre — so a card crosses columns one snap at a
  // time, driven by SortableJS's own target detection (no touch/scroll guesswork).
  // CSS snap is suspended mid-drag (.board.dragging) so the scroll lands clean.
  const touchPaging = window.matchMedia('(max-width: 768px)').matches;
  function centerLane(lane) {
    if (!lane) return;
    const bLeft = board.getBoundingClientRect().left;
    const r = lane.getBoundingClientRect();
    const target = (r.left - bLeft) + board.scrollLeft + r.width / 2 - board.clientWidth / 2;
    if (Math.abs(target - board.scrollLeft) < 4) return; // already centred
    board.scrollTo({ left: target, behavior: 'smooth' });
  }
  function onDragChange(evt) {
    if (touchPaging) centerLane(evt.to.closest('.lane'));
  }
  function startCardDrag() {
    board.classList.add('dragging');
    document.body.classList.add('card-dragging');
    document.addEventListener('dragover', clearNestIfOff, true);
    document.addEventListener('touchmove', clearNestIfOff, { passive: true });
  }
  function endCardDrag() {
    board.classList.remove('dragging');
    document.body.classList.remove('card-dragging');
    document.removeEventListener('dragover', clearNestIfOff, true);
    document.removeEventListener('touchmove', clearNestIfOff, { passive: true });
    // NB: don't clearNest here — onEnd runs this then reads nestTarget for the drop.
  }

  // handleBoardDrop fires when a card is dropped on a board in the sidebar: the
  // card leaves this board and takes the target board's entry lane (promote /
  // demote). Returns true when it handled the drop, so the lane/column onEnd can
  // skip its own move logic. The server resolves the entry lane from the board id.
  function handleBoardDrop(evt) {
    const drop = evt.to.closest('[data-board-drop]');
    if (!drop) return false;
    const id = evt.item.dataset.itemId;
    evt.item.remove(); // it's no longer on this board's view
    api('/items/' + id + '/board', { board_id: drop.dataset.boardId })
      .catch((e) => { if (boardErr) boardErr.textContent = msg(e); location.reload(); });
    return true;
  }

  // Reorder vs nest, decided by where in the hovered card you are. Reorder can
  // only happen *over a card*: SortableJS treats the gaps between cards as inert
  // container space, never a drop target — so the edges of each card are the
  // reorder zones (top fifth inserts before it, bottom fifth inserts after) and
  // the middle is the nest zone. We override SortableJS's eager edge-swap so the
  // card holds still and lights up "↳ subtask" while you're over its middle.
  let nestTarget = null;
  function setNest(card) {
    if (nestTarget === card) return;
    if (nestTarget) nestTarget.classList.remove('nest-target');
    nestTarget = card;
    card.classList.add('nest-target');
  }
  function clearNest() {
    if (nestTarget) nestTarget.classList.remove('nest-target');
    nestTarget = null;
  }
  function pointY(oe) {
    if (!oe) return null;
    if (oe.clientY != null) return oe.clientY;
    if (oe.touches && oe.touches[0]) return oe.touches[0].clientY;
    return null;
  }
  function nestOnMove(evt, oe) {
    const rel = evt.related;
    const y = pointY(oe);
    if (y == null || !rel || !rel.classList || !rel.classList.contains('item')
        || rel.dataset.itemId === evt.dragged.dataset.itemId) { clearNest(); return true; }
    const r = evt.relatedRect || rel.getBoundingClientRect();
    if (y < r.top + r.height * 0.2) { clearNest(); return -1; } // top fifth (or above) → reorder before
    if (y > r.top + r.height * 0.8) { clearNest(); return 1; }  // bottom fifth (or below) → reorder after
    setNest(rel);
    return false; // middle → nest; hold the card still
  }
  // Belt-and-braces for onMove's gaps: SortableJS only fires onMove while
  // evaluating a move over a sibling, so drifting the cursor into dead space
  // (the frozen ghost, empty lane area, off-board) wouldn't otherwise clear a
  // nest highlight. Drop it the moment the cursor leaves the target's box.
  function clearNestIfOff(e) {
    if (!nestTarget) return;
    const t = e.touches && e.touches[0] ? e.touches[0] : e;
    if (t.clientX == null) return;
    const r = nestTarget.getBoundingClientRect();
    if (t.clientX < r.left || t.clientX > r.right || t.clientY < r.top || t.clientY > r.bottom) clearNest();
  }
  // Consume a pending nest target on drop: the dragged card becomes its subtask
  // and leaves the board (subtasks aren't shown as top-level cards). Returns
  // true when it handled the drop. The server rejects cycles/self-parenting.
  function handleNestDrop(evt) {
    const parent = nestTarget;
    clearNest();
    if (!parent) return false;
    const parentId = parent.dataset.itemId;
    const childId = evt.item.dataset.itemId;
    if (!parentId || parentId === childId) return false; // fall through to a normal move
    evt.item.remove();
    bumpSubCount(parent); // reflect the new child on the parent's badge
    api('/items/' + childId + '/parent', { parent_id: parentId })
      .catch((e) => { if (boardErr) boardErr.textContent = msg(e); location.reload(); });
    return true;
  }

  // Optimistically grow a parent card's "done/total" subtask badge by one
  // (creating it if this is its first subtask). The exact figure reconciles on
  // the next load — this just avoids the card looking childless mid-drag.
  function bumpSubCount(card) {
    const sub = card.querySelector('.item-sub');
    if (sub) {
      const [done, total] = sub.textContent.split('/').map((n) => parseInt(n, 10) || 0);
      sub.textContent = done + '/' + (total + 1);
      return;
    }
    const meta = card.querySelector('.item-meta');
    if (!meta) return;
    const badge = document.createElement('span');
    badge.className = 'item-sub';
    badge.title = 'Subtasks done';
    badge.textContent = '0/1';
    meta.insertBefore(badge, meta.querySelector('.meta-spacer'));
  }

  // --- wire the server-rendered board ---

  board.querySelectorAll('.item').forEach(wireItem);
  wireFilters();
  wirePopovers();
  wireDisplay();
  wireViewScroll();

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
        const st = await api('/statuses', { name, board_id: boardId });
        const lane = document.getElementById('lane-tmpl').content.firstElementChild.cloneNode(true);
        lane.dataset.statusId = st.id;
        lane.dataset.color = st.color;
        lane.querySelector('.lane-name').value = st.name;
        const newDot = lane.querySelector('.lane-dot');
        newDot.style.setProperty('--lane-color', st.color);
        if (wrap.dataset.lanesDashed === '1') newDot.classList.add('dashed'); // Backlog lanes render dashed
        board.insertBefore(lane, document.querySelector('.lane-add'));
        wireLane(lane);
        input.value = '';
        input.focus();
      } catch (err) { if (boardErr) boardErr.textContent = msg(err); }
    });
  }

  // Sidebar boards are drop targets: dragging a card onto another board promotes
  // /demotes it there (handled in handleBoardDrop). Skip the current board — a
  // card can't leave for the board it's already on. Works in both view modes.
  document.querySelectorAll('.sidebar [data-board-drop]').forEach((target) => {
    if (target.dataset.boardId === wrap.dataset.boardId) return;
    new Sortable(target, {
      group: { name: 'items', pull: false, put: true },
      sort: false,
      draggable: '.item',
    });
  });

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

  // setCardProject creates/updates/removes a card's project chip in the meta row.
  // Shared by the modal change handler (own edit) and the live SSE path (remote).
  function setCardProject(card, name, color) {
    let chip = card.querySelector('.item-project');
    if (!name) { if (chip) chip.remove(); return; }
    const meta = card.querySelector('.item-meta');
    if (!meta) return;
    if (!chip) {
      chip = document.createElement('span');
      chip.className = 'item-project';
      chip.innerHTML = '<span class="item-project-dot"></span><span class="item-project-name"></span>';
      const spacer = meta.querySelector('.meta-spacer');
      if (spacer) meta.insertBefore(chip, spacer); else meta.appendChild(chip);
    }
    chip.title = 'Project: ' + name;
    chip.style.setProperty('--lane-color', color || '');
    chip.querySelector('.item-project-name').textContent = name;
  }

  function applyUpsert(msg) {
    let card = cardOf(msg.id);
    // A non-root item (e.g. just reparented under another) has no board card; if
    // it had one, drop it.
    if (msg.parent_id) { if (card) card.remove(); return; }

    const lane = board.querySelector('.lane[data-status-id="' + CSS.escape(msg.status_id || '') + '"]');
    const itemsEl = lane ? lane.querySelector('.lane-items') : null;

    // In status mode, a card whose status has no lane here has moved to another
    // board — drop it. Milestone mode has no status lanes, so leave it be.
    if (card && !itemsEl && board.querySelector('.lane')) { card.remove(); return; }

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
    card.dataset.projectId = msg.project ? msg.project.id : '';
    setCardProject(card, msg.project ? msg.project.name : '', msg.project ? msg.project.color : '');
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

  // commentCard builds one feed card from a server payload ({id, author,
  // body, body_html, rel, abs, avatar_style, avatar_text, mine, edited}).
  // body_html is server-rendered and sanitized (bluemonday), so injecting it via
  // innerHTML matches a reload. The owner kebab mirrors comment-card in
  // item_modal.html — keep the two in sync.
  function commentCard(c) {
    const art = document.createElement('article');
    art.className = 'cmt';
    if (c.id) art.dataset.commentId = c.id;
    if (c.mine) art.dataset.mine = '';
    art.dataset.src = c.body || '';
    const head = document.createElement('header');
    head.className = 'cmt-head';
    const av = document.createElement('span');
    av.className = 'avatar cmt-avatar';
    if (c.avatar_style) av.setAttribute('style', c.avatar_style);
    av.textContent = c.avatar_text || '';
    const who = document.createElement('span');
    who.className = 'cmt-author';
    who.textContent = c.author || '';
    const when = document.createElement('time');
    when.className = 'cmt-when';
    if (c.abs) when.title = c.abs;
    when.textContent = c.rel || '';
    const edited = document.createElement('span');
    edited.className = 'cmt-edited';
    edited.textContent = '(edited)';
    edited.hidden = !c.edited;
    head.append(av, who, when, edited);
    if (c.mine) head.appendChild(commentMenu());
    const body = document.createElement('div');
    body.className = 'cmt-body md';
    body.dataset.cmtBody = '';
    body.innerHTML = c.body_html || '';
    art.append(head, body);
    return art;
  }

  function commentMenu() {
    const menu = document.createElement('div');
    menu.className = 'cmt-menu';
    menu.dataset.cmtMenu = '';
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'cmt-menu-btn';
    btn.dataset.cmtMenuBtn = '';
    btn.setAttribute('aria-label', 'Comment actions');
    btn.innerHTML = '<svg class="ico" viewBox="0 0 16 16"><circle cx="3.2" cy="8" r="1.25"/><circle cx="8" cy="8" r="1.25"/><circle cx="12.8" cy="8" r="1.25"/></svg>';
    const pop = document.createElement('div');
    pop.className = 'popover cmt-menu-pop';
    pop.dataset.cmtMenuPop = '';
    pop.hidden = true;
    const edit = document.createElement('button');
    edit.type = 'button'; edit.className = 'cmt-menu-item'; edit.dataset.cmtEdit = ''; edit.textContent = 'Edit';
    const del = document.createElement('button');
    del.type = 'button'; del.className = 'cmt-menu-item danger'; del.dataset.cmtDelete = ''; del.textContent = 'Delete';
    pop.append(edit, del);
    menu.append(btn, pop);
    return menu;
  }

  function appendComment(feed, c, itemId) {
    if (!feed) return null;
    const card = commentCard(c);
    feed.appendChild(card);
    if (c.mine && itemId) wireCommentCard(card, itemId);
    return card;
  }

  // wireCommentCard attaches the owner kebab: a menu toggle, inline edit (the
  // body swaps for a pre-filled composer), and an arm-then-confirm delete.
  function wireCommentCard(card, itemId) {
    const menu = card.querySelector('[data-cmt-menu]');
    if (!menu) return;
    const btn = menu.querySelector('[data-cmt-menu-btn]');
    const pop = menu.querySelector('[data-cmt-menu-pop]');
    const editBtn = menu.querySelector('[data-cmt-edit]');
    const delBtn = menu.querySelector('[data-cmt-delete]');
    const cid = card.dataset.commentId;

    let armed = false, armTimer = null;
    function resetDelete() { armed = false; clearTimeout(armTimer); delBtn.textContent = 'Delete'; delBtn.classList.remove('armed'); }
    const closeMenu = () => { pop.hidden = true; resetDelete(); };

    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      pop.hidden = !pop.hidden;
      if (pop.hidden) resetDelete();
    });

    editBtn.addEventListener('click', () => {
      closeMenu();
      const bodyEl = card.querySelector('[data-cmt-body]');
      if (!bodyEl || card.querySelector('.composer')) return; // already editing
      const composer = buildComposer({ value: card.dataset.src || '', placeholder: 'Edit comment…', withCancel: true });
      bodyEl.hidden = true;
      bodyEl.after(composer);
      const restore = () => { composer.remove(); bodyEl.hidden = false; };
      const h = wireComposer(composer, {
        resetOnSubmit: false,
        onSubmit: async (body) => {
          const c = await api('/items/' + itemId + '/comment/' + cid + '/edit', { body });
          bodyEl.innerHTML = c.body_html || '';
          card.dataset.src = c.body != null ? c.body : body;
          const ed = card.querySelector('.cmt-edited');
          if (ed) ed.hidden = false;
          restore();
        },
        onCancel: restore,
      });
      h.focus();
    });

    delBtn.addEventListener('click', async () => {
      if (!armed) { // first click arms; a second within 2.5s confirms
        armed = true;
        delBtn.textContent = 'Click to confirm';
        delBtn.classList.add('armed');
        armTimer = setTimeout(resetDelete, 2500);
        return;
      }
      resetDelete();
      pop.hidden = true;
      try {
        await api('/items/' + itemId + '/comment/' + cid + '/delete');
        tombstone(card);
      } catch (_) { /* leave the card as-is on failure */ }
    });
  }

  function tombstone(card) {
    card.className = 'cmt cmt-deleted';
    card.removeAttribute('data-mine');
    delete card.dataset.src;
    card.innerHTML = '<span class="cmt-deleted-text">Comment deleted</span>';
  }

  function applyComment(msg) {
    if (!modalEl || modalEl.dataset.itemId !== msg.item) return;
    appendComment(modalEl.querySelector('[data-feed]'), msg, msg.item);
  }

  function applyCommentEdit(msg) {
    if (!modalEl || modalEl.dataset.itemId !== msg.item) return;
    const card = modalEl.querySelector('.cmt[data-comment-id="' + CSS.escape(msg.id) + '"]');
    if (!card) return;
    const bodyEl = card.querySelector('[data-cmt-body]');
    if (bodyEl) bodyEl.innerHTML = msg.body_html || '';
    card.dataset.src = msg.body || '';
    const ed = card.querySelector('.cmt-edited');
    if (ed) ed.hidden = false;
  }

  function applyCommentDelete(msg) {
    if (!modalEl || modalEl.dataset.itemId !== msg.item) return;
    const card = modalEl.querySelector('.cmt[data-comment-id="' + CSS.escape(msg.id) + '"]');
    if (card) tombstone(card);
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
        case 'comment.edit': applyCommentEdit(msg); break;
        case 'comment.delete': applyCommentDelete(msg); break;
        case 'subtask.add': applySubtaskAdd(msg); break;
      }
    } catch (_) { /* a live update must never break the page */ }
  });

  // A pointer down outside an open comment kebab closes it.
  document.addEventListener('pointerdown', (e) => {
    document.querySelectorAll('[data-cmt-menu-pop]:not([hidden])').forEach((pop) => {
      const menu = pop.closest('[data-cmt-menu]');
      if (menu && !menu.contains(e.target)) pop.hidden = true;
    });
  });
})();
