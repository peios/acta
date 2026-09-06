// agent-session.js — the browser chat client for an Acta agent session. It
// renders the transcript (events already in the page, then live ones over a
// websocket) and sends user input back. No framework, vanilla ES.
//
// What arrives is the common event model (internal/agentsession/model): the
// server projects each backend's own frames into events, so this file knows
// nothing about Claude Code's or Codex's wire formats. Rendering rule (per
// ACT-36): every event carries the verbatim frames it came from, and every
// transcript node has a "raw" toggle (shown on hover) listing them, so nothing
// sent over the wire is hidden. An event names the node it creates (ref) and
// the node it lands on (to); folds carry raw frames to a node and draw nothing.
(() => {
  'use strict';

  const stage = document.querySelector('.chat-stage');
  if (!stage) return;
  const sessionID = stage.dataset.session;
  const backend = stage.dataset.backend || 'claude-code';
  const agentName = backend === 'codex' ? 'Codex' : 'Claude';
  const log = stage.querySelector('[data-chat-log]');
  const form = stage.querySelector('[data-chat-form]');
  const box = stage.querySelector('[data-chat-box]');
  const connEl = stage.querySelector('[data-chat-conn]');
  const stopBtn = stage.querySelector('[data-chat-stop]');
  let lastSeq = parseInt(stage.dataset.lastSeq || '0', 10) || 0;
  const seen = new Set();

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }
  // svg builds a house-style .ico from trusted, static path data.
  function svg(paths, cls) {
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('viewBox', '0 0 16 16');
    s.setAttribute('class', 'ico' + (cls ? ' ' + cls : ''));
    for (const d of paths) {
      const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
      p.setAttribute('d', d);
      s.appendChild(p);
    }
    return s;
  }
  const ICONS = {
    terminal: ['M2.5 3.5h11v9h-11z', 'M5 6.5 7 8.5 5 10.5', 'M8.5 10.5h2.5'],
    file: ['M4 2.5h5.5L12.5 5.5v8H4z', 'M9.5 2.5v3h3', 'M6 8.5h4M6 10.5h4'],
    edit: ['M3 13h10', 'M4 10.5 10.8 3.7a1.2 1.2 0 0 1 1.7 1.7L5.7 12.2 3 13z'],
    search: ['M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z', 'M10.3 10.3 13.5 13.5'],
    web: ['M8 13.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z', 'M2.5 8h11', 'M8 2.5c1.7 1.5 2.5 3.4 2.5 5.5S9.7 12 8 13.5C6.3 12 5.5 10.1 5.5 8S6.3 4 8 2.5z'],
    question: ['M8 13.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z', 'M6.3 6.5a1.7 1.7 0 1 1 2.4 1.6c-.5.2-.7.5-.7 1', 'M8 11.2h.01'],
    agent: ['M3 4.5h10v8H3z', 'M8 2.5v2', 'M6 8.5h.01M10 8.5h.01', 'M6.5 11h3'],
    tool: ['M9.5 2.5a3.5 3.5 0 0 0-3.2 4.9L2.5 11.2a1.4 1.4 0 0 0 2 2l3.8-3.8A3.5 3.5 0 0 0 13 6l-2.2 1-1.4-1.4 1-2.2a3.5 3.5 0 0 0-.9-.9z'],
    spark: ['M8 2.5 9.4 6.6 13.5 8 9.4 9.4 8 13.5 6.6 9.4 2.5 8l4.1-1.4z'],
    back: ['M10 3 5 8l5 5'],
    compact: ['M3 3.5h10', 'M3 12.5h10', 'M8 5.5v5', 'M5.5 8 8 10.5 10.5 8'],
    shield: ['M8 2.5 3.5 4.3v3.4c0 2.6 1.9 4.6 4.5 5.8 2.6-1.2 4.5-3.2 4.5-5.8V4.3z', 'M6 8l1.5 1.5L10.2 6.7'],
    open: ['M6 3.5 10.5 8 6 12.5'],
    gauge: ['M3 11.5a5 5 0 0 1 10 0', 'M8 11.5 10.5 7', 'M8 11.5h.01'],
    bolt: ['M9 2.5 4 9h3.5L7 13.5 12 7H8.5z'],
    slash: ['M10 2.5 6 13.5'],
    goal: ['M8 13.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z', 'M8 10.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z'],
    warn: ['M8 2.8 1.8 13.2h12.4z', 'M8 6.5v3', 'M8 11.3h.01'],
    alert: ['M8 13.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z', 'M8 5.2v3.6', 'M8 11.2h.01'],
    info: ['M8 13.5a5.5 5.5 0 1 0 0-11 5.5 5.5 0 0 0 0 11z', 'M8 7.5v3.8', 'M8 5.3h.01'],
    rewind: ['M3.5 8a4.5 4.5 0 1 0 1.6-3.5', 'M2.5 3v2.6h2.6'],
    acta: ['M3 3.5h10v9H3z', 'M3 6.5h10', 'M6 3.5v9', 'M8 9.5l1.2 1.2L11.5 8.2'],
    hook: ['M8 2.5v5', 'M8 7.5a3.5 3.5 0 1 0 3.5 3.5', 'M6 4.5h4'],
    check: ['M3.5 8.5 6.5 11.5 12.5 5'],
    list: ['M3 4h10M3 8h6M3 12h4', 'm10.5 11 1.5 1.5L15 9.5'],
  };
  function toolIcon(name) {
    const n = String(name || '');
    const k = /^Bash$|Shell|Terminal/i.test(n) ? 'terminal'
      : /^(Edit|Write|MultiEdit|NotebookEdit)$/.test(n) ? 'edit'
      : /^(Read|LS)$/.test(n) ? 'file'
      : /^(Grep|Glob|Search)/.test(n) ? 'search'
      : /^Web/.test(n) ? 'web'
      : n === 'AskUserQuestion' ? 'question'
      : /^(Agent|Task)$/.test(n) ? 'agent' : 'tool';
    return svg(ICONS[k]);
  }
  // A lane follows its end until the reader scrolls away, and again once they
  // scroll back (lane.follow, kept by the log's scroll listener). While it
  // follows, the log is pinned to its end whenever its viewport or contents
  // change size: streamed text, a wrapped activity line, the composer growing
  // under it, a phone keyboard. Deciding by distance at append time alone let
  // the activity line, the last and shortest thing in the log, slip below the
  // fold whenever the stream paused or the viewport shrank.
  function atBottom(l) {
    return l.scrollHeight - l.scrollTop - l.clientHeight <= 16;
  }
  function scroll(l) { l.scrollTop = l.scrollHeight; }
  function follow(lane) {
    if (!lane.follow || lane.log.hidden || (lane === mainLane && tailDetached)) return;
    scroll(lane.log);
  }

  // A "Latest" pill floats over the composer whenever the visible lane is
  // scrolled away from its end; it turns accent when something new landed
  // below the fold. Each lane's log repaints it on scroll.
  const jump = stage.querySelector('[data-chat-jump]');
  let unread = false;
  function paintJump() {
    if (!jump || !visible) return;
    const l = visible.log;
    const away = (visible === mainLane && tailDetached) || (!visible.follow && l.scrollHeight > l.clientHeight + 60);
    if (!away) unread = false;
    jump.hidden = !away;
    jump.classList.toggle('has-new', unread);
    jump.querySelector('span').textContent = unread ? 'New below' : 'Latest';
  }
  function noteUnread(lane) { if (lane === visible) { unread = true; paintJump(); } }
  if (jump) jump.addEventListener('click', () => { if (visible === mainLane && tailDetached) { reopenTail(); return; } if (visible) { visible.follow = true; scroll(visible.log); } unread = false; paintJump(); });

  // dayMark: a dated rule the first time a day shows up in a lane, so a
  // transcript that spans days reads in order. Today at the top is implied.
  function dayMark(lane, at) {
    const t = Date.parse(at || '');
    if (!Number.isFinite(t)) return null;
    const d = new Date(t), now = new Date();
    const key = d.toDateString();
    if (lane.day === key) return null;
    const first = !lane.day;
    lane.day = key;
    if (first && key === now.toDateString()) return null;
    const y = new Date(now); y.setDate(now.getDate() - 1);
    const label = key === now.toDateString() ? 'Today' : key === y.toDateString() ? 'Yesterday'
      : d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric' });
    const m = el('div', 'day-mark');
    m.dataset.day = key;
    m.appendChild(el('span', 'day-mark-text', label));
    m.title = d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });
    return m;
  }

  // --- header gauges ---
  //
  // Three rings: the usage windows (weekly, 5h) come from usage.limits
  // events; context from usage.context (the last prompt size over the model's
  // window, which a turn.end names once known).

  const CIRC = 2 * Math.PI * 18; // r=18 in the 44px viewBox
  let contextWindow = 0;
  let contextUsed = 0;
  // Weekly utilisation now, and where it stood when the current turn began, so
  // a turn's divider can say how much of the week it consumed.
  let weeklyNow = null;
  let weeklyAtTurnStart = null;

  function fmtPct(frac) {
    const p = Math.round(frac * 1000) / 10;
    return (Number.isInteger(p) ? p.toFixed(0) : p.toFixed(1)) + '%';
  }
  function fmtTokens(n) {
    if (n >= 1e6) { const v = n / 1e6; return (Number.isInteger(v) ? v.toFixed(0) : v.toFixed(1)) + 'M'; }
    if (n >= 1e3) return Math.round(n / 1e3) + 'k';
    return String(n);
  }
  function setGauge(name, frac, valueText, labelText, tip) {
    const g = stage.querySelector('[data-gauge="' + name + '"]');
    if (!g) return;
    const f = Math.max(0, Math.min(1, frac || 0));
    g.querySelector('[data-arc]').style.strokeDasharray = (f * CIRC).toFixed(2) + ' ' + CIRC.toFixed(2);
    g.querySelector('[data-value]').textContent = valueText;
    if (labelText != null) { const l = g.querySelector('[data-label]'); if (l) l.textContent = labelText; }
    if (tip != null) g.title = tip;
    g.classList.toggle('warn', f >= 0.7 && f < 0.9);
    g.classList.toggle('hot', f >= 0.9);
  }
  function fmtReset(unixSecs) {
    if (!unixSecs) return '';
    const at = new Date(unixSecs * 1000);
    let secs = Math.max(0, Math.round((at - Date.now()) / 1000));
    const d = Math.floor(secs / 86400); secs -= d * 86400;
    const h = Math.floor(secs / 3600); secs -= h * 3600;
    const m = Math.floor(secs / 60);
    const rel = d ? d + 'd ' + h + 'h' : h ? h + 'h ' + m + 'm' : m + 'm';
    const abs = at.toLocaleString(undefined, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
    return 'resets in ' + rel + ' (' + abs + ')';
  }
  function limitTip(label, win, d) {
    const parts = [label + ' limit: ' + fmtPct(win.utilization) + ' used', fmtReset(win.resets_at)];
    if (d.status && d.status !== 'allowed') parts.push('status: ' + d.status);
    if (d.overage) parts.push('overage ' + d.overage + (d.overage_reason ? ' (' + d.overage_reason.replace(/_/g, ' ') + ')' : ''));
    if (d.plan) parts.push('plan: ' + d.plan);
    return parts.filter(Boolean).join('\n');
  }
  function noteLimits(d) {
    const w = d.windows || {};
    if (w.weekly && w.weekly.utilization != null) {
      weeklyNow = w.weekly.utilization;
      if (weeklyAtTurnStart == null) weeklyAtTurnStart = weeklyNow;
      setGauge('weekly', weeklyNow, fmtPct(weeklyNow), null, limitTip('Weekly', w.weekly, d));
    }
    if (w['5h'] && w['5h'].utilization != null) {
      setGauge('fivehour', w['5h'].utilization, fmtPct(w['5h'].utilization), null, limitTip('5-hour', w['5h'], d));
    }
  }
  function noteContext(d, lane) {
    if (d.window) contextWindow = d.window;
    if (!d.used) return;
    const l = lane || cur;
    l.ctxUsed = d.used;
    if (l === visible) { contextUsed = l.ctxUsed; drawContext(); }
  }
  function drawContext() {
    if (!contextUsed) return;
    const win = contextWindow || 200000;
    setGauge('context', contextUsed / win, fmtPct(contextUsed / win), fmtTokens(contextUsed) + ' / ' + fmtTokens(win),
      'Context: ' + contextUsed.toLocaleString() + ' of ' + win.toLocaleString() + ' tokens in the window');
  }

  // --- lanes ---
  //
  // One lane per agent: "main" is the session's own transcript; every
  // subagent the model starts opens a lane keyed by that call's id, and each
  // event the subagent produces (ev.lane set) renders into that lane with the
  // same renderers. Lanes stay in the DOM; the tab strip swaps which one is
  // visible. A lane owns its activity line, thinking state and context figure.

  const lanes = new Map();   // id -> lane
  let cur = null;            // lane the event being rendered belongs to
  let visible = null;        // lane on screen

  function makeActivity() {
    const node = el('div', 'chat-activity');
    node.hidden = true;
    const dots = el('span', 'act-dots');
    dots.append(el('i'), el('i'), el('i'));
    const text = el('span', 'act-text');
    node.append(dots, text);
    return { node, text };
  }

  function makeLane(id, logEl) {
    const act = makeActivity();
    const lane = { id, log: logEl, activity: act.node, actText: act.text, think: null, actBeforeHook: null,
      ctxUsed: 0, model: '', meta: { type: '', desc: '', status: 'running', last: '', taskId: null, startAt: 0, endAt: 0, summary: '', waiting: false },
      card: null, tab: null, steps: 0, day: '', follow: true, pinRaf: 0 };
    lanes.set(id, lane);
    logEl.addEventListener('scroll', () => { lane.follow = atBottom(logEl); if (lane === visible) paintJump(); }, { passive: true });
    // the scroll event lands a frame after the gesture; a wheel or touch
    // upwards lets go at once so a frame arriving that same tick cannot
    // yank the reader back down
    logEl.addEventListener('wheel', (e) => { if (e.deltaY < 0) lane.follow = false; }, { passive: true });
    logEl.addEventListener('touchmove', () => { lane.follow = false; }, { passive: true });
    // pin on the next frame after the viewport resizes (always) or the
    // contents change (while the lane is busy, so opening a fold in a quiet
    // lane does not jump)
    const pin = () => { if (lane.pinRaf) return; lane.pinRaf = requestAnimationFrame(() => { lane.pinRaf = 0; follow(lane); }); };
    if (typeof ResizeObserver === 'function') new ResizeObserver(pin).observe(logEl);
    if (typeof MutationObserver === 'function') new MutationObserver(() => { if (!lane.activity.hidden) pin(); }).observe(logEl, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['hidden', 'class', 'open'] });
    return lane;
  }
  const mainLane = makeLane('main', log);
  cur = mainLane; visible = mainLane;

  function ts(at) { const t = Date.parse(at || ''); return Number.isFinite(t) ? t : Date.now(); }
  // fmtTime: "14:05" today, "Mon 14:05" otherwise, for the hover stamp.
  function fmtTime(at) {
    const t = Date.parse(at || '');
    if (!Number.isFinite(t)) return '';
    const d = new Date(t), now = new Date();
    const hm = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    if (d.toDateString() === now.toDateString()) return hm;
    return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' }) + ' ' + hm;
  }

  function setActivity(text, lane) {
    const l = lane || cur;
    if (!text) { l.activity.hidden = true; return; }
    l.actText.textContent = text;
    l.activity.hidden = false;
  }
  function placeActivity(lane) { const l = lane || cur; if (l === mainLane && typeof laterEl !== 'undefined') l.log.appendChild(laterEl); l.log.appendChild(l.activity); }

  // When the main turn has ended but subagents are still running, the main
  // activity line says so rather than going quiet.
  let mainTurnActive = false;
  function refreshMainIdle() {
    if (mainTurnActive) return;
    const running = [...lanes.values()].filter(l => l !== mainLane && l.meta.status === 'running');
    if (!running.length) { setActivity(null, mainLane); return; }
    const l = running[running.length - 1];
    setActivity((running.length > 1 ? running.length + ' subagents running' : 'Subagent running') + ' · ' + (l.meta.type || 'agent') + (l.meta.last ? ' · ' + l.meta.last : ''), mainLane);
  }

  // noteActivity keeps the activity line at the foot of the lane current.
  function noteActivity(ev) {
    const d = ev.d || {};
    switch (ev.t) {
      case 'input':
        // a message sent mid-turn is a steer: the grey bubble already says
        // it is waiting, so leave the running activity in place
        if (!mainTurnActive) setActivity('Waiting for the session');
        mainTurnActive = true;
        return;
      case 'thinking':
        if (!cur.think) cur.think = { start: ts(ev.at), tokens: 0, last: null, frames: 0 };
        cur.think.tokens = d.tokens || cur.think.tokens;
        if (!d.tokens) { setActivity('Thinking'); if (hydrated) liveThought(cur); return; }
        cur.think.last = ev.raw && ev.raw[0] ? (ev.raw[0].payload || null) : null;
        cur.think.lastSeq = ev.raw && ev.raw[0] ? (ev.raw[0].seq || 0) : 0;
        cur.think.frames = (cur.think.frames || 0) + 1;
        setActivity('Thinking · ' + cur.think.tokens.toLocaleString() + ' tokens');
        if (hydrated) liveThought(cur);
        return;
      case 'assistant': {
        const c = d.blocks || [];
        const tool = c.find(b => b.type === 'tool_use');
        if (tool && tool.role === 'question') setActivity('Waiting for your answer');
        else if (tool) {
          const inp = tool.input || {};
          const arg = inp.command || inp.file_path || inp.path || inp.pattern || inp.description;
          setActivity('Running ' + (tool.name || 'tool') + (arg ? ' · ' + String(arg).slice(0, 60) : ''));
        } else if (c.some(b => b.type === 'text')) setActivity('Working');
        return;
      }
      case 'hook.start': cur.actBeforeHook = cur.activity.hidden ? null : cur.actText.textContent; setActivity('Running hook · ' + (d.name || '')); return;
      case 'hook.end': setActivity(cur.actBeforeHook); return;
      case 'approval.request':
        if (d.kind === 'tool' || d.kind === 'question' || d.kind === 'plan') setActivity(d.kind === 'question' ? 'Waiting for your answer' : d.kind === 'plan' ? 'Waiting for plan approval' : 'Waiting for permission · ' + (d.display || d.tool || 'tool'));
        return;
      case 'tool.result': case 'user.message': case 'peer.message': setActivity('Working'); return;
      case 'session.state': if (d.text && /^status: /.test(d.text)) { const s = d.text.slice(8); setActivity(s.charAt(0).toUpperCase() + s.slice(1)); } return;
      case 'turn.idle': case 'session.spawn_error': case 'session.undelivered':
        turnHasEcho = false; cur.think = null; dropLiveThought(cur);
        if (cur === mainLane) { mainTurnActive = false; lastTurnEnd = Date.now(); refreshMainIdle(); if (hydrated) runQueuedCmd(); }
        else setActivity(null);
        return;
      case 'api.retry': setActivity('Retrying the API · ' + (d.attempt || '') + (d.max ? '/' + d.max : '')); return;
      case 'api.error': if (!d.settled) setActivity('API error'); return;
    }
  }

  // liveThought shows the thinking stretch as it happens: a chip at the
  // foot of the lane with the running token estimate and elapsed time,
  // which the real thought chip replaces when the thought lands.
  function liveThought(lane) {
    if (!lane.think) return;
    if (!lane.thinkLive) {
      const wrap = el('div', 'frame frame--thought is-live');
      const line = el('div', 'thought-line');
      line.appendChild(svg(ICONS.spark));
      line.appendChild(el('span', 'thought-text', 'Thinking'));
      wrap.appendChild(line);
      const stick = lane.follow;
      lane.log.appendChild(wrap);
      placeActivity(lane);
      if (stick) scroll(lane.log);
      lane.thinkLive = wrap;
      lane.thinkTimer = setInterval(() => paintLiveThought(lane), 500);
    }
    paintLiveThought(lane);
  }
  function paintLiveThought(lane) {
    if (!lane.thinkLive || !lane.think) return;
    const secs = Math.max(0, (Date.now() - lane.think.start) / 1000);
    const bits = ['Thinking'];
    if (lane.think.tokens) bits.push('~' + lane.think.tokens.toLocaleString() + ' tokens');
    bits.push((secs < 10 ? secs.toFixed(1) : Math.round(secs)) + 's');
    lane.thinkLive.querySelector('.thought-text').textContent = bits.join(' · ');
  }
  function dropLiveThought(lane) {
    if (lane.thinkTimer) { clearInterval(lane.thinkTimer); lane.thinkTimer = 0; }
    if (lane.thinkLive) { lane.thinkLive.remove(); lane.thinkLive = null; }
  }

  // thoughtChip closes out the current thinking stretch as a transcript line.
  function thoughtChip(d, at) {
    dropLiveThought(cur);
    const think = cur.think;
    const secs = d && d.secs != null ? d.secs : think ? Math.max(0, (ts(at) - think.start) / 1000) : 0;
    const tokens = d && d.tokens != null ? d.tokens : think ? think.tokens : 0;
    const bits = ['Thought'];
    if (secs) bits.push('for ' + (secs < 10 ? secs.toFixed(1) : Math.round(secs)) + 's');
    if (tokens) bits.push('~' + tokens.toLocaleString() + ' tokens');
    const wrap = el('div', 'frame frame--thought');
    wrap.dataset.thoughts = '1'; wrap.dataset.secs = String(secs || 0); wrap.dataset.tokens = String(tokens || 0);
    const line = el('div', 'thought-line');
    line.appendChild(svg(ICONS.spark));
    line.appendChild(el('span', 'thought-text', bits.join(' · ')));
    wrap.appendChild(line);
    if (d && d.text) {
      const det = el('details', 'thought-body');
      det.appendChild(el('summary', null, 'show thinking'));
      det.appendChild(el('div', 'msg-think', d.text));
      wrap.appendChild(det);
    }
    if (think && think.last) attachRaw(wrap, { last_thinking_tokens: think.last, thinking_token_frames: think.frames || 0 }, 'raw (thinking tokens)');
    else if (think && think.lastSeq) attachRaw(wrap, null, 'raw (thinking tokens · ' + (think.frames || 0) + ' frames)', think.lastSeq);
    cur.think = null;
    return wrap;
  }

  // mergeThought folds a thought chip into the one just before it in the
  // lane, so a run of reasoning steps (Codex thinks between every tool
  // call) reads as one "Thought ×N" line with all the texts behind it,
  // rather than a column of identical chips. Returns the chip it merged
  // into, or null when the previous frame is not a settled thought.
  function mergeThought(node, lane) {
    let prev = lane.log.lastElementChild;
    while (prev && (prev === lane.activity || (typeof laterEl !== 'undefined' && prev === laterEl) || prev.classList.contains('is-live') || prev.classList.contains('is-streaming'))) prev = prev.previousElementSibling;
    if (!prev || !prev.classList.contains('frame--thought') || !prev.dataset.thoughts) return null;
    const n = Number(prev.dataset.thoughts) + Number(node.dataset.thoughts || 1);
    const secs = Number(prev.dataset.secs || 0) + Number(node.dataset.secs || 0);
    const tokens = Number(prev.dataset.tokens || 0) + Number(node.dataset.tokens || 0);
    prev.dataset.thoughts = String(n); prev.dataset.secs = String(secs); prev.dataset.tokens = String(tokens);
    const bits = ['Thought ×' + n];
    if (secs) bits.push('for ' + (secs < 10 ? secs.toFixed(1) : Math.round(secs)) + 's');
    if (tokens) bits.push('~' + tokens.toLocaleString() + ' tokens');
    prev.querySelector('.thought-text').textContent = bits.join(' · ');
    const texts = [...node.querySelectorAll('.thought-body .msg-think')];
    if (texts.length) {
      let det = prev.querySelector('.thought-body');
      if (!det) { det = el('details', 'thought-body'); det.appendChild(el('summary', null, 'show thinking')); prev.appendChild(det); }
      for (const t of texts) det.appendChild(t);
      det.querySelector('summary').textContent = 'show thinking (' + det.querySelectorAll('.msg-think').length + ')';
    }
    mergeRaw(node, prev);
    return prev;
  }

  // --- permissions: mode control + approval modal ---
  //
  // An approval.request event is something the backend cannot continue
  // without: a tool permission, a question, a plan, an MCP elicitation. The
  // modal answers it with a control message written back through the server.
  // The mode selector sends a set_permission_mode control the same way.

  const modeSelect = stage.querySelector('[data-mode-select]');
  const modal = stage.querySelector('[data-perm-modal]');
  const permQueue = [];               // pending request ids not yet answered
  const permByID = new Map();         // request id -> {req, node, status, review, state, ...}
  let permShowing = null;

  // sendOp sends a browser operation in neutral terms; the server's driver
  // for the session's backend turns it into that backend's own lines.
  function sendOp(op) {
    if (!op.id) op.id = (op.op === 'answer' ? '' : op.op + '-') + Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'control', payload: op }));
  }
  function setSetting(key, value) { sendOp({ op: 'setting', id: key.replace(/_/g, '') + '-' + Date.now().toString(36), key, value }); }

  if (modeSelect) modeSelect.addEventListener('change', () => setSetting('permission_mode', modeSelect.value));
  // --- model / effort / fast-mode picker ---
  //
  // The catalogue (models, commands, styles, fast-mode availability) arrives
  // as session.catalog events, answered to the queries sent on connect.
  // Switching model sends a set_model control; effort and fast mode are the
  // backend's own commands ("/effort low", "/fast") whose replies come back
  // as effort / fast events.

  const pick = stage.querySelector('[data-model-pick]');
  const pickBtn = stage.querySelector('[data-model-btn]');
  const pickPop = stage.querySelector('[data-model-pop]');
  const pickLabel = stage.querySelector('[data-model-label]');
  let modelCatalog = [];
  let modelsAsked = false;
  let curEffort = stage.dataset.effort || '';  // level chosen this session, else the settings default
  let defaultEffort = '';
  let fastOn = false;
  let fastReason = '';
  const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
  const FAST_REASONS = { sdk_opt_in_required: 'not available in this mode', extra_usage_disabled: 'needs extra usage enabled on the account', model_not_allowed: 'not allowed for this model', disabled_by_env: 'disabled by environment', pending: 'checking availability' };

  function requestModels() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    modelsAsked = true;
    sendOp({ op: 'catalog', id: Date.now().toString(36) });
  }
  function noteCatalog(d) {
    if (Array.isArray(d.models)) modelCatalog = d.models;
    if (Array.isArray(d.commands)) noteCommands(d.commands);
    if (typeof d.fast_mode === 'string') { fastOn = d.fast_mode === 'on'; fastReason = fastOn ? '' : (d.fast_reason || ''); }
    if (typeof d.default_effort === 'string') defaultEffort = d.default_effort;
    if (Array.isArray(d.output_styles) && (!styleCatalog.length || backend !== 'claude-code')) styleCatalog = d.output_styles.map(s => (typeof s === 'string' ? { name: s, description: '' } : s));
    if (Array.isArray(d.models) && !defaultEffort) { const hit = catalogEntry(curModel) || d.models[0]; if (hit && hit.defaultEffort) defaultEffort = hit.defaultEffort; }
    if (typeof d.output_style === 'string' && !curStyle) curStyle = d.output_style;
    if (typeof d.permission_mode === 'string') setMode(d.permission_mode);
    paintModelSelect();
  }
  function catalogEntry(id) {
    if (!id) return null;
    const bare = String(id).replace(/\[.*\]$/, '');
    return modelCatalog.find(m => m.value !== 'default' && String(m.resolvedModel || '').replace(/\[.*\]$/, '') === bare)
      || modelCatalog.find(m => m.value === id) || null;
  }
  function effortNow() { return curEffort || defaultEffort || ''; }

  function paintModelSelect() {
    if (!pickBtn) return;
    const hit = catalogEntry(curModel) || (!curModel ? modelCatalog.find(m => m.value === 'default') : null);
    const name = hit ? (hit.value === 'default' && hit.resolvedModel ? modelName(hit.resolvedModel) : (hit.displayName || hit.value)) : curModel ? modelName(curModel) : 'model…';
    const bits = [];
    if (effortNow()) bits.push(effortNow());
    if (fastOn) bits.push('fast');
    pickLabel.textContent = name;
    if (bits.length) pickLabel.appendChild(el('span', 'pick-extra', ' · ' + bits.join(' · ')));
    const mark = pickBtn.querySelector('.pmark, .ico');
    if (mark) mark.replaceWith(brandIcon(hit && hit.value === 'default' && hit.resolvedModel ? hit.resolvedModel : curModel));
    const list = pickPop.querySelector('[data-pick-models]');
    list.textContent = '';
    if (!modelCatalog.length) list.appendChild(el('div', 'pick-note', curModel ? modelName(curModel) + ' · catalogue loads when the session is running' : 'catalogue loads when the session is running'));
    for (const m of modelCatalog) {
      const row = el('button', 'pick-row' + (hit && hit.value === m.value ? ' is-current' : ''));
      row.type = 'button';
      row.appendChild(el('span', 'pick-row-name', m.displayName || m.value));
      row.appendChild(svg(ICONS.check));
      if (m.description) row.appendChild(el('span', 'pick-row-desc', m.description));
      row.addEventListener('click', () => { setSetting('model', m.value); closePick(); });
      list.appendChild(row);
    }
    const seg = pickPop.querySelector('[data-pick-effort]');
    seg.textContent = '';
    const allowed = hit && Array.isArray(hit.supportedEffortLevels) && hit.supportedEffortLevels.length ? hit.supportedEffortLevels : EFFORTS;
    for (const lvl of (allowed.length > EFFORTS.length || allowed.some(l => !EFFORTS.includes(l)) ? allowed : EFFORTS)) {
      const b = el('button', 'pick-seg-btn' + (effortNow() === lvl ? ' is-current' : ''), lvl);
      b.type = 'button';
      b.disabled = !allowed.includes(lvl);
      b.addEventListener('click', () => { if (effortNow() !== lvl) setSetting('effort', lvl); closePick(); });
      seg.appendChild(b);
    }
    const fast = pickPop.querySelector('[data-pick-fast]');
    const note = pickPop.querySelector('[data-pick-fast-note]');
    const supported = !hit || hit.supportsFastMode !== false;
    fast.classList.toggle('is-on', fastOn);
    fast.disabled = !!fastReason || !supported;
    note.textContent = fastReason ? (FAST_REASONS[fastReason] || fastReason.replace(/_/g, ' ')) : !supported ? 'not supported by this model' : fastOn ? 'faster output for this session' : 'faster output, higher usage';
    note.classList.toggle('is-off', !!fastReason || !supported);
    paintStyles(hit);
    if (curStyle && curStyle.toLowerCase() !== 'default') { pickLabel.textContent += ' · ' + curStyle; }
  }
  // --- output style ---
  let styleCatalog = [];
  let curStyle = '';
  function noteStyles(list) { if (Array.isArray(list)) { styleCatalog = list; paintModelSelect(); } }
  function noteStyle(name) { if (typeof name === 'string' && name !== curStyle) { curStyle = name; paintModelSelect(); } }
  function paintStyles(hit) {
    const list = pickPop.querySelector('[data-pick-styles]');
    if (!list) return;
    list.textContent = '';
    const styles = styleCatalog.length ? styleCatalog : [{ name: 'default', description: '' }];
    const c = (curStyle || 'default').toLowerCase();
    for (const s of styles) {
      const row = el('button', 'pick-row' + (String(s.name).toLowerCase() === c ? ' is-current' : ''));
      row.type = 'button';
      row.appendChild(el('span', 'pick-row-name', s.name + (s.source && s.source !== 'built-in' ? ' · ' + s.source : '')));
      row.appendChild(svg(ICONS.check));
      if (s.description) row.appendChild(el('span', 'pick-row-desc', s.description));
      row.addEventListener('click', () => {
        if (String(s.name).toLowerCase() !== c) {
          setSetting('output_style', s.name);
          curStyle = s.name;
          paintModelSelect();
        }
        closePick();
      });
      list.appendChild(row);
    }
  }
  function openPick() { pickPop.hidden = false; pickBtn.setAttribute('aria-expanded', 'true'); }
  function closePick() { pickPop.hidden = true; pickBtn.setAttribute('aria-expanded', 'false'); }
  if (pickBtn) {
    pickBtn.addEventListener('click', () => { if (pickPop.hidden) { paintModelSelect(); openPick(); } else closePick(); });
    pickPop.querySelector('[data-pick-fast]').addEventListener('click', () => { setSetting('fast', fastOn ? 'off' : 'on'); closePick(); });
    document.addEventListener('click', (e) => { if (!pickPop.hidden && !pick.contains(e.target)) closePick(); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !pickPop.hidden) closePick(); });
  }

  // settingMarker draws a small "X set to Y" line for a setting event.
  function settingMarker(d) {
    const note = el('div', 'frame-note');
    let label, value = d.value;
    if (d.key === 'permission_mode') { note.appendChild(svg(ICONS.shield)); label = 'permissions set to '; }
    else if (d.key === 'personality') { note.appendChild(svg(ICONS.spark)); label = 'personality set to '; }
    else if (d.key === 'model') { note.appendChild(svg(ICONS.spark)); label = 'model set to '; const hit = catalogEntry(d.value) || modelCatalog.find(m => m.value === d.value); value = hit ? (hit.displayName || hit.value) : (d.value === 'default' ? 'default' : modelName(d.value)); }
    else if (d.key === 'output_style') { note.appendChild(svg(ICONS.spark)); label = 'output style set to '; }
    else { note.appendChild(svg(ICONS.spark)); label = (d.key || 'setting').replace(/_/g, ' ') + ' set to '; }
    note.appendChild(el('span', null, label));
    note.appendChild(el('code', 'mode-code', String(value)));
    if (d.error) { note.appendChild(el('span', 'local-text', ' · ' + d.error)); }
    const node = bubble('status', 'state frame--mode', note, null);
    if (d.error) node.classList.add('is-error');
    return node;
  }

  // Until the first init reports the live mode, the select shows the mode the
  // session was created with.
  if (stage.dataset.mode) setTimeout(() => setMode(stage.dataset.mode), 0);
  function setMode(mode) {
    if (!modeSelect || !mode) return;
    if (![...modeSelect.options].some(o => o.value === mode)) {
      const o = document.createElement('option'); o.value = mode; o.textContent = mode; modeSelect.appendChild(o);
    }
    modeSelect.value = mode;
  }

  function permStatusNode(status) {
    return el('span', 'perm-status is-' + status, status);
  }
  function permSummary(d) {
    const inp = d.input || {};
    if (d.kind === 'question' && Array.isArray(d.questions)) return d.questions.map(q => q.question).join('  ·  ');
    return inp.command || inp.file_path || inp.path || inp.pattern || inp.url || d.description || '';
  }

  // renderApproval draws the transcript row for a request (or attaches it to
  // the call it belongs to); the modal is opened separately while pending.
  function renderApproval(ev) {
    const d = ev.d || {};
    if (d.auto) return renderAutoReview(ev);
    if (d.kind === 'plan') return renderPlanPerm(ev);
    if (d.kind === 'elicitation') return renderElicitation(ev);
    if (d.kind === 'other') return bubble('control request', 'control', el('div', 'frame-note', (d.subtype || 'request').replace(/_/g, ' ')), null);
    const isAsk = d.kind === 'question';
    // A prompt for a call that's on screen attaches to that call's pill
    // (status chip + Review) instead of repeating the call in its own row.
    const pill = ev.to ? nodes.get(ev.to) : null;
    const pillEl = pill && pill.isConnected ? (pill.classList.contains('frame') ? pill.querySelector('.msg-tool, .msg-ask, .acta-card, .peer-box, .msg-agent') : pill) : null;
    if (pillEl && pillEl.isConnected && !pillEl.querySelector('.tool-perm')) {
      const frame = pillEl.closest('.frame');
      pillEl.classList.add('has-perm', 'is-pending');
      const boxEl = el('span', 'tool-perm');
      const status = permStatusNode('pending');
      boxEl.appendChild(status);
      const review = el('button', 'perm-review', isAsk ? 'Answer' : 'Review');
      review.type = 'button';
      review.addEventListener('click', () => openPerm(d.id));
      boxEl.appendChild(review);
      (isAsk ? pillEl.querySelector('.ask-head') || pillEl : pillEl).appendChild(boxEl);
      const laneEl = pillEl.closest('.chat-log');
      const permLane = laneEl && laneEl.dataset.lane ? lanes.get(laneEl.dataset.lane) : null;
      if (permLane) { permLane.meta.waiting = true; laneHeaderRefresh(permLane); permLane.pendingPerms = (permLane.pendingPerms || 0) + 1; }
      attachRaws(frame, ev, 'permission');
      permByID.set(d.id, { req: d, node: frame, pill: pillEl, status, review, state: 'pending' });
      if (!cold) permQueue.push(d.id);
      return null;
    }
    const wrap = el('div', 'frame frame--perm');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', isAsk ? 'question' : 'permission'));
    wrap.appendChild(head);
    const line = el('div', 'perm-line');
    line.appendChild(toolIcon(d.tool));
    line.appendChild(el('span', 'tool-name', isAsk ? agentName + ' asks' : (d.display || d.tool || 'tool')));
    const sum = permSummary(d);
    if (sum) { const a = el('code', 'tool-arg', String(sum).slice(0, 160)); a.title = String(sum); line.appendChild(a); }
    const status = permStatusNode('pending');
    line.appendChild(status);
    const review = el('button', 'perm-review', isAsk ? 'Answer' : 'Review');
    review.type = 'button';
    review.addEventListener('click', () => openPerm(d.id));
    line.appendChild(review);
    wrap.appendChild(line);
    permByID.set(d.id, { req: d, node: wrap, status, review, state: 'pending' });
    if (!cold) permQueue.push(d.id);
    return wrap;
  }

  // renderAutoReview: a backend's own reviewer deciding an approval (Codex's
  // auto review). Shown like a permission on the call it concerns, with the
  // reviewer's verdict and reasoning once decided; never a modal.
  function renderAutoReview(ev) {
    const d = ev.d || {};
    const pill = ev.to ? pillOf(ev.to) : null;
    const status = permStatusNode('reviewing');
    if (pill && !pill.querySelector('.tool-perm')) {
      pill.classList.add('has-perm', 'is-pending');
      const boxEl = el('span', 'tool-perm is-auto');
      boxEl.appendChild(status);
      pill.appendChild(boxEl);
      attachRaws(pill.closest('.frame'), ev, 'auto review');
      permByID.set(d.id, { req: d, node: pill.closest('.frame'), pill, status, review: el('span'), state: 'pending', auto: true });
      return null;
    }
    const wrap = el('div', 'frame frame--perm is-auto');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', 'auto review'));
    wrap.appendChild(head);
    const line = el('div', 'perm-line');
    line.appendChild(toolIcon(d.tool));
    line.appendChild(el('span', 'tool-name', d.display || d.tool || 'tool'));
    const sum = permSummary(d);
    if (sum) { const a = el('code', 'tool-arg', String(sum).slice(0, 160)); a.title = String(sum); line.appendChild(a); }
    line.appendChild(status);
    wrap.appendChild(line);
    permByID.set(d.id, { req: d, node: wrap, status, review: el('span'), state: 'pending', auto: true });
    return wrap;
  }

  function resolvePerm(id, outcome) {
    const e = permByID.get(id);
    if (!e || e.state !== 'pending') return; // first resolution wins
    e.state = outcome;
    if (e.plan) planResolved(e.plan, outcome);
    const fresh = permStatusNode(outcome);
    e.status.replaceWith(fresh);
    e.status = fresh;
    e.review.remove();
    e.node.classList.add('is-done');
    if (e.pill) {
      const laneEl = e.pill.closest('.chat-log');
      const permLane = laneEl && laneEl.dataset.lane ? lanes.get(laneEl.dataset.lane) : null;
      if (permLane) { permLane.pendingPerms = Math.max(0, (permLane.pendingPerms || 1) - 1); if (!permLane.pendingPerms) { permLane.meta.waiting = false; laneHeaderRefresh(permLane); } }
      e.pill.classList.remove('is-pending');
      e.pill.classList.add(outcome === 'denied' ? 'is-denied' : 'is-' + outcome);
    }
    const i = permQueue.indexOf(id);
    if (i >= 0) permQueue.splice(i, 1);
    if (permShowing === id) { permShowing = null; modal.hidden = true; }
    showNextPerm();
  }

  // A turn ending, or the process restarting, means any prompt still pending
  // was resolved elsewhere (or is gone): mark it stale rather than re-asking.
  function stalePendingPerms() {
    for (const id of [...permQueue]) resolvePerm(id, 'stale');
  }

  function showNextPerm() {
    if (permShowing || !permQueue.length || !hydrated) return;
    openPerm(permQueue[0]);
  }

  function openPerm(id) {
    const e = permByID.get(id);
    if (!e || e.state !== 'pending') return;
    permShowing = id;
    if (e.plan) { openPlan(e.plan, true); return; }
    const req = e.req;
    modal.classList.toggle('is-elicit', !!e.elicit);
    modal.querySelector('[data-perm-cancel]').hidden = !e.elicit;
    if (e.elicit) {
      modal.querySelector('.perm-title').textContent = (req.server || 'An MCP server') + ' needs some input';
      modal.querySelector('[data-perm-tool]').textContent = '';
      modal.querySelector('[data-perm-desc]').textContent = req.message || '';
      modal.querySelector('[data-perm-count]').textContent = permQueue.length > 1 ? (permQueue.indexOf(id) + 1) + ' of ' + permQueue.length : '';
      modal.querySelector('[data-perm-allow]').textContent = 'Send';
      modal.querySelector('[data-perm-deny]').textContent = 'Decline';
      modal.querySelector('[data-perm-msg]').hidden = true;
      modal.querySelector('[data-perm-suggest]').textContent = '';
      const inputBox = modal.querySelector('[data-perm-input]');
      inputBox.textContent = '';
      const schema = req.schema || {};
      const reqd = new Set(schema.required || []);
      if (req.mode === 'url' && req.url) { const a = el('a', 'elicit-url', req.url); a.href = req.url; a.target = '_blank'; a.rel = 'noopener'; inputBox.appendChild(a); }
      for (const k of Object.keys(schema.properties || {})) inputBox.appendChild(elicitField(k, schema.properties[k] || {}, reqd.has(k)));
      modal.hidden = false;
      const first = inputBox.querySelector('.elicit-ctl'); if (first) first.focus();
      return;
    }
    modal.querySelector('[data-perm-tool]').textContent = req.display || req.tool || 'tool';
    modal.querySelector('[data-perm-desc]').textContent = req.description || '';
    modal.querySelector('[data-perm-count]').textContent = permQueue.length > 1 ? (permQueue.indexOf(id) + 1) + ' of ' + permQueue.length : '';
    const inputBox = modal.querySelector('[data-perm-input]');
    inputBox.textContent = '';
    const inp = req.input || {};
    const isAsk = req.kind === 'question' && Array.isArray(req.questions);
    modal.classList.toggle('is-ask', isAsk);
    modal.querySelector('.perm-title').textContent = isAsk ? agentName + ' has a question' : 'Permission request';
    modal.querySelector('[data-perm-allow]').textContent = isAsk ? 'Answer' : 'Allow';
    modal.querySelector('[data-perm-deny]').textContent = isAsk ? 'Skip' : 'Deny';
    modal.querySelector('[data-perm-msg]').hidden = isAsk;
    if (isAsk) {
      modal.querySelector('[data-perm-tool]').textContent = '';
      modal.querySelector('[data-perm-desc]').textContent = '';
      req.questions.forEach((q, qi) => inputBox.appendChild(questionBlock(q, qi)));
    } else {
      for (const k of Object.keys(inp)) {
        const row = el('div', 'perm-kv');
        row.appendChild(el('span', 'perm-k', k));
        const v = inp[k];
        row.appendChild(el('pre', 'perm-v', typeof v === 'string' ? v : JSON.stringify(v, null, 2)));
        inputBox.appendChild(row);
      }
    }
    const sug = modal.querySelector('[data-perm-suggest]');
    sug.textContent = '';
    if (isAsk) { modal.hidden = false; return; }
    (req.suggestions || []).forEach((sg, i) => {
      const lab = el('label');
      const cb = document.createElement('input'); cb.type = 'checkbox'; cb.dataset.idx = String(i);
      lab.appendChild(cb);
      let text = 'Always ' + (sg.behavior || 'allow');
      if (sg.type === 'addRules' && sg.rules) text += ': ' + sg.rules.map(r => r.toolName + '(' + (r.ruleContent || '*') + ')').join(', ');
      else if (sg.type === 'addDirectories' && sg.directories) text += ' in ' + sg.directories.join(', ');
      else text += ' (' + sg.type + ')';
      const span = el('span'); span.appendChild(el('code', null, text));
      lab.appendChild(span);
      sug.appendChild(lab);
    });
    modal.querySelector('[data-perm-msg]').value = '';
    modal.hidden = false;
  }

  // questionBlock renders one question: radios for single select, checkboxes
  // for multi, each option with its description (and a preview block when one
  // is given), plus an "Other" free-text answer.
  function questionBlock(q, qi) {
    const boxEl = el('fieldset', 'ask-q');
    boxEl.dataset.qi = String(qi);
    const legend = el('legend', 'ask-legend');
    if (q.header) legend.appendChild(el('span', 'ask-header', q.header));
    legend.appendChild(el('span', 'ask-question', q.question || ''));
    boxEl.appendChild(legend);
    const type = q.multiSelect ? 'checkbox' : 'radio';
    (q.options || []).forEach((o, oi) => {
      const lab = el('label', 'ask-opt');
      const ctl = document.createElement('input');
      ctl.type = type; ctl.name = 'q' + qi; ctl.value = o.label || ''; ctl.dataset.oi = String(oi);
      lab.appendChild(ctl);
      const body = el('span', 'ask-opt-body');
      body.appendChild(el('span', 'ask-opt-label', o.label || ''));
      if (o.description) body.appendChild(el('span', 'ask-opt-desc', o.description));
      if (o.preview) body.appendChild(el('pre', 'ask-opt-preview', o.preview));
      lab.appendChild(body);
      boxEl.appendChild(lab);
    });
    const other = el('label', 'ask-opt ask-other');
    const ctl = document.createElement('input');
    ctl.type = type; ctl.name = 'q' + qi; ctl.value = ''; ctl.dataset.other = '1';
    other.appendChild(ctl);
    const txt = document.createElement('input');
    txt.type = 'text'; txt.className = 'ask-other-text'; txt.placeholder = 'Other…';
    txt.addEventListener('input', () => { ctl.checked = txt.value.trim() !== ''; });
    other.appendChild(txt);
    boxEl.appendChild(other);
    return boxEl;
  }

  // collectAnswers reads the question form into the {question: answer} map
  // the backend reads back. Returns null if a question has no answer.
  function collectAnswers(questions) {
    const answers = {};
    for (const boxEl of modal.querySelectorAll('.ask-q')) {
      const q = questions[Number(boxEl.dataset.qi)];
      const picked = [];
      for (const ctl of boxEl.querySelectorAll('input:checked')) {
        if (ctl.dataset.other) {
          const t = boxEl.querySelector('.ask-other-text').value.trim();
          if (t) picked.push(t);
        } else picked.push(ctl.value);
      }
      if (!picked.length) return null;
      answers[q.id || q.question] = picked.join(', ');
    }
    return answers;
  }

  function answerPerm(allow) {
    const id = permShowing;
    const e = permByID.get(id);
    if (!e) return;
    const msg = modal.querySelector('[data-perm-msg]').value.trim();
    if (e.elicit) return answerElicit(allow ? 'accept' : 'decline');
    if (e.req.kind === 'question' && Array.isArray(e.req.questions)) {
      if (allow) {
        const answers = collectAnswers(e.req.questions);
        if (!answers) { modal.querySelector('.perm-title').textContent = 'Answer every question first'; return; }
        sendOp({ op: 'answer', id, kind: e.req.subtype, outcome: 'allow', input: e.req.input || {}, answers });
        showAnswers(e.req.call_id, answers);
        resolvePerm(id, 'answered');
      } else {
        sendOp({ op: 'answer', id, kind: e.req.subtype, outcome: 'deny', message: 'The user skipped the question in Acta' });
        resolvePerm(id, 'skipped');
      }
      return;
    }
    const chosen = [...modal.querySelectorAll('[data-perm-suggest] input:checked')].map(cb => (e.req.suggestions || [])[Number(cb.dataset.idx)]).filter(Boolean);
    const op = allow ? { op: 'answer', id, kind: e.req.subtype, outcome: 'allow', input: e.req.input || {} } : { op: 'answer', id, kind: e.req.subtype, outcome: 'deny', message: msg || 'Denied by the user in Acta' };
    if (allow && chosen.length) op.permissions = chosen;
    sendOp(op);
    resolvePerm(id, allow ? 'allowed' : 'denied');
  }
  if (modal) {
    modal.querySelector('[data-perm-allow]').addEventListener('click', () => answerPerm(true));
    modal.querySelector('[data-perm-deny]').addEventListener('click', () => answerPerm(false));
    modal.querySelector('[data-perm-cancel]').addEventListener('click', () => answerElicit('cancel'));
  }

  // renderAnswer applies an approval.answer event: the outcome on the row,
  // answers on the question card, the plan's feedback.
  function renderAnswer(ev) {
    const d = ev.d || {};
    const e = permByID.get(d.id);
    if (e && e.node && e.node.isConnected) attachRaws(e.node, ev, d.auto ? 'auto review' : 'answer');
    if (e && e.auto) {
      const verdict = d.outcome === 'allowed' ? 'auto-approved' : 'auto-denied';
      e.status.title = [d.message, d.risk ? 'risk: ' + d.risk : '', d.by ? 'decided by ' + d.by : ''].filter(Boolean).join(' · ');
      resolvePerm(d.id, verdict);
      return null;
    }
    if (e && e.plan && d.outcome === 'denied' && d.message) e.plan.feedback = d.message;
    if (e && e.elicit) { showElicitAnswer(d.id, d); }
    if (d.answers && e) showAnswers(e.req.call_id, d.answers);
    resolvePerm(d.id, d.outcome || 'answered');
    if (!(e && e.node && e.node.isConnected)) foldIntoLast(ev, 'answer');
    return null;
  }

  let hydrated = false;
  let hydrating = false;
  // cold: rendering a chunk of older (or already-seen) events into the
  // log without letting them touch the live state — the current model and
  // mode, gauges, pills, lanes' status, the rewind order. renderChunk sets
  // it; the renderers that mutate state check it.
  let cold = false;
  let coldEchoed = [];   // echoed entries a cold chunk produced, merged in afterwards
  let coldPending = [];  // inputs awaiting their echo within the chunk
  let coldSwap = null;   // registers a lane made during a cold render
  // whether a backend process is alive for this session: the context panel
  // only asks for reports of a live process, so a click never resumes a dead one
  let procAlive = stage.dataset.running === '1';
  const liveDot = stage.querySelector('[data-session-dot]');
  if (liveDot && window.MutationObserver) {
    new MutationObserver(() => { procAlive = liveDot.classList.contains('is-running'); if (typeof paintGaugePop === 'function') paintGaugePop(); })
      .observe(liveDot, { attributes: true, attributeFilter: ['class'] });
  }

  // --- rendering ---

  // nodes: every transcript node an event named (its ref), so later events
  // can land on it (their to).
  const nodes = new Map();
  // curAt is the timestamp of the event being rendered, for the hover stamp.
  let curAt = '';

  // attachRaw adds a verbatim payload to a node. The node gets one "raw"
  // button in its hover tools (with a count once it holds more than one
  // payload) that toggles a panel listing every payload folded into it.
  // Stored frames travel without their payloads (the event names them by
  // seq); a panel fetches what it shows the first time it opens.
  const frameCache = new Map(); // seq -> payload
  async function fetchFrames(seqs) {
    const need = seqs.filter(s => !frameCache.has(s));
    if (need.length) {
      try {
        const r = await fetch('/account/sessions/' + encodeURIComponent(sessionID) + '/frames?seq=' + need.join(','), { credentials: 'same-origin' });
        const j = await r.json();
        for (const f of (j.frames || [])) frameCache.set(f.seq, f.payload);
      } catch (_) {}
    }
    return seqs.map(s => frameCache.get(s));
  }
  function rawText(payload) {
    return JSON.stringify(payload, (k, v) => (k === 'data' && typeof v === 'string' && v.length > 2000) ? '<base64 · ' + Math.round(v.length * 3 / 4 / 1024) + ' KB elided>' : v, null, 2);
  }
  async function fillRawBox(raw) {
    const pending = [...raw.box.querySelectorAll('.frame-json[data-seq]')].filter(pre => !pre.dataset.loaded);
    if (!pending.length) return;
    const seqs = [...new Set(pending.map(pre => Number(pre.dataset.seq)))];
    const got = await fetchFrames(seqs);
    for (const pre of pending) {
      const payload = got[seqs.indexOf(Number(pre.dataset.seq))];
      if (payload === undefined) { pre.textContent = '(frame ' + pre.dataset.seq + ' could not be loaded)'; continue; }
      pre.textContent = rawText(payload);
      pre.dataset.loaded = '1';
    }
  }
  function attachRaw(wrap, payload, label, seq) {
    let tools = wrap.querySelector(':scope > .frame-tools');
    if (!tools) {
      tools = el('div', 'frame-tools');
      const t = fmtTime(curAt);
      if (t) tools.appendChild(el('span', 'frame-time', t));
      wrap.appendChild(tools);
    }
    let raw = wrap._raw;
    if (!raw) {
      const btn = el('button', 'raw-btn', 'raw');
      btn.type = 'button';
      const boxEl = el('div', 'frame-json-box');
      boxEl.hidden = true;
      btn.addEventListener('click', () => {
        boxEl.hidden = !boxEl.hidden;
        btn.classList.toggle('is-open', !boxEl.hidden);
        wrap.classList.toggle('is-raw-open', !boxEl.hidden);
        if (!boxEl.hidden) fillRawBox(raw);
      });
      tools.appendChild(btn);
      wrap.appendChild(boxEl);
      raw = wrap._raw = { btn, box: boxEl, n: 0 };
    }
    raw.n++;
    const sec = el('div', 'frame-json-sec');
    const name = label ? label.replace(/^raw\s*\(?/, '').replace(/\)$/, '').trim() : '';
    if (name) sec.appendChild(el('div', 'raw-label', name));
    const pre = el('pre', 'frame-json');
    if (payload == null && seq) {
      pre.dataset.seq = String(seq);
      pre.textContent = '…';
      if (!raw.box.hidden) fillRawBox(raw);
    } else {
      pre.textContent = rawText(payload);
      pre.dataset.loaded = '1';
    }
    sec.appendChild(pre);
    raw.box.appendChild(sec);
    if (raw.n > 1) {
      raw.btn.textContent = 'raw · ' + raw.n;
      const first = raw.box.firstChild;
      if (first && !first.querySelector('.raw-label')) first.insertBefore(el('div', 'raw-label', 'frame'), first.firstChild);
    }
    return pre;
  }
  // attachRaws hangs every verbatim frame an event carries off a node.
  function attachRaws(wrap, ev, label) {
    if (!wrap || !ev || !ev.raw) return;
    for (const r of ev.raw) {
      if (r.payload == null && !r.seq) continue;
      attachRaw(wrap, r.payload == null ? null : r.payload, r.label ? 'raw (' + r.label + ')' : (label ? 'raw (' + label + ')' : undefined), r.seq);
    }
  }

  // lastVisibleFrame: the frame a new one would land under in a lane.
  function lastVisibleFrame(logEl) {
    const kids = logEl.children;
    for (let i = kids.length - 1; i >= 0; i--) {
      const k = kids[i];
      if (k.hidden || k.classList.contains('chat-activity')) continue;
      if (k.classList.contains('is-streaming') || k.classList.contains('is-live')) continue;
      if (k.classList.contains('frame-group')) { const inner = [...k.children].reverse().find(f => !f.hidden); if (inner) return inner; continue; }
      if (k.classList.contains('frame')) return k;
    }
    return null;
  }
  // target resolves an event's `to` into a node: the named one, else the
  // last visible frame of the current lane.
  function target(ev) {
    if (ev && ev.to) {
      const n = nodes.get(ev.to);
      if (n && n.isConnected) return n;
    }
    return lastVisibleFrame(cur.log);
  }
  // foldIntoLast keeps an event that has nothing to show reachable verbatim.
  function foldIntoLast(ev, label) {
    const last = target(ev);
    if (last) attachRaws(last, ev, label);
    return null;
  }
  // mergeRaw moves every payload folded into one frame onto another, so a
  // frame can be hidden without losing anything sent over the wire.
  function mergeRaw(from, into) {
    const raw = from._raw;
    if (!raw || from === into) return;
    for (const sec of [...raw.box.children]) {
      const lbl = sec.querySelector('.raw-label');
      const pre = sec.querySelector('.frame-json');
      const name = 'raw (' + (lbl ? lbl.textContent : 'call') + ')';
      if (pre.dataset.seq && !pre.dataset.loaded) { attachRaw(into, null, name, Number(pre.dataset.seq)); continue; }
      let payload = null;
      try { payload = JSON.parse(pre.textContent); } catch (_) { payload = pre.textContent; }
      attachRaw(into, payload, name);
    }
  }
  // hideIfEmpty hides a frame whose body has nothing visible left (its
  // payloads move to `into`), and reports whether it did.
  function hideIfEmpty(frame, into) {
    if (!frame) return false;
    const body = frame.querySelector(':scope > .frame-body');
    const left = body ? [...body.children].filter(c => !c.hidden) : [];
    if (left.length) return false;
    mergeRaw(frame, into);
    frame.hidden = true;
    return true;
  }

  function bubble(kind, cls, bodyNode, payload) {
    const wrap = el('div', 'frame frame--' + cls);
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', kind));
    wrap.appendChild(head);
    if (bodyNode) wrap.appendChild(bodyNode);
    if (payload != null) attachRaw(wrap, payload);
    return wrap;
  }

  // --- a small, safe markdown renderer for the model's prose ---
  //
  // Builds DOM nodes directly (never innerHTML), so model output can't inject
  // markup. Covers paragraphs, headings, fenced code, inline code, bold and
  // italic, links, bullet and numbered lists, quotes, rules and pipe tables.

  function mdInline(s, out) {
    const re = /(`+)([\s\S]*?[^`])\1(?!`)|\*\*([\s\S]+?)\*\*|(^|[^\w*])\*([^*\n]+?)\*(?=[^\w*]|$)|(^|[^\w_])_([^_\n]+?)_(?=[^\w_]|$)|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/g;
    let i = 0, m;
    while ((m = re.exec(s))) {
      if (m.index > i) out.appendChild(document.createTextNode(s.slice(i, m.index)));
      if (m[2] != null) out.appendChild(el('code', null, m[2]));
      else if (m[3] != null) { const b = el('strong'); mdInline(m[3], b); out.appendChild(b); }
      else if (m[5] != null) { out.appendChild(document.createTextNode(m[4])); const e = el('em'); mdInline(m[5], e); out.appendChild(e); }
      else if (m[7] != null) { out.appendChild(document.createTextNode(m[6])); const e = el('em'); mdInline(m[7], e); out.appendChild(e); }
      else if (m[8] != null) { const a = el('a', null, m[8]); a.href = m[9]; a.target = '_blank'; a.rel = 'noopener'; out.appendChild(a); }
      i = m.index + m[0].length;
    }
    if (i < s.length) out.appendChild(document.createTextNode(s.slice(i)));
  }

  function mdRender(text) {
    const root = el('div', 'md msg-md');
    const lines = String(text).replace(/\r\n?/g, '\n').split('\n');
    let i = 0;
    const isTableSep = l => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(l);
    const splitRow = l => l.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(c => c.trim());
    while (i < lines.length) {
      const line = lines[i];
      if (!line.trim()) { i++; continue; }
      let m;
      if ((m = /^\s*(`{3,}|~{3,})\s*(\S*)/.exec(line))) {
        const fence = m[1][0]; const buf = []; i++;
        while (i < lines.length && !new RegExp('^\\s*' + (fence === '`' ? '`{3,}' : '~{3,}') + '\\s*$').test(lines[i])) buf.push(lines[i++]);
        i++;
        const pre = el('pre'); const code = el('code', null, buf.join('\n'));
        if (m[2]) code.dataset.lang = m[2];
        pre.appendChild(code); root.appendChild(pre);
        continue;
      }
      if ((m = /^\s{0,3}(#{1,4})\s+(.+?)\s*#*\s*$/.exec(line))) {
        const h = el('h' + m[1].length); mdInline(m[2], h); root.appendChild(h); i++; continue;
      }
      if (/^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) { root.appendChild(el('hr')); i++; continue; }
      if (/^\s{0,3}>/.test(line)) {
        const buf = [];
        while (i < lines.length && /^\s{0,3}>/.test(lines[i])) buf.push(lines[i++].replace(/^\s{0,3}>\s?/, ''));
        const q = el('blockquote'); q.appendChild(mdRender(buf.join('\n'))); root.appendChild(q); continue;
      }
      if (/^\s*\|/.test(line) && i + 1 < lines.length && isTableSep(lines[i + 1])) {
        const table = el('table'); const thead = el('thead'); const tr = el('tr');
        for (const c of splitRow(line)) { const th = el('th'); mdInline(c, th); tr.appendChild(th); }
        thead.appendChild(tr); table.appendChild(thead);
        const tbody = el('tbody'); i += 2;
        while (i < lines.length && /^\s*\|/.test(lines[i])) {
          const r = el('tr');
          for (const c of splitRow(lines[i])) { const td = el('td'); mdInline(c, td); r.appendChild(td); }
          tbody.appendChild(r); i++;
        }
        table.appendChild(tbody); root.appendChild(table); continue;
      }
      if ((m = /^(\s*)([-*+]|\d+[.)])\s+/.exec(line))) {
        const ordered = /\d/.test(m[2]);
        const list = el(ordered ? 'ol' : 'ul');
        const indent = m[1].length;
        const itemRe = new RegExp('^\\s{' + indent + '}(' + (ordered ? '\\d+[.)]' : '[-*+]') + ')\\s+(.*)$');
        while (i < lines.length) {
          const im = itemRe.exec(lines[i]);
          if (!im) break;
          const buf = [im[2]]; i++;
          while (i < lines.length && lines[i].trim() && /^\s+/.test(lines[i]) && lines[i].search(/\S/) > indent) buf.push(lines[i++]);
          const li = el('li');
          const text = buf.join('\n');
          const nested = /^(\s+)([-*+]|\d+[.)])\s+/m.exec(text);
          if (buf.length > 1 && nested) {
            const cut = text.indexOf(nested[0]);
            mdInline(text.slice(0, cut).trim(), li);
            li.appendChild(mdRender(text.slice(cut).replace(new RegExp('^' + nested[1], 'gm'), '')));
          } else {
            const tm = /^\[( |x|X)\]\s+/.exec(text);
            if (tm) { const cb = document.createElement('input'); cb.type = 'checkbox'; cb.disabled = true; cb.checked = tm[1] !== ' '; li.appendChild(cb); mdInline(text.slice(tm[0].length), li); }
            else mdInline(buf.join(' ').trim(), li);
          }
          list.appendChild(li);
        }
        root.appendChild(list); continue;
      }
      const buf = [];
      while (i < lines.length && lines[i].trim() && !/^\s*(`{3,}|~{3,})/.test(lines[i]) && !/^\s{0,3}#{1,4}\s/.test(lines[i]) && !/^\s{0,3}>/.test(lines[i]) && !/^\s*([-*+]|\d+[.)])\s+/.test(lines[i]) && !(/^\s*\|/.test(lines[i]) && i + 1 < lines.length && isTableSep(lines[i + 1]))) buf.push(lines[i++]);
      if (!buf.length) { buf.push(line); i++; }
      const p = el('p'); mdInline(buf.join('\n'), p); root.appendChild(p);
    }
    return root;
  }

  // modelName turns a model id into its display name: "claude-fable-5-1" →
  // "Claude Fable 5.1"; "gpt-5.4-mini" → "GPT-5.4 Mini".
  let curModel = '';
  // providerOf: whose model this is, from the id (or the session's backend
  // when the id says nothing), for the mark shown beside its name.
  const BRANDS = {
    anthropic: 'M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5528h3.7442L10.5363 3.5409Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z',
    openai: 'M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z',
  };
  function providerOf(id) {
    const s = String(id || '');
    if (/^(gpt|o\d|codex|chatgpt)/i.test(s)) return 'openai';
    if (/claude|^</i.test(s)) return 'anthropic';
    return stage.dataset.backend === 'codex' ? 'openai' : 'anthropic';
  }
  function brandIcon(id) {
    const who = providerOf(id);
    const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    s.setAttribute('viewBox', '0 0 24 24');
    s.setAttribute('class', 'pmark pmark--' + who);
    s.setAttribute('aria-hidden', 'true');
    const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    p.setAttribute('d', BRANDS[who]);
    s.appendChild(p);
    return s;
  }

  function modelName(id) {
    if (!id) return agentName;
    if (/^</.test(id)) return 'Claude Code';
    if (/^(gpt|o\d)/i.test(id)) return String(id).replace(/^gpt/i, 'GPT').replace(/-(mini|nano|codex|spark|pro)\b/gi, (m, w) => ' ' + w.charAt(0).toUpperCase() + w.slice(1));
    return String(id).replace(/\[[^\]]*\]$/, '')
      .replace(/-\d{8}$/, '').replace(/(\d)-(\d)/g, '$1.$2').replace(/-/g, ' ')
      .replace(/\b\w/g, c => c.toUpperCase());
  }

  // --- questions ---
  //
  // askCard renders a question call as a card listing its questions; the
  // approval attaches to it (Answer button) and the answers show inline once
  // given. askCards: call id -> {node, head, items: {question -> answer slot}}
  const askCards = new Map();

  function askCard(b) {
    const inp = b.input || {};
    const node = el('div', 'msg-ask');
    const head = el('div', 'ask-head');
    head.appendChild(svg(ICONS.question));
    head.appendChild(el('span', 'tool-name', agentName + ' asks'));
    node.appendChild(head);
    const items = new Map();
    for (const q of inp.questions || []) {
      const item = el('div', 'ask-item');
      const line = el('div', 'ask-item-q');
      if (q.header) line.appendChild(el('span', 'ask-header', q.header));
      line.appendChild(el('span', null, q.question || ''));
      item.appendChild(line);
      const ans = el('div', 'ask-answer');
      ans.hidden = true;
      ans.appendChild(el('span', 'ask-answer-lbl', 'you'));
      ans.appendChild(el('span', 'ask-answer-text', ''));
      item.appendChild(ans);
      node.appendChild(item);
      items.set(q.question || '', ans);
    }
    if (b.id) askCards.set(b.id, { node, head, items });
    return node;
  }

  // showAnswers fills a question card's answer slots ({question: answer}).
  function showAnswers(callID, answers) {
    const card = askCards.get(callID);
    if (!card || !answers) return;
    for (const q in answers) {
      const slot = card.items.get(q);
      if (!slot) continue;
      slot.querySelector('.ask-answer-text').textContent = String(answers[q]);
      slot.hidden = false;
    }
    card.node.classList.add('is-answered');
  }
  function renderQuestionAnswer(ev) {
    const d = ev.d || {};
    const card = askCards.get(d.call_id);
    if (card && card.node.isConnected) {
      if (!card.node.classList.contains('is-answered')) {
        if (d.answers) showAnswers(d.call_id, d.answers);
        else if (d.error) { card.node.classList.add('is-denied'); card.node.appendChild(el('div', 'ask-note', d.error)); }
      }
      attachRaws(card.node.closest('.frame'), ev, 'answer');
      return null;
    }
    return foldIntoLast(ev, 'answer');
  }

  // --- MCP elicitation ---
  //
  // An MCP server can pause a tool call to ask the user for structured input
  // (an approval.request of kind elicitation with a message, a mode and a
  // JSON schema). The card in the transcript shows the ask (and the answer
  // once given); the modal builds a form from the schema.

  const elicitCards = new Map(); // request id -> {node, head, body, req}

  function elicitCard(d) {
    const node = el('div', 'msg-ask msg-elicit is-pending');
    const head = el('div', 'ask-head');
    head.appendChild(svg(ICONS.tool));
    head.appendChild(el('span', 'tool-name', (d.server || 'an MCP server') + ' asks'));
    node.appendChild(head);
    if (d.message) node.appendChild(el('div', 'ask-question elicit-msg', d.message));
    if (d.mode === 'url' && d.url) { const a = el('a', 'elicit-url', d.url); a.href = d.url; a.target = '_blank'; a.rel = 'noopener'; node.appendChild(a); }
    const body = el('div', 'elicit-answers'); body.hidden = true;
    node.appendChild(body);
    elicitCards.set(d.id, { node, head, body, req: d });
    return node;
  }
  function renderElicitation(ev) {
    const d = ev.d || {};
    const card = elicitCard(d);
    const boxEl = el('span', 'tool-perm');
    const status = permStatusNode('pending');
    boxEl.appendChild(status);
    const review = el('button', 'perm-review', 'Answer');
    review.type = 'button';
    review.addEventListener('click', () => openPerm(d.id));
    boxEl.appendChild(review);
    card.querySelector('.ask-head').appendChild(boxEl);
    const wrap = bubble('input request', 'elicit', card, null);
    permByID.set(d.id, { req: d, node: wrap, status, review, state: 'pending', elicit: true });
    if (!cold) permQueue.push(d.id);
    return wrap;
  }
  // showElicitAnswer paints the outcome into the card: the values sent, or
  // that it was declined / cancelled.
  function showElicitAnswer(id, r) {
    const c = elicitCards.get(id);
    if (!c) return;
    c.node.classList.remove('is-pending');
    const outcome = (r && r.outcome) || 'cancelled';
    if (outcome === 'answered') {
      c.node.classList.add('is-answered');
      c.body.textContent = '';
      const content = (r && r.content) || {};
      const props = (c.req.schema && c.req.schema.properties) || {};
      for (const k of Object.keys(content)) {
        const row = el('div', 'ask-answer');
        row.appendChild(el('span', 'ask-answer-q', (props[k] && props[k].title) || k));
        row.appendChild(el('span', 'ask-answer-text', String(content[k])));
        c.body.appendChild(row);
      }
      c.body.hidden = !c.body.children.length;
    } else {
      c.node.classList.add('is-denied');
      c.node.appendChild(el('div', 'ask-note', outcome === 'declined' ? 'Declined' : 'Cancelled'));
    }
  }
  // elicitField builds one form control from a JSON-schema property.
  function elicitField(key, prop, required) {
    const boxEl = el('label', 'elicit-field');
    boxEl.dataset.key = key;
    const lab = el('span', 'elicit-label', prop.title || key);
    if (required) lab.appendChild(el('span', 'elicit-req', ' *'));
    boxEl.appendChild(lab);
    if (prop.description) boxEl.appendChild(el('span', 'elicit-desc', prop.description));
    let ctl;
    const type = Array.isArray(prop.type) ? prop.type[0] : prop.type;
    if (Array.isArray(prop.enum)) {
      ctl = document.createElement('select');
      if (!required) { const o = document.createElement('option'); o.value = ''; o.textContent = '—'; ctl.appendChild(o); }
      prop.enum.forEach((v, i) => { const o = document.createElement('option'); o.value = String(v); o.textContent = (prop.enumNames && prop.enumNames[i]) || String(v); ctl.appendChild(o); });
      if (prop.default != null) ctl.value = String(prop.default);
    } else if (type === 'boolean') {
      boxEl.classList.add('is-bool');
      ctl = document.createElement('input'); ctl.type = 'checkbox'; ctl.checked = !!prop.default;
      boxEl.insertBefore(ctl, lab);
    } else if (type === 'integer' || type === 'number') {
      ctl = document.createElement('input'); ctl.type = 'number';
      if (type === 'integer') ctl.step = '1';
      if (prop.minimum != null) ctl.min = String(prop.minimum);
      if (prop.maximum != null) ctl.max = String(prop.maximum);
      if (prop.default != null) ctl.value = String(prop.default);
    } else {
      const fmt = prop.format || '';
      if (prop.maxLength > 200) { ctl = document.createElement('textarea'); ctl.rows = 3; }
      else { ctl = document.createElement('input'); ctl.type = fmt === 'email' ? 'email' : fmt === 'uri' ? 'url' : fmt === 'date' ? 'date' : fmt === 'date-time' ? 'datetime-local' : 'text'; }
      if (prop.minLength != null) ctl.minLength = prop.minLength;
      if (prop.maxLength != null) ctl.maxLength = prop.maxLength;
      if (prop.default != null) ctl.value = String(prop.default);
    }
    ctl.classList.add('elicit-ctl');
    ctl.dataset.type = type || 'string';
    ctl.required = !!required && type !== 'boolean';
    if (!boxEl.contains(ctl)) boxEl.appendChild(ctl);
    return boxEl;
  }
  function collectElicit() {
    const out = {};
    let bad = null;
    for (const f of modal.querySelectorAll('.elicit-field')) {
      const ctl = f.querySelector('.elicit-ctl');
      const t = ctl.dataset.type;
      f.classList.remove('is-bad');
      if (t === 'boolean') { out[f.dataset.key] = !!ctl.checked; continue; }
      const v = ctl.value;
      if (v === '' || v == null) { if (ctl.required) { f.classList.add('is-bad'); bad = bad || ctl; } continue; }
      if (t === 'integer') out[f.dataset.key] = parseInt(v, 10);
      else if (t === 'number') out[f.dataset.key] = Number(v);
      else out[f.dataset.key] = v;
    }
    if (bad) { bad.focus(); return null; }
    return out;
  }
  function answerElicit(action) {
    const id = permShowing;
    const e = permByID.get(id);
    if (!e || !e.elicit) return;
    const op = { op: 'answer', id, kind: e.req.subtype, outcome: action };
    if (action === 'accept') {
      const content = collectElicit();
      if (!content) { modal.querySelector('.perm-title').textContent = 'Fill in the required fields first'; return; }
      op.content = content;
    }
    sendOp(op);
    showElicitAnswer(id, { outcome: action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled', content: op.content });
    resolvePerm(id, action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled');
  }

  // --- tasks ---
  //
  // The model's task list arrives as `tasks` events (the whole list each
  // time). One checklist card in the transcript, placed where the first task
  // call happened, repaints in place; a header pill shows progress and jumps
  // to it. When the last task completes a marker says so where it happened.

  let taskCard = null;           // {node, list, count, frame()}
  let taskState = { list: [], done: 0, total: 0 };
  const taskPill = stage.querySelector('[data-task-pill]');

  function taskRow(t) {
    const row = el('div', 'task-row is-' + t.status);
    const glyph = el('span', 'task-glyph');
    if (t.status === 'completed') glyph.appendChild(svg(ICONS.check));
    else if (t.status === 'in_progress') glyph.appendChild(el('span', 'task-spin'));
    row.appendChild(glyph);
    const body = el('div', 'task-body');
    body.appendChild(el('div', 'task-subject', t.subject || ('Task #' + t.id)));
    if (t.status === 'in_progress' && t.active_form) body.appendChild(el('div', 'task-active', t.active_form));
    if (t.description) { const d = el('div', 'task-desc'); d.appendChild(mdRender(t.description)); d.hidden = true; body.appendChild(d); row.classList.add('has-desc'); row.addEventListener('click', () => { d.hidden = !d.hidden; }); }
    row.appendChild(body);
    row.appendChild(el('span', 'task-id', '#' + t.id));
    return row;
  }
  function paintTasks() {
    const { done, total, list } = taskState;
    if (taskPill) {
      taskPill.hidden = !total;
      taskPill.className = 'plan-pill task-pill' + (total && done === total ? ' is-approved' : list.some(t => t.status === 'in_progress') ? ' is-active' : '');
      taskPill.querySelector('[data-task-pill-text]').textContent = 'Tasks · ' + done + '/' + total;
    }
    if (!taskCard) return;
    taskCard.count.textContent = done + ' of ' + total + ' done';
    taskCard.node.classList.toggle('is-done', total > 0 && done === total);
    taskCard.node.querySelector('.task-bar-fill').style.width = (total ? Math.round(done / total * 100) : 0) + '%';
    taskCard.list.textContent = '';
    for (const t of list) taskCard.list.appendChild(taskRow(t));
  }
  function taskCardNode() {
    const node = el('div', 'msg-tasks');
    const head = el('div', 'agent-head');
    head.appendChild(svg(ICONS.list));
    head.appendChild(el('span', 'agent-type', 'tasks'));
    head.appendChild(el('span', 'agent-desc', 'Task list'));
    const count = el('span', 'task-count', '');
    head.appendChild(count);
    node.appendChild(head);
    const bar = el('div', 'task-bar'); bar.appendChild(el('div', 'task-bar-fill')); node.appendChild(bar);
    const list = el('div', 'task-list');
    node.appendChild(list);
    taskCard = { node, list, count, frame: () => (node.isConnected ? node.closest('.frame') : null) };
    return node;
  }
  if (taskPill) taskPill.addEventListener('click', () => { const f = taskCard && taskCard.frame(); if (f) { showLane('main'); f.scrollIntoView({ block: 'center', behavior: 'smooth' }); f.classList.add('is-flash'); setTimeout(() => f.classList.remove('is-flash'), 1200); } });

  // taskBlock: a task tool call in an assistant event places the card the
  // first time; later calls fold into it. Returns the node to place, or null.
  function taskBlock() {
    const fresh = !(taskCard && taskCard.node.isConnected);
    return fresh ? taskCardNode() : null;
  }
  function renderTasks(ev) {
    const d = ev.d || {};
    if (!cold) { taskState = { list: d.list || [], done: d.done || 0, total: d.total || 0 }; paintTasks(); }
    const f = taskCard && taskCard.frame();
    if (f) attachRaws(f, ev); else foldIntoLast(ev, 'tasks');
    if (d.all_done) {
      const note = el('div', 'frame-note');
      note.appendChild(svg(ICONS.check));
      note.appendChild(el('span', null, 'all ' + d.total + (d.total === 1 ? ' task' : ' tasks') + ' done'));
      return bubble('tasks', 'state frame--planmark is-approved', note, null);
    }
    return null;
  }

  // --- plans ---
  //
  // Plan events (plan.update while drafting, plan.submit when the model asks
  // for approval, plan.verdict once decided) fold into one card per plan key,
  // and the text lives in the side panel, where approval happens. On a wide
  // screen the panel opens by itself as soon as a plan is being written;
  // narrower, only the approval request opens it (as a sheet).

  const plans = new Map();     // key -> plan
  let curPlan = null;
  const planPanel = document.querySelector('[data-plan-panel]');
  const planBackdrop = document.querySelector('[data-plan-backdrop]');
  const planPill = stage.querySelector('[data-plan-pill]');
  const planSide = window.matchMedia('(min-width: 1200px)');
  const PLAN_LABEL = { drafting: 'drafting', pending: 'awaiting approval', approved: 'approved', rejected: 'changes requested', stale: 'not answered' };

  function planFor(key) {
    let plan = plans.get(key);
    if (!plan) { plan = { key, text: '', state: 'drafting', revisions: 0, card: null, feedback: '', verdict: '' }; plans.set(key, plan); }
    return plan;
  }
  function planTitle(plan) {
    const m = /^\s{0,3}#{1,3}\s+(.+?)\s*#*\s*$/m.exec(plan.text || '');
    const t = (m ? m[1] : (plan.text || '').split('\n').find(l => l.trim()) || '').replace(/[*_`]/g, '').trim();
    return t.length > 90 ? t.slice(0, 88) + '…' : t;
  }
  function planStats(plan) {
    const lines = (plan.text || '').split('\n');
    const steps = lines.filter(l => /^\s*(\d+[.)]|[-*+])\s+\S/.test(l)).length;
    const bits = [];
    if (steps) bits.push(steps + (steps === 1 ? ' step' : ' steps'));
    else if (plan.text) bits.push(plan.text.split(/\s+/).filter(Boolean).length + ' words');
    if (plan.revisions > 1) bits.push('revised ×' + (plan.revisions - 1));
    return bits.join(' · ');
  }

  function planCard(plan) {
    const node = el('div', 'msg-plan');
    node.tabIndex = 0; node.setAttribute('role', 'button'); node.title = 'Open the plan';
    const head = el('div', 'agent-head');
    head.appendChild(svg(ICONS.list));
    head.appendChild(el('span', 'agent-type', 'plan'));
    head.appendChild(el('span', 'agent-desc', ''));
    head.appendChild(el('span', 'agent-status', ''));
    const review = el('button', 'perm-review plan-review', 'Review');
    review.type = 'button'; review.hidden = true;
    review.addEventListener('click', (e) => { e.stopPropagation(); openPlan(plan, true); });
    head.appendChild(review);
    head.appendChild(svg(ICONS.open, 'agent-open'));
    node.appendChild(head);
    node.appendChild(el('div', 'plan-sub'));
    const fb = el('div', 'plan-feedback-note'); fb.hidden = true;
    node.appendChild(fb);
    node.addEventListener('click', (e) => { if (e.target.closest('button, a')) return; openPlan(plan, true); });
    node.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openPlan(plan, true); } });
    plan.card = node; plan.review = review; plan.fbNode = fb;
    paintPlanCard(plan);
    return node;
  }
  function paintPlanCard(plan) {
    const c = plan.card;
    if (!c) return;
    c.className = 'msg-plan is-' + plan.state;
    c.querySelector('.agent-desc').textContent = planTitle(plan) || 'Plan';
    const st = c.querySelector('.agent-status');
    st.className = 'agent-status is-' + plan.state;
    st.textContent = PLAN_LABEL[plan.state] || plan.state;
    c.querySelector('.plan-sub').textContent = planStats(plan);
    plan.review.hidden = plan.state !== 'pending';
    plan.fbNode.hidden = !(plan.state === 'rejected' && plan.feedback);
    plan.fbNode.textContent = plan.feedback || '';
  }
  function planFrame(plan) { return plan.card && plan.card.isConnected ? plan.card.closest('.frame') : null; }

  function paintPlanPill() {
    if (!planPill) return;
    const plan = curPlan;
    planPill.hidden = !plan;
    if (!plan) return;
    planPill.className = 'plan-pill is-' + plan.state + (planPanel && !planPanel.hidden ? ' is-open' : '');
    planPill.querySelector('[data-plan-pill-text]').textContent = 'Plan · ' + (PLAN_LABEL[plan.state] || plan.state);
  }
  function paintPlanPanel() {
    if (!planPanel || planPanel.hidden || !curPlan) return;
    const plan = curPlan;
    const st = planPanel.querySelector('[data-plan-status]');
    st.className = 'plan-status is-' + plan.state;
    st.textContent = PLAN_LABEL[plan.state] || plan.state;
    const body = planPanel.querySelector('[data-plan-body]');
    const keep = body.scrollTop;
    body.textContent = '';
    if (plan.verdict) body.appendChild(el('div', 'plan-verdict is-' + plan.state, plan.verdict));
    body.appendChild(plan.text ? mdRender(plan.text) : el('div', 'plan-empty', 'The plan is being written…'));
    body.scrollTop = keep;
    planPanel.querySelector('[data-plan-foot]').hidden = plan.state !== 'pending';
    paintPlanPill();
  }
  function openPlan(plan, force) {
    if (!planPanel) return;
    curPlan = plan || curPlan;
    if (!curPlan) return;
    if (!force && !planSide.matches) { paintPlanPill(); return; }
    planPanel.hidden = false;
    if (planBackdrop) planBackdrop.hidden = false;
    paintPlanPanel();
  }
  function closePlan() {
    if (!planPanel) return;
    planPanel.hidden = true;
    if (planBackdrop) planBackdrop.hidden = true;
    paintPlanPill();
  }
  if (planPanel) {
    planPanel.querySelector('[data-plan-close]').addEventListener('click', closePlan);
    planBackdrop.addEventListener('click', closePlan);
    planPill.addEventListener('click', () => { if (planPanel.hidden) openPlan(curPlan, true); else closePlan(); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !planPanel.hidden && !planSide.matches) closePlan(); });
    planPanel.querySelector('[data-plan-approve]').addEventListener('click', () => answerPlan('approve'));
    planPanel.querySelector('[data-plan-approve-edits]').addEventListener('click', () => answerPlan('approve-edits'));
    planPanel.querySelector('[data-plan-changes]').addEventListener('click', () => answerPlan('changes'));
  }
  function repaintPlan(plan) {
    paintPlanCard(plan);
    if (cold) return;
    if (planPanel && !planPanel.hidden && curPlan === plan) paintPlanPanel(); else paintPlanPill();
  }

  // planBlock places the card for a plan-role tool block on first sighting.
  function planBlock(b) {
    const plan = planFor(b.plan_key || b.id);
    if (plan.card && plan.card.isConnected) return null;
    return planCard(plan);
  }
  function renderPlanUpdate(ev) {
    const d = ev.d || {};
    const plan = planFor(d.key);
    plan.text = d.text || '';
    plan.revisions = d.revisions || plan.revisions;
    if (d.state) plan.state = d.state;
    if (plan.state === 'drafting') plan.verdict = '';
    curPlan = plan;
    repaintPlan(plan);
    if (hydrated && (!planPanel || planPanel.hidden)) openPlan(plan, false);
    const f = planFrame(plan);
    if (f) attachRaws(f, ev, 'plan write'); else foldIntoLast(ev, 'plan write');
    return null;
  }
  function renderPlanSubmit(ev) {
    const d = ev.d || {};
    const plan = planFor(d.key);
    if (typeof d.text === 'string') plan.text = d.text;
    plan.revisions = d.revisions || plan.revisions;
    plan.exitID = d.call_id;
    plan.state = 'pending'; plan.verdict = '';
    curPlan = plan;
    repaintPlan(plan);
    const f = planFrame(plan);
    if (f) attachRaws(f, ev, 'plan submitted'); else foldIntoLast(ev, 'plan submitted');
    return null;
  }
  function renderPlanVerdict(ev) {
    const d = ev.d || {};
    const plan = planFor(d.key);
    if (d.state) plan.state = d.state;
    if (d.feedback) plan.feedback = d.feedback;
    plan.verdict = plan.state === 'approved' ? 'Approved · ' + agentName + ' is implementing it' : plan.state === 'rejected' ? 'Changes requested' + (plan.feedback ? ' · ' + plan.feedback : '') : '';
    repaintPlan(plan);
    const f = planFrame(plan);
    if (f) attachRaws(f, ev, 'verdict'); else foldIntoLast(ev, 'verdict');
    if (d.marker && (plan.state === 'approved' || plan.state === 'rejected')) return planMarker(plan);
    return null;
  }
  // planMarker records the verdict where it landed in the transcript.
  function planMarker(plan) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.list));
    if (plan.state === 'approved') note.appendChild(el('span', null, 'plan approved'));
    else {
      note.appendChild(el('span', null, 'changes requested'));
      if (plan.feedback) { const q = el('span', 'planmark-quote', plan.feedback.length > 220 ? plan.feedback.slice(0, 218) + '…' : plan.feedback); q.title = plan.feedback; note.appendChild(q); }
    }
    return bubble('plan', 'state frame--planmark is-' + plan.state, note, null);
  }

  // renderPlanPerm: the plan approval attaches to the card and opens the
  // panel, whose footer answers it (in place of the permission modal).
  function renderPlanPerm(ev) {
    const d = ev.d || {};
    const plan = planFor(d.plan_key || d.call_id || 'plan');
    if (typeof d.plan_text === 'string' && d.plan_text) plan.text = d.plan_text;
    plan.exitID = plan.exitID || d.call_id;
    plan.state = 'pending'; plan.verdict = ''; plan.reqID = d.id;
    let node = planFrame(plan);
    let out = null;
    if (!node) { out = bubble('plan', 'plan', planCard(plan), null); node = out; }
    else attachRaws(node, ev, 'approval request');
    repaintPlan(plan);
    curPlan = plan;
    permByID.set(d.id, { req: d, node, status: el('span'), review: el('span'), state: 'pending', plan });
    if (!cold) permQueue.push(d.id);
    return out;
  }
  function planResolved(plan, outcome) {
    plan.reqID = null;
    if (outcome === 'allowed') { plan.state = 'approved'; plan.verdict = 'Approved · ' + agentName + ' is implementing it'; }
    else if (outcome === 'denied') { plan.state = 'rejected'; plan.verdict = 'Changes requested' + (plan.feedback ? ' · ' + plan.feedback : ''); }
    else if (plan.state === 'pending') { plan.state = 'stale'; plan.verdict = ''; }
    repaintPlan(plan);
  }
  function answerPlan(how) {
    const plan = curPlan;
    if (!plan || !plan.reqID) return;
    const id = plan.reqID;
    const e = permByID.get(id);
    if (!e || e.state !== 'pending') return;
    const fb = planPanel.querySelector('[data-plan-feedback]');
    const msg = fb.value.trim();
    let op;
    if (how === 'changes') {
      if (!msg) { fb.focus(); fb.placeholder = 'Say what should change first'; return; }
      plan.feedback = msg;
      op = { op: 'answer', id, kind: e.req.subtype, outcome: 'deny', message: msg };
    } else {
      op = { op: 'answer', id, kind: e.req.subtype, outcome: 'allow', input: e.req.input || {} };
      if (how === 'approve-edits') op.permissions = [{ type: 'setMode', mode: 'acceptEdits', destination: 'session' }];
    }
    sendOp(op);
    fb.value = '';
    resolvePerm(id, how === 'changes' ? 'denied' : 'allowed');
  }

  // --- subagents ---
  //
  // An agent call renders as a card in the parent lane (type, description,
  // status, latest activity, final summary) and opens a lane of its own;
  // clicking the card or its tab shows that lane. agent.* events fold into
  // the card.

  const tabs = el('div', 'lane-tabs');
  tabs.hidden = true;
  const mainTab = el('button', 'lane-tab is-active');
  mainTab.type = 'button';
  mainTab.appendChild(el('span', 'lane-tab-label', 'Main'));
  mainTab.addEventListener('click', () => showLane('main'));
  tabs.appendChild(mainTab);
  mainLane.tab = mainTab;
  stage.insertBefore(tabs, log);
  const laneNote = el('div', 'lane-note');
  laneNote.hidden = true;
  stage.insertBefore(laneNote, form);

  function laneByAgent(id, hint) {
    let lane = lanes.get(id);
    if (lane) return lane;
    const logEl = el('div', 'chat-log');
    logEl.hidden = true;
    logEl.dataset.lane = id;
    log.parentNode.insertBefore(logEl, log.nextSibling);
    lane = makeLane(id, logEl);
    if (coldSwap) coldSwap(lane);
    if (hint) { lane.meta.type = hint.type || ''; lane.meta.desc = hint.description || ''; }
    const tab = el('button', 'lane-tab');
    tab.type = 'button';
    tab.appendChild(el('span', 'live-dot is-running'));
    tab.appendChild(el('span', 'lane-tab-type', ''));
    tab.appendChild(el('span', 'lane-tab-label', ''));
    tab.appendChild(el('span', 'lane-tab-sub', ''));
    tab.addEventListener('click', () => showLane(id));
    tabs.appendChild(tab);
    tabs.hidden = false;
    lane.tab = tab;
    laneHeaderRefresh(lane);
    return lane;
  }

  function agentCard(b) {
    const inp = b.input || {};
    const lane = laneByAgent(b.id, { type: inp.subagent_type, description: inp.description });
    if (inp.subagent_type) lane.meta.type = inp.subagent_type;
    if (inp.description) lane.meta.desc = inp.description;
    const node = el('div', 'msg-agent');
    node.tabIndex = 0;
    node.setAttribute('role', 'button');
    node.title = 'Open this subagent\'s transcript';
    const head = el('div', 'agent-head');
    head.appendChild(svg(ICONS.agent));
    head.appendChild(el('span', 'agent-type', ''));
    head.appendChild(el('span', 'agent-desc', ''));
    head.appendChild(el('span', 'agent-status', ''));
    head.appendChild(svg(ICONS.open, 'agent-open'));
    node.appendChild(head);
    node.appendChild(el('div', 'agent-last'));
    node.appendChild(el('div', 'agent-summary'));
    if (inp.prompt) {
      const t = el('button', 'hook-toggle agent-prompt-toggle', 'show prompt');
      t.type = 'button';
      const body = el('div', 'agent-prompt');
      body.hidden = true;
      body.appendChild(mdRender(inp.prompt));
      t.addEventListener('click', (e) => { e.stopPropagation(); body.hidden = !body.hidden; t.textContent = body.hidden ? 'show prompt' : 'hide prompt'; });
      node.appendChild(t);
      node.appendChild(body);
    }
    node.addEventListener('click', (e) => { if (e.target.closest('button, a, .agent-prompt')) return; showLane(b.id); });
    node.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); showLane(b.id); } });
    lane.card = node;
    laneHeaderRefresh(lane);
    return node;
  }

  // refreshTabs shows only agents still running (plus the one being viewed,
  // so you can get back to Main), and hides the strip when Main is alone.
  function refreshTabs() {
    let shown = 0;
    for (const l of lanes.values()) {
      if (!l.tab || l === mainLane) continue;
      const show = l.meta.status === 'running' || l === visible;
      l.tab.hidden = !show;
      if (show) shown++;
    }
    tabs.hidden = shown === 0;
  }

  // laneHeaderRefresh repaints a subagent's card and tab from its meta.
  function laneHeaderRefresh(lane) {
    if (lane === mainLane) return;
    const m = lane.meta;
    const statusText = m.waiting ? 'waiting for permission' : m.status === 'running' ? 'running' : m.status;
    const statusCls = m.waiting ? 'is-waiting' : m.status === 'running' ? 'is-running' : m.status === 'completed' ? 'is-done' : 'is-failed';
    const label = m.desc || (m.type ? m.type + ' agent' : 'subagent');
    if (lane.tab) {
      lane.tab.querySelector('.lane-tab-type').textContent = m.type || 'agent';
      lane.tab.querySelector('.lane-tab-label').textContent = label;
      lane.tab.querySelector('.lane-tab-sub').textContent = m.status === 'running' ? (m.waiting ? 'waiting for permission' : m.last || '') : m.status === 'completed' ? '' : m.status;
      const dot = lane.tab.querySelector('.live-dot');
      dot.className = 'live-dot ' + (m.status === 'running' ? (m.waiting ? 'is-held' : 'is-running') : '');
      lane.tab.classList.toggle('is-done', m.status !== 'running');
      lane.tab.classList.toggle('is-failed', m.status !== 'running' && m.status !== 'completed');
    }
    refreshTabs();
    const dur = m.status !== 'running' && m.startAt && m.endAt ? ((m.endAt - m.startAt) / 1000).toFixed(1) + 's' : '';
    for (const c of [lane.card, lane.doneCard]) {
      if (!c) continue;
      c.querySelector('.agent-type').textContent = m.type || 'agent';
      c.querySelector('.agent-desc').textContent = m.desc || '';
      const st = c.querySelector('.agent-status');
      st.textContent = c === lane.doneCard && m.status === 'completed' ? 'result' : statusText;
      st.className = 'agent-status ' + statusCls;
      const last = c.querySelector('.agent-last');
      const bits = [];
      if (lane.model) bits.push(modelName(lane.model));
      if (lane.steps) bits.push(lane.steps + (lane.steps === 1 ? ' step' : ' steps'));
      if (m.status === 'running' && m.last) bits.push(m.last);
      if (dur) bits.push(dur);
      last.textContent = bits.join(' · ');
      last.hidden = !bits.length;
      c.classList.toggle('is-running', m.status === 'running');
      c.classList.toggle('is-done', m.status === 'completed');
      c.classList.toggle('is-failed', m.status !== 'running' && m.status !== 'completed');
    }
    if (lane.card && lane.card.classList.contains('is-collapsed')) {
      const cl = lane.card.querySelector('.agent-collapsed-meta') || lane.card.querySelector('.agent-head').insertBefore(el('span', 'agent-collapsed-meta'), lane.card.querySelector('.agent-status'));
      cl.textContent = dur;
    }
  }

  function laneSummary(lane, text) {
    const card = lane.doneCard || lane.card;
    if (!card || !text) return;
    const boxEl = card.querySelector('.agent-summary');
    boxEl.textContent = '';
    boxEl.appendChild(mdRender(text));
    boxEl.classList.add('has-summary');
    lane.meta.summary = text;
  }

  // applyLanes takes what the server knows of the lanes a window's events
  // belong to (the subagent's name, whether it has finished, its last
  // word) so a lane whose opening lies outside the window still has its
  // tab labelled and its state right.
  function applyLanes(map) {
    if (!map) return;
    for (const id of Object.keys(map)) {
      const info = map[id] || {};
      const lane = laneByAgent(id, { type: info.type, description: info.description });
      if (info.type && !lane.meta.type) lane.meta.type = info.type;
      if (info.description && !lane.meta.desc) lane.meta.desc = info.description;
      if (info.started_at && !lane.meta.startAt) lane.meta.startAt = ts(info.started_at);
      if (info.status && info.status !== 'running' && lane.meta.status === 'running') laneFinish(lane, info.status, info.ended_at ? ts(info.ended_at) : 0);
      else if (info.last && lane.meta.status === 'running') lane.meta.last = info.last;
      laneHeaderRefresh(lane);
    }
  }
  function laneFinish(lane, status, endAt) {
    lane.meta.status = status || 'completed';
    if (endAt) lane.meta.endAt = endAt;
    lane.meta.last = '';
    lane.meta.waiting = false;
    setActivity(null, lane);
    laneHeaderRefresh(lane);
    refreshMainIdle();
  }

  // showLane swaps the visible lane: log, activity, gauge, composer, tabs.
  function showLane(id) {
    const lane = lanes.get(id) || mainLane;
    for (const l of lanes.values()) l.log.hidden = l !== lane;
    for (const l of lanes.values()) if (l.tab) l.tab.classList.toggle('is-active', l === lane);
    visible = lane;
    unread = false;
    paintJump();
    refreshTabs();
    form.hidden = lane !== mainLane;
    laneNote.hidden = lane === mainLane;
    if (lane !== mainLane) {
      laneNote.textContent = '';
      const back = el('button', 'hook-toggle', 'Back to main');
      back.type = 'button';
      back.addEventListener('click', () => showLane('main'));
      laneNote.appendChild(el('span', null, 'Subagent transcript · messages go to the main agent · '));
      laneNote.appendChild(back);
    }
    contextUsed = lane.ctxUsed;
    if (contextUsed) drawContext(); else setGauge('context', 0, '–', 'Context', 'Context window in use');
    scroll(lane.log);
    const h = lane === mainLane ? '' : '#agent=' + encodeURIComponent(id);
    if ((location.hash || '') !== h) history.replaceState(null, '', location.pathname + location.search + h);
  }

  // agentDoneCard is the card that appears in the parent lane where the
  // subagent's completion actually arrives. The original card collapses.
  function agentDoneCard(lane) {
    const m = lane.meta;
    const node = el('div', 'msg-agent is-result');
    node.tabIndex = 0; node.setAttribute('role', 'button'); node.title = 'Open this subagent\'s transcript';
    const head = el('div', 'agent-head');
    head.appendChild(svg(ICONS.agent));
    head.appendChild(el('span', 'agent-type', m.type || 'agent'));
    head.appendChild(el('span', 'agent-desc', m.desc || 'subagent'));
    head.appendChild(el('span', 'agent-status', ''));
    head.appendChild(svg(ICONS.open, 'agent-open'));
    node.appendChild(head);
    node.appendChild(el('div', 'agent-last'));
    node.appendChild(el('div', 'agent-summary'));
    node.addEventListener('click', (e) => { if (e.target.closest('button, a')) return; showLane(lane.id); });
    node.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); showLane(lane.id); } });
    lane.doneCard = node;
    const wrap = bubble('subagent finished', 'agentdone', node, null);
    if (lane.card) {
      lane.card.classList.add('is-collapsed');
      const from = lane.card.closest('.frame');
      if (from && lastVisibleFrame(mainLane.log) === from) {
        lane.card.hidden = true;
        if (!hideIfEmpty(from, wrap)) mergeRaw(from, wrap);
      }
    }
    return wrap;
  }

  function renderAgentStart(ev) {
    const d = ev.d || {};
    const lane = laneByAgent(d.id, d);
    lane.meta.startAt = ts(curAt);
    if (cold && lane.meta.endAt) { laneHeaderRefresh(lane); return foldIntoLast(ev); }
    if (d.description && !lane.meta.desc) lane.meta.desc = d.description;
    if (d.type && !lane.meta.type) lane.meta.type = d.type;
    laneHeaderRefresh(lane);
    return foldIntoLast(ev);
  }
  function renderAgentProgress(ev) {
    const d = ev.d || {};
    const lane = laneByAgent(d.id, d);
    if (!cold || lane.meta.status === 'running') lane.meta.last = d.last || '';
    if (d.type && !lane.meta.type) lane.meta.type = d.type;
    if (d.description && !lane.meta.desc) lane.meta.desc = d.description;
    if (!cold) refreshMainIdle();
    laneHeaderRefresh(lane);
    return foldIntoLast(ev);
  }
  function renderAgentEnd(ev) {
    const d = ev.d || {};
    const lane = laneByAgent(d.id, d);
    let out = null;
    if (d.card && !lane.doneCard) { out = agentDoneCard(lane); nodes.set(ev.ref, out); }
    laneFinish(lane, d.status, d.ended_at || ts(curAt));
    if (d.summary) laneSummary(lane, d.summary);
    laneHeaderRefresh(lane);
    if (!out) foldIntoLast(ev);
    return out;
  }

  // --- background shells ---
  //
  // A backgrounded Bash call gets a "background" chip on its pill
  // (tool.background), and its output paints under the pill as it runs
  // (tool.output chunks, then one final).
  function renderToolBackground(ev) {
    const d = ev.d || {};
    const pill = pillOf(ev.to);
    if (!pill) return foldIntoLast(ev);
    if (d.status === 'background' && !pill.querySelector('.tool-bg')) pill.appendChild(el('span', 'tool-bg', 'background'));
    else if (d.status && d.status !== 'background') { const bg = pill.querySelector('.tool-bg'); if (bg) bg.textContent = 'background · ' + d.status; }
    attachRaws(pill.closest('.frame'), ev);
    return null;
  }
  const taskOut = new Map(); // task id -> {box, pre, text, toggle}
  function renderToolOutput(ev) {
    const d = ev.d || {};
    const pill = pillOf(ev.to);
    if (!pill) return d.done ? foldIntoLast(ev, 'task output') : null;
    let t = taskOut.get(d.task_id);
    if (!t) {
      const boxEl = el('div', 'tool-live');
      const head = el('div', 'tool-live-head');
      head.appendChild(el('span', 'tool-live-dot'));
      head.appendChild(el('span', 'tool-live-label', 'output'));
      const toggle = el('button', 'hook-toggle tool-live-toggle', 'show all');
      toggle.type = 'button'; toggle.hidden = true;
      head.appendChild(toggle);
      boxEl.appendChild(head);
      const pre = el('pre', 'tool-live-out');
      boxEl.appendChild(pre);
      toggle.addEventListener('click', () => { boxEl.classList.toggle('is-open'); toggle.textContent = boxEl.classList.contains('is-open') ? 'show less' : 'show all'; });
      pill.classList.add('has-live');
      pill.appendChild(boxEl);
      t = { box: boxEl, pre, text: '', toggle };
      taskOut.set(d.task_id, t);
    }
    if (d.done) { t.text = d.text || t.text; t.box.classList.add('is-done'); t.box.querySelector('.tool-live-label').textContent = 'output · finished'; }
    else t.text += d.text || '';
    t.pre.textContent = t.text.length > 200000 ? t.text.slice(-200000) : t.text;
    const lines = (t.text.match(/\n/g) || []).length;
    t.toggle.hidden = lines < 12;
    if (!t.box.classList.contains('is-open')) t.pre.scrollTop = t.pre.scrollHeight;
    if (d.done) attachRaws(pill.closest('.frame'), ev, 'task output');
    return null;
  }

  // pillOf resolves a "tool:<id>" ref to the call's pill/card element.
  const toolPills = new Map(); // call id -> pill element
  function pillOf(ref) {
    if (!ref) return null;
    const id = ref.replace(/^tool:/, '');
    const p = toolPills.get(id);
    return p && p.isConnected ? p : null;
  }

  function toolRow(b) {
    let node;
    if (b.role === 'question') node = askCard(b);
    else if (b.role === 'agent') node = agentCard(b);
    else if (b.role === 'peer') node = peerOutCard(b);
    else if (b.role === 'acta') node = actaCard(b);
    else {
      node = el('div', 'msg-tool');
      node.appendChild(toolIcon(b.name));
      node.appendChild(el('span', 'tool-name', b.name || 'tool'));
      const inp = b.input || {};
      const arg = inp.command || inp.file_path || inp.path || inp.pattern || inp.url || inp.description || inp.prompt;
      if (arg) { const a = el('code', 'tool-arg', String(arg)); a.title = String(arg); node.appendChild(a); }
    }
    if (b.id) { toolPills.set(b.id, node); }
    return node;
  }

  // A tool.denied event (the backend refused the call itself) paints the
  // pill red with the reason.
  function renderToolDenied(ev) {
    const d = ev.d || {};
    const pill = pillOf(ev.to);
    if (!pill) return bubble('permission denied', 'state', el('div', 'frame-note', d.message || d.reason || 'permission denied'), null);
    pill.classList.add('is-denied');
    const fail = el('div', 'tool-fail');
    fail.appendChild(el('span', 'tool-fail-why', 'Not allowed' + (d.reason ? ' · ' + d.reason : '')));
    if (d.message) fail.appendChild(el('span', 'tool-fail-msg', d.message));
    pill.appendChild(fail);
    attachRaws(pill.closest('.frame'), ev, 'permission denied');
    return null;
  }

  function renderAssistant(ev) {
    const d = ev.d || {};
    const blocks = d.blocks || [];
    if (d.model && !curModel && !d.synthetic && !cold) { curModel = d.model; paintModelSelect(); }
    if (d.model && !cur.model && cur !== mainLane) { cur.model = d.model; if (cur.card) laneHeaderRefresh(cur); }
    const body = el('div', 'frame-body');
    for (const b of blocks) {
      if (b.type === 'text') body.appendChild(mdRender(b.text || ''));
      else if (b.type === 'thinking') body.appendChild(thoughtChip(b, ev.at));
      else if (b.type === 'tool_use') {
        if (b.role === 'task') { const tn = taskBlock(); if (tn) body.appendChild(tn); if (b.id) toolPills.set(b.id, tn || (taskCard && taskCard.node)); }
        else if (b.role === 'plan') { const pn = planBlock(b); if (pn) body.appendChild(pn); if (b.id) toolPills.set(b.id, pn || (planFor(b.plan_key || b.id).card)); }
        else body.appendChild(toolRow(b));
      }
      else body.appendChild(el('div', 'msg-unknown', 'unrendered block: ' + b.type));
    }
    if (!body.children.length) return foldIntoLast(ev);
    const node = bubble(d.synthetic ? 'Claude Code' : modelName(d.model || curModel), 'assistant', body, null);
    node.querySelector('.frame-kind').prepend(brandIcon(d.synthetic ? '<synthetic>' : (d.model || curModel)));
    for (const b of blocks) if (b.type === 'tool_use' && b.id) nodes.set('tool:' + b.id, node);
    return node;
  }

  // resultBlock shows a tool result's text, clamped past ~12 lines.
  function resultBlock(txt, isError) {
    const node = el('div', 'msg-result' + (isError ? ' is-error' : ''), txt);
    if (!txt) { node.textContent = '(no output)'; node.classList.add('is-empty'); return node; }
    const lines = txt.split('\n').length;
    if (lines > 12 || txt.length > 1500) {
      node.classList.add('is-clamped');
      const wrap = el('div', 'frame-body');
      wrap.appendChild(node);
      const more = el('button', 'result-more', 'Show all · ' + lines.toLocaleString() + ' lines');
      more.type = 'button';
      more.addEventListener('click', () => {
        const open = node.classList.toggle('is-open');
        node.classList.toggle('is-clamped', !open);
        more.textContent = open ? 'Show less' : 'Show all · ' + lines.toLocaleString() + ' lines';
      });
      wrap.appendChild(more);
      return wrap;
    }
    return node;
  }

  // --- diffs ---
  //
  // A tool.result may carry the change as data (diff: hunks for an edit,
  // the whole content for a created file), rendered as a diff instead of
  // the backend's one-line "updated successfully" text.
  function diffLine(sign, oldNo, newNo, text) {
    const row = el('div', 'diff-line ' + (sign === '+' ? 'is-add' : sign === '-' ? 'is-del' : 'is-ctx'));
    row.appendChild(el('span', 'diff-no', oldNo == null ? '' : String(oldNo)));
    row.appendChild(el('span', 'diff-no', newNo == null ? '' : String(newNo)));
    row.appendChild(el('span', 'diff-sign', sign === ' ' ? '' : sign));
    row.appendChild(el('span', 'diff-text', text));
    return row;
  }
  function diffBlock(diff) {
    const wrap = el('div', 'diff');
    const head = el('div', 'diff-head');
    const file = diff.file || '';
    const nameEl = el('span', 'diff-file', file.split('/').pop() || 'file');
    nameEl.title = file;
    head.appendChild(nameEl);
    const rows = el('div', 'diff-rows');
    let add = 0, del = 0, count = 0;
    if (diff.kind === 'create' && typeof diff.content === 'string') {
      const lines = diff.content.replace(/\n$/, '').split('\n');
      lines.forEach((l, i) => { rows.appendChild(diffLine('+', null, i + 1, l)); });
      add = lines.length; count = lines.length;
      head.appendChild(el('span', 'diff-kind', 'new file'));
    } else if (diff.kind === 'unified' && typeof diff.text === 'string') {
      let o = 0, n = 0;
      for (const l of diff.text.split('\n')) {
        const hm = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(l);
        if (hm) { o = +hm[1]; n = +hm[2]; rows.appendChild(el('div', 'diff-hunk', l)); continue; }
        if (/^(diff |index |--- |\+\+\+ |new file mode|deleted file mode|old mode|new mode|similarity index|rename from|rename to|Binary files)/.test(l)) continue;
        if (l === '' ) continue;
        const sign = l.charAt(0), text = l.slice(1);
        if (sign === '+') { rows.appendChild(diffLine('+', null, n++, text)); add++; }
        else if (sign === '-') { rows.appendChild(diffLine('-', o++, null, text)); del++; }
        else { rows.appendChild(diffLine(' ', o++, n++, text)); }
        count++;
      }
    } else {
      for (const h of diff.hunks || []) {
        rows.appendChild(el('div', 'diff-hunk', '@@ -' + h.oldStart + ',' + h.oldLines + ' +' + h.newStart + ',' + h.newLines + ' @@'));
        let o = h.oldStart, n = h.newStart;
        for (const l of h.lines || []) {
          const sign = l.charAt(0), text = l.slice(1);
          if (sign === '+') { rows.appendChild(diffLine('+', null, n++, text)); add++; }
          else if (sign === '-') { rows.appendChild(diffLine('-', o++, null, text)); del++; }
          else { rows.appendChild(diffLine(' ', o++, n++, text)); }
          count++;
        }
      }
      if (diff.replace_all) head.appendChild(el('span', 'diff-kind', 'replace all'));
    }
    const stats = el('span', 'diff-stats');
    if (add) stats.appendChild(el('span', 'diff-add', '+' + add));
    if (del) stats.appendChild(el('span', 'diff-del', '−' + del));
    head.appendChild(stats);
    wrap.appendChild(head);
    wrap.appendChild(rows);
    if (count > 40) {
      wrap.classList.add('is-clamped');
      const more = el('button', 'result-more', 'Show all · ' + count.toLocaleString() + ' lines');
      more.type = 'button';
      more.addEventListener('click', () => { const open = wrap.classList.toggle('is-open'); wrap.classList.toggle('is-clamped', !open); more.textContent = open ? 'Show less' : 'Show all · ' + count.toLocaleString() + ' lines'; });
      wrap.appendChild(more);
    }
    return wrap;
  }

  // renderToolCall: a call that arrives on its own (Codex reports each tool
  // as an item, not inside a message): a frame holding the pill.
  function renderToolCall(ev) {
    const d = ev.d || {};
    const body = el('div', 'frame-body');
    body.appendChild(toolRow({ id: d.id, name: d.name, input: d.input || {}, role: d.role }));
    const node = bubble(modelName(curModel), 'assistant', body, null);
    node.querySelector('.frame-kind').prepend(brandIcon(curModel));
    return node;
  }

  // renderToolResult: one frame per result. When the call's pill is the last
  // thing on screen the pill moves in as the result frame's header, and the
  // assistant frame it came from is hidden if that emptied it. Otherwise the
  // result gets a copy of the call as its header.
  function renderToolResult(ev) {
    const d = ev.d || {};
    const pill = pillOf(ev.to);
    if (d.role === 'acta' && pill && actaCards.has(d.call_id)) return actaResult(actaCards.get(d.call_id), ev);
    const body = el('div', 'frame-body');
    if (!d.error && Array.isArray(d.diffs) && d.diffs.length > 1) { for (const df of d.diffs) body.appendChild(diffBlock(df)); }
    else if (!d.error && d.diff) body.appendChild(diffBlock(d.diff));
    else body.appendChild(resultBlock(d.text || '', !!d.error));
    if (d.exit_code) body.appendChild(el('div', 'you-status', 'exit code ' + d.exit_code));
    const node = bubble(d.name ? d.name + ' result' : 'result', 'toolresult', body, null);
    if (d.error) node.classList.add('is-error');
    if (pill && pill.classList.contains('msg-tool')) {
      const from = pill.closest('.frame');
      const adjacent = from && pill.closest('.chat-log') === cur.log && lastVisibleFrame(cur.log) === from;
      const head = el('div', 'tool-call');
      if (adjacent) {
        head.appendChild(pill);
        hideIfEmpty(from, node);
      } else {
        const copy = pill.cloneNode(true);
        for (const x of copy.querySelectorAll('.tool-perm, .tool-fail, .tool-bg')) x.remove();
        copy.classList.remove('has-perm', 'is-pending', 'is-allowed', 'is-denied');
        head.appendChild(copy);
      }
      node.insertBefore(head, body);
      node.classList.add('has-call');
    }
    return node;
  }

  // A turn.end event: its text duplicates the last assistant message, so
  // only the accounting shows, as a slim divider.
  function renderTurnEnd(ev) {
    const d = ev.d || {};
    const bits = [];
    if (!d.interrupted && d.error) bits.push(d.error);
    if (typeof d.calls === 'number') bits.push(d.calls + (d.calls === 1 ? ' call' : ' calls'));
    if (typeof d.duration_ms === 'number') bits.push((d.duration_ms / 1000).toFixed(1) + 's');
    if (d.tokens) bits.push(fmtTokens(d.tokens) + ' tok');
    if (weeklyNow != null && weeklyAtTurnStart != null) {
      const delta = weeklyNow - weeklyAtTurnStart;
      bits.push('weekly ' + (delta > 0.0005 ? '+' + fmtPct(delta) : '+<1%'));
      weeklyAtTurnStart = weeklyNow;
    }
    if (d.denials) bits.push(d.denials + ' denied');
    if (d.context_window && !cold) { contextWindow = d.context_window; drawContext(); }
    if (d.interrupted) return divider('turn interrupted', bits, 'is-interrupted');
    const node = divider('turn ended', bits, d.ok === false ? 'is-error' : '');
    if (d.ok === false && Array.isArray(d.errors) && d.errors.length) {
      const boxEl = el('div', 'result-errors');
      for (const m of d.errors) boxEl.appendChild(el('div', 'result-error', m));
      node.appendChild(boxEl);
    }
    return node;
  }

  // divider is the slim full-width rule used to mark a boundary in the
  // transcript: a turn ending, a session starting or resuming.
  function divider(label, bits, extraCls) {
    const wrap = el('div', 'frame frame--result' + (extraCls ? ' ' + extraCls : ''));
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', label));
    if (bits && bits.length) line.appendChild(el('span', 'result-stats', bits.join(' · ')));
    wrap.appendChild(line);
    return wrap;
  }

  // --- compaction ---
  //
  // compact.start opens a block (an obvious in-progress banner), compact.end
  // settles it with the token counts, compact.summary adds a toggle to read
  // the summary that replaced the earlier context. Every folded frame's raw
  // payload hangs off the block.
  let compact = null; // { node, title, stats, bar, line }

  function renderCompactStart(ev) {
    const d = ev.d || {};
    const node = el('div', 'frame frame--compact');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', 'compaction'));
    node.appendChild(head);
    const line = el('div', 'compact-line');
    line.appendChild(svg(ICONS.compact));
    const title = el('span', 'compact-title', 'Compacting context…');
    line.appendChild(title);
    const stats = el('span', 'compact-stats', d.args ? '“' + d.args + '”' : contextUsed ? fmtTokens(contextUsed) + ' tokens in the window' : '');
    line.appendChild(stats);
    node.appendChild(line);
    node.appendChild(el('div', 'compact-bar'));
    const marker = ev.to ? nodes.get(ev.to) : null;
    if (marker && marker.isConnected) { mergeRaw(marker, node); marker.remove(); }
    compact = { node, title, stats, line };
    return node;
  }
  function renderCompactEnd(ev) {
    const d = ev.d || {};
    let fresh = false;
    if (!compact || !compact.node.isConnected) { renderCompactStart({ d: {}, raw: [] }); fresh = true; }
    compact.node.classList.add('is-done');
    if (d.ok === false) {
      compact.node.classList.add('is-error');
      compact.title.textContent = 'Compaction failed';
      compact.stats.textContent = d.error || '';
    } else {
      const bits = [];
      if (d.pre && d.post) bits.push(fmtTokens(d.pre) + ' → ' + fmtTokens(d.post) + ' tokens');
      else if (d.pre) bits.push('from ' + fmtTokens(d.pre) + ' tokens');
      else if (d.post) bits.push('to ' + fmtTokens(d.post) + ' tokens');
      if (typeof d.duration_ms === 'number' && d.duration_ms) bits.push((d.duration_ms / 1000).toFixed(1) + 's');
      if (d.trigger) bits.push(d.trigger);
      compact.title.textContent = 'Compacted context';
      compact.stats.textContent = bits.join(' · ');
    }
    if (!fresh) attachRaws(compact.node, ev);
    return fresh ? compact.node : null;
  }
  function renderCompactSummary(ev) {
    const d = ev.d || {};
    if (!compact || !compact.node.isConnected) return foldIntoLast(ev, 'summary');
    attachRaws(compact.node, ev);
    const body = el('div', 'compact-body');
    body.hidden = true;
    body.appendChild(mdRender(d.text || ''));
    const size = ' · ' + fmtTokens(Math.round((d.text || '').length / 4)) + ' tok';
    const toggle = el('button', 'compact-toggle', 'show summary' + size);
    toggle.type = 'button';
    toggle.addEventListener('click', () => {
      body.hidden = !body.hidden;
      toggle.textContent = (body.hidden ? 'show summary' : 'hide summary') + size;
    });
    compact.line.appendChild(toggle);
    compact.node.insertBefore(body, compact.node.querySelector(':scope > .frame-tools'));
    return null;
  }

  // --- hooks ---
  const hooks = new Map(); // hook id -> {node, status, line, answered}

  function hookStatusNode(status, text) {
    return el('span', 'hook-status is-' + status, text || status);
  }
  function hookRow(d) {
    const node = el('div', 'frame frame--hook');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', 'hook'));
    node.appendChild(head);
    const line = el('div', 'hook-line');
    line.appendChild(svg(ICONS.hook));
    line.appendChild(el('span', 'hook-lbl', 'Hook'));
    line.appendChild(el('code', 'hook-name', d.name || d.event || d.id || 'hook'));
    const status = hookStatusNode('running');
    line.appendChild(status);
    node.appendChild(line);
    const e = { node, line, status, answered: false };
    if (d.id) hooks.set(d.id, e);
    return e;
  }
  function renderHookStart(ev) {
    const e = hookRow(ev.d || {});
    return e.node;
  }
  function renderHookEnd(ev) {
    const d = ev.d || {};
    let e = hooks.get(d.id);
    const fresh = !e || !e.node.isConnected;
    if (fresh) e = hookRow(d);
    if (e.answered) return fresh ? e.node : foldIntoLast(ev, 'duplicate response');
    e.answered = true;
    const ok = d.ok !== false;
    const text = ok ? 'ok' : ((d.outcome && d.outcome !== 'success') ? d.outcome.replace(/_/g, ' ') : 'failed') + (d.exit_code ? ' · exit ' + d.exit_code : '');
    e.status.replaceWith(hookStatusNode(ok ? 'success' : 'failed', text));
    e.status = e.line.querySelector('.hook-status');
    if (!ok) e.node.classList.add('is-error');
    const inj = d.injected;
    const err = typeof d.stderr === 'string' && d.stderr.trim() ? d.stderr : '';
    if (inj || err) {
      const body = el('div', 'hook-body');
      body.hidden = true;
      if (inj && inj.md) body.appendChild(mdRender(inj.md));
      if (inj && inj.text) body.appendChild(el('pre', 'hook-pre', inj.text));
      if (inj && inj.json) body.appendChild(el('pre', 'hook-pre', JSON.stringify(inj.json, null, 2)));
      if (inj && inj.extra) { body.appendChild(el('div', 'hook-sub', 'other output')); body.appendChild(el('pre', 'hook-pre', JSON.stringify(inj.extra, null, 2))); }
      if (err) { body.appendChild(el('div', 'hook-sub', 'stderr')); body.appendChild(el('pre', 'hook-pre is-error', err)); }
      const what = inj && inj.md ? 'injected context' : inj ? 'output' : 'stderr';
      const size = inj && inj.md ? ' · ' + fmtTokens(Math.round(inj.md.length / 4)) + ' tok' : '';
      const toggle = el('button', 'hook-toggle', 'show ' + what + size);
      toggle.type = 'button';
      toggle.addEventListener('click', () => {
        body.hidden = !body.hidden;
        toggle.textContent = (body.hidden ? 'show ' : 'hide ') + what + size;
      });
      e.line.appendChild(toggle);
      e.node.insertBefore(body, e.node.querySelector(':scope > .frame-tools'));
    } else {
      e.line.appendChild(el('span', 'hook-none', 'no output'));
    }
    if (!fresh) { attachRaws(e.node, ev); return null; }
    return e.node;
  }

  // notice draws a leveled message (model fallback, refusal, an
  // informational line) as a pill coloured by level.
  function notice(level, text) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(level === 'error' ? ICONS.alert : level === 'warning' ? ICONS.warn : ICONS.info));
    note.appendChild(el('span', null, text));
    const node = bubble('notice', 'state frame--notice', note, null);
    if (level === 'error') node.classList.add('is-error');
    else if (level === 'warning') node.classList.add('is-warn');
    return node;
  }

  // API retries fold into one row that updates in place and settles once the
  // API answers again or the turn fails.
  let apiRetry = null; // { node, text }
  function renderApiRetry(ev) {
    const d = ev.d || {};
    let fresh = false;
    if (!ev.to || !apiRetry || !apiRetry.node.isConnected) {
      const note = el('div', 'frame-note');
      note.appendChild(svg(ICONS.warn));
      const text = el('span', 'local-text', '');
      note.appendChild(text);
      const node = bubble('api', 'state frame--notice frame--apiretry is-warn', note, null);
      apiRetry = { node, text };
      fresh = true;
    }
    const wait = typeof d.delay_ms === 'number' && d.delay_ms ? ' · next in ' + (d.delay_ms >= 1000 ? Math.round(d.delay_ms / 1000) + 's' : d.delay_ms + 'ms') : '';
    const status = d.status ? ' · HTTP ' + d.status : '';
    apiRetry.text.textContent = 'Retrying the API · attempt ' + (d.attempt || '?') + (d.max ? ' of ' + d.max : '') + wait + status + (d.error ? ' · ' + d.error : '');
    if (!fresh) attachRaws(apiRetry.node, ev);
    return fresh ? apiRetry.node : null;
  }
  function renderApiError(ev) {
    const d = ev.d || {};
    if (d.settled) {
      // the retry row closes once something else happened
      if (apiRetry && apiRetry.node.isConnected) {
        apiRetry.node.classList.remove('is-warn');
        if (d.ok) apiRetry.text.textContent = 'API recovered after ' + (d.attempts || 0) + ((d.attempts || 0) === 1 ? ' retry' : ' retries');
        else { apiRetry.node.classList.add('is-error'); apiRetry.text.textContent = 'API gave up after ' + (d.attempts || 0) + ' retries'; }
      }
      apiRetry = null;
      return null;
    }
    const text = 'API error' + (d.attempts ? ' after ' + d.attempts + (d.attempts === 1 ? ' retry' : ' retries') : '') + (d.error ? ' · ' + d.error : '');
    if (apiRetry && apiRetry.node.isConnected) {
      apiRetry.node.classList.remove('is-warn'); apiRetry.node.classList.add('is-error');
      apiRetry.text.textContent = text;
      attachRaws(apiRetry.node, ev);
      apiRetry = null;
      return null;
    }
    apiRetry = null;
    return notice('error', text);
  }

  // --- session events ---

  // The most recent session started/resumed divider, filled in by the init
  // that follows (model name, raw payload) via its `to`.
  function renderSpawned(ev) {
    const d = ev.d || {};
    if (cold) return divider(d.resumed ? 'session resumed' : 'session started', [], 'frame--session');
    if (d.styles) noteStyles(d.styles);
    procAlive = true;
    return divider(d.resumed ? 'session resumed' : 'session started', [], 'frame--session');
  }
  function renderInit(ev) {
    const d = ev.d || {};
    if (!cold) {
      if (d.permission_mode) setMode(d.permission_mode);
      if (d.model) { curModel = d.model; paintModelSelect(); }
      if (d.effort) { defaultEffort = d.effort; paintModelSelect(); }
      if (d.output_style) noteStyle(d.output_style);
      if (typeof d.fast_mode === 'string') { fastOn = d.fast_mode === 'on'; fastReason = fastOn ? '' : (d.fast_reason || ''); paintModelSelect(); }
      procAlive = true;
      if (hydrated && !modelsAsked) requestModels();
    }
    const host = ev.to ? nodes.get(ev.to) : null;
    if (host && host.isConnected) {
      if (host.classList.contains('frame--session') || host.classList.contains('frame--reset')) {
        const stats = host.querySelector('.result-stats');
        if (stats) stats.textContent = modelName(d.model); else host.querySelector('.result-line').appendChild(el('span', 'result-stats', modelName(d.model)));
      }
      attachRaws(host, ev);
      return null;
    }
    return divider('session init', [modelName(d.model)], 'frame--session');
  }
  function renderExit(ev) {
    const d = ev.d || {};
    if (!cold) { procAlive = false; cmdQueue.length = 0; }
    if (d.expected && ev.to && nodes.get(ev.to)) return foldIntoLast(ev);
    const node = bubble('status', 'state', el('div', 'frame-note', 'process exited (code ' + (d.code != null ? d.code : '?') + ')'), null);
    if (d.code) node.classList.add('is-error');
    return node;
  }
  function renderFailure(ev) {
    const d = ev.d || {};
    if (!cold) { procAlive = false; cmdQueue.length = 0; }
    const reason = ev.t === 'session.undelivered' ? (d.reason || 'no harness connected') : ('spawn failed: ' + (d.error || 'unknown error'));
    if (!cold && failInput(reason, ev)) return null;
    const node = bubble('status', 'state', el('div', 'frame-note', ev.t === 'session.undelivered' ? 'not delivered: ' + reason : reason), null);
    node.classList.add('is-error');
    return node;
  }
  // A catch-up or import: the messages that follow were read off the
  // backend's own transcript on the host (taken in a terminal, or before
  // the session was Acta's at all).
  function renderCatchup(ev) {
    const d = ev.d || {};
    const bits = [];
    if (d.count) bits.push(d.count + (d.count === 1 ? ' message' : ' messages'));
    if (d.from) {
      const a = new Date(d.from), b = new Date(d.to || d.from);
      const day = (t) => t.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: t.getFullYear() === new Date().getFullYear() ? undefined : 'numeric' });
      bits.push(day(a) === day(b) ? day(a) : day(a) + ' – ' + day(b));
    }
    if (d.skipped) bits.push('older history left on the harness (' + (d.skipped < 1048576 ? Math.max(1, Math.round(d.skipped / 1024)) + ' KB' : (d.skipped / 1048576).toFixed(0) + ' MB') + ')');
    return divider(d.source === 'import' ? 'imported from the local transcript' : 'caught up from the local transcript', bits, 'frame--session frame--catchup');
  }
  function renderStateNote(ev) {
    const d = ev.d || {};
    const node = bubble('status', 'state', el('div', 'frame-note', d.text || 'state'), null);
    if (d.error) node.classList.add('is-error');
    return node;
  }

  // --- /clear ---
  //
  // A session.reset: the transcript before it folds under a "context
  // cleared" rule (like a rewind), and the gauges start over.
  function renderReset(ev) {
    const logEl = mainLane.log;
    const marker = ev.to ? nodes.get(ev.to) : null;
    const wrap = el('div', 'frame frame--rewind frame--reset');
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', 'context cleared'));
    wrap.appendChild(line);
    const kids = [...logEl.children].filter(k => k !== mainLane.activity && k !== marker);
    if (kids.length) {
      const boxEl = el('details', 'rewind-branch');
      boxEl.appendChild(el('summary', 'rewind-branch-sum', 'show what was cleared · ' + kids.length + (kids.length === 1 ? ' frame' : ' frames')));
      const body = el('div', 'rewind-branch-body');
      for (const k of kids) body.appendChild(k);
      boxEl.appendChild(body);
      wrap.appendChild(boxEl);
    }
    if (marker && marker.isConnected) { mergeRaw(marker, wrap); marker.remove(); }
    echoed.length = 0;
    pendingInputs.length = 0;
    turnHasEcho = false;
    contextUsed = 0; mainLane.ctxUsed = 0;
    setGauge('context', 0, '–', 'Context', 'Context window in use');
    for (const k in reports) delete reports[k];
    paintGaugePop();
    return wrap;
  }

  // imageSig identifies a message's pictures so a retry can be matched to the
  // attempt it repeats (sizes and types, not the whole base64).
  function imageSig(images) {
    return (images || []).map(i => (i.media_type || '') + ':' + (i.data || '').length).join(',');
  }

  // youBubble draws a message of yours: images above the text. pending=true
  // is the grey form (submitted, not yet in the conversation); the blue form
  // is drawn from the backend's echo, which is the proof it got there.
  function youBubble(text, images, pending) {
    const body = el('div', 'frame-body');
    if (images && images.length) {
      const row = el('div', 'msg-images');
      for (const im of images) {
        if (!im || !im.data) continue;
        const img = document.createElement('img');
        img.src = 'data:' + (im.media_type || 'image/png') + ';base64,' + im.data;
        img.alt = 'attached image';
        img.addEventListener('click', () => openLightbox(img.src));
        const lane = cur;
        img.addEventListener('load', () => follow(lane));
        row.appendChild(img);
      }
      body.appendChild(row);
    }
    if (text) body.appendChild(el('div', 'msg-text', text));
    if (pending) body.appendChild(el('div', 'you-status', 'waiting to enter the conversation'));
    return bubble('you', 'you' + (pending ? ' is-pending' : ''), body, null);
  }
  function inputKey(text, images) { return (text || '') + ' ' + imageSig(images); }

  // The most recent input bubble, so a delivery failure that follows it can
  // be shown on the message itself (red, with the reason and a Retry button).
  let lastInput = null; // { node, text, images, failed }
  const retryButtons = [];
  const failedInputs = [];
  // Submitted messages still waiting for their echo, oldest first.
  const pendingInputs = []; // { key, node, record }

  // --- slash commands in the transcript ---
  //
  // A "/command" input is a marker, not a message. The backend's reply
  // (cmd.reply) and the closing result fold into it. A report the context
  // panel renders (/context, /usage) never reaches here: it arrives as a fold
  // plus a report event.
  const cmdMarkers = new Map(); // ref -> { node, kind, name, args, out, status, line }
  function cmdMarker(c, ev) {
    if (c.kind === 'effort' || c.kind === 'fast') {
      const note = el('div', 'frame-note');
      note.appendChild(svg(c.kind === 'effort' ? ICONS.gauge : ICONS.bolt));
      note.appendChild(el('span', 'local-text', c.kind === 'effort' ? 'effort set to ' + c.args : 'fast mode toggled'));
      const node = bubble('status', 'state frame--mode', note, null);
      cmdMarkers.set(ev.ref, { node, kind: c.kind, name: c.name, args: c.args, out: null, status: null, line: null, text: note.querySelector('.local-text') });
      return node;
    }
    const wrap = el('div', 'frame frame--cmd');
    const line = el('div', 'cmd-line');
    line.appendChild(svg(c.kind === 'goal' ? ICONS.goal : ICONS.slash));
    const code = el('code', 'cmd-code', '/' + c.name + (c.args ? ' ' + c.args : ''));
    code.title = '/' + c.name + (c.args ? ' ' + c.args : '');
    line.appendChild(code);
    const status = el('span', 'cmd-status', '');
    line.appendChild(status);
    wrap.appendChild(line);
    const out = el('div', 'cmd-out');
    out.hidden = true;
    wrap.appendChild(out);
    cmdMarkers.set(ev.ref, { node: wrap, kind: c.kind, name: c.name, args: c.args, out, status, line });
    return wrap;
  }
  function cmdToggle(lc, label) {
    const t = el('button', 'cmd-toggle', 'show ' + label);
    t.type = 'button';
    t.addEventListener('click', () => { lc.out.hidden = !lc.out.hidden; t.textContent = (lc.out.hidden ? 'show ' : 'hide ') + label; });
    lc.line.appendChild(t);
  }
  // renderCmdReply folds the backend's answer to a command into its marker.
  function renderCmdReply(ev) {
    const d = ev.d || {};
    const lc = cmdMarkers.get(ev.to);
    if (d.standalone || !lc || !lc.node.isConnected) {
      // a command's answer with no marker of its own on screen
      const note = el('div', 'frame-note');
      note.appendChild(svg(ICONS.slash));
      note.appendChild(el('span', 'local-text', d.text || ''));
      const node = bubble('status', 'state frame--mode', note, null);
      if (d.error) node.classList.add('is-error');
      return node;
    }
    attachRaws(lc.node, ev, 'reply');
    const txt = d.text || '';
    lc.node.classList.toggle('is-error', !!d.error);
    if (!lc.out) { if (lc.text) lc.text.textContent = txt; return null; }
    const plain = !/[#*|`_\[]/.test(txt);
    if (plain && !txt.includes('\n') && txt.length <= 120) { lc.status.textContent = '· ' + txt; return null; }
    lc.out.appendChild(mdRender(txt));
    if (plain) lc.out.classList.add('is-text');
    lc.out.hidden = false;
    return null;
  }
  function renderEffort(ev) {
    const d = ev.d || {};
    if (d.value && !cold) { curEffort = d.value; paintModelSelect(); }
    const lc = cmdMarkers.get(ev.to);
    if (lc && lc.node.isConnected) { if (lc.text) lc.text.textContent = 'effort set to ' + d.value; attachRaws(lc.node, ev, 'reply'); return null; }
    if (!ev.raw || !ev.raw.length) return null;
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.gauge));
    note.appendChild(el('span', 'local-text', 'effort set to ' + d.value));
    return bubble('status', 'state frame--mode', note, null);
  }
  function renderFast(ev) {
    const d = ev.d || {};
    if (!d.unavailable && !cold) { fastOn = !!d.on; fastReason = ''; paintModelSelect(); }
    const text = d.unavailable ? 'fast mode unavailable · ' + (d.reason || '') : d.on ? 'fast mode on' : 'fast mode off';
    const lc = cmdMarkers.get(ev.to);
    if (lc && lc.node.isConnected) { if (lc.text) lc.text.textContent = text; lc.node.classList.toggle('is-error', !!d.unavailable); attachRaws(lc.node, ev, 'reply'); return null; }
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.bolt));
    note.appendChild(el('span', 'local-text', text));
    const node = bubble('status', 'state frame--mode', note, null);
    if (d.unavailable) node.classList.add('is-error');
    return node;
  }

  // --- reports: /context, /usage, /autocompact ---
  const reports = {}; // key -> { text, at }
  const ac = { enabled: null, window: '' }; // auto-compact, as last reported
  let lastTurnEnd = 0;
  function renderReport(ev) {
    const d = ev.d || {};
    reports[d.key] = { text: d.text || '', at: Date.now() };
    if (d.key === 'autocompact' && d.window) ac.window = d.window;
    paintGaugePop();
    return foldIntoLast(ev);
  }

  // --- goal ---
  //
  // A goal event carries the goal's state (active / met / unmet / cleared),
  // parsed by the server from the backend's replies; the header pill shows it.
  let goal = null; // { cond, state, turns, last }
  function renderGoal(ev) {
    const d = ev.d || {};
    if (!cold) {
      goal = d.state && d.state !== 'cleared' ? { cond: d.cond || '', state: d.state, turns: d.turns || 0, last: d.last || '' } : null;
      paintGoal();
    }
    if (!ev.raw || !ev.raw.length) return null;
    const host = ev.to ? nodes.get(ev.to) : null;
    if (host && host.isConnected) { attachRaws(host, ev, 'goal'); return null; }
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.goal));
    const txt = d.text || '';
    note.appendChild(el('span', 'local-text', txt.length > 160 ? txt.slice(0, 157) + '…' : txt));
    return bubble('status', 'state frame--mode', note, null);
  }

  function renderInput(ev) {
    const d = ev.d || {};
    const text = d.text || '';
    const images = Array.isArray(d.images) ? d.images : [];
    if (cold) {
      // history: settled, never pending, never the input a failure lands on
      if (d.cmd && !images.length) return cmdMarker(d.cmd, ev);
      const node = youBubble(text, images, false);
      if (d.images_pruned) node.appendChild(el('div', 'frame-note', d.images_pruned + (d.images_pruned === 1 ? ' image pruned' : ' images pruned')));
      coldPending.push({ key: inputKey(text, images), node });
      return node;
    }
    while (retryButtons.length) retryButtons.pop().remove();
    if (d.cmd && !images.length) {
      const n = cmdMarker(d.cmd, ev);
      lastInput = { node: n, text, images: [], failed: false };
      return n;
    }
    const node = youBubble(text, images, true);
    const sig = imageSig(images);
    for (let i = failedInputs.length - 1; i >= 0; i--) {
      const f = failedInputs[i];
      if (f.text !== text || imageSig(f.images) !== sig) continue;
      failedInputs.splice(i, 1);
      mergeRaw(f.node, node);
      f.node.remove();
    }
    const rec = { node, text, images, failed: false };
    // a prompt read off the backend's transcript was never Acta's to
    // deliver: a failure that follows belongs to the message sent from here
    if (!d.transcript) lastInput = rec;
    pendingInputs.push({ key: inputKey(text, images), node, record: rec });
    return node;
  }

  // Blue bubbles in order, so a rewind can walk back from the newest message
  // to the target one.
  const echoed = []; // { id, node, text }
  let turnHasEcho = false;

  // renderUserMessage: the backend's replay of a message of ours means it is
  // now in the conversation. Draw the blue bubble here (where it actually
  // landed) and retire the grey one, carrying its raw payloads over.
  function renderUserMessage(ev) {
    const d = ev.d || {};
    if (d.system) {
      const body = el('div', 'frame-body');
      body.appendChild(el('div', 'msg-text', d.text || ''));
      return bubble('user', 'toolresult', body, null);
    }
    const text = d.text || '';
    const images = Array.isArray(d.images) ? d.images : [];
    const key = inputKey(text, images);
    const pendList = cold ? coldPending : pendingInputs;
    const i = pendList.findIndex(e => e.key === key);
    const node = youBubble(text, images, false);
    if (d.id && !d.steer) {
      const entry = { id: d.id, node, text, seq: ev.seq || 0 };
      (cold ? coldEchoed : echoed).push(entry);
      node.appendChild(rewindMenu(entry));
    } else if (d.id) {
      node.classList.add('is-steer');
      node.title = 'Steered into the turn already running';
    }
    if (!cold) turnHasEcho = true;
    if (i >= 0) {
      const pend = pendList.splice(i, 1)[0];
      mergeRaw(pend.node, node);
      pend.node.remove();
      if (lastInput && lastInput.node === pend.node) lastInput.node = node;
    } else if (!cold && lastInput && !lastInput.failed && lastInput.node.isConnected && lastInput.node.classList.contains('is-pending') && lastInput.text === text) {
      mergeRaw(lastInput.node, node);
      lastInput.node.remove();
      lastInput.node = node;
    }
    return node;
  }

  // --- cross-session messages ---
  //
  // A message from a peer session (peer.message: from, name, mode, text)
  // shows as a peer bubble named after the sender, linked to the Acta
  // session of the same name when there is one.
  const peerNames = new Map();  // address -> the name that peer gave itself
  function peerLink(name) {
    if (!name) return null;
    for (const n of document.querySelectorAll('[data-session-name]')) {
      if (n.dataset.sessionName !== sessionID && n.textContent.trim() === name) return '/account/sessions/' + encodeURIComponent(n.dataset.sessionName);
    }
    return null;
  }
  function peerBubble(ev) {
    const d = ev.d || {};
    const name = d.name || 'another session';
    if (d.from && d.name) peerNames.set(d.from, d.name);
    const body = el('div', 'frame-body');
    const head = el('div', 'peer-head');
    head.appendChild(svg(ICONS.agent));
    head.appendChild(el('span', null, 'from '));
    const href = peerLink(name);
    if (href) { const a = el('a', 'peer-from', name); a.href = href; a.title = 'Open that session'; head.appendChild(a); }
    else {
      const span = el('span', 'peer-from', name);
      head.appendChild(span);
      fetch('/account/sessions/lookup?title=' + encodeURIComponent(name) + '&exclude=' + encodeURIComponent(sessionID), { headers: { 'X-Requested-With': 'fetch' } })
        .then(r => r.ok ? r.json() : null)
        .then(j => { if (j && j.id && span.isConnected) { const a = el('a', 'peer-from', name); a.href = '/account/sessions/' + encodeURIComponent(j.id); a.title = 'Open that session'; span.replaceWith(a); } })
        .catch(() => {});
    }
    if (d.mode) head.appendChild(el('span', 'peer-mode', d.mode));
    head.title = d.from || '';
    body.appendChild(head);
    body.appendChild(mdRender((d.text || '').trim()));
    turnHasEcho = true;
    return bubble('peer message', 'peer', body, null);
  }
  function peerName(to) {
    if (!to) return '?';
    if (peerNames.has(to)) return peerNames.get(to);
    if (/^uds:/.test(to)) return to.replace(/^uds:.*\//, '').replace(/\.sock$/, '');
    return to;
  }
  const peerOut = new Map(); // call id -> { card, status }
  function peerOutCard(b) {
    const inp = b.input || {};
    const to = inp.to || inp.recipient || '';
    const card = el('div', 'peer-box peer-out');
    const head = el('div', 'peer-head');
    head.appendChild(svg(ICONS.agent));
    head.appendChild(el('span', null, 'to '));
    const name = peerName(to);
    const href = peerLink(name.replace(/\s*\[[0-9a-f]{6}\]$/, ''));
    if (href) { const a = el('a', 'peer-from', name); a.href = href; a.title = 'Open that session'; head.appendChild(a); }
    else head.appendChild(el('span', 'peer-from', name));
    if (inp.summary && inp.summary !== inp.message) head.appendChild(el('span', 'peer-mode', inp.summary));
    head.title = to;
    card.appendChild(head);
    card.appendChild(mdRender(String(inp.message || inp.content || '')));
    const status = el('div', 'peer-status', 'sending…');
    card.appendChild(status);
    if (b.id) peerOut.set(b.id, { card, status });
    return card;
  }
  function renderPeerDelivery(ev) {
    const d = ev.d || {};
    const entry = peerOut.get(d.call_id);
    if (!entry || !entry.card.isConnected) return foldIntoLast(ev, 'delivery');
    let msg = String(d.text || '').replace(/uds:\S+/g, m => peerName(m));
    if (d.ok && /^[“"]/.test(msg)) msg = 'delivered' + (/ → /.test(msg) ? msg.slice(msg.lastIndexOf(' → ')) : '');
    entry.status.textContent = '';
    entry.status.appendChild(svg(d.ok ? ICONS.check : ICONS.warn));
    entry.status.appendChild(el('span', null, msg));
    entry.status.classList.toggle('is-error', !d.ok);
    attachRaws(entry.card.closest('.frame'), ev, 'delivery');
    return null;
  }

  // --- Acta MCP tool cards ---
  //
  // Calls to Acta's own tools get a card instead of a pill: a verb, the item
  // it touched as a linked chip, the meaningful part of the input, and once
  // the result lands what came back.
  const ACTA_VERBS = {
    whoami: 'Who am I', list_principals: 'Listed principals', list_workspaces: 'Listed workspaces', list_boards: 'Listed boards', list_statuses: 'Listed statuses',
    list_items: 'Listed items', get_item: 'Read item', create_item: 'Created item', set_item_status: 'Set status', claim_item: 'Claimed item', set_item_title: 'Renamed item',
    set_item_assignee: 'Assigned item', set_item_description: 'Described item', set_item_milestone: 'Set milestone', set_item_priority: 'Set priority', set_item_type: 'Set type',
    set_item_size: 'Set size', set_item_due: 'Set due date', set_item_parent: 'Set parent', list_projects: 'Listed projects', create_project: 'Created project',
    set_item_project: 'Filed under project', list_releases: 'Listed releases', create_release: 'Created release', set_release_target: 'Set release target',
    set_item_release: 'Put in release', set_release_status: 'Set release status', list_subscriptions: 'Listed subscriptions', subscribe: 'Subscribed', unsubscribe: 'Unsubscribed',
    add_comment: 'Commented', list_documents: 'Listed documents', get_document: 'Read document', create_document: 'Created document', update_document: 'Updated document',
    delete_document: 'Deleted document', watch_comments: 'Watched comments', archive_item: 'Archived item', unarchive_item: 'Unarchived item', list_activity: 'Read activity',
    list_notifications: 'Read notifications', mark_notification_read: 'Marked notification read', memory_recall: 'Recalled memories', memory_get: 'Read memory',
    memory_save: 'Saved memory', memory_edit: 'Edited memory', memory_delete: 'Deleted memory',
  };
  const itemURLs = new Map();
  const actaCards = new Map(); // call id -> { card, tool, input, res, status, chip, head }
  function noteItem(it) {
    if (!it || typeof it !== 'object' || !it.id) return;
    const rec = { url: it.url || '', ref: it.ref || '', title: it.title || '', status: it.status || '' };
    itemURLs.set(it.id, rec);
    if (it.ref) itemURLs.set(it.ref, rec);
  }
  function itemChip(idOrItem) {
    const it = typeof idOrItem === 'object' ? idOrItem : null;
    const id = it ? it.id : idOrItem;
    const known = it || itemURLs.get(id) || null;
    const chip = el(known && known.url ? 'a' : 'span', 'acta-chip');
    if (known && known.url) { chip.href = known.url; chip.title = 'Open in Acta'; }
    chip.appendChild(el('code', 'acta-ref', (known && known.ref) || id || '?'));
    if (known && known.title) chip.appendChild(el('span', 'acta-chip-title', known.title));
    return chip;
  }
  function fold(node, label) {
    const wrap = el('div', 'acta-fold');
    wrap.appendChild(node);
    if ((node.textContent || '').length < 700) return wrap;
    wrap.classList.add('is-folded');
    const t = el('button', 'cmd-toggle', 'show ' + (label || 'all'));
    t.type = 'button';
    t.addEventListener('click', () => { const f = wrap.classList.toggle('is-folded'); t.textContent = (f ? 'show ' : 'hide ') + (label || 'all'); });
    wrap.appendChild(t);
    return wrap;
  }
  function kv(pairs) {
    const row = el('div', 'acta-kv');
    for (const [k, v] of pairs) {
      if (v == null || v === '' || v === false) continue;
      const pill = el('span', 'acta-pill');
      pill.appendChild(el('span', 'acta-k', k));
      pill.appendChild(el('span', 'acta-v', Array.isArray(v) ? v.join(', ') : String(v)));
      row.appendChild(pill);
    }
    return row.children.length ? row : null;
  }
  function arrow(v) { const n = el('div', 'acta-arrow'); n.appendChild(el('span', null, '→ ')); n.appendChild(el('strong', null, String(v))); return n; }
  function actaCard(b) {
    const tool = String(b.name || '').replace(/^mcp__acta__/, '');
    const inp = b.input || {};
    const card = el('div', 'acta-card');
    const head = el('div', 'acta-head');
    head.appendChild(svg(ICONS.acta));
    head.appendChild(el('span', 'acta-verb', ACTA_VERBS[tool] || tool.replace(/_/g, ' ')));
    let chip = null;
    const tgt = inp.id || inp.item || '';
    if (tgt && /item|comment|document|archive|subscribe|watch/.test(tool) && !/^(create_document|list_documents)$/.test(tool)) { chip = itemChip(tgt); head.appendChild(chip); }
    if (tool === 'create_document' && inp.item) { chip = itemChip(inp.item); head.appendChild(chip); }
    head.appendChild(el('code', 'acta-tool', tool));
    card.appendChild(head);
    const body = el('div', 'acta-body');
    const md = (t) => fold(mdRender(String(t)), 'full text');
    switch (tool) {
      case 'add_comment': if (inp.body) body.appendChild(md(inp.body)); break;
      case 'create_item': {
        if (inp.title) body.appendChild(el('div', 'acta-title', inp.title));
        const f = kv([['workspace', inp.workspace], ['board', inp.board], ['status', inp.status], ['parent', inp.parent], ['project', inp.project], ['release', inp.release], ['priority', inp.priority], ['type', inp.type], ['size', inp.size], ['due', inp.due], ['assignee', inp.assignee]]);
        if (f) body.appendChild(f);
        if (inp.description) body.appendChild(md(inp.description));
        break;
      }
      case 'set_item_status': case 'claim_item': { if (inp.status) body.appendChild(arrow(inp.status)); if (Array.isArray(inp.checklist) && inp.checklist.length) { const ul = el('ul', 'acta-check'); for (const c of inp.checklist) ul.appendChild(el('li', null, c)); body.appendChild(ul); } break; }
      case 'set_item_title': if (inp.title) body.appendChild(arrow(inp.title)); break;
      case 'set_item_description': if (inp.description) body.appendChild(md(inp.description)); break;
      case 'set_item_assignee': body.appendChild(arrow(inp.assignee || 'nobody')); break;
      case 'set_item_priority': body.appendChild(arrow(inp.priority || 'none')); break;
      case 'set_item_type': body.appendChild(arrow(inp.type || 'none')); break;
      case 'set_item_size': body.appendChild(arrow(inp.size || 'none')); break;
      case 'set_item_due': body.appendChild(arrow(inp.due || 'no due date')); break;
      case 'set_item_milestone': body.appendChild(arrow(inp.milestone === false ? 'not a milestone' : 'milestone')); break;
      case 'set_item_parent': body.appendChild(arrow(inp.parent || 'no parent')); break;
      case 'set_item_project': body.appendChild(arrow(inp.project || 'unfiled')); break;
      case 'set_item_release': body.appendChild(arrow(inp.release || 'no release')); break;
      case 'memory_save': case 'memory_edit': {
        const f = kv([['scope', inp.scope], ['workspace', inp.workspace], ['project', inp.project], ['name', inp.name], ['mode', inp.mode]]);
        if (f) body.appendChild(f);
        if (inp.summary) body.appendChild(el('div', 'acta-title', inp.summary));
        if (inp.body) body.appendChild(md(inp.body));
        break;
      }
      case 'create_document': case 'update_document': { if (inp.title) body.appendChild(el('div', 'acta-title', inp.title)); if (inp.body) body.appendChild(md(inp.body)); break; }
      default: { const f = kv(Object.entries(inp).filter(([k, v]) => k !== 'id' && (typeof v !== 'object' || Array.isArray(v)))); if (f) body.appendChild(f); }
    }
    if (body.children.length) card.appendChild(body);
    const res = el('div', 'acta-result');
    res.hidden = true;
    card.appendChild(res);
    const status = el('div', 'acta-status', 'calling…');
    card.appendChild(status);
    if (b.id) actaCards.set(b.id, { card, tool, input: inp, res, status, chip, head });
    return card;
  }
  function itemRow(it) {
    const row = el('div', 'acta-item');
    noteItem(it);
    row.appendChild(itemChip(it));
    const bits = [it.status, it.assignee ? '@' + it.assignee : '', it.priority, it.due ? 'due ' + it.due : ''].filter(Boolean);
    if (bits.length) row.appendChild(el('span', 'acta-item-meta', bits.join(' · ')));
    return row;
  }
  function actaResult(entry, ev) {
    const d = ev.d || {};
    const txt = d.text || '';
    let data = d.data != null ? d.data : null;
    if (data == null) { try { data = JSON.parse(txt); } catch (_) { data = null; } }
    const { res, status, tool } = entry;
    status.textContent = '';
    const wrapped = /^Error:/.test(txt.trim()) ? txt.trim() : (data && typeof data === 'object' && typeof data.content === 'string' && /^Error:/.test(data.content) ? data.content : '');
    if (d.error || wrapped) {
      status.classList.add('is-error');
      status.appendChild(svg(ICONS.warn));
      const m = /^Error:\s*(.*?)(?:\.\s*Output has been saved to (\S+?)\.?)?(?:\n|$)/s.exec(wrapped || txt);
      status.appendChild(el('span', null, m ? m[1] + (m[2] ? ' · saved to ' + m[2].replace(/^.*\//, '') : '') : (wrapped || txt).slice(0, 400) || 'failed'));
      status.title = (wrapped || txt).slice(0, 2000);
    } else {
      status.hidden = true;
      res.hidden = false;
      const isItem = data && typeof data === 'object' && data.id && data.title != null && data.status != null;
      if (isItem) {
        noteItem(data);
        if (entry.chip) entry.chip.replaceWith(itemChip(data)); else entry.head.insertBefore(itemChip(data), entry.head.querySelector('.acta-tool'));
        const bits = [data.status, data.assignee ? '@' + data.assignee : '', data.priority, data.type, data.size, data.due ? 'due ' + data.due : '', data.project ? 'project ' + data.project : '', data.release ? 'release ' + data.release : '', data.subtasks_total ? data.subtasks_done + '/' + data.subtasks_total + ' subtasks' : '', Array.isArray(data.comments) ? data.comments.length + (data.comments.length === 1 ? ' comment' : ' comments') : ''].filter(Boolean);
        if (bits.length) res.appendChild(el('div', 'acta-item-meta', bits.join(' · ')));
        if (tool === 'get_item' && data.description) res.appendChild(fold(mdRender(data.description), 'description'));
        if (tool === 'get_item' && Array.isArray(data.subtasks) && data.subtasks.length) { const l = el('div', 'acta-list'); for (const it of data.subtasks.slice(0, 8)) l.appendChild(itemRow(it)); if (data.subtasks.length > 8) l.appendChild(el('div', 'acta-more', '+' + (data.subtasks.length - 8) + ' more')); res.appendChild(l); }
      } else if (data && Array.isArray(data.items)) {
        const l = el('div', 'acta-list');
        for (const it of data.items.slice(0, 8)) l.appendChild(itemRow(it));
        if (data.items.length > 8) l.appendChild(el('div', 'acta-more', '+' + (data.items.length - 8) + ' more'));
        if (!data.items.length) l.appendChild(el('div', 'acta-more', 'no items'));
        res.appendChild(l);
      } else if (data && typeof data === 'object' && data.body != null && data.at && !data.name) {
        const when = (() => { try { return new Date(data.at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }); } catch (_) { return data.at; } })();
        res.appendChild(el('div', 'acta-item-meta', (data.author ? 'by ' + data.author + ' · ' : '') + when + (data.id ? ' · ' + data.id : '')));
      } else if (data && typeof data === 'object' && data.name && data.scope) {
        res.appendChild(el('div', 'acta-item-meta', data.scope + (data.workspace ? '/' + data.workspace : '') + (data.project ? '/' + data.project : '') + ' · ' + data.name + (data.updated_at ? ' · ' + data.updated_at.slice(0, 16).replace('T', ' ') : '')));
        if (data.summary && !(entry.input && entry.input.summary)) res.appendChild(el('div', 'acta-title', data.summary));
        if (data.body && tool === 'memory_get') res.appendChild(fold(mdRender(data.body), 'memory'));
      } else if (data && typeof data === 'object' && data.username) {
        res.appendChild(el('div', 'acta-item-meta', data.username + (data.display ? ' · ' + data.display : '') + (data.is_agent ? ' · agent' + (data.owner ? ' of ' + data.owner : '') : '')));
      } else if (data && typeof data === 'object' && data.title && (data.item || tool.includes('document'))) {
        res.appendChild(el('div', 'acta-title', data.title));
        if (data.body && tool === 'get_document') res.appendChild(fold(mdRender(data.body), 'document'));
      } else if (data && typeof data === 'object') {
        const listKey = Object.keys(data).find(k => Array.isArray(data[k]));
        const list = listKey ? data[listKey] : null;
        if (list && list.length && list.every(x => x && typeof x === 'object' && (x.name || x.title || x.summary))) {
          const l = el('div', 'acta-list');
          for (const x of list.slice(0, 10)) { const row = el('div', 'acta-item'); row.appendChild(el('strong', null, x.name || x.title || x.slug || '')); if (x.summary || x.description || x.item_title) row.appendChild(el('span', 'acta-item-meta', x.summary || x.description || x.item_title)); l.appendChild(row); }
          if (list.length > 10) l.appendChild(el('div', 'acta-more', '+' + (list.length - 10) + ' more'));
          res.appendChild(l);
        } else if (list) {
          res.appendChild(el('div', 'acta-item-meta', list.length + ' ' + listKey.replace(/_/g, ' ')));
          res.appendChild(fold(resultBlock(JSON.stringify(data, null, 2), false), 'result'));
        } else res.appendChild(fold(resultBlock(JSON.stringify(data, null, 2), false), 'result'));
      } else if (txt.trim()) res.appendChild(fold(resultBlock(txt, false), 'result'));
      else res.hidden = true;
    }
    attachRaws(entry.card.closest('.frame'), ev, 'acta result');
    return null;
  }

  // --- rewind ---
  //
  // Rewind walks the backend back one message at a time from the tip; the
  // control requests are answered as reply events. A "rewind" marker records
  // what was discarded, and the client collapses that stretch into a branch.
  const pendingCtl = new Map(); // request id -> resolve

  function control(op, timeout) {
    return new Promise((resolve) => {
      const id = 'rw-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 7);
      const done = (v) => { if (pendingCtl.delete(id)) resolve(v); };
      pendingCtl.set(id, done);
      setTimeout(() => done(null), timeout || 30000);
      sendOp(Object.assign({ id }, op));
    });
  }
  function renderReply(ev) {
    const d = ev.d || {};
    if (d.id && pendingCtl.has(d.id)) pendingCtl.get(d.id)(d.error ? { error: d.error } : (d.response || {}));
    return foldIntoLast(ev, 'reply');
  }

  function rewindMenu(entry) {
    const wrap = el('details', 'msg-menu');
    const sum = el('summary', 'msg-menu-btn');
    sum.title = 'Rewind to this message';
    sum.appendChild(svg(ICONS.rewind));
    wrap.appendChild(sum);
    const list = el('div', 'msg-menu-pop');
    const add = (label, hint, fn) => {
      const b = el('button', 'msg-menu-item');
      b.type = 'button';
      b.appendChild(el('span', 'msg-menu-name', label));
      if (hint) b.appendChild(el('span', 'msg-menu-hint', hint));
      b.addEventListener('click', () => { wrap.open = false; fn(); });
      list.appendChild(b);
    };
    add('Edit this message', 'undo the conversation from here and put the text back', () => doRewind(entry, 'conversation', true));
    add('Rewind conversation', 'forget everything from here on', () => doRewind(entry, 'conversation', false));
    add('Rewind files', 'restore files to before this message', () => doRewind(entry, 'files', false));
    add('Rewind both', 'conversation and files', () => doRewind(entry, 'both', false));
    add('Summarise from here', 'replace this stretch with a summary of it', () => doRewind(entry, 'summarise', true));
    wrap.appendChild(list);
    document.addEventListener('click', (e) => { if (wrap.open && !wrap.contains(e.target)) wrap.open = false; });
    return wrap;
  }
  function rewindBusy(node, text) {
    let line = node.querySelector('.rewind-busy');
    if (!text) { if (line) line.remove(); return; }
    if (!line) { line = el('div', 'rewind-busy'); node.insertBefore(line, node.querySelector(':scope > .frame-tools')); }
    line.textContent = text;
  }
  async function doRewind(entry, mode, prefill) {
    const idx = echoed.indexOf(entry);
    if (idx < 0) return;
    const node = entry.node;
    let summary = '';
    if (mode === 'summarise') {
      rewindBusy(node, 'Summarising this stretch…');
      const r = await control({ op: 'side_question', question: 'Summarise, in a short paragraph, everything that has happened in this conversation since (and including) my message: "' + entry.text.slice(0, 400) + '". Cover what was tried, what was learned, and anything still outstanding.' }, 120000);
      summary = (r && (r.response || r.answer)) || '';
      if (!summary) { rewindBusy(node, 'Could not summarise; nothing was rewound.'); setTimeout(() => rewindBusy(node, ''), 4000); return; }
    }
    let files = null;
    if (mode === 'files' || mode === 'both') {
      rewindBusy(node, 'Checking which files would change…');
      const dry = await control({ op: 'rewind_files', target: entry.id, dry_run: true });
      const changed = (dry && dry.filesChanged) || [];
      if (!dry || dry.canRewind === false) {
        rewindBusy(node, 'Files cannot be rewound' + (dry && dry.error ? ' · ' + dry.error : '') + '.');
        setTimeout(() => rewindBusy(node, ''), 6000);
        if (mode === 'files') return;
      } else if (!changed.length) {
        rewindBusy(node, 'No file changes to restore since this message.');
        setTimeout(() => rewindBusy(node, ''), 5000);
        if (mode === 'files') return;
      } else {
        const n = changed.length;
        if (!confirm('Restore ' + n + (n === 1 ? ' file' : ' files') + ' to their state before this message?\n\n' + changed.join('\n') + '\n\n+' + (dry.insertions || 0) + ' / -' + (dry.deletions || 0))) { rewindBusy(node, ''); return; }
        rewindBusy(node, 'Restoring files…');
        const done = await control({ op: 'rewind_files', target: entry.id, dry_run: false });
        files = { changed: changed, insertions: dry.insertions || 0, deletions: dry.deletions || 0, ok: !!done };
      }
    }
    let walked = 0, text = entry.text;
    if (mode !== 'files') {
      for (let i = echoed.length - 1; i >= idx; i--) {
        rewindBusy(node, 'Rewinding the conversation… ' + (echoed.length - i) + ' of ' + (echoed.length - idx));
        const r = await control({ op: 'rewind', target: echoed[i].id });
        if (!r || r.rewound !== true) {
          rewindBusy(node, 'Rewind stopped' + (r && r.error ? ' · ' + r.error : '') + '.');
          setTimeout(() => rewindBusy(node, ''), 6000);
          break;
        }
        walked++;
        if (i === idx && typeof r.prefillText === 'string') text = r.prefillText;
      }
    }
    rewindBusy(node, '');
    if (!walked && !files) return;
    ws.send(JSON.stringify({ t: 'mark', payload: { kind: 'rewind', mode, target_uuid: entry.id, messages: walked, files, summary: summary || undefined, at: new Date().toISOString() } }));
    if (prefill) {
      box.value = mode === 'summarise' ? summary : text;
      box.style.height = 'auto';
      box.style.height = Math.min(box.scrollHeight, 200) + 'px';
      box.focus();
    }
  }

  // failInput marks the last input as undelivered with a reason and a Retry.
  function failInput(reason, ev) {
    const host = ev.to ? nodes.get(ev.to) : null;
    const rec = lastInput && host && lastInput.node === host ? lastInput : (host && !lastInput ? null : lastInput);
    if (!rec || rec.failed || !rec.node.isConnected) return false;
    rec.failed = true;
    failedInputs.push(rec);
    const { node, text, images } = rec;
    node.classList.add('is-failed');
    const line = el('div', 'fail-line');
    line.appendChild(el('span', 'fail-reason', 'Not delivered: ' + reason));
    const btn = el('button', 'btn-retry', 'Retry');
    btn.type = 'button';
    btn.addEventListener('click', () => {
      btn.disabled = true;
      btn.textContent = 'Retrying…';
      send(text, (images || []).map(i => ({ media_type: i.media_type, data: i.data })));
    });
    line.appendChild(btn);
    retryButtons.push(btn);
    node.insertBefore(line, node.querySelector(':scope > .frame-json-box'));
    attachRaws(node, ev, 'failure');
    return true;
  }

  // --- streamed replies (live events, never stored) ---
  //
  // Each lane keeps one growing "live" assistant frame built from the
  // deltas; the settled assistant event that follows replaces it.
  function liveFrame(lane, model) {
    if (lane.live) return lane.live;
    const wrap = el('div', 'frame frame--assistant is-streaming');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', modelName(model || lane.model || curModel)));
    wrap.appendChild(head);
    const body = el('div', 'frame-body');
    wrap.appendChild(body);
    const stick = lane.follow;
    lane.log.insertBefore(wrap, lane.activity.isConnected && lane.activity.parentNode === lane.log ? lane.activity : null);
    if (stick) scroll(lane.log);
    lane.live = wrap;
    lane.liveBlocks = new Map();
    return wrap;
  }
  function dropLive(lane) {
    if (lane.live) lane.live.remove();
    lane.live = null;
    lane.liveBlocks = null;
  }
  function paintLiveBlock(lane, b) {
    if (b.raf) return;
    b.raf = requestAnimationFrame(() => {
      b.raf = 0;
      const stick = lane.follow;
      if (b.type === 'text') { const fresh = mdRender(b.text); b.node.replaceWith(fresh); b.node = fresh; }
      if (stick) scroll(lane.log);
    });
  }
  function renderStream(ev) {
    const d = ev.d || {};
    const lane = cur;
    if (ev.t === 'stream.start') {
      dropLive(lane);
      if (d.model && !lane.model && lane !== mainLane) lane.model = d.model;
      liveFrame(lane, d.model);
      return null;
    }
    if (ev.t === 'stream.stop') {
      if (lane.live && !lane.live.querySelector('.frame-body').children.length) dropLive(lane);
      return null;
    }
    if (ev.t === 'stream.block') {
      if (d.type === 'thinking') { setActivity('Thinking'); if (!lane.think) lane.think = { start: Date.now(), tokens: 0, last: null, frames: 0 }; liveThought(lane); if (lane.liveBlocks) lane.liveBlocks.set(d.index, { type: 'thinking' }); return null; }
      if (!lane.live) liveFrame(lane);
      const b = { type: d.type, text: d.text || '', node: null, raf: 0, name: d.name };
      const body = lane.live.querySelector('.frame-body');
      if (d.type === 'text') { b.node = mdRender(b.text); body.appendChild(b.node); }
      else if (d.type === 'tool_use') { b.node = el('div', 'msg-tool is-streaming'); b.node.appendChild(toolIcon(d.name)); b.node.appendChild(el('span', 'tool-name', d.name || 'tool')); b.node.appendChild(el('span', 'tool-arg', '…')); body.appendChild(b.node); setActivity('Preparing ' + (d.name || 'tool')); }
      lane.liveBlocks.set(d.index, b);
      return null;
    }
    if (ev.t === 'stream.delta') {
      if (!lane.liveBlocks) {
        // a backend that streams without announcing the message (Codex)
        liveFrame(lane);
        const b0 = { type: 'text', text: '', node: mdRender(''), raf: 0 };
        lane.live.querySelector('.frame-body').appendChild(b0.node);
        lane.liveBlocks.set(d.index, b0);
      }
      const b = lane.liveBlocks.get(d.index);
      if (!b || b.type === 'thinking') return null;
      if (typeof d.text === 'string') { b.text += d.text; paintLiveBlock(lane, b); setActivity('Writing'); }
      else if (typeof d.json === 'string') { b.json = (b.json || '') + d.json; const arg = /"(?:command|file_path|path|pattern|url|description|prompt|message)"\s*:\s*"((?:[^"\\]|\\.)*)/.exec(b.json); if (arg && b.node) b.node.querySelector('.tool-arg').textContent = arg[1].replace(/\\n/g, ' ').slice(0, 120); }
      return null;
    }
    return null;
  }

  // renderRewind draws the marker and folds the stretch it discarded into a
  // collapsed branch.
  const REWIND_LABEL = { conversation: 'conversation rewound', files: 'files restored', both: 'conversation rewound and files restored', summarise: 'summarised and rewound' };
  function renderRewind(ev) {
    const p = ev.d || {};
    const wrap = el('div', 'frame frame--rewind');
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', REWIND_LABEL[p.mode] || 'rewound'));
    const bits = [];
    if (p.messages) bits.push(p.messages + (p.messages === 1 ? ' message' : ' messages'));
    if (p.files && p.files.changed && p.files.changed.length) bits.push(p.files.changed.length + (p.files.changed.length === 1 ? ' file' : ' files') + ' · +' + (p.files.insertions || 0) + '/-' + (p.files.deletions || 0));
    if (bits.length) line.appendChild(el('span', 'result-stats', bits.join(' · ')));
    wrap.appendChild(line);
    const echoList = cold ? coldEchoed : echoed;
    const t = echoList.find(x => x.id === p.target_uuid);
    const tnode = t ? t.node : null;
    const logEl = cur.log;
    const kids = [...logEl.children];
    const from = tnode ? kids.indexOf(tnode.closest('.frame') || tnode) : -1;
    if (from >= 0) {
      const boxEl = el('details', 'rewind-branch');
      const sum = el('summary', 'rewind-branch-sum', '');
      boxEl.appendChild(sum);
      const body = el('div', 'rewind-branch-body');
      let moved = 0;
      for (const k of kids.slice(from)) {
        if (k === cur.activity) continue;
        body.appendChild(k);
        moved++;
      }
      boxEl.appendChild(body);
      sum.textContent = 'show what was discarded · ' + moved + (moved === 1 ? ' frame' : ' frames');
      wrap.appendChild(boxEl);
      const i = echoList.findIndex(e => e.id === p.target_uuid);
      if (i >= 0) echoList.splice(i);
    }
    if (p.summary) {
      const s = el('div', 'rewind-summary');
      s.appendChild(mdRender(p.summary));
      wrap.appendChild(s);
    }
    if (p.files && p.files.changed && p.files.changed.length) {
      const f = el('details', 'rewind-files');
      f.appendChild(el('summary', null, 'files restored · ' + p.files.changed.length));
      const list = el('div', 'rewind-file-list');
      for (const path of p.files.changed) list.appendChild(el('code', null, path));
      f.appendChild(list);
      wrap.appendChild(f);
    }
    return wrap;
  }

  // --- dispatch ---

  // renderEvent draws one event: the node it creates (or null), with its raw
  // frames attached. Events with `to` and no node of their own land their
  // raws on the named node inside their renderer (or via foldIntoLast).
  function renderEvent(ev) {
    const d = ev.d || {};
    switch (ev.t) {
      case 'fold': return foldIntoLast(ev);
      case 'session.spawned': return renderSpawned(ev);
      case 'session.init': return renderInit(ev);
      case 'session.exit': return renderExit(ev);
      case 'session.spawn_error': case 'session.undelivered': return renderFailure(ev);
      case 'session.resume_failed': return bubble('status', 'state', el('div', 'frame-note', d.reason || 'resume failed; starting fresh'), null);
      case 'session.reset': return renderReset(ev);
      case 'session.catalog': if (!cold) noteCatalog(d); return foldIntoLast(ev);
      case 'session.state': return renderStateNote(ev);
      case 'session.catchup': return renderCatchup(ev);
      case 'input': return renderInput(ev);
      case 'user.message': return renderUserMessage(ev);
      case 'peer.message': return peerBubble(ev);
      case 'assistant': return renderAssistant(ev);
      case 'thought': return thoughtChip(d, ev.at);
      case 'thinking': return null; // folded into the activity line and the thought chip
      case 'stream.start': case 'stream.block': case 'stream.delta': case 'stream.stop': return renderStream(ev);
      case 'cmd.reply': return renderCmdReply(ev);
      case 'effort': return renderEffort(ev);
      case 'fast': return renderFast(ev);
      case 'goal': return renderGoal(ev);
      case 'report': return renderReport(ev);
      case 'autocompact': if (!cold) { ac.enabled = d.enabled; paintGaugePop(); } return null;
      case 'notice': if (d.model && !cold) { curModel = d.model; paintModelSelect(); } return notice(d.level || 'info', d.text || '');
      case 'api.retry': return renderApiRetry(ev);
      case 'api.error': return renderApiError(ev);
      case 'tool.call': return renderToolCall(ev);
      case 'tool.result': return renderToolResult(ev);
      case 'turn.diff': return renderTurnDiff(ev);
      case 'tool.denied': return renderToolDenied(ev);
      case 'tool.background': return renderToolBackground(ev);
      case 'tool.output': return renderToolOutput(ev);
      case 'question.answer': return renderQuestionAnswer(ev);
      case 'peer.delivery': return renderPeerDelivery(ev);
      case 'approval.request': return renderApproval(ev);
      case 'approval.answer': return renderAnswer(ev);
      case 'turn.idle': if (!cold) stalePendingPerms(); return null;
      case 'turn.end': return renderTurnEnd(ev);
      case 'setting': {
        if (!cold) {
          if (d.key === 'permission_mode' && d.value && !d.requested) setMode(d.value);
          if ((d.key === 'output_style' || d.key === 'personality') && d.value) noteStyle(d.value);
          if (d.key === 'model' && d.value) { curModel = d.value === 'default' ? '' : d.value; paintModelSelect(); }
        }
        const host = ev.to ? nodes.get(ev.to) : null;
        if (host && host.isConnected) { attachRaws(host, ev, 'response'); if (d.error) { host.classList.add('is-error'); host.querySelector('.frame-note').appendChild(el('span', 'local-text', ' · ' + d.error)); } return null; }
        return settingMarker(d);
      }
      case 'usage.limits': if (!cold) noteLimits(d); return foldIntoLast(ev, 'rate limits');
      case 'usage.context': if (!cold) noteContext(d, cur); return null;
      case 'hook.start': return renderHookStart(ev);
      case 'hook.end': return renderHookEnd(ev);
      case 'agent.start': return renderAgentStart(ev);
      case 'agent.progress': return renderAgentProgress(ev);
      case 'agent.end': return renderAgentEnd(ev);
      case 'tasks': return renderTasks(ev);
      case 'plan.update': return renderPlanUpdate(ev);
      case 'plan.submit': return renderPlanSubmit(ev);
      case 'plan.verdict': return renderPlanVerdict(ev);
      case 'compact.start': return renderCompactStart(ev);
      case 'compact.end': return renderCompactEnd(ev);
      case 'compact.summary': return renderCompactSummary(ev);
      case 'rewind': return renderRewind(ev);
      case 'reply': return renderReply(ev);
      default: {
        const kind = d.kind || ev.t || 'event';
        return bubble(kind, 'unknown', d.text ? el('div', 'frame-note', d.text) : null, null);
      }
    }
  }

  // The raw rule: a renderer that returns a node leaves the event's raws to
  // addEvent; one that returns null has attached them to the node they
  // belong to itself (foldIntoLast when there is no better home).

  function addEvent(ev) {
    if (!ev || typeof ev !== 'object') return;
    const key = ev.seq + ':' + (ev.sub || 0);
    if (ev.seq && !ev.live && !cold) {
      if (seen.has(key)) return;
      seen.add(key);
      if (ev.seq > lastSeq) lastSeq = ev.seq;
    }
    curAt = ev.at || '';
    cur = ev.lane ? laneByAgent(ev.lane, null) : mainLane;
    // a live event for the main lane while its tail is pruned away renders
    // into the discard bucket: the state moves, the screen does not
    const detached = !cold && tailDetached && cur === mainLane;
    let savedLog = null, savedDay = '';
    if (detached) { savedLog = mainLane.log; savedDay = mainLane.day; mainLane.log = discard; }
    if (!cold) {
      tabAlert(ev);
      noteActivity(ev);
      if ((ev.t === 'assistant' || ev.t === 'turn.end' || ev.t === 'session.exit' || ev.t === 'thought') && cur.live) dropLive(cur);
      if (ev.t === 'assistant' || ev.t === 'thought') dropLiveThought(cur);
    }
    let node = null;
    try { node = renderEvent(ev); } catch (err) { console.error('render', ev.t, err); node = bubble(ev.t, 'unknown', el('div', 'frame-note', 'could not render: ' + (err && err.message)), null); }
    if (node) {
      if (ev.ref) nodes.set(ev.ref, node);
      attachRaws(node, ev);
      if (ev.seq) { node.dataset.seq = String(ev.seq); node.dataset.at = ev.at || ''; }
      if (cur !== mainLane) { cur.steps++; laneHeaderRefresh(cur); }
      const stick = !cold && !detached && cur.follow;
      const live = cur.log.querySelector(':scope > .is-streaming, :scope > .is-live');
      const mark = dayMark(cur, ev.at);
      if (mark) cur.log.insertBefore(mark, live);
      // a settled thought straight after another joins it as "Thought ×N"
      const merged = !mark && node.classList.contains('frame--thought') && !node.classList.contains('is-live') ? mergeThought(node, cur) : null;
      if (merged) { if (ev.ref) nodes.set(ev.ref, merged); }
      else cur.log.insertBefore(node, live);
      if (!cold && !detached) placeActivity();
      // hydrate scrolls once at the end: a scroll per frame forces a layout
      // per frame, and the page is built from hundreds of them
      if (stick && !hydrating) scroll(cur.log); else if (hydrated && !cold) noteUnread(cur);
    } else {
      // an event that drew nothing still names a node: whatever its raws
      // landed on, so later events addressed to it find the same host
      if (ev.ref) { const host = target(ev); if (host) nodes.set(ev.ref, host); }
      if (!cold && !detached) placeActivity();
    }
    if (detached) { mainLane.log = savedLog; mainLane.day = savedDay; }
    cur = mainLane;
    if (!cold) showNextPerm();
  }

  // --- the window ---
  //
  // The page opens on the last turns. A sentinel above the log fetches the
  // turns before them as it scrolls into view; past a cap of rendered
  // frames the far end is pruned, and a sentinel below fetches the pruned
  // tail back when the reader returns. While the tail is pruned, live
  // events still drive the state (activity, approvals, gauges) but render
  // into a discard bucket; "Latest" reopens the log at its end.

  let WIN_CAP_OVERRIDE = 0;
  const WIN_CAP_DEFAULT = 800; // frames kept in the main log
  const WIN_CAP = { valueOf() { return WIN_CAP_OVERRIDE || WIN_CAP_DEFAULT; } };
  const WIN_KEEP = 2.5;     // screens beyond the viewport kept when pruning
  let topSeq = 0;           // first event seq rendered in the main log
  let hasEarlier = stage.dataset.earlier === '1';
  let tailSeq = 0;          // last event seq rendered while the tail is pruned
  let tailDetached = false;
  let loadingWin = false;
  const discard = el('div', 'chat-log');
  discard.hidden = true;
  log.parentNode.appendChild(discard);
  const earlierEl = el('div', 'chat-more chat-earlier', 'Loading earlier turns…');
  const laterEl = el('div', 'chat-more chat-later', 'Loading later turns…');
  earlierEl.hidden = true; laterEl.hidden = true;
  log.prepend(earlierEl);

  async function fetchWindow(params) {
    const r = await fetch('/account/sessions/' + encodeURIComponent(sessionID) + '/events?' + params, { credentials: 'same-origin' });
    if (!r.ok) throw new Error('events ' + r.status);
    return r.json();
  }
  function frameNodes(l) { return [...l.children].filter(k => k !== mainLane.activity && k !== laterEl && k !== earlierEl && k !== discard); }
  function frameCount(l) { return l.querySelectorAll(':scope > .frame, :scope > .frame-group, :scope > .msg-agent').length; }
  function seqOf(node) { return Number(node.dataset.seq || 0); }
  function edgeSeq(kids, fromEnd) {
    for (let i = 0; i < kids.length; i++) { const k = kids[fromEnd ? kids.length - 1 - i : i]; if (seqOf(k)) return seqOf(k); }
    return 0;
  }

  // renderChunk renders events cold and places what they drew: at the top
  // of each lane's log (older turns), at the bottom (the pruned tail coming
  // back), or in place of the whole log (reopening at the tail).
  function renderChunk(evs, where) {
    const swapped = new Map();
    const swap = (lane) => {
      if (swapped.has(lane)) return;
      swapped.set(lane, { log: lane.log, day: lane.day });
      // in the document (hidden), so hosts found in it count as connected
      const c = el('div', 'chat-log'); c.hidden = true;
      log.parentNode.appendChild(c);
      lane.log = c;
      if (where !== 'bottom') lane.day = '';
    };
    for (const lane of lanes.values()) swap(lane);
    cold = true; coldEchoed = []; coldPending = []; coldSwap = swap;
    const savedPlan = curPlan;
    try { for (const ev of evs) addEvent(ev); }
    finally { cold = false; coldSwap = null; }
    curPlan = savedPlan;
    for (const [lane, saved] of swapped) {
      const c = lane.log; lane.log = saved.log;
      const kids = [...c.children];
      c.remove();
      const chunkLastDay = lane.day;
      if (where === 'replace' && lane === mainLane) {
        for (const k of frameNodes(lane.log)) k.remove();
        lane.day = chunkLastDay;
        lane.log.insertBefore(earlierEl, lane.log.firstChild);
        earlierEl.after(...kids);
        continue;
      }
      if (where === 'bottom' || !kids.length) {
        if (kids.length) { const anchor = lane === mainLane ? laterEl : lane.activity; if (anchor.parentNode === lane.log) anchor.before(...kids); else lane.log.append(...kids); }
        if (where !== 'bottom') lane.day = saved.day;
        continue;
      }
      // top: the existing content's first day mark is redundant when the
      // chunk ends on that day, and missing when the day was implied
      const first = frameNodes(lane.log).find(k => !k.classList.contains('chat-more'));
      if (first) {
        if (first.classList.contains('day-mark')) { if (first.dataset.day === chunkLastDay) first.remove(); }
        else if (first.dataset.at) { const m = dayMark(lane, first.dataset.at); if (m) first.before(m); }
      }
      lane.day = saved.day;
      const at = lane === mainLane ? earlierEl : null;
      if (at && at.parentNode === lane.log) at.after(...kids); else lane.log.prepend(...kids);
    }
    // rewind order: bubbles are walked newest-first, by seq
    if (coldEchoed.length) {
      const ids = new Set(coldEchoed.map(e => e.id));
      const kept = echoed.filter(e => !ids.has(e.id));
      echoed.length = 0;
      echoed.push(...kept, ...coldEchoed);
      echoed.sort((a, b) => (a.seq || 0) - (b.seq || 0));
    }
    coldEchoed = []; coldPending = [];
    paintPlanPill(); paintTasks();
  }

  async function loadEarlier() {
    if (!hasEarlier || loadingWin || !topSeq) return;
    loadingWin = true;
    try {
      const j = await fetchWindow('before=' + topSeq + '&turns=20');
      applyLanes(j.lanes);
      const l = mainLane.log; const h0 = l.scrollHeight, t0 = l.scrollTop;
      renderChunk(j.events || [], 'top');
      hasEarlier = !!j.more; earlierEl.hidden = !hasEarlier;
      if (j.events && j.events.length) topSeq = j.events[0].seq;
      l.scrollTop = t0 + (l.scrollHeight - h0);
      pruneBottom();
    } catch (err) { console.error('earlier', err); }
    finally { loadingWin = false; }
    // a chunk that adds little to this lane (its events were folds, or a
    // subagent's) leaves the sentinel in view, and the observer, which
    // fires on change, would not fire again: keep going until the log is
    // taller than its viewport or nothing earlier is left
    if (hasEarlier && sentinelInView(earlierEl)) requestAnimationFrame(loadEarlier);
  }
  // sentinelInView says whether a scroll sentinel is within the log's
  // viewport (plus the observer's margin) in the lane on screen.
  function sentinelInView(el) {
    if (el.hidden || mainLane.log.hidden) return false;
    const r = el.getBoundingClientRect(), b = mainLane.log.getBoundingClientRect();
    return r.bottom >= b.top - 300 && r.top <= b.bottom + 300;
  }
  async function loadLater() {
    if (!tailDetached || loadingWin) return;
    loadingWin = true;
    try {
      const j = await fetchWindow('after=' + tailSeq + '&turns=20');
      applyLanes(j.lanes);
      renderChunk(j.events || [], 'bottom');
      if (j.events && j.events.length) tailSeq = j.events[j.events.length - 1].seq;
      if (j.more && sentinelInView(laterEl)) requestAnimationFrame(loadLater);
      if (!j.more) reattachTail();
      pruneTop();
    } catch (err) { console.error('later', err); }
    finally { loadingWin = false; }
  }
  // reattachTail ends a detachment: what arrived live meanwhile (rendered
  // into the discard bucket) moves into the log after the refetched turns.
  function dayKeyOf(at) { const t = Date.parse(at || ''); return Number.isFinite(t) ? new Date(t).toDateString() : ''; }
  function reattachTail() {
    const l = mainLane.log;
    let lastDay = '';
    for (const k of [...frameNodes(l)].reverse()) { if (k.dataset.at) { lastDay = dayKeyOf(k.dataset.at); break; } }
    for (const k of [...discard.children]) {
      if (k.classList.contains('day-mark')) { if (k.dataset.day === lastDay) { k.remove(); continue; } lastDay = k.dataset.day; laterEl.before(k); continue; }
      if (seqOf(k) > tailSeq || !seqOf(k)) { if (k.dataset.at) lastDay = dayKeyOf(k.dataset.at); laterEl.before(k); } else k.remove();
    }
    tailDetached = false; tailSeq = 0; laterEl.hidden = true;
    placeActivity(mainLane);
    paintJump();
  }
  async function reopenTail() {
    if (loadingWin) return;
    loadingWin = true;
    try {
      const j = await fetchWindow('tail=1');
      applyLanes(j.lanes);
      renderChunk(j.events || [], 'replace');
      discard.textContent = '';
      tailDetached = false; tailSeq = 0; laterEl.hidden = true;
      hasEarlier = !!j.more; earlierEl.hidden = !hasEarlier;
      topSeq = j.events && j.events.length ? j.events[0].seq : 0;
      placeActivity(mainLane);
      scroll(mainLane.log);
      unread = false; paintJump();
    } catch (err) { console.error('tail', err); }
    finally { loadingWin = false; }
  }
  // pruneBottom drops the tail once the log is over the cap and the reader
  // is well above it; never while a turn is running.
  function pruneBottom() {
    const l = mainLane.log;
    if (mainTurnActive) return;
    if (frameCount(l) <= WIN_CAP) return;
    const limit = l.scrollTop + l.clientHeight * (1 + WIN_KEEP);
    const kids = frameNodes(l);
    let removed = 0;
    for (let i = kids.length - 1; i >= 0 && frameCount(l) > WIN_CAP - 100; i--) {
      const k = kids[i];
      if (k.offsetTop < limit) break;
      k.remove(); removed++;
    }
    if (!removed) return;
    const rest = frameNodes(l);
    tailSeq = edgeSeq(rest, true);
    // the day state follows what is left, so the tail coming back gets its
    // marks where the days actually change
    const lastAt = [...rest].reverse().find(k => k.dataset.at);
    if (lastAt) { const t = Date.parse(lastAt.dataset.at); if (Number.isFinite(t)) mainLane.day = new Date(t).toDateString(); }
    tailDetached = true; laterEl.hidden = false;
    placeActivity(mainLane);
    paintJump();
  }
  function pruneTop() {
    const l = mainLane.log;
    if (frameCount(l) <= WIN_CAP) return;
    const limit = l.scrollTop - l.clientHeight * WIN_KEEP;
    const kids = frameNodes(l);
    let removedH = 0, removed = 0;
    for (let i = 0; i < kids.length && frameCount(l) > WIN_CAP - 100; i++) {
      const k = kids[i];
      if (k.offsetTop + k.offsetHeight > limit) break;
      removedH += k.offsetHeight; k.remove(); removed++;
    }
    if (!removed) return;
    l.scrollTop -= removedH;
    topSeq = edgeSeq(frameNodes(l), false);
    hasEarlier = true; earlierEl.hidden = false;
  }
  const winIO = new IntersectionObserver((entries) => {
    for (const en of entries) {
      if (!en.isIntersecting) continue;
      if (en.target === earlierEl) loadEarlier(); else if (en.target === laterEl) loadLater();
    }
  }, { root: log, rootMargin: '300px 0px' });
  winIO.observe(earlierEl); winIO.observe(laterEl);
  // for tests: drive the window by hand
  stage._win = { addEvent, loadEarlier, loadLater, reopenTail, pruneBottom, pruneTop, state: () => ({ topSeq, hasEarlier, tailSeq, tailDetached, cap: +WIN_CAP, frames: frameCount(mainLane.log) }), setCap: (n) => { WIN_CAP_OVERRIDE = n; } };

  // hydrate renders the events the server put in the page.
  function hydrate() {
    const script = document.querySelector('[data-chat-events]');
    let evs = [];
    try { evs = JSON.parse(script ? script.textContent : '[]') || []; } catch (_) { evs = []; }
    const lanesScript = document.querySelector('[data-chat-lanes]');
    try { applyLanes(JSON.parse(lanesScript ? lanesScript.textContent : 'null')); } catch (_) {}
    hydrating = true;
    for (const ev of evs) addEvent(ev);
    hydrating = false;
    for (const l of lanes.values()) { placeActivity(l); scroll(l.log); }
    topSeq = evs.length ? evs[0].seq : 0;
    earlierEl.hidden = !hasEarlier;
    hydrated = true;
    paintModelSelect();
    paintGoal();
    showNextPerm();
    const m = /agent=([^&]+)/.exec(location.hash || '');
    if (m && lanes.has(decodeURIComponent(m[1]))) showLane(decodeURIComponent(m[1]));
  }

  // --- rename ---
  const nameBtn = stage.querySelector('[data-chat-rename]');
  const nameInput = stage.querySelector('[data-rename-input]');
  let openRename = null;
  // The name shown is the title without its status marker; the marker is
  // the picker beside it. Both write the full title, marker first, and the
  // rename reaches the backend's own record too (see Hub.Rename).
  const SS = window.sessionStatus;
  async function renameTo(bare, status) {
    if (!nameBtn) return;
    if (status === undefined) status = stage.dataset.status || '';
    const title = SS ? SS.compose(status, bare) : bare;
    const current = SS ? SS.compose(stage.dataset.status || '', nameBtn.textContent.trim()) : nameBtn.textContent.trim();
    if (title === current) return;
    const body = new URLSearchParams({ title, csrf_token: stage.dataset.csrf || '' });
    try {
      const res = await fetch('/account/sessions/' + encodeURIComponent(sessionID) + '/title', {
        method: 'POST', body, headers: { 'X-Requested-With': 'fetch', 'X-CSRF-Token': stage.dataset.csrf || '' },
      });
      if (res.ok && title) {
        const parts = SS ? SS.apply(sessionID, title) : { status: '', bare: title };
        stage.dataset.status = parts.status;
        nameBtn.textContent = parts.bare || nameBtn.textContent;
        document.title = (parts.bare || title) + ' · Acta';
      }
    } catch (_) {}
  }
  const statusPick = stage.querySelector('[data-status-pick]');
  if (statusPick) statusPick.addEventListener('change', () => renameTo(nameBtn ? nameBtn.textContent.trim() : '', statusPick.value));
  if (nameBtn && nameInput) {
    const open = () => {
      nameInput.value = nameBtn.textContent.trim();
      nameBtn.hidden = true;
      nameInput.hidden = false;
      nameInput.focus();
      nameInput.select();
    };
    const close = () => { nameInput.hidden = true; nameBtn.hidden = false; };
    // Enter saves and hides the input, which blurs it: the blur must not
    // save a second time
    let saving = false;
    const save = () => { if (saving) return; saving = true; const title = nameInput.value.trim(); close(); renameTo(title); setTimeout(() => { saving = false; }, 0); };
    openRename = open;
    nameBtn.addEventListener('click', open);
    nameInput.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); save(); }
      else if (e.key === 'Escape') { e.preventDefault(); close(); }
    });
    nameInput.addEventListener('blur', save);
  }

  // --- websocket ---
  let ws = null;
  function setConn(state) {
    if (!connEl) return;
    connEl.textContent = state;
    connEl.className = 'chat-conn chat-conn--' + state;
  }
  function connect() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = proto + '//' + location.host + '/account/sessions/' + encodeURIComponent(sessionID) + '/ws?after=' + lastSeq;
    setConn('connecting');
    ws = new WebSocket(url);
    ws.onopen = () => { setConn('connected'); reportFocus(); if (stage.dataset.running === '1' && !modelsAsked) requestModels(); };
    ws.onmessage = (e) => {
      let m;
      try { m = JSON.parse(e.data); } catch (_) { return; }
      addEvent(m);
    };
    ws.onclose = () => { setConn('offline'); ws = null; setTimeout(connect, 1500); };
    ws.onerror = () => { try { ws.close(); } catch (_) {} };
  }

  // --- attention ---
  function tabFocused() { return document.visibilityState === 'visible' && document.hasFocus(); }
  function reportFocus() {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'focus', on: tabFocused() }));
  }
  for (const evName of ['visibilitychange', 'focus', 'blur']) (evName === 'visibilitychange' ? document : window).addEventListener(evName, reportFocus);

  let pushHere = null;
  if ('serviceWorker' in navigator && 'PushManager' in window) {
    navigator.serviceWorker.getRegistration().then(reg => reg ? reg.pushManager.getSubscription() : null).then(sub => { pushHere = !!sub; }).catch(() => { pushHere = false; });
  } else pushHere = false;

  function alertText(ev) {
    const d = ev.d || {};
    if (ev.t === 'approval.request') {
      const inp = d.input || {};
      if (d.kind === 'elicitation') return 'needs input for ' + (d.server || 'an MCP server');
      if (d.kind === 'question') return 'has a question' + (d.questions && d.questions[0] ? ': ' + d.questions[0].question : '');
      if (d.kind === 'plan') return 'wants approval for a plan';
      if (d.kind !== 'tool') return '';
      const det = inp.command || inp.file_path || inp.description || d.description || '';
      return 'needs permission for ' + (d.display || d.tool) + (det ? ': ' + String(det).split('\n')[0].slice(0, 80) : '');
    }
    if (ev.t === 'turn.end') return d.ok === false ? 'stopped on an error' : 'finished a turn' + (d.result ? ': ' + String(d.result).split('\n')[0].slice(0, 100) : '');
    if (ev.t === 'session.exit' && d.code) return 'exited with code ' + d.code;
    if (ev.t === 'session.spawn_error') return "couldn't start";
    if (ev.t === 'session.resume_failed') return "couldn't resume the conversation";
    return '';
  }
  function tabAlert(ev) {
    if (!hydrated || tabFocused() || pushHere !== false) return;
    if (!('Notification' in window) || Notification.permission !== 'granted') return;
    const text = alertText(ev);
    if (!text) return;
    const title = (stage.querySelector('[data-chat-rename]') || {}).textContent || 'Agent session';
    const opts = { body: agentName + ' ' + text, tag: 'session-' + sessionID, icon: '/static/icon-192.png' };
    try {
      const reg = navigator.serviceWorker && navigator.serviceWorker.controller ? navigator.serviceWorker.ready : null;
      if (reg) reg.then(r => r.showNotification(title.trim(), opts)).catch(() => {});
      else { const n = new Notification(title.trim(), opts); n.onclick = () => { window.focus(); n.close(); }; }
    } catch (_) {}
  }
  function maybeAskNotify() {
    if (!('Notification' in window) || Notification.permission !== 'default' || pushHere) return;
    let asked = false;
    try { asked = localStorage.getItem('acta.notify.asked') === '1'; } catch (_) {}
    if (asked) return;
    try { localStorage.setItem('acta.notify.asked', '1'); } catch (_) {}
    Notification.requestPermission().catch(() => {});
  }

  function send(text, images) {
    maybeAskNotify();
    if (ws && ws.readyState === WebSocket.OPEN) {
      const m = { t: 'input', text };
      if (images && images.length) m.images = images;
      ws.send(JSON.stringify(m));
    }
  }

  // --- image attachments ---
  const attachRow = stage.querySelector('[data-chat-attach]');
  const attachFile = stage.querySelector('[data-attach-file]');
  const attachBtn = stage.querySelector('[data-attach-btn]');
  const lightbox = stage.querySelector('[data-lightbox]');
  const pendingImages = [];

  function openLightbox(src) { if (!lightbox) return; lightbox.querySelector('img').src = src; lightbox.hidden = false; }
  if (lightbox) lightbox.addEventListener('click', () => { lightbox.hidden = true; lightbox.querySelector('img').src = ''; });
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && lightbox && !lightbox.hidden) { lightbox.hidden = true; } });

  function refreshAttach() { if (attachRow) attachRow.hidden = !pendingImages.length; }

  function shrinkImage(file) {
    return new Promise((resolve, reject) => {
      const url = URL.createObjectURL(file);
      const img = new Image();
      img.onload = () => {
        const MAX = 1568;
        const scale = Math.min(1, MAX / Math.max(img.naturalWidth, img.naturalHeight));
        const keepType = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(file.type);
        if (scale === 1 && keepType && file.size <= 3 << 20) {
          const r = new FileReader();
          r.onload = () => { URL.revokeObjectURL(url); resolve({ media_type: file.type, data: String(r.result).split(',')[1] }); };
          r.onerror = reject;
          r.readAsDataURL(file);
          return;
        }
        const c = document.createElement('canvas');
        c.width = Math.round(img.naturalWidth * scale); c.height = Math.round(img.naturalHeight * scale);
        c.getContext('2d').drawImage(img, 0, 0, c.width, c.height);
        const png = file.type === 'image/png';
        const out = c.toDataURL(png ? 'image/png' : 'image/jpeg', 0.88);
        URL.revokeObjectURL(url);
        resolve({ media_type: png ? 'image/png' : 'image/jpeg', data: out.split(',')[1] });
      };
      img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('not an image')); };
      img.src = url;
    });
  }
  function addImage(file) {
    if (!file || !/^image\//.test(file.type) || !attachRow) return;
    if (pendingImages.length >= 8) return;
    const chip = el('div', 'attach-chip is-busy');
    const img = document.createElement('img');
    img.src = URL.createObjectURL(file);
    chip.appendChild(img);
    const x = el('button', 'attach-x', '×');
    x.type = 'button'; x.title = 'Remove';
    chip.appendChild(x);
    attachRow.appendChild(chip);
    const entry = { chip, media_type: '', data: '' };
    pendingImages.push(entry);
    refreshAttach();
    x.addEventListener('click', () => { const i = pendingImages.indexOf(entry); if (i >= 0) pendingImages.splice(i, 1); chip.remove(); refreshAttach(); });
    shrinkImage(file).then(r => { entry.media_type = r.media_type; entry.data = r.data; chip.classList.remove('is-busy'); })
      .catch(() => { const i = pendingImages.indexOf(entry); if (i >= 0) pendingImages.splice(i, 1); chip.remove(); refreshAttach(); });
  }
  if (attachBtn && attachFile) {
    attachBtn.addEventListener('click', () => attachFile.click());
    attachFile.addEventListener('change', () => { for (const f of attachFile.files) addImage(f); attachFile.value = ''; });
  }
  box.addEventListener('paste', (e) => {
    const files = [...(e.clipboardData && e.clipboardData.files || [])].filter(f => /^image\//.test(f.type));
    if (!files.length) return;
    e.preventDefault();
    for (const f of files) addImage(f);
  });
  form.addEventListener('dragover', (e) => { if ([...e.dataTransfer.types].includes('Files')) { e.preventDefault(); form.classList.add('is-dragover'); } });
  form.addEventListener('dragleave', () => form.classList.remove('is-dragover'));
  form.addEventListener('drop', (e) => { e.preventDefault(); form.classList.remove('is-dragover'); for (const f of e.dataTransfer.files) addImage(f); });

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    const text = box.value.trim();
    const ready = pendingImages.filter(i => i.data);
    if (!text && !ready.length) return;
    if (pendingImages.some(i => !i.data)) return;
    slashClose();
    const rn = /^\/(?:rename|name)(?:\s+([\s\S]*))?$/.exec(text);
    if (rn && !ready.length) {
      const title = (rn[1] || '').trim();
      box.value = ''; box.style.height = 'auto'; hintRefresh();
      if (title) renameTo(title); else if (openRename) openRename();
      return;
    }
    send(text, ready.map(i => ({ media_type: i.media_type, data: i.data })));
    for (const i of pendingImages) i.chip.remove();
    pendingImages.length = 0;
    refreshAttach();
    box.value = '';
    box.style.height = 'auto';
    slashRefresh();
  });
  box.addEventListener('keydown', (e) => {
    if (slashKey(e)) return;
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); form.requestSubmit(); }
  });
  box.addEventListener('input', () => {
    box.style.height = 'auto';
    box.style.height = Math.min(box.scrollHeight, 200) + 'px';
    slashRefresh();
  });
  box.addEventListener('blur', () => setTimeout(() => { if (document.activeElement !== box && !slashMenu.contains(document.activeElement)) slashClose(); }, 120));
  box.addEventListener('focus', slashRefresh);

  // --- "/" command menu ---
  const slashMenu = stage.querySelector('[data-slash-menu]');
  const hintEl = stage.querySelector('.chat-hint');
  const HINT_DEFAULT = hintEl ? hintEl.textContent : '';
  const HIDDEN_CMDS = new Set(['context', 'usage', 'cost', 'stats', 'model', 'effort', 'fast', 'autocompact', 'color', 'agents', 'extra-usage', '__remote-workflow', 'workflow-launch-exec', 'heapdump']);
  const ACTA_CMDS = backend === 'codex' ? [
    { cmd: 'compact', description: 'Free up context by summarising the conversation so far', argumentHint: '' },
    { cmd: 'rename', description: 'Rename this session, here and in Codex', argumentHint: '<name>' },
    { cmd: 'goal', description: 'Set a goal: Codex keeps working until the objective is met', argumentHint: '<objective> | clear' },
  ] : [
    { cmd: 'clear', description: 'Start over with an empty context; what came before folds away', argumentHint: '' },
    { cmd: 'compact', description: 'Free up context by summarising the conversation so far', argumentHint: '[instructions for the summary]' },
    { cmd: 'recap', description: 'A one-line recap of the session so far', argumentHint: '' },
    { cmd: 'rename', description: 'Rename this session, here and in the backend', argumentHint: '<name>' },
    { cmd: 'goal', description: 'Set a goal: the agent keeps working until the condition is met', argumentHint: '<condition> | clear' },
    { cmd: 'config', description: 'Set a Claude Code setting by key', argumentHint: 'key=value' },
  ];
  const CMD_NOTES = { insights: ' (analyses every session on the harness host: slow and costly)' };
  const CONFIG_KEYS = {
    agentPushNotifEnabled: 'true|false', autoCompact: 'true|false', autoConnectIde: 'true|false', autoScroll: 'true|false', checkpoints: 'true|false', chrome: 'true|false', copyFullResponse: 'true|false', copyOnSelect: 'true|false', defaultToAgentsView: 'true|false', editor: 'normal|vim', externalEditorContext: 'true|false', gitignore: 'true|false', inputNeededNotifEnabled: 'true|false', language: '<value>', leftArrowOpensAgents: 'true|false', model: 'default|sonnet|opus|haiku|fable|best|sonnet[1m]|opus[1m]|fable[1m]|opusplan', notifChannel: 'auto|iterm2|terminal_bell|iterm2_with_bell|kitty|ghostty|notifications_disabled', outputStyle: '<value>', permissionMode: 'default|plan|acceptEdits|auto|dontAsk', prStatus: 'true|false', progressBar: 'true|false', promptSuggestionEnabled: 'true|false', recap: 'true|false', reduceMotion: 'true|false', remoteControl: 'true|false|default', switchModelsOnFlag: 'true|false', theme: 'auto|dark|light|light-daltonized|dark-daltonized|light-ansi|dark-ansi', thinking: 'true|false', timeFormat: 'auto|12-hour|24-hour|24-hour-utc', tips: 'true|false', turnDuration: 'true|false', useAutoModeDuringPlan: 'true|false', verbose: 'true|false',
  };
  let slashCatalog = [];
  let slashRows = [];
  let slashIdx = 0;
  let slashLastQuery = null;
  let slashDismissed = null;
  let slashPassive = false;

  function noteCommands(list) {
    const out = [];
    for (const c of list) {
      if (!c || !c.name) continue;
      let name = c.name, cmd = c.name, mcp = false;
      const m = /^([\w-]+):([\w-]+) \(MCP\)$/.exec(c.name);
      if (m) { cmd = 'mcp__' + m[1] + '__' + m[2]; name = m[1] + ':' + m[2]; mcp = true; }
      if (HIDDEN_CMDS.has(cmd)) continue;
      out.push({ name, cmd, description: (c.description || '') + (CMD_NOTES[cmd] || ''), argumentHint: c.argumentHint || '', aliases: Array.isArray(c.aliases) ? c.aliases : [], mcp });
    }
    for (const a of ACTA_CMDS) {
      const hit = out.find(o => o.cmd === a.cmd);
      if (hit) { hit.description = a.description; hit.argumentHint = a.argumentHint; hit.acta = true; }
      else out.push({ name: a.cmd, cmd: a.cmd, description: a.description, argumentHint: a.argumentHint, aliases: [], mcp: false, acta: true });
    }
    const rank = c => c.acta ? ACTA_CMDS.findIndex(a => a.cmd === c.cmd) : 100;
    out.sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
    slashCatalog = out;
    slashRefresh();
  }
  function slashCommands() {
    return slashCatalog.length ? slashCatalog : ACTA_CMDS.map(a => ({ name: a.cmd, cmd: a.cmd, description: a.description, argumentHint: a.argumentHint, aliases: [], mcp: false, acta: true }));
  }
  function slashParse(v) {
    const m = /^\/([\w:-]*)(\s([\s\S]*))?$/.exec(v);
    return m ? { cmd: m[1], args: m[3] || '', atArgs: m[2] != null } : null;
  }
  function slashFind(cmd) {
    const c = cmd.toLowerCase();
    return slashCommands().find(x => x.cmd.toLowerCase() === c || x.name.toLowerCase() === c || x.aliases.some(a => a.toLowerCase() === c)) || null;
  }
  function slashOptions(v) {
    const sp = slashParse(v);
    if (!sp) return { rows: [], hint: null };
    if (!sp.atArgs) {
      const q = sp.cmd.toLowerCase();
      const rows = slashCommands().filter(c => !q || c.cmd.toLowerCase().startsWith(q) || c.name.toLowerCase().startsWith(q) || c.aliases.some(a => a.toLowerCase().startsWith(q)) || c.cmd.toLowerCase().includes(q))
        .map(c => ({ kind: 'cmd', c, label: '/' + c.name, hint: c.argumentHint, desc: c.description + (c.aliases.length ? ' · also /' + c.aliases.join(', /') : '') }));
      return { rows, hint: null, query: 'c:' + q };
    }
    const c = slashFind(sp.cmd);
    if (!c) return { rows: [], hint: null };
    const hint = { cmd: '/' + c.name + (c.argumentHint ? ' ' + c.argumentHint : ''), desc: c.description };
    let rows = [];
    const tok = sp.args.split(/\s+/).pop() || '';
    if (c.cmd === 'config') {
      const eq = tok.indexOf('=');
      if (eq < 0) rows = Object.keys(CONFIG_KEYS).filter(k => k.toLowerCase().startsWith(tok.toLowerCase())).map(k => ({ kind: 'arg', insert: k + '=', label: k, hint: CONFIG_KEYS[k], desc: '' }));
      else {
        const key = tok.slice(0, eq), val = tok.slice(eq + 1), vals = CONFIG_KEYS[key];
        if (vals && vals !== '<value>') rows = vals.split('|').filter(x => x.toLowerCase().startsWith(val.toLowerCase())).map(x => ({ kind: 'arg', insert: key + '=' + x, label: key + '=' + x, hint: '', desc: '', done: true }));
        else if (key === 'outputStyle') rows = (styleCatalog.length ? styleCatalog.map(x => x.name) : ['default']).filter(x => x.toLowerCase().startsWith(val.toLowerCase())).map(x => ({ kind: 'arg', insert: key + '=' + x, label: key + '=' + x, hint: '', desc: '', done: true }));
      }
    } else if (c.cmd === 'goal' && !sp.args.trim()) {
      rows = [{ kind: 'arg', insert: 'clear', label: 'clear', hint: '', desc: 'Drop the current goal', done: true }];
      hint.desc = 'Send with no condition to see the goal\'s status';
    }
    return { rows, hint, query: 'a:' + c.cmd + ':' + tok, passive: tok === '' };
  }
  function hintRefresh(hint) {
    if (!hintEl) return;
    hintEl.textContent = '';
    hintEl.classList.toggle('is-cmd', !!hint);
    if (!hint) { hintEl.textContent = HINT_DEFAULT; return; }
    hintEl.appendChild(el('code', null, hint.cmd));
    if (hint.desc) hintEl.appendChild(el('span', null, ' — ' + hint.desc));
    hintEl.title = hint.cmd + (hint.desc ? ' — ' + hint.desc : '');
  }
  function slashClose() { if (slashMenu) { slashMenu.hidden = true; slashMenu.textContent = ''; } slashRows = []; }
  function slashRefresh() {
    if (!slashMenu) return;
    const v = box.value;
    if (!v.startsWith('/') || v.includes('\n')) { slashClose(); hintRefresh(null); slashDismissed = null; return; }
    const o = slashOptions(v);
    hintRefresh(o.hint);
    if (!o.rows.length || slashDismissed === v) { slashClose(); return; }
    slashPassive = !!o.passive;
    if (o.query !== slashLastQuery) { slashIdx = 0; slashLastQuery = o.query; }
    slashRows = o.rows;
    slashIdx = Math.min(slashIdx, slashRows.length - 1);
    slashMenu.textContent = '';
    slashRows.forEach((r, i) => {
      const b = el('button', 'slash-item' + (i === slashIdx ? ' is-active' : ''));
      b.type = 'button'; b.setAttribute('role', 'option');
      b.appendChild(el('span', 'slash-name', r.label));
      b.appendChild(el('span', 'slash-hint', r.hint || ''));
      if (r.desc) b.appendChild(el('span', 'slash-desc', r.desc));
      b.title = r.desc || r.label;
      b.addEventListener('mousedown', (e) => e.preventDefault());
      b.addEventListener('click', () => slashPick(r, false));
      slashMenu.appendChild(b);
    });
    slashMenu.hidden = false;
    const act = slashMenu.children[slashIdx];
    if (act && act.scrollIntoView) act.scrollIntoView({ block: 'nearest' });
  }
  function slashPick(r, complete) {
    if (r.kind === 'cmd') {
      box.value = '/' + r.c.cmd + ' ';
      if (!r.c.argumentHint && !complete) { box.value = '/' + r.c.cmd; slashClose(); form.requestSubmit(); return; }
    } else {
      const sp = slashParse(box.value);
      const parts = sp.args.split(/\s+/);
      parts[parts.length - 1] = r.insert;
      box.value = '/' + sp.cmd + ' ' + parts.join(' ') + (r.done ? ' ' : '');
    }
    slashDismissed = null;
    box.focus();
    box.setSelectionRange(box.value.length, box.value.length);
    box.dispatchEvent(new Event('input'));
  }
  function slashKey(e) {
    if (!slashMenu || slashMenu.hidden || !slashRows.length) return false;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      slashIdx = (slashIdx + (e.key === 'ArrowDown' ? 1 : slashRows.length - 1)) % slashRows.length;
      slashRefresh();
      return true;
    }
    if (e.key === 'Tab') { e.preventDefault(); slashPick(slashRows[slashIdx], true); return true; }
    if (e.key === 'Enter' && !e.shiftKey) {
      const r = slashRows[slashIdx];
      if (slashPassive || (r.kind === 'cmd' && box.value.trim().toLowerCase() === ('/' + r.c.cmd).toLowerCase())) { slashClose(); return false; }
      e.preventDefault(); slashPick(r, false); return true;
    }
    if (e.key === 'Escape') { e.preventDefault(); slashDismissed = box.value; slashClose(); return true; }
    return false;
  }

  // --- turn diff: pill + aside ---
  //
  // A backend that keeps an aggregate diff of what the turn changed (Codex)
  // pushes it as turn.diff; the pill opens it in a side panel, rendered like
  // any other diff.
  const diffPill = stage.querySelector('[data-diff-pill]');
  const diffPanel = document.querySelector('[data-diff-panel]');
  let turnDiff = '';
  function paintDiffPanel() {
    if (!diffPanel || diffPanel.hidden) return;
    const body = diffPanel.querySelector('[data-diff-body]');
    body.textContent = '';
    if (!turnDiff) { body.appendChild(el('div', 'plan-empty', 'No changes yet.')); return; }
    const files = turnDiff.split(/^(?=diff --git )/m).filter(s => s.trim());
    for (const f of files) {
      const m = /^diff --git a\/(\S+) b\/(\S+)/.exec(f);
      body.appendChild(diffBlock({ kind: 'unified', file: m ? m[2] : '', text: f }));
    }
    diffPanel.querySelector('[data-diff-status]').textContent = files.length + (files.length === 1 ? ' file' : ' files');
  }
  function openDiff() { if (!diffPanel) return; diffPanel.hidden = false; if (planBackdrop) planBackdrop.hidden = false; paintDiffPanel(); diffPill.classList.add('is-open'); }
  function closeDiff() { if (!diffPanel) return; diffPanel.hidden = true; if (planBackdrop && (!planPanel || planPanel.hidden)) planBackdrop.hidden = true; diffPill.classList.remove('is-open'); }
  if (diffPill && diffPanel) {
    diffPill.addEventListener('click', () => { if (diffPanel.hidden) openDiff(); else closeDiff(); });
    diffPanel.querySelector('[data-diff-close]').addEventListener('click', closeDiff);
    if (planBackdrop) planBackdrop.addEventListener('click', closeDiff);
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !diffPanel.hidden) closeDiff(); });
  }
  function renderTurnDiff(ev) {
    const d = ev.d || {};
    turnDiff = d.text || '';
    if (diffPill) {
      const files = (turnDiff.match(/^diff --git /gm) || []).length;
      diffPill.hidden = !turnDiff;
      diffPill.querySelector('[data-diff-pill-text]').textContent = 'Diff · ' + files + (files === 1 ? ' file' : ' files');
    }
    paintDiffPanel();
    return foldIntoLast(ev, 'diff');
  }

  // --- goal pill + popover ---
  const goalPill = stage.querySelector('[data-goal-pill]');
  const goalPop = stage.querySelector('[data-goal-pop]');
  function paintGoal() {
    if (!goalPill) return;
    goalPill.hidden = !goal;
    if (!goal) { if (goalPop) goalPop.hidden = true; goalPill.setAttribute('aria-expanded', 'false'); return; }
    goalPill.classList.toggle('is-met', goal.state === 'met');
    goalPill.classList.toggle('is-unmet', goal.state === 'unmet');
    goalPill.querySelector('[data-goal-pill-text]').textContent = goal.state === 'met' ? 'Goal met' : goal.state === 'unmet' ? 'Goal not met' : 'Goal' + (goal.turns ? ' · ' + goal.turns : '');
    goalPill.title = goal.cond || 'Goal';
    if (!goalPop) return;
    goalPop.querySelector('[data-goal-cond]').textContent = goal.cond || '(condition not reported)';
    const st = goalPop.querySelector('[data-goal-status]');
    st.textContent = goal.state === 'met' ? 'met · the turn ended once this held' : goal.state === 'unmet' ? 'not met · the agent stopped holding the turn open' : 'active' + (goal.turns ? ' · ' + goal.turns + (goal.turns === 1 ? ' turn' : ' turns') : '') + ' · the agent keeps working until this holds';
    st.classList.toggle('is-met', goal.state === 'met');
    st.classList.toggle('is-unmet', goal.state === 'unmet');
    const last = goalPop.querySelector('[data-goal-last]');
    last.hidden = !goal.last;
    last.textContent = goal.last ? 'Last check: ' + goal.last : '';
    goalPop.querySelector('[data-goal-clear]').hidden = goal.state !== 'active';
    goalPop.querySelector('[data-goal-check]').hidden = goal.state !== 'active';
  }
  if (goalPill && goalPop) {
    const openGoal = () => { paintGoal(); goalPop.hidden = false; goalPill.classList.add('is-open'); goalPill.setAttribute('aria-expanded', 'true'); };
    const closeGoal = () => { goalPop.hidden = true; goalPill.classList.remove('is-open'); goalPill.setAttribute('aria-expanded', 'false'); };
    goalPill.addEventListener('click', () => { if (goalPop.hidden) openGoal(); else closeGoal(); });
    goalPop.querySelector('[data-goal-check]').addEventListener('click', () => { queueCmd('/goal'); closeGoal(); });
    goalPop.querySelector('[data-goal-clear]').addEventListener('click', () => { queueCmd('/goal clear'); closeGoal(); });
    document.addEventListener('click', (e) => { if (!goalPop.hidden && !goalPop.contains(e.target) && !goalPill.contains(e.target)) closeGoal(); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !goalPop.hidden) closeGoal(); });
  }

  // --- context panel under the gauges ---
  const gaugePop = stage.querySelector('[data-gauge-pop]');
  const gaugeWrap = stage.querySelector('[data-gauges]');
  const cmdQueue = [];
  let queuedNow = false;
  function queueCmd(text) {
    if (!cmdQueue.includes(text)) cmdQueue.push(text);
    runQueuedCmd();
  }
  function runQueuedCmd() {
    if (!cmdQueue.length || mainTurnActive || queuedNow) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    queuedNow = true;
    send(cmdQueue.shift());
    setTimeout(() => { queuedNow = false; runQueuedCmd(); }, 400);
  }
  function fmtAgo(at) {
    const s = Math.max(0, Math.round((Date.now() - at) / 1000));
    return s < 5 ? 'just now' : s < 60 ? s + 's ago' : s < 3600 ? Math.round(s / 60) + 'm ago' : Math.round(s / 3600) + 'h ago';
  }
  function paintGaugePop() {
    if (!gaugePop || gaugePop.hidden) return;
    const win = contextWindow || 200000;
    gaugePop.querySelector('[data-gpop-ctx]').textContent = contextUsed ? contextUsed.toLocaleString() + ' of ' + win.toLocaleString() + ' tokens (' + fmtPct(contextUsed / win) + ')' : 'no turn yet';
    const ctx = gaugePop.querySelector('[data-gpop-context]');
    ctx.textContent = '';
    if (reports.context) ctx.appendChild(mdRender(reports.context.text));
    else ctx.appendChild(el('div', 'pick-note', mainTurnActive ? 'Reports when the turn ends.' : 'No report yet.'));
    const us = gaugePop.querySelector('[data-gpop-usage]');
    us.textContent = reports.usage ? reports.usage.text : '';
    if (!reports.usage) us.appendChild(el('div', 'pick-note', mainTurnActive ? 'Reports when the turn ends.' : 'No report yet.'));
    const on = gaugePop.querySelector('[data-gpop-ac-on]');
    on.classList.toggle('is-on', ac.enabled !== false);
    gaugePop.querySelector('[data-gpop-ac-note]').textContent = ac.enabled === false ? 'off: the window fills until you compact by hand' : 'summarises automatically when the window fills';
    gaugePop.querySelector('[data-gpop-ac]').textContent = ac.window ? 'window ' + ac.window : '';
    const auto = gaugePop.querySelector('[data-gpop-ac-auto]');
    auto.classList.toggle('is-current', ac.window === 'auto' || !ac.window);
    const num = gaugePop.querySelector('[data-gpop-ac-num]');
    if (document.activeElement !== num) num.value = ac.window && ac.window !== 'auto' ? ac.window.replace(/k$/i, '000').replace(/[^\d]/g, '') : '';
    const note = gaugePop.querySelector('[data-gpop-note]');
    const at = Math.max(reports.context ? reports.context.at : 0, reports.usage ? reports.usage.at : 0);
    note.textContent = backend !== 'claude-code' ? agentName + ' reports usage after every turn' : mainTurnActive ? 'turn in progress · refreshes when it ends' : cmdQueue.length || queuedNow ? 'refreshing…' : !procAlive ? 'session not running · refresh starts it' : at ? 'as of ' + fmtAgo(at) : '';
    for (const sec of gaugePop.querySelectorAll('.gpop-sec')) { if (backend !== 'claude-code' && !sec.querySelector('[data-gpop-ctx]')) sec.hidden = true; }
    if (backend !== 'claude-code') { const c = gaugePop.querySelector('[data-gpop-context]'); c.textContent = ''; c.appendChild(el('div', 'pick-note', contextUsed ? 'Last request: ' + contextUsed.toLocaleString() + ' tokens of ' + win.toLocaleString() : 'No turn yet.')); }
    const rf = gaugePop.querySelector('[data-gpop-refresh]');
    rf.classList.toggle('is-busy', !!(cmdQueue.length || queuedNow));
  }
  function gaugeRefresh(force) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    if (backend !== 'claude-code') { paintGaugePop(); return; } // other backends report usage after every turn
    if (!procAlive && !force) { paintGaugePop(); return; }
    const stale = k => !reports[k] || reports[k].at < lastTurnEnd || force;
    if (stale('context')) queueCmd('/context');
    if (stale('usage')) queueCmd('/usage');
    if (!reports.autocompact || force) queueCmd('/autocompact');
    paintGaugePop();
  }
  function openGaugePop() {
    if (!gaugePop) return;
    gaugePop.hidden = false;
    for (const g of gaugeWrap.querySelectorAll('.gauge')) g.classList.add('is-open');
    paintGaugePop();
    gaugeRefresh(false);
  }
  function closeGaugePop() {
    if (!gaugePop) return;
    gaugePop.hidden = true;
    for (const g of gaugeWrap.querySelectorAll('.gauge')) g.classList.remove('is-open');
  }
  if (gaugePop && gaugeWrap) {
    for (const g of gaugeWrap.querySelectorAll('.gauge')) {
      g.setAttribute('role', 'button'); g.tabIndex = 0;
      g.addEventListener('click', () => { if (gaugePop.hidden) openGaugePop(); else closeGaugePop(); });
      g.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); g.click(); } });
    }
    gaugePop.querySelector('[data-gpop-refresh]').addEventListener('click', () => gaugeRefresh(true));
    gaugePop.querySelector('[data-gpop-compact]').addEventListener('click', () => { queueCmd('/compact'); closeGaugePop(); });
    gaugePop.querySelector('[data-gpop-ac-on]').addEventListener('click', () => { queueCmd('/config autoCompact=' + (ac.enabled === false ? 'true' : 'false')); });
    gaugePop.querySelector('[data-gpop-ac-auto]').addEventListener('click', () => { if (ac.window !== 'auto') queueCmd('/autocompact auto'); });
    const setWin = () => {
      const n = parseInt(gaugePop.querySelector('[data-gpop-ac-num]').value.replace(/[^\d]/g, ''), 10);
      if (n > 0) queueCmd('/autocompact ' + n);
    };
    gaugePop.querySelector('[data-gpop-ac-set]').addEventListener('click', setWin);
    gaugePop.querySelector('[data-gpop-ac-num]').addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); setWin(); } });
    document.addEventListener('click', (e) => { if (!gaugePop.hidden && !gaugeWrap.contains(e.target)) closeGaugePop(); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !gaugePop.hidden) closeGaugePop(); });
  }
  if (stopBtn) stopBtn.addEventListener('click', () => {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'stop' }));
  });

  if (window.matchMedia && window.matchMedia('(hover: none)').matches) {
    stage.addEventListener('click', (e) => {
      if (!e.target.closest('.chat-log')) return;
      if (e.target.closest('button, a, input, summary, .frame-json, .msg-agent')) return;
      const f = e.target.closest('.frame');
      if (f) f.classList.toggle('show-tools');
    });
  }

  window.addEventListener('hashchange', () => {
    const m = /agent=([^&]+)/.exec(location.hash || '');
    showLane(m ? decodeURIComponent(m[1]) : 'main');
  });

  // --- storage dialog ---
  //
  // The size in the header opens it: what each prune category would save
  // (measured by the server on open), and a re-read from the harness.
  (function storage() {
    const modal = document.querySelector('[data-storage-modal]');
    const open = document.querySelector('[data-storage-open]');
    if (!modal || !open) return;
    const cats = modal.querySelector('[data-storage-cats]');
    const submit = modal.querySelector('[data-storage-submit]');
    const total = modal.querySelector('[data-storage-total]');
    const harness = modal.querySelector('[data-storage-harness]');
    const rereadBtn = modal.querySelector('[data-storage-reread-btn]');
    const rereadNote = modal.querySelector('[data-storage-reread-note]');
    const fmt = (n) => n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : n >= 1024 ? Math.round(n / 1024) + ' KB' : n + ' B';
    function count() { submit.disabled = !modal.querySelector('input[name="cat"]:checked'); }
    async function load() {
      cats.textContent = '';
      cats.appendChild(el('div', 'import-empty', 'Measuring…'));
      let j = null;
      try { const r = await fetch('/account/sessions/' + encodeURIComponent(stage.dataset.session) + '/storage', { credentials: 'same-origin' }); j = await r.json(); } catch (_) {}
      cats.textContent = '';
      if (!j) { cats.appendChild(el('div', 'import-empty', 'Could not measure the transcript.')); return; }
      total.textContent = fmt(j.bytes || 0) + ' before compression';
      for (const c of j.categories || []) {
        const lab = el('label', 'storage-cat');
        const cb = el('input'); cb.type = 'checkbox'; cb.name = 'cat'; cb.value = c.id; cb.disabled = !c.bytes;
        cb.addEventListener('change', count);
        const main = el('span', 'storage-cat-main');
        main.appendChild(el('span', null, c.label));
        main.appendChild(el('span', 'storage-cat-note', c.note));
        lab.append(cb, main, el('span', 'storage-cat-bytes', c.bytes ? fmt(c.bytes) + ' · ' + c.frames + (c.frames === 1 ? ' frame' : ' frames') : 'nothing'));
        cats.appendChild(lab);
      }
      count();
      if (j.harness) { harness.textContent = j.harness; rereadBtn.disabled = !!j.running; if (j.running) rereadNote.textContent = 'The session is running there; stop it before re-reading.'; }
      else { harness.textContent = 'a harness'; rereadBtn.disabled = true; rereadNote.textContent = 'No harness is connected. Run acta harness on the machine that holds this transcript.'; }
    }
    open.addEventListener('click', () => { modal.hidden = false; load(); });
    for (const b of modal.querySelectorAll('[data-storage-close]')) b.addEventListener('click', () => { modal.hidden = true; });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !modal.hidden) modal.hidden = true; });
    modal.querySelector('[data-storage-prune]').addEventListener('submit', () => { submit.disabled = true; submit.textContent = 'Pruning…'; });
    modal.querySelector('[data-storage-reimport]').addEventListener('submit', (e) => {
      if (!confirm('Replace everything Acta holds for this session with a fresh read of the transcript on ' + harness.textContent + '?')) { e.preventDefault(); return; }
      rereadBtn.disabled = true; rereadBtn.textContent = 'Reading…';
    });
  })();

  hydrate();
  connect();
})();
