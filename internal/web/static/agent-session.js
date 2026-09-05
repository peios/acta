// agent-session.js — the browser chat client for an Acta agent session. It
// renders the transcript (server-rendered frames already in the DOM, then live
// frames over a websocket) and sends user input back. No framework, vanilla ES.
//
// Rendering rule (per ACT-36): known frame kinds get a friendly view, and EVERY
// frame also carries its verbatim JSON behind a "raw" toggle (shown on hover),
// so nothing sent over the wire is hidden — anything the pretty view doesn't
// fully use is still there to read.
(() => {
  'use strict';

  const stage = document.querySelector('.chat-stage');
  if (!stage) return;
  const sessionID = stage.dataset.session;
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
  function atBottom(l) {
    return l.scrollHeight - l.scrollTop - l.clientHeight < 60;
  }
  function scroll(l) { l.scrollTop = l.scrollHeight; }

  // --- header gauges ---
  //
  // Three rings: weekly and 5h come straight off Claude Code's per-response
  // rate_limit_event; context is derived — the last assistant message's prompt
  // size (input + cache read + cache creation tokens) over the model's
  // contextWindow reported in the result frame's modelUsage.

  const CIRC = 2 * Math.PI * 18; // r=18 in the 44px viewBox
  let contextWindow = 0;
  let contextUsed = 0;
  // Weekly utilisation now, and where it stood when the current turn began, so
  // a result frame can say how much of the week this turn consumed. Claude Code
  // reports utilisation in whole percents, so most turns read as "<1%".
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
    // Inline style, not the attribute: a stylesheet rule would win over a
    // presentation attribute and pin the arc at zero.
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
  function limitTip(label, win, info) {
    const parts = [label + ' limit: ' + fmtPct(win.utilization) + ' used', fmtReset(win.resetsAt)];
    if (info.status && info.status !== 'allowed') parts.push('status: ' + info.status);
    if (info.overageStatus) parts.push('overage ' + info.overageStatus + (info.overageDisabledReason ? ' (' + info.overageDisabledReason.replace(/_/g, ' ') + ')' : ''));
    return parts.filter(Boolean).join('\n');
  }
  function noteRateLimit(p) {
    const info = p.rate_limit_info || {};
    const w = info.unifiedWindows || {};
    if (w.seven_day && w.seven_day.utilization != null) {
      weeklyNow = w.seven_day.utilization;
      if (weeklyAtTurnStart == null) weeklyAtTurnStart = weeklyNow;
      setGauge('weekly', weeklyNow, fmtPct(weeklyNow), null, limitTip('Weekly', w.seven_day, info));
    }
    if (w.five_hour && w.five_hour.utilization != null) {
      setGauge('fivehour', w.five_hour.utilization, fmtPct(w.five_hour.utilization), null, limitTip('5-hour', w.five_hour, info));
    }
  }
  function noteAssistant(p) {
    if (p.message && p.message.model && !cur.model && !/^</.test(p.message.model)) { cur.model = p.message.model; if (cur.card) laneHeaderRefresh(cur); }
    const u = p.message && p.message.usage;
    if (!u || p.is_meta || (p.message && p.message.model === '<synthetic>')) return;
    const used = (u.input_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0);
    if (!used) return;
    cur.ctxUsed = used;
    if (cur === visible) { contextUsed = cur.ctxUsed; drawContext(); }
  }
  function noteResult(p) {
    const mu = p.modelUsage || {};
    for (const k in mu) { if (mu[k] && mu[k].contextWindow) { contextWindow = mu[k].contextWindow; break; } }
    drawContext();
  }
  function noteCompact(p) {
    const m = p.compact_metadata;
    if (m && typeof m.post_tokens === 'number') { contextUsed = m.post_tokens; drawContext(); }
  }
  function drawContext() {
    if (!contextUsed) return;
    const win = contextWindow || 200000;
    setGauge('context', contextUsed / win, fmtPct(contextUsed / win), fmtTokens(contextUsed) + ' / ' + fmtTokens(win),
      'Context: ' + contextUsed.toLocaleString() + ' of ' + win.toLocaleString() + ' tokens in the window');
  }

  // --- activity line + thinking ---
  //
  // Claude Code redacts thinking text in stream-json (the block arrives with a
  // signature and an empty body), but it streams `thinking_tokens` estimates
  // while it thinks. So: a live activity line at the foot of the log shows
  // "Thinking · N tokens" (then "Running <tool>"), and when the thought lands
  // it collapses into a quiet chip in the transcript.

  // --- lanes ---
  //
  // One lane per agent: "main" is the session's own transcript; every Agent
  // call the model makes opens a lane keyed by that call's tool_use_id, and
  // each frame the subagent produces (parent_tool_use_id set) renders into
  // that lane with the same renderers. Lanes stay in the DOM; the tab strip
  // swaps which one is visible. A lane owns its activity line, thinking
  // state and context figure.

  const lanes = new Map();   // id -> lane
  let cur = null;            // lane the frame being rendered belongs to
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
      card: null, tab: null, steps: 0 };
    lanes.set(id, lane);
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
  function placeActivity(lane) { const l = lane || cur; l.log.appendChild(l.activity); }

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

  function noteActivity(kind, p, at) {
    if (kind === 'input') {
      // a message sent mid-turn is a steer: the grey bubble already says it is
      // waiting, so leave the running activity ("Running Bash…") in place
      if (!mainTurnActive) setActivity('Waiting for the session');
      mainTurnActive = true;
      return;
    }
    if (kind === 'system' && p && p.subtype === 'thinking_tokens') {
      if (!cur.think) cur.think = { start: ts(at), tokens: 0, last: null, frames: 0 };
      cur.think.tokens = p.estimated_tokens || cur.think.tokens;
      cur.think.last = p;
      cur.think.frames = (cur.think.frames || 0) + 1;
      setActivity('Thinking · ' + cur.think.tokens.toLocaleString() + ' tokens');
      if (hydrated) liveThought(cur);
      return;
    }
    if (kind === 'assistant' && p && p.message) {
      const c = p.message.content || [];
      const tool = c.find(b => b.type === 'tool_use');
      if (tool && tool.name === 'AskUserQuestion') {
        setActivity('Waiting for your answer');
      } else if (tool) {
        const arg = tool.input && (tool.input.command || tool.input.file_path || tool.input.path || tool.input.pattern || tool.input.description);
        setActivity('Running ' + (tool.name || 'tool') + (arg ? ' · ' + String(arg).slice(0, 60) : ''));
      } else if (c.some(b => b.type === 'text')) {
        setActivity('Working');
      }
      return;
    }
    if (kind === 'system' && p && p.subtype === 'hook_started') { cur.actBeforeHook = cur.activity.hidden ? null : cur.actText.textContent; setActivity('Running hook · ' + (p.hook_name || p.hook_event || '')); return; }
    if (kind === 'system' && p && p.subtype === 'hook_response') { setActivity(cur.actBeforeHook); return; }
    if (kind === 'control_request' && p && p.request && p.request.subtype === 'can_use_tool') {
      setActivity(p.request.tool_name === 'AskUserQuestion' ? 'Waiting for your answer' : 'Waiting for permission · ' + (p.request.display_name || p.request.tool_name || 'tool'));
      return;
    }
    if (kind === 'user') { setActivity('Working'); return; }
    if (kind === 'system' && p && p.subtype === 'status' && p.status) { setActivity(p.status.charAt(0).toUpperCase() + p.status.slice(1)); return; }
    if (kind === 'result' || kind === 'state') { turnHasEcho = false; cur.think = null; dropLiveThought(cur); if (cur === mainLane) { mainTurnActive = false; lastTurnEnd = Date.now(); refreshMainIdle(); if (kind === 'result') goalTurnEnded(); if (hydrated) runQueuedCmd(); } else setActivity(null); }
  }

  // liveThought shows the thinking stretch as it happens: a chip at the
  // foot of the lane with the running token estimate and elapsed time,
  // which the real thought chip replaces when the assistant frame lands.
  function liveThought(lane) {
    if (!lane.think) return;
    if (!lane.thinkLive) {
      const wrap = el('div', 'frame frame--thought is-live');
      const line = el('div', 'thought-line');
      line.appendChild(svg(ICONS.spark));
      line.appendChild(el('span', 'thought-text', 'Thinking'));
      wrap.appendChild(line);
      const stick = atBottom(lane.log);
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
  function thoughtChip(block, at) {
    dropLiveThought(cur);
    const think = cur.think;
    const secs = think ? Math.max(0, (ts(at) - think.start) / 1000) : 0;
    const tokens = think ? think.tokens : 0;
    const bits = ['Thought'];
    if (secs) bits.push('for ' + (secs < 10 ? secs.toFixed(1) : Math.round(secs)) + 's');
    if (tokens) bits.push('~' + tokens.toLocaleString() + ' tokens');
    const wrap = el('div', 'frame frame--thought');
    const line = el('div', 'thought-line');
    line.appendChild(svg(ICONS.spark));
    line.appendChild(el('span', 'thought-text', bits.join(' · ')));
    wrap.appendChild(line);
    if (block.thinking) { // if a future version ever ships the text, show it
      const d = el('details', 'thought-body');
      d.appendChild(el('summary', null, 'show thinking'));
      d.appendChild(el('div', 'msg-think', block.thinking));
      wrap.appendChild(d);
    }
    attachRaw(wrap, think && think.last ? { thinking_block: block, last_thinking_tokens: think.last, thinking_token_frames: think.frames || 0 } : block);
    cur.think = null;
    return wrap;
  }

  // --- permissions: mode control + approval modal ---
  //
  // The harness launches Claude Code with --permission-prompt-tool stdio, so a
  // prompt arrives as a control_request {subtype: can_use_tool} frame and is
  // answered with a control_response written back through the harness. The
  // mode selector sends a set_permission_mode control_request the same way.

  const modeSelect = stage.querySelector('[data-mode-select]');
  const modal = stage.querySelector('[data-perm-modal]');
  const permQueue = [];               // pending {id, req, node} not yet shown/answered
  const permByID = new Map();         // request_id -> {req, node, status}
  let permShowing = null;

  function sendControl(payload) {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'control', payload }));
  }

  if (modeSelect) modeSelect.addEventListener('change', () => {
    sendControl({ type: 'control_request', request_id: 'mode-' + Date.now().toString(36),
      request: { subtype: 'set_permission_mode', mode: modeSelect.value } });
  });
  // --- model / effort / fast-mode picker ---
  //
  // Claude Code answers a list_models control_request with its catalogue
  // ({value, resolvedModel, displayName, description, supportedEffortLevels…})
  // and switches on a set_model control_request {model}. Effort and fast mode
  // are the CLI's own local commands, sent as ordinary messages ("/effort
  // low", "/fast"); the confirmation reply names the outcome. get_settings
  // gives the default effort; init reports fast_mode_state and the reason
  // it is unavailable.

  const pick = stage.querySelector('[data-model-pick]');
  const pickBtn = stage.querySelector('[data-model-btn]');
  const pickPop = stage.querySelector('[data-model-pop]');
  const pickLabel = stage.querySelector('[data-model-label]');
  let modelCatalog = [];
  let modelsAsked = false;
  let curEffort = '';          // level chosen this session ("/effort …"), else the settings default
  let defaultEffort = '';      // from get_settings effective.effortLevel
  let fastOn = false;
  let fastReason = '';         // init.fast_mode_disabled_reason when unavailable
  const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
  const FAST_REASONS = { sdk_opt_in_required: 'not available in this mode', extra_usage_disabled: 'needs extra usage enabled on the account', model_not_allowed: 'not allowed for this model', disabled_by_env: 'disabled by environment', pending: 'checking availability' };

  function requestModels() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    modelsAsked = true;
    sendControl({ type: 'control_request', request_id: 'models-' + Date.now().toString(36), request: { subtype: 'list_models' } });
    sendControl({ type: 'control_request', request_id: 'settings-' + Date.now().toString(36), request: { subtype: 'get_settings' } });
    // initialize answers with the command catalogue (name, description,
    // argument hint, aliases) the composer's "/" menu is built from
    sendControl({ type: 'control_request', request_id: 'init-' + Date.now().toString(36), request: { subtype: 'initialize' } });
  }
  function noteInitialize(r) {
    if (Array.isArray(r.models)) noteModelCatalog(r.models);
    if (Array.isArray(r.commands)) noteCommands(r.commands);
    if (typeof r.fast_mode_state === 'string') noteFastState(r);
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
    // before the first turn no init has named the model: show the catalogue's default
    const hit = catalogEntry(curModel) || (!curModel ? modelCatalog.find(m => m.value === 'default') : null);
    const name = hit ? (hit.value === 'default' && hit.resolvedModel ? modelName(hit.resolvedModel) : (hit.displayName || hit.value)) : curModel ? modelName(curModel) : 'model…';
    const bits = [name];
    if (effortNow()) bits.push(effortNow());
    if (fastOn) bits.push('fast');
    pickLabel.textContent = bits.join(' · ');
    // model rows
    const list = pickPop.querySelector('[data-pick-models]');
    list.textContent = '';
    if (!modelCatalog.length) list.appendChild(el('div', 'pick-note', curModel ? modelName(curModel) + ' · catalogue loads when the session is running' : 'catalogue loads when the session is running'));
    for (const m of modelCatalog) {
      const row = el('button', 'pick-row' + (hit && hit.value === m.value ? ' is-current' : ''));
      row.type = 'button';
      row.appendChild(el('span', 'pick-row-name', m.displayName || m.value));
      row.appendChild(svg(['M3.5 8.5 6.5 11.5 12.5 5']));
      if (m.description) row.appendChild(el('span', 'pick-row-desc', m.description));
      row.addEventListener('click', () => {
        sendControl({ type: 'control_request', request_id: 'model-' + Date.now().toString(36), request: { subtype: 'set_model', model: m.value } });
        closePick();
      });
      list.appendChild(row);
    }
    // effort segments (limited to what the current model supports)
    const seg = pickPop.querySelector('[data-pick-effort]');
    seg.textContent = '';
    const allowed = hit && Array.isArray(hit.supportedEffortLevels) && hit.supportedEffortLevels.length ? hit.supportedEffortLevels : EFFORTS;
    for (const lvl of EFFORTS) {
      const b = el('button', 'pick-seg-btn' + (effortNow() === lvl ? ' is-current' : ''), lvl);
      b.type = 'button';
      b.disabled = !allowed.includes(lvl);
      b.addEventListener('click', () => { if (effortNow() !== lvl) send('/effort ' + lvl); closePick(); });
      seg.appendChild(b);
    }
    // fast mode
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
  function noteModelCatalog(models) {
    modelCatalog = Array.isArray(models) ? models : [];
    paintModelSelect();
  }
  function noteSettings(eff) {
    if (eff && typeof eff.effortLevel === 'string') defaultEffort = eff.effortLevel;
    paintModelSelect();
  }
  // --- output style ---
  //
  // The harness lists the styles a session can use (built-ins plus any custom
  // ones on that host) in the spawned state; each turn's init names the one
  // in force. Switching sends update_settings {outputStyle}, which Claude
  // Code writes to the project's .claude/settings.local.json and applies
  // from the next turn — the same thing the terminal's /output-style does.
  let styleCatalog = [];
  let curStyle = '';
  function noteStyles(list) { if (Array.isArray(list)) { styleCatalog = list; paintModelSelect(); } }
  function noteStyle(name) { if (typeof name === 'string' && name !== curStyle) { curStyle = name; paintModelSelect(); } }
  function styleMarker(name, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.spark));
    note.appendChild(el('span', null, 'output style set to '));
    note.appendChild(el('code', 'mode-code', name));
    return bubble('status', 'state frame--mode', note, p);
  }
  function paintStyles(hit) {
    const list = pickPop.querySelector('[data-pick-styles]');
    if (!list) return;
    list.textContent = '';
    const styles = styleCatalog.length ? styleCatalog : [{ name: 'default', description: '' }];
    const cur = (curStyle || 'default').toLowerCase();
    for (const s of styles) {
      const row = el('button', 'pick-row' + (String(s.name).toLowerCase() === cur ? ' is-current' : ''));
      row.type = 'button';
      row.appendChild(el('span', 'pick-row-name', s.name + (s.source && s.source !== 'built-in' ? ' · ' + s.source : '')));
      row.appendChild(svg(['M3.5 8.5 6.5 11.5 12.5 5']));
      if (s.description) row.appendChild(el('span', 'pick-row-desc', s.description));
      row.addEventListener('click', () => {
        if (String(s.name).toLowerCase() !== cur) {
          sendControl({ type: 'control_request', request_id: 'style-' + Date.now().toString(36), request: { subtype: 'update_settings', source: 'localSettings', settings: { outputStyle: s.name } } });
          curStyle = s.name;
          paintModelSelect();
        }
        closePick();
      });
      list.appendChild(row);
    }
  }
  function noteFastState(p) {
    if (typeof p.fast_mode_state === 'string') { fastOn = p.fast_mode_state === 'on'; fastReason = fastOn ? '' : (p.fast_mode_disabled_reason || ''); if (!fastOn && !fastReason) fastReason = ''; }
    paintModelSelect();
  }
  function openPick() { pickPop.hidden = false; pickBtn.setAttribute('aria-expanded', 'true'); }
  function closePick() { pickPop.hidden = true; pickBtn.setAttribute('aria-expanded', 'false'); }
  if (pickBtn) {
    pickBtn.addEventListener('click', () => { if (pickPop.hidden) { paintModelSelect(); openPick(); } else closePick(); });
    pickPop.querySelector('[data-pick-fast]').addEventListener('click', () => { send('/fast'); closePick(); });
    document.addEventListener('click', (e) => { if (!pickPop.hidden && !pick.contains(e.target)) closePick(); });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !pickPop.hidden) closePick(); });
  }

  function modelMarker(model, p) {
    const hit = catalogEntry(model) || modelCatalog.find(m => m.value === model);
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.spark));
    note.appendChild(el('span', null, 'model set to '));
    note.appendChild(el('code', 'mode-code', hit ? (hit.displayName || hit.value) : (model === 'default' ? 'default' : modelName(model))));
    return bubble('status', 'state frame--mode', note, p);
  }

  // foldIntoLast keeps a frame that has nothing to show (our own list_models
  // query, its answer once painted into the select) reachable verbatim.
  function foldIntoLast(p, label) {
    const last = lastVisibleFrame(mainLane.log);
    if (last) attachRaw(last, p, label);
    return null;
  }

  // Until the first turn's init reports the live mode, the select shows the
  // mode the session was created with.
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
  function permSummary(req) {
    const inp = req.input || {};
    if (req.tool_name === 'AskUserQuestion' && Array.isArray(inp.questions)) {
      return inp.questions.map(q => q.question).join('  ·  ');
    }
    return inp.command || inp.file_path || inp.path || inp.pattern || inp.url || req.description || '';
  }

  // renderPermRequest draws the transcript row for a prompt; the modal is
  // opened separately for ones still pending.
  function renderPermRequest(p) {
    const req = p.request || {};
    if (req.tool_name === 'ExitPlanMode') return renderPlanPerm(p);
    if (req.subtype === 'elicitation') return renderElicitation(p);
    if (req.subtype && req.subtype !== 'can_use_tool') return bubble('control request', 'control', el('div', 'frame-note', req.subtype.replace(/_/g, ' ')), p);
    // A tool prompt for a call that's on screen attaches to that call's pill
    // (status chip + Review) instead of repeating the call in its own row.
    const isAskReq = req.tool_name === 'AskUserQuestion';
    const pill = req.tool_use_id ? toolRows.get(req.tool_use_id) : null;
    if (pill && pill.isConnected && !pill.querySelector('.tool-perm')) {
      const frame = pill.closest('.frame');
      pill.classList.add('has-perm', 'is-pending');
      const box = el('span', 'tool-perm');
      const status = permStatusNode('pending');
      box.appendChild(status);
      const review = el('button', 'perm-review', isAskReq ? 'Answer' : 'Review');
      review.type = 'button';
      review.addEventListener('click', () => openPerm(p.request_id));
      box.appendChild(review);
      (isAskReq ? pill.querySelector('.ask-head') : pill).appendChild(box);
      const laneEl = pill.closest('.chat-log');
      const permLane = laneEl && laneEl.dataset.lane ? lanes.get(laneEl.dataset.lane) : null;
      if (permLane) { permLane.meta.waiting = true; laneHeaderRefresh(permLane); permLane.pendingPerms = (permLane.pendingPerms || 0) + 1; }
      if (isAskReq) askReqs.set(p.request_id, req.tool_use_id);
      attachRaw(frame, p, 'raw (permission)');
      permByID.set(p.request_id, { req, node: frame, pill, status, review, state: 'pending', payload: p });
      permQueue.push(p.request_id);
      return null;
    }
    const wrap = el('div', 'frame frame--perm');
    const head = el('div', 'frame-head');
    const isAsk = req.tool_name === 'AskUserQuestion';
    head.appendChild(el('span', 'frame-kind', isAsk ? 'question' : 'permission'));
    wrap.appendChild(head);
    const line = el('div', 'perm-line');
    line.appendChild(toolIcon(req.tool_name));
    line.appendChild(el('span', 'tool-name', isAsk ? 'Claude asks' : (req.display_name || req.tool_name || 'tool')));
    const sum = permSummary(req);
    if (sum) { const a = el('code', 'tool-arg', String(sum).slice(0, 160)); a.title = String(sum); line.appendChild(a); }
    const status = permStatusNode('pending');
    line.appendChild(status);
    const review = el('button', 'perm-review', isAsk ? 'Answer' : 'Review');
    review.type = 'button';
    review.addEventListener('click', () => openPerm(p.request_id));
    line.appendChild(review);
    wrap.appendChild(line);
    attachRaw(wrap, p);
    permByID.set(p.request_id, { req, node: wrap, status, review, state: 'pending', payload: p });
    permQueue.push(p.request_id);
    return wrap;
  }

  function resolvePerm(id, outcome) {
    const e = permByID.get(id);
    if (!e || e.state !== 'pending') return; // first resolution wins (our answer beats Claude's echo of it)
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
      modal.querySelector('.perm-title').textContent = (req.mcp_server_name || 'An MCP server') + ' needs some input';
      modal.querySelector('[data-perm-tool]').textContent = '';
      modal.querySelector('[data-perm-desc]').textContent = req.message || '';
      modal.querySelector('[data-perm-count]').textContent = permQueue.length > 1 ? (permQueue.indexOf(id) + 1) + ' of ' + permQueue.length : '';
      modal.querySelector('[data-perm-allow]').textContent = 'Send';
      modal.querySelector('[data-perm-deny]').textContent = 'Decline';
      modal.querySelector('[data-perm-msg]').hidden = true;
      modal.querySelector('[data-perm-suggest]').textContent = '';
      const inputBox = modal.querySelector('[data-perm-input]');
      inputBox.textContent = '';
      const schema = req.requested_schema || {};
      const reqd = new Set(schema.required || []);
      if (req.mode === 'url' && req.url) { const a = el('a', 'elicit-url', req.url); a.href = req.url; a.target = '_blank'; a.rel = 'noopener'; inputBox.appendChild(a); }
      for (const k of Object.keys(schema.properties || {})) inputBox.appendChild(elicitField(k, schema.properties[k] || {}, reqd.has(k)));
      modal.hidden = false;
      const first = inputBox.querySelector('.elicit-ctl'); if (first) first.focus();
      return;
    }
    modal.querySelector('[data-perm-tool]').textContent = req.display_name || req.tool_name || 'tool';
    modal.querySelector('[data-perm-desc]').textContent = req.description || '';
    modal.querySelector('[data-perm-count]').textContent = permQueue.length > 1 ? (permQueue.indexOf(id) + 1) + ' of ' + permQueue.length : '';
    const inputBox = modal.querySelector('[data-perm-input]');
    inputBox.textContent = '';
    const inp = req.input || {};
    const isAsk = req.tool_name === 'AskUserQuestion' && Array.isArray(inp.questions);
    modal.classList.toggle('is-ask', isAsk);
    modal.querySelector('.perm-title').textContent = isAsk ? 'Claude has a question' : 'Permission request';
    modal.querySelector('[data-perm-allow]').textContent = isAsk ? 'Answer' : 'Allow';
    modal.querySelector('[data-perm-deny]').textContent = isAsk ? 'Skip' : 'Deny';
    modal.querySelector('[data-perm-msg]').hidden = isAsk;
    if (isAsk) {
      modal.querySelector('[data-perm-tool]').textContent = '';
      modal.querySelector('[data-perm-desc]').textContent = '';
      inp.questions.forEach((q, qi) => inputBox.appendChild(questionBlock(q, qi)));
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
    (req.permission_suggestions || []).forEach((sg, i) => {
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

  // questionBlock renders one AskUserQuestion question: radios for single
  // select, checkboxes for multi, each option with its description (and a
  // preview block when one is given), plus an "Other" free-text answer.
  function questionBlock(q, qi) {
    const box = el('fieldset', 'ask-q');
    box.dataset.qi = String(qi);
    const legend = el('legend', 'ask-legend');
    if (q.header) legend.appendChild(el('span', 'ask-header', q.header));
    legend.appendChild(el('span', 'ask-question', q.question || ''));
    box.appendChild(legend);
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
      box.appendChild(lab);
    });
    const other = el('label', 'ask-opt ask-other');
    const ctl = document.createElement('input');
    ctl.type = type; ctl.name = 'q' + qi; ctl.value = ''; ctl.dataset.other = '1';
    other.appendChild(ctl);
    const txt = document.createElement('input');
    txt.type = 'text'; txt.className = 'ask-other-text'; txt.placeholder = 'Other…';
    txt.addEventListener('input', () => { ctl.checked = txt.value.trim() !== ''; });
    other.appendChild(txt);
    box.appendChild(other);
    return box;
  }

  // collectAnswers reads the question form into the {question: answer} map
  // Claude reads back (labels, comma-joined for multi-select, free text for
  // "Other"). Returns null if a question has no answer.
  function collectAnswers(questions) {
    const answers = {};
    for (const box of modal.querySelectorAll('.ask-q')) {
      const q = questions[Number(box.dataset.qi)];
      const picked = [];
      for (const ctl of box.querySelectorAll('input:checked')) {
        if (ctl.dataset.other) {
          const t = box.querySelector('.ask-other-text').value.trim();
          if (t) picked.push(t);
        } else picked.push(ctl.value);
      }
      if (!picked.length) return null;
      answers[q.question] = picked.join(', ');
    }
    return answers;
  }

  function answerPerm(allow) {
    const id = permShowing;
    const e = permByID.get(id);
    if (!e) return;
    const msg = modal.querySelector('[data-perm-msg]').value.trim();
    if (e.elicit) return answerElicit(allow ? 'accept' : 'decline');
    if (e.req.tool_name === 'AskUserQuestion' && Array.isArray((e.req.input || {}).questions)) {
      if (allow) {
        const answers = collectAnswers(e.req.input.questions);
        if (!answers) { modal.querySelector('.perm-title').textContent = 'Answer every question first'; return; }
        sendControl({ type: 'control_response', response: { subtype: 'success', request_id: id,
          response: { behavior: 'allow', updatedInput: Object.assign({}, e.req.input, { answers }) } } });
        showAnswers(askReqs.get(id) || (e.req.tool_use_id), answers);
        resolvePerm(id, 'answered');
      } else {
        sendControl({ type: 'control_response', response: { subtype: 'success', request_id: id,
          response: { behavior: 'deny', message: 'The user skipped the question in Acta' } } });
        resolvePerm(id, 'skipped');
      }
      return;
    }
    const chosen = [...modal.querySelectorAll('[data-perm-suggest] input:checked')].map(cb => (e.req.permission_suggestions || [])[Number(cb.dataset.idx)]).filter(Boolean);
    const response = allow
      ? { behavior: 'allow', updatedInput: e.req.input || {} }
      : { behavior: 'deny', message: msg || 'Denied by the user in Acta' };
    if (allow && chosen.length) response.updatedPermissions = chosen;
    sendControl({ type: 'control_response', response: { subtype: 'success', request_id: id, response } });
    resolvePerm(id, allow ? 'allowed' : 'denied');
  }
  if (modal) {
    modal.querySelector('[data-perm-allow]').addEventListener('click', () => answerPerm(true));
    modal.querySelector('[data-perm-deny]').addEventListener('click', () => answerPerm(false));
    modal.querySelector('[data-perm-cancel]').addEventListener('click', () => answerElicit('cancel'));
  }

  // A permission-mode change is three frames — our set_permission_mode
  // request, Claude Code's control_response {mode}, and a status frame
  // carrying permissionMode — folded into one small marker.
  const modeMarks = new Map(); // request_id -> marker node
  let lastModeMark = null;      // marker still waiting for its status frame

  function modeMarker(mode, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.shield));
    note.appendChild(el('span', null, 'permissions set to '));
    note.appendChild(el('code', 'mode-code', mode));
    return bubble('status', 'state frame--mode', note, p);
  }

  // A browser-authored control frame (ours or another tab's) in the transcript.
  function renderControl(p) {
    const r = p.response && p.response.response;
    if (p.type === 'control_response' && p.response && p.response.request_id) {
      const ans = r && r.updatedInput && r.updatedInput.answers;
      if (ans) showAnswers(askReqs.get(p.response.request_id), ans);
      const e = permByID.get(p.response.request_id);
      if (e && e.plan && r && r.behavior === 'deny' && r.message) e.plan.feedback = r.message;
      if (e && e.node && e.node.isConnected) attachRaw(e.node, p, 'raw (answer)');
      if (e && e.elicit) { showElicitAnswer(p.response.request_id, r); resolvePerm(p.response.request_id, r && r.action === 'accept' ? 'answered' : r && r.action === 'decline' ? 'declined' : 'cancelled'); return null; }
      resolvePerm(p.response.request_id, r && r.behavior === 'allow' ? (ans ? 'answered' : 'allowed') : (askReqs.has(p.response.request_id) ? 'skipped' : 'denied'));
      return null; // the permission row shows the outcome
    }
    if (p.type === 'control_request' && p.request && /^(rewind_conversation|rewind_files|side_question)$/.test(p.request.subtype || '')) return foldIntoLast(p, 'raw (' + p.request.subtype.replace(/_/g, ' ') + ')');
    if (p.type === 'control_request' && p.request && p.request.subtype === 'list_models') return foldIntoLast(p, 'raw (list models)');
    if (p.type === 'control_request' && p.request && p.request.subtype === 'get_settings') return foldIntoLast(p, 'raw (get settings)');
    if (p.type === 'control_request' && p.request && p.request.subtype === 'initialize') return foldIntoLast(p, 'raw (initialize)');
    if (p.type === 'control_request' && p.request && p.request.subtype === 'update_settings' && p.request.settings && p.request.settings.outputStyle) {
      const node = styleMarker(p.request.settings.outputStyle, p);
      if (p.request_id) modeMarks.set(p.request_id, node);
      lastModeMark = node;
      return node;
    }
    if (p.type === 'control_request' && p.request && p.request.subtype === 'set_model') {
      const node = modelMarker(p.request.model, p);
      if (p.request_id) modeMarks.set(p.request_id, node);
      lastModeMark = node;
      return node;
    }
    if (p.type === 'control_request' && p.request && p.request.subtype === 'set_permission_mode') {
      const node = modeMarker(p.request.mode, p);
      if (p.request_id) modeMarks.set(p.request_id, node);
      lastModeMark = node;
      return node;
    }
    return bubble('control', 'control', null, p);
  }
  // A control_response emitted by Claude Code: the answer to a
  // set_permission_mode, or its echo of a permission answer we sent (which the
  // permission row already reflects).
  function renderControlResponse(p) {
    const r = p.response && p.response.response;
    const rid = p.response && p.response.request_id;
    if (rid && pendingCtl.has(rid)) {
      pendingCtl.get(rid)(p.response.subtype === 'error' ? { error: p.response.error } : (r || {}));
      return foldIntoLast(p, 'raw (rewind response)');
    }
    if (rid && /^init-/.test(rid)) { if (r) noteInitialize(r); return foldIntoLast(p, 'raw (initialize)'); }
    if (r && Array.isArray(r.models)) { noteModelCatalog(r.models); return foldIntoLast(p, 'raw (models)'); }
    if (r && r.effective && rid && /^settings-/.test(rid)) { noteSettings(r.effective); return foldIntoLast(p, 'raw (settings)'); }
    if (rid && /^interrupt-/.test(rid)) return foldIntoLast(p, 'raw (interrupt response)');
    if (rid && /^style-/.test(rid)) {
      const mark = modeMarks.get(rid);
      if (mark && mark.isConnected) { attachRaw(mark, p, 'raw (response)'); return null; }
      return foldIntoLast(p, 'raw (style response)');
    }
    if (rid && /^model-/.test(rid)) {
      const mark = modeMarks.get(rid);
      if (mark && mark.isConnected) { attachRaw(mark, p, 'raw (response)'); return null; }
      return foldIntoLast(p, 'raw (set model response)');
    }
    if (r && r.mode) {
      setMode(r.mode);
      const mark = modeMarks.get(p.response.request_id);
      if (mark && mark.isConnected) { attachRaw(mark, p, 'raw (response)'); lastModeMark = mark; return null; }
      lastModeMark = modeMarker(r.mode, p);
      return lastModeMark;
    }
    if (r && r.action && p.response.request_id && permByID.has(p.response.request_id)) {
      showElicitAnswer(p.response.request_id, r);
      resolvePerm(p.response.request_id, r.action === 'accept' ? 'answered' : r.action === 'decline' ? 'declined' : 'cancelled');
      return null;
    }
    if (r && r.behavior && p.response.request_id) {
      const ans = r.updatedInput && r.updatedInput.answers;
      if (ans) showAnswers(askReqs.get(p.response.request_id), ans);
      resolvePerm(p.response.request_id, r.behavior === 'allow' ? (ans ? 'answered' : 'allowed') : (askReqs.has(p.response.request_id) ? 'skipped' : 'denied'));
      return null;
    }
    return bubble('control response', 'control', null, p);
  }

  let hydrated = false;
  // whether a Claude Code process is alive for this session (a spawned state
  // or an init frame says yes, an exit says no): the context panel only
  // asks for reports of a live process, so a click never resumes a dead one
  let procAlive = stage.dataset.running === '1';
  // the presence dot is the server's word on whether a process is alive
  // (harness connected and holding a running process); follow it
  const liveDot = stage.querySelector('[data-session-dot]');
  if (liveDot && window.MutationObserver) {
    new MutationObserver(() => { procAlive = liveDot.classList.contains('is-running'); if (typeof paintGaugePop === 'function') paintGaugePop(); })
      .observe(liveDot, { attributes: true, attributeFilter: ['class'] });
  }

  // --- rendering ---

  // curAt is the timestamp of the frame being rendered, for the hover stamp.
  let curAt = '';

  // attachRaw adds a verbatim payload to a frame. The frame gets one "raw"
  // button in its hover tools (with a count once it holds more than one
  // payload) that toggles a panel listing every payload folded into it, each
  // under a short label ("init", "summary"…).
  function attachRaw(wrap, payload, label) {
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
      const box = el('div', 'frame-json-box');
      box.hidden = true;
      btn.addEventListener('click', () => {
        box.hidden = !box.hidden;
        btn.classList.toggle('is-open', !box.hidden);
        wrap.classList.toggle('is-raw-open', !box.hidden);
      });
      tools.appendChild(btn);
      wrap.appendChild(box);
      raw = wrap._raw = { btn, box, n: 0 };
    }
    raw.n++;
    const sec = el('div', 'frame-json-sec');
    const name = label ? label.replace(/^raw\s*\(?/, '').replace(/\)$/, '').trim() : '';
    if (name) sec.appendChild(el('div', 'raw-label', name));
    const pre = el('pre', 'frame-json');
    pre.textContent = JSON.stringify(payload, (k, v) => (k === 'data' && typeof v === 'string' && v.length > 2000) ? '<base64 · ' + Math.round(v.length * 3 / 4 / 1024) + ' KB elided>' : v, null, 2);
    sec.appendChild(pre);
    raw.box.appendChild(sec);
    if (raw.n > 1) {
      raw.btn.textContent = 'raw · ' + raw.n;
      // a single unlabelled first payload gets a label once it has company
      const first = raw.box.firstChild;
      if (first && !first.querySelector('.raw-label')) first.insertBefore(el('div', 'raw-label', 'frame'), first.firstChild);
    }
    return pre;
  }

  // lastVisibleFrame: the frame a new one would land under in a lane.
  function lastVisibleFrame(logEl) {
    const kids = logEl.children;
    for (let i = kids.length - 1; i >= 0; i--) {
      const k = kids[i];
      if (k.hidden || k.classList.contains('chat-activity') || k.hasAttribute('data-payload')) continue; // data-payload = not-yet-hydrated placeholder
      if (k.classList.contains('is-streaming') || k.classList.contains('is-live')) continue; // a reply or thought still being streamed sits below everything settled
      if (k.classList.contains('frame-group')) { const inner = [...k.children].reverse().find(f => !f.hidden); if (inner) return inner; continue; }
      if (k.classList.contains('frame')) return k;
    }
    return null;
  }
  // mergeRaw moves every payload folded into one frame onto another, so a
  // frame can be hidden without losing anything sent over the wire.
  function mergeRaw(from, into) {
    const raw = from._raw;
    if (!raw || from === into) return;
    for (const sec of [...raw.box.children]) {
      const lbl = sec.querySelector('.raw-label');
      let payload = null;
      try { payload = JSON.parse(sec.querySelector('.frame-json').textContent); } catch (_) { payload = sec.querySelector('.frame-json').textContent; }
      attachRaw(into, payload, 'raw (' + (lbl ? lbl.textContent : 'call') + ')');
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
    attachRaw(wrap, payload);
    return wrap;
  }

  // --- a small, safe markdown renderer for Claude's prose ---
  //
  // Builds DOM nodes directly (never innerHTML), so model output can't inject
  // markup. Covers what Claude actually emits: paragraphs, headings, fenced
  // code, inline code, bold/italic, links, bullet/numbered lists, quotes,
  // rules and pipe tables. Anything else falls through as plain text.

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
          // continuation: deeper-indented lines belong to this item
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
      // paragraph: run until a blank line or the start of another block
      const buf = [];
      while (i < lines.length && lines[i].trim() && !/^\s*(`{3,}|~{3,})/.test(lines[i]) && !/^\s{0,3}#{1,4}\s/.test(lines[i]) && !/^\s{0,3}>/.test(lines[i]) && !/^\s*([-*+]|\d+[.)])\s+/.test(lines[i]) && !(/^\s*\|/.test(lines[i]) && i + 1 < lines.length && isTableSep(lines[i + 1]))) buf.push(lines[i++]);
      if (!buf.length) { buf.push(line); i++; }
      const p = el('p'); mdInline(buf.join('\n'), p); root.appendChild(p);
    }
    return root;
  }

  // Tool names and pills by tool_use_id, so a tool_result can say which call
  // it answers and a permission_denied can mark the call itself.
  const toolNames = new Map();
  const toolRows = new Map();
  const deniedTools = new Set();

  // modelName turns a model id into its display name: "claude-fable-5-1" →
  // "Claude Fable 5.1" (a dash between digits is a point, other dashes are
  // spaces, every word capitalised; a trailing date stamp is dropped).
  let curModel = '';
  function modelName(id) {
    if (!id) return 'Claude';
    if (/^</.test(id)) return 'Claude Code'; // "<synthetic>": a message from the CLI itself
    // "claude-opus-5[1m]" -> "Claude Opus 5"; a trailing date stamp is dropped
    return String(id).replace(/\[[^\]]*\]$/, '')
      .replace(/-\d{8}$/, '').replace(/(\d)-(\d)/g, '$1.$2').replace(/-/g, ' ')
      .replace(/\b\w/g, c => c.toUpperCase());
  }

  // askCard renders an AskUserQuestion call as a card listing its questions;
  // the permission prompt attaches to it (Answer button) and the answers show
  // inline once given. askCards: tool_use_id -> {node, head, items: {question -> answer slot}}
  const askCards = new Map();
  const askReqs = new Map(); // request_id -> tool_use_id for question prompts

  function askCard(b) {
    const inp = b.input || {};
    const node = el('div', 'msg-ask');
    const head = el('div', 'ask-head');
    head.appendChild(svg(ICONS.question));
    head.appendChild(el('span', 'tool-name', 'Claude asks'));
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
    const card = { node, head, items };
    if (b.id) { toolNames.set(b.id, 'AskUserQuestion'); toolRows.set(b.id, node); askCards.set(b.id, card); }
    return node;
  }

  // showAnswers fills a question card's answer slots ({question: answer}).
  function showAnswers(toolUseID, answers) {
    const card = askCards.get(toolUseID);
    if (!card || !answers) return;
    for (const q in answers) {
      const slot = card.items.get(q);
      if (!slot) continue;
      slot.querySelector('.ask-answer-text').textContent = String(answers[q]);
      slot.hidden = false;
    }
    card.node.classList.add('is-answered');
  }

  // --- MCP elicitation ---
  //
  // An MCP server can pause a tool call to ask the user for structured input.
  // Claude Code relays that as control_request {subtype: "elicitation",
  // mcp_server_name, message, mode: "form", requested_schema} and expects
  // {action: "accept", content} | {action: "decline"} | {action: "cancel"}
  // back. The card in the transcript shows the ask (and the answer once
  // given); the modal builds a form from the schema: text, number, boolean,
  // enum, each with its title and description.

  const elicitCards = new Map(); // request_id -> {node, head, body}

  function elicitCard(p) {
    const req = p.request || {};
    const node = el('div', 'msg-ask msg-elicit is-pending');
    const head = el('div', 'ask-head');
    head.appendChild(svg(ICONS.tool));
    head.appendChild(el('span', 'tool-name', (req.mcp_server_name || 'an MCP server') + ' asks'));
    node.appendChild(head);
    if (req.message) node.appendChild(el('div', 'ask-question elicit-msg', req.message));
    if (req.mode === 'url' && req.url) { const a = el('a', 'elicit-url', req.url); a.href = req.url; a.target = '_blank'; a.rel = 'noopener'; node.appendChild(a); }
    const body = el('div', 'elicit-answers'); body.hidden = true;
    node.appendChild(body);
    elicitCards.set(p.request_id, { node, head, body, req });
    return node;
  }
  function renderElicitation(p) {
    const card = elicitCard(p);
    const box = el('span', 'tool-perm');
    const status = permStatusNode('pending');
    box.appendChild(status);
    const review = el('button', 'perm-review', 'Answer');
    review.type = 'button';
    review.addEventListener('click', () => openPerm(p.request_id));
    box.appendChild(review);
    card.querySelector('.ask-head').appendChild(box);
    const wrap = bubble('input request', 'elicit', card, p);
    permByID.set(p.request_id, { req: p.request || {}, node: wrap, status, review, state: 'pending', payload: p, elicit: true });
    permQueue.push(p.request_id);
    return wrap;
  }
  // showElicitAnswer paints the outcome into the card: the values sent, or
  // that it was declined / cancelled.
  function showElicitAnswer(id, r) {
    const c = elicitCards.get(id);
    if (!c) return;
    c.node.classList.remove('is-pending');
    const action = (r && r.action) || 'cancel';
    if (action === 'accept') {
      c.node.classList.add('is-answered');
      c.body.textContent = '';
      const content = (r && r.content) || {};
      const props = (c.req.requested_schema && c.req.requested_schema.properties) || {};
      for (const k of Object.keys(content)) {
        const row = el('div', 'ask-answer');
        row.appendChild(el('span', 'ask-answer-q', (props[k] && props[k].title) || k));
        row.appendChild(el('span', 'ask-answer-text', String(content[k])));
        c.body.appendChild(row);
      }
      c.body.hidden = !c.body.children.length;
    } else {
      c.node.classList.add('is-denied');
      c.node.appendChild(el('div', 'ask-note', action === 'decline' ? 'Declined' : 'Cancelled'));
    }
  }
  // elicitField builds one form control from a JSON-schema property.
  function elicitField(key, prop, required) {
    const box = el('label', 'elicit-field');
    box.dataset.key = key;
    const lab = el('span', 'elicit-label', prop.title || key);
    if (required) lab.appendChild(el('span', 'elicit-req', ' *'));
    box.appendChild(lab);
    if (prop.description) box.appendChild(el('span', 'elicit-desc', prop.description));
    let ctl;
    const type = Array.isArray(prop.type) ? prop.type[0] : prop.type;
    if (Array.isArray(prop.enum)) {
      ctl = document.createElement('select');
      if (!required) { const o = document.createElement('option'); o.value = ''; o.textContent = '—'; ctl.appendChild(o); }
      prop.enum.forEach((v, i) => { const o = document.createElement('option'); o.value = String(v); o.textContent = (prop.enumNames && prop.enumNames[i]) || String(v); ctl.appendChild(o); });
      if (prop.default != null) ctl.value = String(prop.default);
    } else if (type === 'boolean') {
      box.classList.add('is-bool');
      ctl = document.createElement('input'); ctl.type = 'checkbox'; ctl.checked = !!prop.default;
      box.insertBefore(ctl, lab);
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
    if (!box.contains(ctl)) box.appendChild(ctl);
    return box;
  }
  // collectElicit reads the form back into {key: value} per the schema's
  // types. Returns null (and marks the field) when a required one is empty.
  function collectElicit(schema) {
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
    let response = { action };
    if (action === 'accept') {
      const content = collectElicit(e.req.requested_schema || {});
      if (!content) { modal.querySelector('.perm-title').textContent = 'Fill in the required fields first'; return; }
      response = { action: 'accept', content };
    }
    sendControl({ type: 'control_response', response: { subtype: 'success', request_id: id, response } });
    showElicitAnswer(id, response);
    resolvePerm(id, action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled');
  }

  // --- tasks ---
  //
  // Claude Code's task list is driven by tool calls (TaskCreate, TaskUpdate,
  // TaskList, TaskGet; TodoWrite in older builds). One checklist card in the
  // transcript, placed where the first task was created, absorbs every later
  // call and result and repaints in place; a header pill shows progress and
  // jumps to it. When the last task completes a marker says so where it
  // happened.

  const tasks = new Map();       // id -> {id, subject, description, status, activeForm}
  const taskCalls = new Map();   // tool_use_id -> {name, input, tmp}
  let taskCard = null;           // {node, list, count, frame()}
  let tasksAllDone = false;
  const taskPill = stage.querySelector('[data-task-pill]');
  const TASK_TOOLS = /^(TaskCreate|TaskUpdate|TaskList|TaskGet|TodoWrite)$/;

  function taskEntry(id) {
    let t = tasks.get(String(id));
    if (!t) { t = { id: String(id), subject: '', description: '', status: 'pending', activeForm: '' }; tasks.set(t.id, t); }
    return t;
  }
  function taskRow(t) {
    const row = el('div', 'task-row is-' + t.status);
    const glyph = el('span', 'task-glyph');
    if (t.status === 'completed') glyph.appendChild(svg(['M3.5 8.5 6.5 11.5 12.5 5']));
    else if (t.status === 'in_progress') glyph.appendChild(el('span', 'task-spin'));
    row.appendChild(glyph);
    const body = el('div', 'task-body');
    body.appendChild(el('div', 'task-subject', t.subject || ('Task #' + t.id)));
    if (t.status === 'in_progress' && t.activeForm) body.appendChild(el('div', 'task-active', t.activeForm));
    if (t.description) { const d = el('div', 'task-desc'); d.appendChild(mdRender(t.description)); d.hidden = true; body.appendChild(d); row.classList.add('has-desc'); row.addEventListener('click', () => { d.hidden = !d.hidden; }); }
    row.appendChild(body);
    row.appendChild(el('span', 'task-id', '#' + t.id));
    return row;
  }
  function taskStats() {
    const all = [...tasks.values()].filter(t => t.status !== 'deleted');
    return { done: all.filter(t => t.status === 'completed').length, total: all.length, all };
  }
  function paintTasks() {
    const { done, total, all } = taskStats();
    if (taskPill) {
      taskPill.hidden = !total;
      taskPill.className = 'plan-pill task-pill' + (total && done === total ? ' is-approved' : all.some(t => t.status === 'in_progress') ? ' is-active' : '');
      taskPill.querySelector('[data-task-pill-text]').textContent = 'Tasks · ' + done + '/' + total;
    }
    if (!taskCard) return;
    taskCard.count.textContent = done + ' of ' + total + ' done';
    taskCard.node.classList.toggle('is-done', total > 0 && done === total);
    const bar = taskCard.node.querySelector('.task-bar-fill');
    bar.style.width = (total ? Math.round(done / total * 100) : 0) + '%';
    taskCard.list.textContent = '';
    all.sort((a, b) => (parseInt(a.id, 10) || 0) - (parseInt(b.id, 10) || 0));
    for (const t of all) taskCard.list.appendChild(taskRow(t));
  }
  function taskCardNode() {
    const node = el('div', 'msg-tasks');
    const head = el('div', 'agent-head');
    head.appendChild(svg(['M3 4h10M3 8h6M3 12h4', 'm10.5 11 1.5 1.5L15 9.5']));
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

  // taskBlock handles a task tool call in an assistant frame: the first
  // one places the card, later ones fold into it. Returns the node to
  // place, null when folded, undefined when it isn't a task call.
  function taskBlock(b) {
    if (!TASK_TOOLS.test(b.name || '')) return undefined;
    const inp = b.input || {};
    const call = { name: b.name, input: inp, tmp: null };
    if (b.id) { taskCalls.set(b.id, call); toolNames.set(b.id, b.name); }
    if (b.name === 'TaskCreate') {
      call.tmp = 'new-' + (b.id || Math.random().toString(36).slice(2));
      const t = taskEntry(call.tmp);
      t.subject = inp.subject || ''; t.description = inp.description || ''; t.activeForm = inp.activeForm || ''; t.status = 'pending';
    } else if (b.name === 'TaskUpdate' && inp.taskId != null) {
      const t = taskEntry(inp.taskId);
      if (inp.status) t.status = inp.status;
      if (inp.subject) t.subject = inp.subject;
      if (inp.description) t.description = inp.description;
      if (inp.activeForm) t.activeForm = inp.activeForm;
    } else if (b.name === 'TodoWrite' && Array.isArray(inp.todos)) {
      tasks.clear();
      inp.todos.forEach((td, i) => { const t = taskEntry(td.id || String(i + 1)); t.subject = td.content || td.subject || ''; t.status = td.status || 'pending'; t.activeForm = td.activeForm || ''; });
    }
    const fresh = !(taskCard && taskCard.node.isConnected);
    const node = fresh ? taskCardNode() : null;
    paintTasks();
    return node;
  }
  // taskResult folds a task tool's result into the card and takes the real
  // ids and statuses it reports; the last completion leaves a marker.
  function taskResult(call, b, p) {
    const tur = p.tool_use_result || {};
    if (call.name === 'TaskCreate' && tur.task && tur.task.id != null && call.tmp) {
      const t = tasks.get(call.tmp);
      tasks.delete(call.tmp);
      if (t) { t.id = String(tur.task.id); if (tur.task.subject) t.subject = tur.task.subject; tasks.set(t.id, t); }
    } else if (call.name === 'TaskUpdate' && tur.statusChange && tur.taskId != null) {
      taskEntry(tur.taskId).status = tur.statusChange.to || taskEntry(tur.taskId).status;
    } else if ((call.name === 'TaskList' || call.name === 'TaskGet') && Array.isArray(tur.tasks)) {
      for (const x of tur.tasks) { const t = taskEntry(x.id); if (x.subject) t.subject = x.subject; if (x.status) t.status = x.status; if (x.description) t.description = x.description; if (x.activeForm) t.activeForm = x.activeForm; }
    } else if (call.name === 'TaskGet' && tur.task) {
      const x = tur.task; const t = taskEntry(x.id); if (x.subject) t.subject = x.subject; if (x.status) t.status = x.status; if (x.description) t.description = x.description;
    }
    paintTasks();
    const f = taskCard && taskCard.frame();
    if (f) attachRaw(f, p, 'raw (' + call.name.replace(/^Task/, 'task ').toLowerCase() + ' result)'); else foldIntoLast(p, 'raw (task result)');
    const { done, total } = taskStats();
    if (total && done === total && !tasksAllDone) {
      tasksAllDone = true;
      const note = el('div', 'frame-note');
      note.appendChild(svg(['M3.5 8.5 6.5 11.5 12.5 5']));
      note.appendChild(el('span', null, 'all ' + total + (total === 1 ? ' task' : ' tasks') + ' done'));
      return bubble('tasks', 'state frame--planmark is-approved', note, p);
    }
    if (total && done < total) tasksAllDone = false;
    return null;
  }

  // --- plans ---
  //
  // Plan mode leaves no special frame: Claude writes the plan as a markdown
  // file under ~/.claude/plans with the Write/Edit tools, then calls
  // ExitPlanMode with the final text, which arrives as a permission request.
  // Those calls fold into one card per plan file (drafting -> awaiting
  // approval -> approved / changes requested), and the text lives in the
  // side panel, where approval happens. On a wide screen the panel opens
  // by itself as soon as a plan is being written; narrower, only the
  // approval request opens it (as a sheet), the card and pill do otherwise.

  const PLAN_PATH = /[\\/]\.claude[\\/]plans[\\/][^\\/]+\.md$/;
  const plans = new Map();     // key (file path, or exit call id) -> plan
  const planTools = new Map(); // tool_use_id -> plan, for calls that fold into a card
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
    head.appendChild(svg(['M3 4h10M3 8h6M3 12h4', 'm10.5 11 1.5 1.5L15 9.5']));
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
    body.appendChild(plan.text ? mdRender(plan.text) : el('div', 'plan-empty', 'Claude is writing the plan…'));
    body.scrollTop = keep;
    planPanel.querySelector('[data-plan-foot]').hidden = plan.state !== 'pending';
    paintPlanPill();
  }
  function openPlan(plan, force) {
    if (!planPanel) return;
    curPlan = plan || curPlan;
    if (!curPlan) return;
    // drafting only opens the panel by itself where it has room; a request
    // for approval opens it anywhere, since the session is waiting on it
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

  // planUpdate replaces the text (a Write, or ExitPlanMode's final copy) or
  // patches it (an Edit's old -> new), and repaints wherever it shows.
  function planUpdate(plan, text, isEdit) {
    const before = plan.text;
    if (isEdit) {
      const { old_string: o = '', new_string: n = '', replace_all: all } = isEdit;
      plan.text = all ? plan.text.split(o).join(n) : plan.text.replace(o, n);
    } else plan.text = text || '';
    if (plan.text !== before) plan.revisions++;
    if (plan.state === 'approved' || plan.state === 'rejected' || plan.state === 'stale') { plan.state = 'drafting'; plan.verdict = ''; }
    paintPlanCard(plan);
    curPlan = plan;
    if (planPanel && !planPanel.hidden) paintPlanPanel(); else paintPlanPill();
    if (hydrated && (!planPanel || planPanel.hidden)) openPlan(plan, false);
  }

  // planBlock handles a tool_use block that belongs to a plan. Returns the
  // card to place (first sighting), null when it folded into an existing
  // card, or undefined when the block isn't a plan call at all.
  function planBlock(b) {
    const inp = b.input || {};
    let plan = null, isEdit = null, text = '';
    if ((b.name === 'Write' || b.name === 'Edit') && typeof inp.file_path === 'string' && PLAN_PATH.test(inp.file_path)) {
      plan = planFor(inp.file_path);
      if (b.name === 'Edit') { isEdit = inp; if (!plan.text) text = inp.new_string || ''; } else text = inp.content || '';
    } else if (b.name === 'ToolSearch' && /ExitPlanMode/.test(String(inp.query || '')) && curPlan && curPlan.card && curPlan.card.isConnected) {
      plan = curPlan;
    } else if (b.name === 'ExitPlanMode') {
      plan = (inp.planFilePath && plans.get(inp.planFilePath)) || (inp.planFilePath ? planFor(inp.planFilePath) : (curPlan || planFor(b.id || 'plan')));
      text = typeof inp.plan === 'string' ? inp.plan : plan.text;
    } else return undefined;
    if (b.id) { planTools.set(b.id, plan); toolNames.set(b.id, b.name); }
    const fresh = !(plan.card && plan.card.isConnected);
    const node = fresh ? planCard(plan) : null;
    if (b.name === 'ExitPlanMode') { plan.exitID = b.id; if (text !== plan.text) plan.revisions++; plan.text = text; plan.state = 'pending'; plan.verdict = ''; paintPlanCard(plan); curPlan = plan; if (planPanel && !planPanel.hidden) paintPlanPanel(); else paintPlanPill(); }
    else if (b.name === 'ToolSearch') { /* the lookup that precedes ExitPlanMode: nothing to show */ }
    else planUpdate(plan, text, isEdit && plan.text ? isEdit : null);
    return node;
  }

  // planResult folds a plan call's tool_result into the card; the
  // ExitPlanMode result says whether the plan was approved.
  function planResult(plan, b, p) {
    const c = b.content;
    const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : '';
    if (b.tool_use_id === plan.exitID) {
      if (b.is_error || /rejected|not approved|denied|changes/i.test(txt.slice(0, 200))) {
        if (plan.state !== 'rejected') { plan.state = 'rejected'; plan.feedback = plan.feedback || txt.trim().slice(0, 600); /* a rejection's result is the feedback itself */ }
      } else if (/approved/i.test(txt.slice(0, 200)) && plan.state !== 'approved') plan.state = 'approved';
      plan.verdict = plan.state === 'approved' ? 'Approved · Claude is implementing it' : plan.state === 'rejected' ? 'Changes requested' + (plan.feedback ? ' · ' + plan.feedback : '') : '';
      paintPlanCard(plan);
      if (planPanel && !planPanel.hidden && curPlan === plan) paintPlanPanel(); else paintPlanPill();
      if (plan.state === 'approved' || plan.state === 'rejected') return planMarker(plan, p);
    }
    const f = planFrame(plan);
    if (f) attachRaw(f, p, 'raw (' + (b.tool_use_id === plan.exitID ? 'approval result' : toolNames.get(b.tool_use_id) === 'ToolSearch' ? 'tool search result' : 'plan write result') + ')');
    else foldIntoLast(p, 'raw (plan result)');
    return null;
  }
  // planMarker records the verdict where it landed in the transcript: the
  // feedback sent back with a rejection is the user's own words, so it
  // shows here rather than only inside the card.
  function planMarker(plan, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(['M3 4h10M3 8h6M3 12h4', 'm10.5 11 1.5 1.5L15 9.5']));
    if (plan.state === 'approved') note.appendChild(el('span', null, 'plan approved'));
    else {
      note.appendChild(el('span', null, 'changes requested'));
      if (plan.feedback) { const q = el('span', 'planmark-quote', plan.feedback.length > 220 ? plan.feedback.slice(0, 218) + '…' : plan.feedback); q.title = plan.feedback; note.appendChild(q); }
    }
    const wrap = bubble('plan', 'state frame--planmark is-' + plan.state, note, p);
    return wrap;
  }

  // renderPlanPerm: the ExitPlanMode prompt attaches to the card and opens
  // the panel, whose footer answers it (in place of the permission modal).
  function renderPlanPerm(p) {
    const req = p.request || {};
    const inp = req.input || {};
    let plan = planTools.get(req.tool_use_id) || (inp.planFilePath && plans.get(inp.planFilePath)) || curPlan;
    if (!plan) { plan = planFor(inp.planFilePath || req.tool_use_id || 'plan'); }
    if (typeof inp.plan === 'string') plan.text = inp.plan;
    plan.exitID = plan.exitID || req.tool_use_id;
    plan.state = 'pending'; plan.verdict = ''; plan.reqID = p.request_id;
    let node = planFrame(plan);
    let out = null;
    if (!node) { out = bubble('plan', 'plan', planCard(plan), p); node = out; }
    else attachRaw(node, p, 'raw (approval request)');
    paintPlanCard(plan);
    curPlan = plan;
    if (planPanel && !planPanel.hidden) paintPlanPanel(); else paintPlanPill();
    permByID.set(p.request_id, { req, node, status: el('span'), review: el('span'), state: 'pending', payload: p, plan });
    permQueue.push(p.request_id);
    return out;
  }
  function planResolved(plan, outcome) {
    plan.reqID = null;
    if (outcome === 'allowed') { plan.state = 'approved'; plan.verdict = 'Approved · Claude is implementing it'; }
    else if (outcome === 'denied') { plan.state = 'rejected'; plan.verdict = 'Changes requested' + (plan.feedback ? ' · ' + plan.feedback : ''); }
    else if (plan.state === 'pending') { plan.state = 'stale'; plan.verdict = ''; }
    paintPlanCard(plan);
    if (planPanel && !planPanel.hidden && curPlan === plan) paintPlanPanel(); else paintPlanPill();
  }
  function answerPlan(how) {
    const plan = curPlan;
    if (!plan || !plan.reqID) return;
    const id = plan.reqID;
    const e = permByID.get(id);
    if (!e || e.state !== 'pending') return;
    const fb = planPanel.querySelector('[data-plan-feedback]');
    const msg = fb.value.trim();
    let response;
    if (how === 'changes') {
      if (!msg) { fb.focus(); fb.placeholder = 'Say what should change first'; return; }
      plan.feedback = msg;
      response = { behavior: 'deny', message: msg };
    } else {
      response = { behavior: 'allow', updatedInput: e.req.input || {} };
      if (how === 'approve-edits') response.updatedPermissions = [{ type: 'setMode', mode: 'acceptEdits', destination: 'session' }];
    }
    sendControl({ type: 'control_response', response: { subtype: 'success', request_id: id, response } });
    fb.value = '';
    resolvePerm(id, how === 'changes' ? 'denied' : 'allowed');
  }

  // --- subagents ---
  //
  // An Agent call renders as a card in the parent lane (type, description,
  // status, latest activity, final summary) and opens a lane of its own;
  // clicking the card or its tab shows that lane. The task_* system frames
  // Claude Code emits around the run fold into the card.

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
  const tasksByID = new Map(); // task_id -> lane

  function laneByAgent(id, hint) {
    let lane = lanes.get(id);
    if (lane) return lane;
    const logEl = el('div', 'chat-log');
    logEl.hidden = true;
    logEl.dataset.lane = id;
    log.parentNode.insertBefore(logEl, log.nextSibling);
    lane = makeLane(id, logEl);
    if (hint) { lane.meta.type = hint.subagent_type || ''; lane.meta.desc = hint.task_description || hint.description || ''; }
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
    const lane = laneByAgent(b.id, { subagent_type: inp.subagent_type, description: inp.description });
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
    const open = (e) => { if (e.target.closest('button, a, .agent-prompt')) return; showLane(b.id); };
    node.addEventListener('click', open);
    node.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); showLane(b.id); } });
    lane.card = node;
    if (b.id) { toolNames.set(b.id, b.name || 'Agent'); toolRows.set(b.id, node); }
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
    const box = card.querySelector('.agent-summary');
    box.textContent = '';
    box.appendChild(mdRender(text));
    box.classList.add('has-summary');
    lane.meta.summary = text;
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
    if (contextUsed) drawContext(); else setGauge('context', 0, '\u2013', 'Context', 'Context window in use');
    scroll(lane.log);
    const h = lane === mainLane ? '' : '#agent=' + encodeURIComponent(id);
    if ((location.hash || '') !== h) history.replaceState(null, '', location.pathname + location.search + h);
  }

  // Task frames: link by tool_use_id when given, else task_id, else the
  // newest lane still without a task.
  function laneForTask(p) {
    if (p.tool_use_id && lanes.has(p.tool_use_id)) { const l = lanes.get(p.tool_use_id); if (p.task_id && !l.meta.taskId) { l.meta.taskId = p.task_id; tasksByID.set(p.task_id, l); } return l; }
    if (p.task_id && tasksByID.has(p.task_id)) return tasksByID.get(p.task_id);
    const free = [...lanes.values()].filter(l => l !== mainLane && !l.meta.taskId && l.meta.status === 'running');
    const l = free.length ? free[free.length - 1] : null;
    if (l && p.task_id) { l.meta.taskId = p.task_id; tasksByID.set(p.task_id, l); }
    return l;
  }

  // Shell tasks (a backgrounded or long-running Bash call, task_type
  // local_bash) fold into the Bash pill they belong to; a "background" chip
  // marks a call Claude Code moved to the background.
  const taskPills = new Map(); // task_id -> tool pill

  function shellTaskFrame(p) {
    const st = p.subtype;
    let pill = p.tool_use_id && !lanes.has(p.tool_use_id) ? toolRows.get(p.tool_use_id) : null;
    if (!pill && p.task_id) pill = taskPills.get(p.task_id);
    if (!pill || !pill.isConnected) return null;
    if (p.task_id) taskPills.set(p.task_id, pill);
    if (st === 'task_started' && p.is_backgrounded && !pill.querySelector('.tool-bg')) pill.appendChild(el('span', 'tool-bg', 'background'));
    if (st === 'task_notification' || (st === 'task_updated' && p.patch && p.patch.status && p.patch.status !== 'running')) {
      const bg = pill.querySelector('.tool-bg');
      if (bg) bg.textContent = 'background · ' + ((p.status || (p.patch && p.patch.status)) || 'done');
    }
    attachRaw(pill.closest('.frame'), p, 'raw (' + st.replace(/_/g, ' ') + ')');
    return true;
  }

  // agentDoneCard is the card that appears in the parent lane where the
  // subagent's completion actually arrives — that's the point its summary
  // enters the parent's context. The original card collapses to a line.
  function agentDoneCard(lane, p) {
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
    const wrap = bubble('subagent finished', 'agentdone', node, p);
    if (lane.card) {
      lane.card.classList.add('is-collapsed');
      const from = lane.card.closest('.frame');
      if (from && lastVisibleFrame(mainLane.log) === from) {
        // nothing happened between spawn and finish: the start line is noise
        lane.card.hidden = true;
        if (!hideIfEmpty(from, wrap)) mergeRaw(from, wrap);
      }
    }
    return wrap;
  }

  // renderTaskOutput paints a background shell's output under its pill as
  // it runs (live chunks) and settles on the whole output when it ends.
  const taskOut = new Map(); // task_id -> {box, pre, text}
  function renderTaskOutput(p) {
    const pill = p && p.task_id ? taskPills.get(p.task_id) : null;
    if (!pill || !pill.isConnected) return p && p.done ? foldIntoLast(p, 'raw (task output)') : null;
    let t = taskOut.get(p.task_id);
    if (!t) {
      const box = el('div', 'tool-live');
      const head = el('div', 'tool-live-head');
      head.appendChild(el('span', 'tool-live-dot'));
      head.appendChild(el('span', 'tool-live-label', 'output'));
      const toggle = el('button', 'hook-toggle tool-live-toggle', 'show all');
      toggle.type = 'button'; toggle.hidden = true;
      head.appendChild(toggle);
      box.appendChild(head);
      const pre = el('pre', 'tool-live-out');
      box.appendChild(pre);
      toggle.addEventListener('click', () => { box.classList.toggle('is-open'); toggle.textContent = box.classList.contains('is-open') ? 'show less' : 'show all'; });
      pill.classList.add('has-live');
      pill.appendChild(box);
      t = { box, pre, text: '', toggle };
      taskOut.set(p.task_id, t);
    }
    if (p.done) { t.text = p.text || t.text; t.box.classList.add('is-done'); t.box.querySelector('.tool-live-label').textContent = 'output · finished'; }
    else t.text += p.text || '';
    t.pre.textContent = t.text.length > 200000 ? t.text.slice(-200000) : t.text;
    const lines = (t.text.match(/\n/g) || []).length;
    t.toggle.hidden = lines < 12;
    if (!t.box.classList.contains('is-open')) t.pre.scrollTop = t.pre.scrollHeight;
    if (p.done) attachRaw(pill.closest('.frame'), p, 'raw (task output)');
    return null;
  }

  function renderTaskFrame(p) {
    const st = p.subtype;
    if (st !== 'background_tasks_changed' && (p.task_type === 'local_bash' || (p.tool_use_id && !lanes.has(p.tool_use_id) && toolRows.has(p.tool_use_id)) || taskPills.has(p.task_id))) {
      if (shellTaskFrame(p)) return null;
    }
    let lane = null;
    if (st === 'background_tasks_changed') {
      const pill = (p.tasks || []).map(t => taskPills.get(t.task_id)).find(Boolean);
      if (pill && pill.isConnected && !(p.tasks || []).some(t => tasksByID.has(t.task_id))) { attachRaw(pill.closest('.frame'), p, 'raw (background tasks changed)'); return null; }
      // only shells (or nothing at all) changed: the pill tells that story
      if (!(p.tasks || []).some(t => t.task_type !== 'local_bash' || tasksByID.has(t.task_id))) return foldIntoLast(p, 'raw (background tasks changed)');
      const ids = (p.tasks || []).map(t => t.task_id);
      lane = ids.map(id => tasksByID.get(id)).find(Boolean) || null;
      if (!lane) { const running = [...lanes.values()].filter(l => l !== mainLane && l.meta.status === 'running'); lane = running[running.length - 1] || null; }
      if (!lane) { const all = [...lanes.values()].filter(l => l !== mainLane); lane = all[all.length - 1] || null; }
      for (const t of p.tasks || []) { const l = tasksByID.get(t.task_id); if (l && t.description && !l.meta.desc) l.meta.desc = t.description; }
    } else lane = laneForTask(p);
    if (!lane || !lane.card) return bubble('system', 'system', el('div', 'frame-note', st.replace(/_/g, ' ') + (p.description ? ' · ' + p.description : '')), p);
    const m = lane.meta;
    const label = 'raw (' + st.replace(/_/g, ' ') + ')';
    if (st === 'task_started') { m.startAt = ts(curAt); if (p.description && !m.desc) m.desc = p.description; }
    else if (st === 'task_progress') { m.last = p.description || (p.last_tool_name ? 'Running ' + p.last_tool_name : ''); if (p.subagent_type && !m.type) m.type = p.subagent_type; refreshMainIdle(); }
    else if (st === 'task_updated' || st === 'task_notification') {
      const patch = p.patch || {};
      const status = st === 'task_updated' ? patch.status : (p.status || 'completed');
      if (status && status !== 'running') {
        let out = null;
        if (!lane.doneCard) out = agentDoneCard(lane, p); else attachRaw(lane.doneCard.closest('.frame'), p, label);
        laneFinish(lane, status, st === 'task_updated' ? (patch.end_time || ts(curAt)) : (m.endAt || ts(curAt)));
        if (st === 'task_notification' && p.summary) laneSummary(lane, p.summary);
        laneHeaderRefresh(lane);
        return out;
      }
    }
    laneHeaderRefresh(lane);
    attachRaw(lane.card.closest('.frame'), p, label);
    return null;
  }

  function toolRow(b) {
    if (b.name === 'AskUserQuestion' && b.input && Array.isArray(b.input.questions)) return askCard(b);
    if ((b.name === 'Agent' || b.name === 'Task') && b.input && (b.input.prompt || b.input.description)) return agentCard(b);
    if (b.name === 'SendMessage' && b.input && (b.input.message || b.input.content)) return peerOutCard(b);
    if (/^mcp__acta__/.test(b.name || '')) return actaCard(b);
    const t = el('div', 'msg-tool');
    t.appendChild(toolIcon(b.name));
    t.appendChild(el('span', 'tool-name', b.name || 'tool'));
    const inp = b.input || {};
    let arg = inp.command || inp.file_path || inp.path || inp.pattern || inp.url || inp.description || inp.prompt;
    if (b.name === 'SendMessage') { const to = inp.to || inp.recipient || '?'; arg = '\u2192 ' + (peerNames.get(to) || to) + ': ' + (inp.message || inp.content || inp.summary || ''); }
    if (arg) { const a = el('code', 'tool-arg', String(arg)); a.title = String(arg); t.appendChild(a); }
    if (b.id) { toolNames.set(b.id, b.name || 'tool'); toolRows.set(b.id, t); }
    return t;
  }

  // A permission_denied (Claude Code refused the call itself: outside the
  // working directory, a deny rule, no prompt host…) paints the tool pill
  // red with the reason; the frame folds into the pill's message. The
  // error tool_result that follows just repeats the message, so it folds too.
  function renderPermissionDenied(p) {
    const pill = toolRows.get(p.tool_use_id);
    if (!pill || !pill.isConnected) return bubble('permission denied', 'state', el('div', 'frame-note', p.message || p.decision_reason || 'permission denied'), p);
    pill.classList.add('is-denied');
    const fail = el('div', 'tool-fail');
    fail.appendChild(el('span', 'tool-fail-why', 'Not allowed' + (p.decision_reason ? ' · ' + p.decision_reason : '')));
    if (p.message) fail.appendChild(el('span', 'tool-fail-msg', p.message));
    pill.appendChild(fail);
    deniedTools.add(p.tool_use_id);
    attachRaw(pill.closest('.frame'), p, 'raw (permission denied)');
    return null;
  }

  function renderAssistant(p, at) {
    const content = (p.message && p.message.content) || [];
    const synthetic = !!(p.is_meta || (p.message && p.message.model === '<synthetic>'));
    // a real model turn (a skill, or a goal's work) ends the local-command
    // run: the result that follows belongs to the turn, not to a marker
    if (!synthetic && cmdFifo.length) { cmdFifo.length = 0; localCmd = null; }
    if (synthetic && !compact && content.length && content.every(b => b.type === 'text')) {
      const txt = content.map(b => b.text || '').join('\n').trim();
      const m = /^(Set effort level to (\w+)|Fast mode (enabled|disabled|unavailable|on|off)[^.]*)/i.exec(txt);
      const lc = cmdPick(txt, m ? (m[2] ? 'effort' : 'fast') : null);
      if (m && lc && !lc.out && lc.node.isConnected) {
        const t = lc.node.querySelector('.local-text');
        if (lc.kind === 'effort') { t.textContent = 'effort set to ' + m[2]; curEffort = m[2]; }
        else {
          const on = /enabled|on\b/i.test(m[3] || '');
          const un = /unavailable/i.test(m[3] || '');
          t.textContent = un ? txt.replace(/^Fast mode unavailable:\s*/i, 'fast mode unavailable · ') : on ? 'fast mode on' : 'fast mode off';
          if (!un) { fastOn = on; fastReason = ''; }
          lc.node.classList.toggle('is-error', un);
        }
        paintModelSelect();
        lc.replied = true;
        attachRaw(lc.node, p, 'raw (reply)');
        return null;
      }
      if (lc && (lc.out || lc.silent) && lc.node.isConnected) return cmdReply(lc, txt, p);
    }
    if (synthetic && !compact && content.length && content.every(b => b.type === 'text')) {
      const txt = content.map(b => b.text || '').join('\n').trim();
      if (/^(Goal |No goal)/.test(txt) && noteGoalText(txt, null)) return goalMarker(txt, p);
    }
    // A meta message from Claude Code itself (model "<synthetic>"): during a
    // compaction it's the explanation of the outcome, so it folds in.
    // Claude Code replays the output of every earlier local command
    // (/context, /usage…) and its own notices as synthetic messages after a
    // compaction: they fold in verbatim; the block's line keeps its counts.
    if (compact && compact.done && (p.is_meta || (p.message && p.message.model === '<synthetic>'))) {
      const txt = content.filter(b => b.type === 'text').map(b => b.text).join('\n').trim();
      return compactFold(p, txt.length <= 200 && !txt.includes('\n') ? 'raw (message)' : 'raw (replayed command output)');
    }
    if (content.length && content.every(b => b.type === 'thinking')) {
      return thoughtChip(content[0], at);
    }
    const body = el('div', 'frame-body');
    let folded = null, foldedTask = false;
    for (const b of content) {
      if (b.type === 'text') body.appendChild(mdRender(b.text));
      else if (b.type === 'thinking') body.appendChild(thoughtChip(b, at));
      else if (b.type === 'tool_use') {
        const tn = taskBlock(b);
        const pn = tn === undefined ? planBlock(b) : undefined;
        if (tn !== undefined) { if (tn) body.appendChild(tn); else foldedTask = true; }
        else if (pn === undefined) body.appendChild(toolRow(b));
        else if (pn) body.appendChild(pn);
        else folded = planTools.get(b.id) || folded;
      }
      else body.appendChild(el('div', 'msg-unknown', 'unrendered block: ' + b.type));
    }
    if (folded && !body.children.length) {
      // every block folded into an existing plan card: the frame is only its payload
      const f = planFrame(folded);
      if (f) { attachRaw(f, p, 'raw (plan write)'); return null; }
    }
    if (foldedTask && !body.children.length) {
      const f = taskCard && taskCard.frame();
      if (f) { attachRaw(f, p, 'raw (task call)'); return null; }
    }
    return bubble(modelName((p.message && p.message.model) || curModel), 'assistant', body, p);
  }

  // resultBlock shows a tool result's text, clamped past ~12 lines with a
  // "show all" control so a long file read doesn't swallow the transcript.
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
  // An Edit or Write result carries the change as data: tool_use_result has
  // structuredPatch (unified-diff hunks) for an edit, and the whole content
  // for a file Write creates. Those render as a diff instead of Claude Code's
  // one-line "updated successfully" text.

  function diffable(tur) {
    return tur && ((Array.isArray(tur.structuredPatch) && tur.structuredPatch.length > 0) || (tur.type === 'create' && typeof tur.content === 'string'));
  }
  function diffLine(sign, oldNo, newNo, text) {
    const row = el('div', 'diff-line ' + (sign === '+' ? 'is-add' : sign === '-' ? 'is-del' : 'is-ctx'));
    row.appendChild(el('span', 'diff-no', oldNo == null ? '' : String(oldNo)));
    row.appendChild(el('span', 'diff-no', newNo == null ? '' : String(newNo)));
    row.appendChild(el('span', 'diff-sign', sign === ' ' ? '' : sign));
    row.appendChild(el('span', 'diff-text', text));
    return row;
  }
  function diffBlock(tur) {
    const wrap = el('div', 'diff');
    const head = el('div', 'diff-head');
    const file = tur.filePath || '';
    const nameEl = el('span', 'diff-file', file.split('/').pop() || 'file');
    nameEl.title = file;
    head.appendChild(nameEl);
    const rows = el('div', 'diff-rows');
    let add = 0, del = 0, count = 0;
    if (tur.type === 'create' && typeof tur.content === 'string') {
      const lines = tur.content.replace(/\n$/, '').split('\n');
      lines.forEach((l, i) => { rows.appendChild(diffLine('+', null, i + 1, l)); });
      add = lines.length; count = lines.length;
      head.appendChild(el('span', 'diff-kind', 'new file'));
    } else {
      for (const h of tur.structuredPatch) {
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
      if (tur.replaceAll) head.appendChild(el('span', 'diff-kind', 'replace all'));
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

  function renderUser(p) {
    // Claude stream-json "user" messages are either a replay of our own input
    // (string content — skip it, the 'input' frame already showed it) or
    // tool_result blocks (array content — render them).
    const content = (p.message && p.message.content);
    if (compact && compact.done) {
      if (typeof content === 'string' && !p.isReplay && !compact.summary) { compactSummary(p, content); return null; }
      if (p.isReplay) return compactFold(p, 'raw (echo)');
    }
    if (p.isReplay) return renderEcho(p);
    if (typeof content === 'string') return null;
    const blocks = content || [];
    if (blocks.length && blocks.every(b => b.type === 'tool_result' && b.is_error && deniedTools.has(b.tool_use_id))) {
      const pill = toolRows.get(blocks[0].tool_use_id);
      if (pill && pill.isConnected) { attachRaw(pill.closest('.frame'), p, 'raw (denied result)'); return null; }
    }
    if (blocks.length && blocks.every(b => b.type === 'text')) {
      const txt = blocks.map(b => b.text || '').join('\n').trim();
      // Stop: Claude Code injects "[Request interrupted by user]" as a user
      // turn, then ends the turn with an error result and (SIGINT) exits.
      // The three fold into one "turn interrupted" divider.
      if (/^\[Request interrupted/i.test(txt)) { pendingInterrupt = p; dropLive(cur); return null; }
      const body = el('div', 'frame-body');
      body.appendChild(el('div', 'msg-text', txt));
      return bubble('user', 'toolresult', body, p);
    }
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && lanes.has(blocks[0].tool_use_id) && lanes.get(blocks[0].tool_use_id).card) {
      const lane = lanes.get(blocks[0].tool_use_id);
      const c = blocks[0].content;
      const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : '';
      if (/^Async agent launched/i.test(txt.trim())) attachRaw(lane.card.closest('.frame'), p, 'raw (launch)');
      else {
        if (blocks[0].is_error) laneFinish(lane, 'failed', ts(curAt)); else { laneSummary(lane, txt); laneFinish(lane, 'completed', ts(curAt)); }
        attachRaw(lane.card.closest('.frame'), p, 'raw (agent result)');
      }
      return null;
    }
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && taskCalls.has(blocks[0].tool_use_id)) return taskResult(taskCalls.get(blocks[0].tool_use_id), blocks[0], p);
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && peerOut.has(blocks[0].tool_use_id) && peerOut.get(blocks[0].tool_use_id).card.isConnected) return peerOutResult(peerOut.get(blocks[0].tool_use_id), blocks[0], p);
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && actaCards.has(blocks[0].tool_use_id) && actaCards.get(blocks[0].tool_use_id).card.isConnected) return actaResult(actaCards.get(blocks[0].tool_use_id), blocks[0], p);
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && planTools.has(blocks[0].tool_use_id)) return planResult(planTools.get(blocks[0].tool_use_id), blocks[0], p);
    if (blocks.length === 1 && blocks[0].type === 'tool_result' && askCards.has(blocks[0].tool_use_id)) {
      const card = askCards.get(blocks[0].tool_use_id);
      if (card.node.isConnected) {
        const c = blocks[0].content;
        const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : '';
        if (!card.node.classList.contains('is-answered')) {
          const parsed = {};
          for (const m of txt.matchAll(/"([^"]+)"="([^"]*)"/g)) parsed[m[1]] = m[2];
          if (Object.keys(parsed).length) showAnswers(blocks[0].tool_use_id, parsed);
          else if (blocks[0].is_error) { card.node.classList.add('is-denied'); card.node.appendChild(el('div', 'ask-note', txt)); }
        }
        attachRaw(card.node.closest('.frame'), p, 'raw (answer result)');
        return null;
      }
    }
    // One result frame per tool_result. When the call's pill is the last thing
    // on screen (nothing happened in between) the pill moves in as the result
    // frame's header, and the assistant frame it came from is hidden if that
    // emptied it. Otherwise the result gets a copy of the call as its header.
    const frames = [];
    const misc = blocks.filter(b => b.type !== 'tool_result');
    for (const b of blocks) {
      if (b.type !== 'tool_result') continue;
      const c = b.content;
      const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : c == null ? '' : JSON.stringify(c);
      const name = toolNames.get(b.tool_use_id);
      const body = el('div', 'frame-body');
      let sent = null;
      if (name === 'SendMessage' && !b.is_error) { try { sent = JSON.parse(txt); } catch (_) { sent = null; } }
      if (sent && typeof sent === 'object' && 'success' in sent) {
        const line = el('div', 'msg-result peer-sent' + (sent.success ? '' : ' is-error'));
        line.appendChild(svg(sent.success ? ['M3.5 8.5 6.5 11.5 12.5 5'] : ICONS.warn));
        line.appendChild(el('span', null, sent.success ? (sent.message || 'delivered') : (sent.error || sent.message || 'not delivered')));
        body.appendChild(line);
      }
      else if (!b.is_error && diffable(p.tool_use_result)) body.appendChild(diffBlock(p.tool_use_result));
      else body.appendChild(resultBlock(txt, b.is_error));
      const node = bubble(name ? name + ' result' : 'result', 'toolresult', body, p);
      if (b.is_error) node.classList.add('is-error');
      const pill = toolRows.get(b.tool_use_id);
      if (pill && pill.isConnected && pill.classList.contains('msg-tool')) {
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
      frames.push(node);
    }
    if (misc.length) {
      const body = el('div', 'frame-body');
      for (const b of misc) body.appendChild(el('div', 'msg-unknown', 'unrendered block: ' + b.type));
      frames.push(bubble('user', 'toolresult', body, p));
    }
    if (frames.length === 1) return frames[0];
    const group = el('div', 'frame-group');
    for (const f of frames) group.appendChild(f);
    return group;
  }

  // A result frame ends a turn. Its text duplicates the last assistant
  // message, so render only the accounting as a slim divider; the verbatim
  // payload (text included) stays behind the raw toggle.
  let pendingInterrupt = null;  // the "[Request interrupted by user]" frame awaiting its result
  let interruptDivider = null;  // the divider still waiting for the exit that follows a Stop

  function renderResult(p) {
    const done = cmdFifo.find(x => x.replied) || (!(p.num_turns > 0) ? cmdFifo[0] : null);
    if (done && done.node.isConnected) {
      // the empty turn that closes a local command
      attachRaw(done.node, p, done.silent ? 'raw (/' + done.name + ' result)' : 'raw (result)');
      cmdForget(done);
      return null;
    }
    if (done) cmdForget(done);
    const bits = [];
    const interrupted = !!pendingInterrupt || p.terminal_reason === 'aborted_streaming';
    const RESULT_ERRORS = { error_max_turns: 'max turns reached', error_max_budget_usd: 'budget exhausted', error_max_structured_output_retries: 'structured output failed', error_during_execution: 'error during execution' };
    if (!interrupted && p.subtype && p.subtype !== 'success') bits.push(RESULT_ERRORS[p.subtype] || p.subtype.replace(/_/g, ' '));
    if (typeof p.num_turns === 'number') bits.push(p.num_turns + (p.num_turns === 1 ? ' call' : ' calls'));
    if (typeof p.duration_ms === 'number') bits.push((p.duration_ms / 1000).toFixed(1) + 's');
    const u = p.usage || {};
    const tok = (u.input_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0) + (u.output_tokens || 0);
    if (tok) bits.push(fmtTokens(tok) + ' tok');
    if (weeklyNow != null && weeklyAtTurnStart != null) {
      const d = weeklyNow - weeklyAtTurnStart;
      bits.push('weekly ' + (d > 0.0005 ? '+' + fmtPct(d) : '+<1%'));
      weeklyAtTurnStart = weeklyNow;
    }
    if (p.permission_denials && p.permission_denials.length) bits.push(p.permission_denials.length + ' denied');
    if (interrupted) {
      const d = divider('turn interrupted', bits, p, 'is-interrupted');
      if (pendingInterrupt) { attachRaw(d, pendingInterrupt, 'raw (interrupt)'); pendingInterrupt = null; }
      interruptDivider = d;
      return d;
    }
    const failed = p.is_error || (p.subtype && p.subtype !== 'success');
    const d = divider('turn ended', bits, p, failed ? 'is-error' : '');
    // the CLI's own explanation(s), minus its internal diagnostics tags
    const msgs = (Array.isArray(p.errors) ? p.errors : []).map(String).filter(m => m.trim() && !/^\[ede_diagnostic\]/.test(m));
    if (failed && msgs.length) {
      const box = el('div', 'result-errors');
      for (const m of msgs) box.appendChild(el('div', 'result-error', m));
      d.insertBefore(box, d.querySelector(':scope > .frame-tools'));
    }
    return d;
  }

  // The most recent session started/resumed divider still waiting for its
  // init frame, which fills in the model name and adds its raw payload.
  let lastSessionDivider = null;

  // divider is the slim full-width rule used to mark a boundary in the
  // transcript: a turn ending, a session starting or resuming.
  function divider(label, bits, payload, extraCls, rawLabel) {
    const wrap = el('div', 'frame frame--result' + (extraCls ? ' ' + extraCls : ''));
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', label));
    if (bits && bits.length) line.appendChild(el('span', 'result-stats', bits.join(' · ')));
    wrap.appendChild(line);
    attachRaw(wrap, payload, rawLabel);
    return wrap;
  }

  // --- compaction ---
  //
  // A compaction is a run of frames: status {compacting}… → the
  // SessionStart:compact hook → status {compact_result} → a fresh init →
  // compact_boundary {compact_metadata} → a user frame carrying the summary
  // text → an echoed local-command replay → an empty result. They fold into
  // one block: an obvious in-progress banner while it runs, then a marker
  // with the token counts and a toggle to read the summary that replaced the
  // earlier context. Every folded frame's raw payload hangs off the block.

  let compact = null; // { node, title, stats, bar, line, done, summary }

  function compactBlock(p, rawLabel) {
    const node = el('div', 'frame frame--compact');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', 'compaction'));
    node.appendChild(head);
    const line = el('div', 'compact-line');
    line.appendChild(svg(ICONS.compact));
    const title = el('span', 'compact-title', 'Compacting context…');
    line.appendChild(title);
    const stats = el('span', 'compact-stats', contextUsed ? fmtTokens(contextUsed) + ' tokens in the window' : '');
    line.appendChild(stats);
    node.appendChild(line);
    const bar = el('div', 'compact-bar');
    node.appendChild(bar);
    attachRaw(node, p, rawLabel);
    const cc = cmdFifo.find(x => x.kind === 'compact' && x.node.isConnected);
    if (cc) {
      if (cc.args) stats.textContent = '“' + cc.args + '”';
      mergeRaw(cc.node, node);
      cc.node.remove();
      cmdForget(cc);
    }
    compact = { node, title, stats, bar, line, done: false, summary: false };
    return node;
  }

  function compactFold(p, rawLabel) {
    attachRaw(compact.node, p, rawLabel);
    return null;
  }

  function compactDone(p) {
    const m = p.compact_metadata || {};
    const bits = [];
    if (m.pre_tokens || m.post_tokens) bits.push((m.pre_tokens ? fmtTokens(m.pre_tokens) : '?') + ' → ' + (m.post_tokens ? fmtTokens(m.post_tokens) : '?') + ' tokens');
    if (typeof m.duration_ms === 'number') bits.push((m.duration_ms / 1000).toFixed(1) + 's');
    if (m.trigger) bits.push(m.trigger);
    const fresh = !compact;
    if (fresh) compactBlock(p, 'raw (boundary)'); else attachRaw(compact.node, p, 'raw (boundary)');
    compact.done = true;
    compact.node.classList.add('is-done');
    compact.title.textContent = 'Compacted context';
    compact.stats.textContent = bits.join(' · ');
    return fresh ? compact.node : null;
  }

  function compactSummary(p, text) {
    attachRaw(compact.node, p, 'raw (summary)');
    compact.summary = true;
    const body = el('div', 'compact-body');
    body.hidden = true;
    body.appendChild(mdRender(text));
    const size = ' · ' + fmtTokens(Math.round(text.length / 4)) + ' tok';
    const toggle = el('button', 'compact-toggle', 'show summary' + size);
    toggle.type = 'button';
    toggle.addEventListener('click', () => {
      body.hidden = !body.hidden;
      toggle.textContent = (body.hidden ? 'show summary' : 'hide summary') + size;
    });
    compact.line.appendChild(toggle);
    compact.node.insertBefore(body, compact.node.querySelector(':scope > .frame-tools'));
  }

  // compactClose ends the fold: anything that isn't part of the compaction
  // run renders on its own again.
  function compactClose() { compact = null; }

  // --- hooks ---
  //
  // A hook run arrives as two system frames sharing a hook_id: hook_started
  // (name + event) and hook_response (outcome, exit code, stdout/stderr and
  // the JSON output whose additionalContext is injected into the
  // conversation). They fold into one row that can be expanded to read what
  // was injected; both frames' raw payloads hang off that row.

  const hooks = new Map(); // hook_id -> {node, status, line}

  function hookStatusNode(status, text) {
    return el('span', 'hook-status is-' + status, text || status);
  }

  function hookRow(p) {
    const node = el('div', 'frame frame--hook');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', 'hook'));
    node.appendChild(head);
    const line = el('div', 'hook-line');
    line.appendChild(svg(ICONS.hook));
    line.appendChild(el('span', 'hook-lbl', 'Hook'));
    line.appendChild(el('code', 'hook-name', p.hook_name || p.hook_event || p.hook_id || 'hook'));
    const status = hookStatusNode('running');
    line.appendChild(status);
    node.appendChild(line);
    const e = { node, line, status, answered: false };
    if (p.hook_id) hooks.set(p.hook_id, e);
    return e;
  }

  function renderHookStarted(p) {
    const e = hookRow(p);
    attachRaw(e.node, p, 'raw (started)');
    return e.node;
  }

  // hookInjected extracts what the hook fed back: the additionalContext (or
  // any other structured output) from the JSON on stdout, else raw stdout.
  function hookInjected(p) {
    const out = (typeof p.output === 'string' && p.output) || (typeof p.stdout === 'string' && p.stdout) || '';
    if (!out.trim()) return null;
    let parsed = null;
    try { parsed = JSON.parse(out); } catch (_) {}
    if (parsed && typeof parsed === 'object') {
      const hso = parsed.hookSpecificOutput || {};
      const ctx = hso.additionalContext || parsed.additionalContext || parsed.systemMessage;
      if (typeof ctx === 'string' && ctx.trim()) {
        const rest = Object.assign({}, parsed);
        if (rest.hookSpecificOutput) { rest.hookSpecificOutput = Object.assign({}, hso); delete rest.hookSpecificOutput.additionalContext; if (Object.keys(rest.hookSpecificOutput).length <= 1) delete rest.hookSpecificOutput; }
        delete rest.additionalContext; delete rest.systemMessage;
        return { md: ctx, extra: Object.keys(rest).length ? rest : null };
      }
      return { json: parsed };
    }
    return { text: out };
  }

  function renderHookResponse(p) {
    let e = hooks.get(p.hook_id);
    const fresh = !e;
    if (fresh) e = hookRow(p);
    if (e.answered) return fresh ? e.node : null; // duplicate response: keep the first
    e.answered = true;
    const ok = (p.outcome ? p.outcome === 'success' : true) && !(p.exit_code);
    const text = ok ? 'ok' : ((p.outcome && p.outcome !== 'success') ? p.outcome.replace(/_/g, ' ') : 'failed') + (p.exit_code ? ' · exit ' + p.exit_code : '');
    e.status.replaceWith(hookStatusNode(ok ? 'success' : 'failed', text));
    e.status = e.line.querySelector('.hook-status');
    if (!ok) e.node.classList.add('is-error');
    const inj = hookInjected(p);
    const err = typeof p.stderr === 'string' && p.stderr.trim() ? p.stderr : '';
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
    attachRaw(e.node, p, fresh ? 'raw' : 'raw (response)');
    return fresh ? e.node : null;
  }

  // notice draws a leveled system message (model fallback, refusal, a
  // permission retry, an informational line) as a pill coloured by level.
  function notice(level, text, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(level === 'error' ? ICONS.alert : level === 'warning' ? ICONS.warn : ICONS.info));
    note.appendChild(el('span', null, text));
    const node = bubble('notice', 'state frame--notice', note, p);
    if (level === 'error') node.classList.add('is-error');
    else if (level === 'warning') node.classList.add('is-warn');
    return node;
  }

  // API retries arrive as one frame per attempt (attempt, max_retries,
  // retry_delay_ms, error, error_status) — ten of them with growing delays
  // when the API is unreachable — followed by api_error if they run out.
  // They fold into one row that updates in place and settles once the API
  // answers again or the turn fails.
  let apiRetry = null; // { node, text, count, failed }

  function renderApiRetry(p) {
    if (!apiRetry || !apiRetry.node.isConnected) {
      const note = el('div', 'frame-note');
      note.appendChild(svg(ICONS.warn));
      const text = el('span', 'local-text', '');
      note.appendChild(text);
      const node = bubble('api', 'state frame--notice frame--apiretry is-warn', note, p);
      apiRetry = { node, text, count: 0, failed: false };
    } else attachRaw(apiRetry.node, p, 'raw (' + p.subtype.replace(/_/g, ' ') + ')');
    const err = p.error && p.error !== 'unknown' ? String(p.error) : '';
    if (p.subtype === 'api_error') {
      apiRetry.failed = true;
      apiRetry.node.classList.remove('is-warn'); apiRetry.node.classList.add('is-error');
      apiRetry.text.textContent = 'API error' + (apiRetry.count ? ' after ' + apiRetry.count + (apiRetry.count === 1 ? ' retry' : ' retries') : '') + (err ? ' · ' + err : '');
      setActivity('API error');
      const done = apiRetry; apiRetry = null;
      return done.node.isConnected ? null : done.node;
    }
    apiRetry.count = p.attempt || apiRetry.count + 1;
    const wait = typeof p.retry_delay_ms === 'number' ? ' · next in ' + (p.retry_delay_ms >= 1000 ? Math.round(p.retry_delay_ms / 1000) + 's' : p.retry_delay_ms + 'ms') : '';
    const status = p.error_status ? ' · HTTP ' + p.error_status : '';
    apiRetry.text.textContent = 'Retrying the API · attempt ' + apiRetry.count + (p.max_retries ? ' of ' + p.max_retries : '') + wait + status + (err ? ' · ' + err : '');
    setActivity('Retrying the API · ' + apiRetry.count + (p.max_retries ? '/' + p.max_retries : ''));
    return apiRetry.count === 1 ? apiRetry.node : null;
  }

  // apiRetrySettle closes the retry row once something else happens: an
  // answer means the API recovered; a failed turn means it did not.
  function apiRetrySettle(kind, payload) {
    if (!apiRetry) return;
    if (kind === 'assistant' || kind === 'stream_event') {
      apiRetry.node.classList.remove('is-warn');
      apiRetry.text.textContent = 'API recovered after ' + apiRetry.count + (apiRetry.count === 1 ? ' retry' : ' retries');
      apiRetry = null;
    } else if (kind === 'result') {
      if (payload && payload.is_error) { apiRetry.node.classList.remove('is-warn'); apiRetry.node.classList.add('is-error'); apiRetry.text.textContent = 'API gave up after ' + apiRetry.count + ' retries'; }
      apiRetry = null;
    }
  }

  function renderSystem(p) {
    if (p.subtype === 'thinking_tokens') return null; // folded into the activity line and the thought chip
    if (p.subtype === 'hook_started') return renderHookStarted(p);
    if (p.subtype === 'hook_response') return renderHookResponse(p);
    if (p.subtype === 'status' && p.status === 'compacting') {
      return compact && !compact.done ? compactFold(p, 'raw (status)') : compactBlock(p, 'raw (status)');
    }
    if (p.subtype === 'status' && p.compact_result && compact) {
      if (p.compact_result !== 'success') {
        compact.done = true;
        compact.node.classList.add('is-done', 'is-error');
        compact.title.textContent = 'Compaction ' + String(p.compact_result).replace(/_/g, ' ');
        compact.stats.textContent = p.compact_error || '';
      }
      return compactFold(p, 'raw (status)');
    }
    if (p.subtype === 'compact_boundary') return compactDone(p);
    if (p.subtype === 'permission_denied') return renderPermissionDenied(p);
    if (/^(task_started|task_progress|task_updated|task_notification|background_tasks_changed)$/.test(p.subtype || '')) return renderTaskFrame(p);
    if (p.subtype === 'status' && p.permissionMode) {
      setMode(p.permissionMode);
      if (lastModeMark && lastModeMark.isConnected) { attachRaw(lastModeMark, p, 'raw (status)'); lastModeMark = null; return null; }
      return modeMarker(p.permissionMode, p);
    }
    if (p.subtype === 'status' && typeof p.status === 'string' && Object.keys(p).every(k => ['type', 'subtype', 'uuid', 'session_id', 'status'].includes(k))) return foldIntoLast(p, 'raw (status ' + p.status + ')');
    if (p.subtype === 'api_retry' || p.subtype === 'api_error') return renderApiRetry(p);
    if (p.subtype === 'mirror_error') return notice('error', 'mirror error · ' + (p.error || ''), p);
    if (typeof p.content === 'string' && /hook blocked the turn from ending/i.test(p.content) && /overriding/i.test(p.content)) goalOverridden = true;
    if (typeof p.content === 'string' && p.content && (p.level || /fallback|refusal|informational|permission_retry|bridge_status|notification/.test(p.subtype || ''))) {
      if (/^model_.*fallback$/.test(p.subtype) && p.fallbackModel) { curModel = p.fallbackModel; paintModelSelect(); }
      return notice(p.level || 'info', p.content, p);
    }
    let note = p.subtype || 'system';
    if (p.subtype === 'status') {
      // an otherwise-empty status frame: name whatever it does carry
      const keys = Object.keys(p).filter(k => !['type', 'subtype', 'uuid', 'session_id'].includes(k) && p[k] != null);
      note = p.status ? 'status: ' + p.status : keys.length ? 'status · ' + keys.map(k => k + ' ' + (typeof p[k] === 'object' ? JSON.stringify(p[k]) : p[k])).join(' · ') : 'status update';
    }
    else if (p.subtype === 'init') {
      if (compact) return compactFold(p, 'raw (init)');
      // Folded into the session started/resumed divider: its stats get the
      // model name and its verbatim payload hangs off the divider as a
      // second raw button. Claude Code also emits an init at the start of
      // every turn; with no divider waiting, that one hangs off the message
      // that started the turn (the nearest thing above it). Neither present
      // (a replay starting mid-session)? Then it gets its own quiet divider.
      if (!lastSessionDivider && lastInput && lastInput.node.isConnected) {
        attachRaw(lastInput.node, p, 'raw (init)');
        return null;
      }
      if (lastSessionDivider) {
        const stats = lastSessionDivider.querySelector('.result-stats');
        if (stats) stats.textContent = modelName(p.model); else lastSessionDivider.querySelector('.result-line').appendChild(el('span', 'result-stats', modelName(p.model)));
        attachRaw(lastSessionDivider, p, 'raw (init)');
        lastSessionDivider = null;
        return null;
      }
      return divider('session init', [modelName(p.model)], p, 'frame--session');
    }
    else if (p.subtype === 'api_retry') note = 'API retry ' + (p.attempt || '') + ': ' + (p.error || '');
    return bubble('system', 'system', el('div', 'frame-note', note), p);
  }

  // rate_limit_event is fully rendered by the header gauges (utilisation,
  // reset time, status, overage all live there), so it takes no room in the log.
  function renderRateLimit() { return null; }

  // The most recent input bubble, so a delivery failure that follows it can
  // be shown on the message itself (red, with the reason and a Retry button)
  // rather than as a separate frame.
  let lastInput = null; // { node, text, failed, payload, failPayload }
  // Retry buttons still showing on earlier failed messages. A newer message
  // (an input frame, not just any frame — a resume produces frames too)
  // supersedes them, so they're removed when the next input renders.
  const retryButtons = [];
  // Failed inputs still on screen. A retry sends the same text again, so a
  // later input with identical text replaces the failed bubble: the failed
  // attempt and its failure fold into the new bubble's raw panel (whether or
  // not the new one fails in turn).
  const failedInputs = [];

  // A local command turn ("/effort low", "/fast") is a marker in the
  // transcript rather than a message: the CLI's one-line confirmation and
  // the empty result that follow fold into it.
  let localCmd = null; // the marker made last: { node, kind, name, args, out, status, line, replied }
  // Commands still waiting for their reply or their closing result, oldest
  // first. Several can be outstanding (a message queued before the process
  // started, the rename Acta sends at spawn) and Claude Code answers them in
  // the order it reads them, which need not be the order they were shown —
  // so a reply is matched to a marker by its shape first, then by age.
  const cmdFifo = [];
  const REPLY_RE = {
    rename: /^Session renamed/i, name: /^Session renamed/i, model: /^(Set model|Current model)/i, effort: /^Set effort/i, fast: /^Fast mode/i,
    context: /Context Usage/i, usage: /(subscription|usage|limits)/i, cost: /(subscription|usage|limits)/i, stats: /(subscription|usage|limits)/i,
    autocompact: /^Auto-compact window/i, config: /^(Set |Usage: \/config)/i, goal: /^(Goal |No goal)/i, compact: /compact/i, color: /^Session color/i, mcp: /MCP server/i,
  };
  function cmdPick(txt, kindHint) {
    const open = cmdFifo.filter(x => !x.replied);
    if (!open.length) return null;
    if (kindHint) return open.find(x => x.kind === kindHint) || null;
    return open.find(x => REPLY_RE[x.name] && REPLY_RE[x.name].test(txt)) || open.find(x => !REPLY_RE[x.name]) || open[0];
  }
  function cmdForget(lc) { const i = cmdFifo.indexOf(lc); if (i >= 0) cmdFifo.splice(i, 1); if (localCmd === lc) localCmd = cmdFifo[cmdFifo.length - 1] || null; }
  function localMarker(kind, text, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(kind === 'effort' ? ICONS.gauge : ICONS.bolt));
    note.appendChild(el('span', 'local-text', text));
    const node = bubble('status', 'state frame--mode', note, p);
    localCmd = { node, kind, name: kind, args: '', out: null, status: null, line: null, replied: false };
    cmdFifo.push(localCmd);
    return node;
  }

  // --- slash commands in the transcript ---
  //
  // A "/command" input is a marker, not a message. Claude Code's own
  // commands answer with a synthetic assistant text (model "<synthetic>",
  // is_meta) and an empty result, which fold into the marker as its output.
  // A skill runs a real model turn: its echo (<command-message>…) folds into
  // the marker and everything after renders as usual. /context, /usage and
  // /autocompact feed the context panel under the gauges; /goal feeds the
  // goal pill; /clear is answered by a conversation_reset frame (below).
  const REPORT_CMDS = { context: 'context', usage: 'usage', cost: 'usage', stats: 'usage', autocompact: 'autocompact' };
  function cmdMarker(name, args, p) {
    const kind = name === 'clear' ? 'clear' : name === 'compact' ? 'compact' : name === 'goal' ? 'goal' : REPORT_CMDS[name] ? 'report' : 'cmd';
    // a report the context panel renders takes no room in the transcript:
    // its input, reply and result hang off the frame above it, verbatim
    if (kind === 'report') {
      const host = lastVisibleFrame(mainLane.log);
      if (host) {
        attachRaw(host, p, 'raw (/' + name + ')');
        localCmd = { node: host, kind, name, args, out: null, status: null, line: null, replied: false, silent: true };
        cmdFifo.push(localCmd);
        return null;
      }
    }
    const wrap = el('div', 'frame frame--cmd');
    const line = el('div', 'cmd-line');
    line.appendChild(svg(kind === 'goal' ? ICONS.goal : ICONS.slash));
    const code = el('code', 'cmd-code', '/' + name + (args ? ' ' + args : ''));
    code.title = '/' + name + (args ? ' ' + args : '');
    line.appendChild(code);
    const status = el('span', 'cmd-status', '');
    line.appendChild(status);
    wrap.appendChild(line);
    const out = el('div', 'cmd-out');
    out.hidden = true;
    wrap.appendChild(out);
    attachRaw(wrap, p, 'raw (input)');
    localCmd = { node: wrap, kind, name, args, out, status, line, replied: false };
    cmdFifo.push(localCmd);
    return wrap;
  }
  function cmdToggle(lc, label) {
    const t = el('button', 'cmd-toggle', 'show ' + label);
    t.type = 'button';
    t.addEventListener('click', () => { lc.out.hidden = !lc.out.hidden; t.textContent = (lc.out.hidden ? 'show ' : 'hide ') + label; });
    lc.line.appendChild(t);
  }
  // cmdReply folds Claude Code's answer to a local command into its marker.
  function cmdReply(lc, txt, p) {
    lc.replied = true;
    if (lc.silent) { attachRaw(lc.node, p, 'raw (/' + lc.name + ' reply)'); noteReport(REPORT_CMDS[lc.name], txt); return null; }
    attachRaw(lc.node, p, 'raw (reply)');
    const err = /isn't available in this environment|^Unknown command|^Usage:/i.test(txt);
    lc.node.classList.toggle('is-error', err);
    if (lc.kind === 'goal') noteGoalText(txt, lc);
    if (/^Set Auto-compact to (true|false)/i.test(txt)) { ac.enabled = /true/i.test(txt); paintGaugePop(); }
    if (lc.kind === 'report') {
      noteReport(REPORT_CMDS[lc.name], txt);
      lc.out.appendChild(mdRender(txt));
      if (REPORT_CMDS[lc.name] === 'usage') lc.out.classList.add('is-text');
      lc.status.textContent = '· in the context panel';
      cmdToggle(lc, 'output');
      return null;
    }
    const plain = !/[#*|`_\[]/.test(txt);
    if (plain && !txt.includes('\n') && txt.length <= 120) { lc.status.textContent = '· ' + txt; return null; }
    lc.out.appendChild(mdRender(txt));
    if (plain) lc.out.classList.add('is-text');
    lc.out.hidden = false;
    return null;
  }

  // --- reports: /context, /usage, /autocompact ---
  const reports = {}; // key -> { text, at }
  const ac = { enabled: null, window: '' }; // auto-compact, as last reported
  let lastTurnEnd = 0;
  function noteReport(key, txt) {
    reports[key] = { text: txt, at: Date.now() };
    if (key === 'autocompact') {
      const m = /Auto-compact window(?: set to|:)\s*([\w.,]+)/i.exec(txt);
      if (m) ac.window = m[1].toLowerCase() === 'auto' ? 'auto' : m[1];
    }
    paintGaugePop();
  }

  // --- goal ---
  //
  // /goal <condition> makes Claude Code keep working until the condition is
  // met (a Stop hook keeps the turn going). Its state only ever arrives as
  // synthetic text ("Goal set: …", "Goal active: … (N turns) Last check: …",
  // "No goal set"), parsed here into the header pill.
  let goal = null; // { cond, state: 'active'|'met'|'unmet', turns, last }
  let goalOverridden = false; // Claude Code gave up blocking the stop (hook cap)
  // goalTurnEnded: a turn ending while a goal is active means the Stop hook
  // let it through — the condition held — unless Claude Code reported
  // overriding the hook, in which case it gave up on it.
  function goalTurnEnded() {
    if (!goal || goal.state !== 'active') return;
    goal = { ...goal, state: goalOverridden ? 'unmet' : 'met' };
    goalOverridden = false;
    paintGoal();
  }
  function noteGoalText(txt, lc) {
    let m;
    const t = txt.trim();
    if ((m = /^Goal set:\s*([\s\S]+)$/.exec(t))) goal = { cond: m[1].trim(), state: 'active', turns: 0, last: '' };
    else if ((m = /^Goal active:\s*([\s\S]*?)\s*\((\d+) turns?\)\s*(?:Last check:\s*([\s\S]*))?$/.exec(t))) goal = { cond: m[1].trim(), state: 'active', turns: +m[2], last: (m[3] || '').trim() };
    else if (/^No goal set/i.test(t)) { if (!goal || goal.state === 'active' || (lc && /^clear\b/i.test(lc.args || ''))) goal = null; }
    else if ((m = /^Goal (?:met|reached|completed?|satisfied|achieved|done)\b[:\s]*([\s\S]*)$/i.exec(t))) goal = { cond: goal ? goal.cond : '', state: 'met', turns: goal ? goal.turns : 0, last: m[1].trim() };
    else return false;
    paintGoal();
    return true;
  }
  function goalMarker(txt, p) {
    const note = el('div', 'frame-note');
    note.appendChild(svg(ICONS.goal));
    note.appendChild(el('span', 'local-text', txt.length > 160 ? txt.slice(0, 157) + '…' : txt));
    return bubble('status', 'state frame--mode', note, p);
  }

  // --- /clear ---
  //
  // Claude Code answers /clear with conversation_reset {new_conversation_id}
  // then a fresh init and an empty result. The transcript before it folds
  // under a "context cleared" rule (like a rewind), and the gauges and
  // rewind targets start over.
  function renderReset(p) {
    const logEl = mainLane.log;
    const clearCmd = cmdFifo.find(x => x.kind === 'clear' && !x.replied) || null;
    const marker = clearCmd && clearCmd.node.isConnected ? clearCmd.node : null;
    const wrap = el('div', 'frame frame--rewind frame--reset');
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', 'context cleared'));
    wrap.appendChild(line);
    const kids = [...logEl.children].filter(k => k !== mainLane.activity && k !== marker && !k.hasAttribute('data-payload'));
    if (kids.length) {
      const box = el('details', 'rewind-branch');
      box.appendChild(el('summary', 'rewind-branch-sum', 'show what was cleared · ' + kids.length + (kids.length === 1 ? ' frame' : ' frames')));
      const body = el('div', 'rewind-branch-body');
      for (const k of kids) body.appendChild(k);
      box.appendChild(body);
      wrap.appendChild(box);
    }
    if (marker) { mergeRaw(marker, wrap); marker.remove(); }
    attachRaw(wrap, p, 'raw (reset)');
    echoed.length = 0;
    pendingInputs.length = 0;
    turnHasEcho = false;
    contextUsed = 0; mainLane.ctxUsed = 0;
    setGauge('context', 0, '\u2013', 'Context', 'Context window in use');
    for (const k in reports) delete reports[k];
    paintGaugePop();
    lastSessionDivider = wrap;
    if (clearCmd) { clearCmd.node = wrap; clearCmd.replied = true; }
    else { localCmd = { node: wrap, kind: 'clear', name: 'clear', args: '', out: null, status: null, line: null, replied: true }; cmdFifo.push(localCmd); }
    return wrap;
  }

  // imageSig identifies a message's pictures so a retry can be matched to the
  // attempt it repeats (sizes and types, not the whole base64).
  function imageSig(images) {
    return (images || []).map(i => (i.media_type || '') + ':' + (i.data || '').length).join(',');
  }

  // youBubble draws a message of yours: images above the text. pending=true
  // is the grey form (submitted, not yet in the conversation); the blue form
  // is drawn from Claude Code's echo, which is the proof it got there.
  function youBubble(text, images, payload, pending) {
    const body = el('div', 'frame-body');
    if (images && images.length) {
      const row = el('div', 'msg-images');
      for (const im of images) {
        if (!im || !im.data) continue;
        const img = document.createElement('img');
        img.src = 'data:' + (im.media_type || 'image/png') + ';base64,' + im.data;
        img.alt = 'attached image';
        img.addEventListener('click', () => openLightbox(img.src));
        row.appendChild(img);
      }
      body.appendChild(row);
    }
    if (text) body.appendChild(el('div', 'msg-text', text));
    if (pending) body.appendChild(el('div', 'you-status', 'waiting to enter the conversation'));
    const node = bubble('you', 'you' + (pending ? ' is-pending' : ''), body, payload);
    return node;
  }
  function inputKey(text, images) { return (text || '') + '\u0000' + imageSig(images); }

  // Submitted messages still waiting for their echo, oldest first.
  const pendingInputs = []; // { key, node, record }

  function renderInput(p) {
    while (retryButtons.length) retryButtons.pop().remove();
    const text = p.text || '';
    const eff = /^\/effort\s+(low|medium|high|xhigh|max)\s*$/.exec(text);
    if (eff) { curEffort = eff[1]; paintModelSelect(); const n = localMarker('effort', 'effort set to ' + eff[1], p); lastInput = { node: n, text, images: [], failed: false, payload: p, failPayload: null }; return n; }
    if (/^\/fast\s*$/.test(text)) { const n = localMarker('fast', 'fast mode toggled', p); lastInput = { node: n, text, images: [], failed: false, payload: p, failPayload: null }; return n; }
    const images = Array.isArray(p.images) ? p.images : [];
    const sc = /^\/([\w:-]+)(?:\s+([\s\S]*))?$/.exec(text);
    if (sc && !images.length) { const n = cmdMarker(sc[1], (sc[2] || '').trim(), p); if (n) lastInput = { node: n, text, images: [], failed: false, payload: p, failPayload: null }; return n; }
    const node = youBubble(text, images, p, true);
    const sig = imageSig(images);
    for (let i = failedInputs.length - 1; i >= 0; i--) {
      const f = failedInputs[i];
      if (f.text !== text || imageSig(f.images) !== sig) continue;
      failedInputs.splice(i, 1);
      f.node.remove();
      attachRaw(node, f.payload, 'raw (failed attempt)');
      if (f.failPayload) attachRaw(node, f.failPayload, 'raw (failure)');
    }
    lastInput = { node, text, images, failed: false, payload: p, failPayload: null };
    pendingInputs.push({ key: inputKey(text, images), node, record: lastInput });
    return node;
  }

  // echoContent splits an echoed user message into text + images.
  function echoContent(content) {
    if (typeof content === 'string') return { text: content, images: [] };
    const images = [], texts = [];
    for (const b of content || []) {
      if (b.type === 'text') texts.push(b.text || '');
      else if (b.type === 'image' && b.source && b.source.data) images.push({ media_type: b.source.media_type || 'image/png', data: b.source.data });
    }
    return { text: texts.join('\n'), images };
  }

  // Blue bubbles in order, so a rewind can walk back from the newest message
  // to the target one (Claude Code only rewinds the tip, repeatedly).
  const echoed = []; // { uuid, node, text }
  let turnHasEcho = false; // a message has already entered the current turn

  // renderEcho: Claude Code's replay of a message of ours means it is now in
  // the conversation. Draw the blue bubble here (where it actually landed)
  // and retire the grey one, carrying its raw payloads over.
  // --- cross-session messages ---
  //
  // Claude Code's own peer messaging (SendMessage / ListAgents, sessions on
  // the same host found through per-user sockets). A message from a peer
  // arrives as a user turn: boilerplate around a <cross-session-message
  // from="uds:…" from-name="…" from-mode="…"> element, bracketed by
  // command_lifecycle {state: started|completed} frames. It shows as a peer
  // bubble named after the sender — linked to the Acta session of the same
  // name when there is one (Acta names Claude's sessions after its titles) —
  // with the boilerplate and lifecycle frames folded in verbatim.
  const PEER_RE = /^Another Claude session sent a message:\s*<cross-session-message\s+([^>]*)>([\s\S]*?)<\/cross-session-message>/;
  const peerNames = new Map();  // socket address -> the name that peer gave itself
  let lastPeer = null;          // the latest peer bubble, for the lifecycle frame that closes it
  let pendingLifecycle = null;  // a lifecycle "started" frame waiting for its message
  function peerLink(name) {
    if (!name) return null;
    for (const n of document.querySelectorAll('[data-session-name]')) {
      if (n.dataset.sessionName !== sessionID && n.textContent.trim() === name) return '/account/sessions/' + encodeURIComponent(n.dataset.sessionName);
    }
    return null;
  }
  function peerBubble(attrs, text, p) {
    const at = {};
    for (const m of attrs.matchAll(/([\w-]+)="([^"]*)"/g)) at[m[1]] = m[2];
    const name = at['from-name'] || (at.from ? at.from.replace(/^uds:.*\//, '').replace(/\.sock$/, '') : 'another session');
    if (at.from && at['from-name']) peerNames.set(at.from, at['from-name']);
    const body = el('div', 'frame-body');
    const head = el('div', 'peer-head');
    head.appendChild(svg(ICONS.agent));
    head.appendChild(el('span', null, 'from '));
    const href = peerLink(name);
    if (href) { const a = el('a', 'peer-from', name); a.href = href; a.title = 'Open that session'; head.appendChild(a); }
    else {
      const span = el('span', 'peer-from', name);
      head.appendChild(span);
      // not in the sidebar (a session made since this page loaded): ask
      fetch('/account/sessions/lookup?title=' + encodeURIComponent(name) + '&exclude=' + encodeURIComponent(sessionID), { headers: { 'X-Requested-With': 'fetch' } })
        .then(r => r.ok ? r.json() : null)
        .then(j => { if (j && j.id && span.isConnected) { const a = el('a', 'peer-from', name); a.href = '/account/sessions/' + encodeURIComponent(j.id); a.title = 'Open that session'; span.replaceWith(a); } })
        .catch(() => {});
    }
    if (at['from-mode']) head.appendChild(el('span', 'peer-mode', at['from-mode']));
    head.title = at.from || '';
    body.appendChild(head);
    body.appendChild(mdRender(text.trim()));
    const node = bubble('peer message', 'peer', body, p);
    if (pendingLifecycle) { attachRaw(node, pendingLifecycle, 'raw (lifecycle started)'); pendingLifecycle = null; }
    lastPeer = node;
    return node;
  }
  // peerName turns a SendMessage address into what the transcript calls that
  // peer: a socket address becomes the name that peer wrote to us under.
  function peerName(to) {
    if (!to) return '?';
    if (peerNames.has(to)) return peerNames.get(to);
    if (/^uds:/.test(to)) return to.replace(/^uds:.*\//, '').replace(/\.sock$/, '');
    return to;
  }
  const peerOut = new Map(); // tool_use_id -> { card, status }
  // peerOutCard renders a SendMessage call as the mirror of an incoming peer
  // bubble: "to <name>", the whole message, and the delivery outcome below
  // once the tool result lands (which then takes no frame of its own).
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
    if (b.id) { toolNames.set(b.id, 'SendMessage'); toolRows.set(b.id, card); peerOut.set(b.id, { card, status }); }
    return card;
  }
  function peerOutResult(entry, block, p) {
    const c = block.content;
    const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : '';
    let sent = null;
    try { sent = JSON.parse(txt); } catch (_) { sent = null; }
    const ok = sent && typeof sent === 'object' ? !!sent.success : !block.is_error;
    let msg = sent && typeof sent === 'object' ? (ok ? (sent.message || 'delivered') : (sent.error || sent.message || 'not delivered')) : (txt || (ok ? 'delivered' : 'not delivered'));
    msg = msg.replace(/uds:\S+/g, m => peerName(m));
    if (ok && /^[“"]/.test(msg)) msg = 'delivered' + (/ \u2192 /.test(msg) ? msg.slice(msg.lastIndexOf(' \u2192 ')) : '');
    entry.status.textContent = '';
    entry.status.appendChild(svg(ok ? ['M3.5 8.5 6.5 11.5 12.5 5'] : ICONS.warn));
    entry.status.appendChild(el('span', null, msg));
    entry.status.classList.toggle('is-error', !ok);
    const frame = entry.card.closest('.frame');
    if (frame) attachRaw(frame, p, 'raw (delivery)');
    return null;
  }
  // --- Acta MCP tool cards ---
  //
  // Calls to Acta's own MCP tools (mcp__acta__*) get a card instead of a
  // pill: a verb ("Commented", "Set status"), the item it touched as a
  // linked chip, the meaningful part of the input (a comment body, the new
  // status, an item's title), and — once the tool result lands — what came
  // back (the item's ref/title/status, a list of items, a memory…). The
  // result frame folds into the card; its verbatim payload stays reachable.
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
  const itemURLs = new Map();  // item id / ref -> { url, ref, title, status }
  const actaCards = new Map(); // tool_use_id -> { card, tool, input, res, status, chip }
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
    // a long body shows its first lines; a toggle reveals the rest
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
  function arrow(v) { const n = el('div', 'acta-arrow'); n.appendChild(el('span', null, '\u2192 ')); n.appendChild(el('strong', null, String(v))); return n; }
  function actaCard(b) {
    const tool = b.name.replace(/^mcp__acta__/, '');
    const inp = b.input || {};
    const card = el('div', 'acta-card');
    const head = el('div', 'acta-head');
    head.appendChild(svg(ICONS.acta));
    head.appendChild(el('span', 'acta-verb', ACTA_VERBS[tool] || tool.replace(/_/g, ' ')));
    let chip = null;
    const target = inp.id || inp.item || (tool === 'watch_comments' ? inp.item : '');
    if (target && /item|comment|document|archive|subscribe|watch/.test(tool) && !/^(create_document|list_documents)$/.test(tool)) { chip = itemChip(target); head.appendChild(chip); }
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
    if (b.id) { toolNames.set(b.id, b.name); toolRows.set(b.id, card); actaCards.set(b.id, { card, tool, input: inp, res, status, chip, head }); }
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
  function actaResult(entry, block, p) {
    const c = block.content;
    const txt = typeof c === 'string' ? c : Array.isArray(c) ? c.map(x => x.text || '').join('') : '';
    let data = null;
    try { data = JSON.parse(txt); } catch (_) { data = null; }
    if (data == null && p.tool_use_result && typeof p.tool_use_result === 'object' && !Array.isArray(p.tool_use_result)) data = p.tool_use_result;
    const { res, status, tool } = entry;
    status.textContent = '';
    // Claude Code wraps an oversized result as {content: "Error: … exceeds
    // maximum allowed tokens. Output has been saved to <path>"} without
    // flagging it as an error: treat that as one
    const wrapped = /^Error:/.test(txt.trim()) ? txt.trim() : (data && typeof data === 'object' && typeof data.content === 'string' && /^Error:/.test(data.content) ? data.content : '');
    if (block.is_error || wrapped) {
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
        // a comment: the body is what we sent; say who and when it landed
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
    const frame = entry.card.closest('.frame');
    if (frame) attachRaw(frame, p, 'raw (acta result)');
    return null;
  }
  function renderLifecycle(p) {
    if (p.state === 'started') { pendingLifecycle = p; return null; }
    if (lastPeer && lastPeer.isConnected) { attachRaw(lastPeer, p, 'raw (lifecycle ' + (p.state || '') + ')'); if (p.state === 'completed') lastPeer = null; return null; }
    return foldIntoLast(p, 'raw (lifecycle ' + (p.state || '') + ')');
  }

  function renderEcho(p) {
    const { text, images } = echoContent(p.message && p.message.content);
    const peer = PEER_RE.exec(text.trim());
    if (peer) { turnHasEcho = true; return peerBubble(peer[1], peer[2], p); }
    if (/^<command-(message|name)>/.test(text.trim())) {
      // a skill's expansion of our "/command": the marker already shows it
      const cn = /<command-name>\/([\w:-]+)<\/command-name>/.exec(text);
      const lc = (cn && cmdFifo.find(x => x.name === cn[1])) || cmdFifo[cmdFifo.length - 1] || null;
      if (lc && lc.node.isConnected) { attachRaw(lc.node, p, 'raw (echo)'); return null; }
      return foldIntoLast(p, 'raw (echo)');
    }
    if (/^<local-command-stdout>/.test(text.trim())) {
      // the CLI's own output for a local command: nothing to show
      const lc = cmdFifo.find(x => x.replied) || localCmd;
      if (lc && lc.node.isConnected) { attachRaw(lc.node, p, 'raw (echo)'); return null; }
      return foldIntoLast(p, 'raw (echo)');
    }
    const key = inputKey(text, images);
    const i = pendingInputs.findIndex(e => e.key === key);
    const node = youBubble(text, images, p, false);
    // Only a message that starts a turn is a rewind target: one steered into
    // a running turn is stored as an attachment, which Claude Code will not
    // rewind to ("target not found"). The first echo after a turn boundary is
    // the turn-starting message; later echoes in the same turn are steers.
    if (p.uuid && !turnHasEcho) {
      const entry = { uuid: p.uuid, node, text };
      echoed.push(entry);
      node.appendChild(rewindMenu(entry));
    } else if (p.uuid) {
      node.classList.add('is-steer');
      node.title = 'Steered into the turn already running';
    }
    turnHasEcho = true;
    if (i >= 0) {
      const pend = pendingInputs.splice(i, 1)[0];
      mergeRaw(pend.node, node);
      pend.node.remove();
      if (lastInput && lastInput.node === pend.node) lastInput.node = node;
      // a grey bubble that is the only thing left before this one: skip the gap
    } else if (lastInput && !lastInput.failed && lastInput.node.isConnected && lastInput.node.classList.contains('is-pending') && lastInput.text === text) {
      mergeRaw(lastInput.node, node);
      lastInput.node.remove();
      lastInput.node = node;
    }
    return node;
  }

  // --- rewind ---
  //
  // Claude Code rewinds the conversation one message at a time from the tip
  // (rewind_conversation {target_message_uuid} -> {rewound, prefillText}),
  // and restores the files a message changed (rewind_files {user_message_id,
  // dry_run} -> {canRewind, filesChanged, insertions, deletions}). Rewinding
  // to an older message means walking back through every message after it.
  // Acta's transcript keeps everything: a "rewind" marker frame records what
  // was discarded, and the client collapses that stretch into a branch.

  const pendingCtl = new Map(); // request_id -> resolve

  function control(request, timeout) {
    return new Promise((resolve) => {
      const id = 'rw-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 7);
      const done = (v) => { if (pendingCtl.delete(id)) resolve(v); };
      pendingCtl.set(id, done);
      setTimeout(() => done(null), timeout || 30000);
      sendControl({ type: 'control_request', request_id: id, request });
    });
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

  // rewindBusy shows what is happening on the target bubble while it runs.
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
      const r = await control({ subtype: 'side_question', question: 'Summarise, in a short paragraph, everything that has happened in this conversation since (and including) my message: "' + entry.text.slice(0, 400) + '". Cover what was tried, what was learned, and anything still outstanding.' }, 120000);
      summary = (r && (r.response || r.answer)) || '';
      if (!summary) { rewindBusy(node, 'Could not summarise; nothing was rewound.'); setTimeout(() => rewindBusy(node, ''), 4000); return; }
    }
    let files = null;
    if (mode === 'files' || mode === 'both') {
      rewindBusy(node, 'Checking which files would change…');
      const dry = await control({ subtype: 'rewind_files', user_message_id: entry.uuid, dry_run: true });
      const changed = (dry && dry.filesChanged) || [];
      if (!dry || dry.canRewind === false) {
        // no checkpoint: Claude Code snapshots files edited with its file
        // tools, so a file written by a shell command has nothing to restore
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
        const done = await control({ subtype: 'rewind_files', user_message_id: entry.uuid, dry_run: false });
        files = { changed: changed, insertions: dry.insertions || 0, deletions: dry.deletions || 0, ok: !!done };
      }
    }
    let walked = 0, text = entry.text;
    if (mode !== 'files') {
      // walk back from the newest message to this one
      for (let i = echoed.length - 1; i >= idx; i--) {
        rewindBusy(node, 'Rewinding the conversation… ' + (echoed.length - i) + ' of ' + (echoed.length - idx));
        const r = await control({ subtype: 'rewind_conversation', target_message_uuid: echoed[i].uuid });
        if (!r || r.rewound !== true) {
          rewindBusy(node, 'Rewind stopped' + (r && r.error ? ' · ' + r.error : '') + '.');
          setTimeout(() => rewindBusy(node, ''), 6000);
          break;
        }
        walked++;
        if (i === idx && typeof r.prefillText === 'string') text = r.prefillText;
      }
      // the marker frame prunes `echoed` when it draws the branch
    }
    rewindBusy(node, '');
    if (!walked && !files) return;
    // Record what happened; the marker is what draws the collapsed branch.
    ws.send(JSON.stringify({ t: 'mark', payload: { kind: 'rewind', mode, target_uuid: entry.uuid, messages: walked, files, summary: summary || undefined, at: new Date().toISOString() } }));
    if (prefill) {
      box.value = mode === 'summarise' ? summary : text;
      box.style.height = 'auto';
      box.style.height = Math.min(box.scrollHeight, 200) + 'px';
      box.focus();
    }
  }

  // failInput marks the last input as undelivered with a reason and a Retry.
  // Returns true if there was an input to attach to.
  function failInput(reason, statePayload) {
    if (!lastInput || lastInput.failed) return false;
    lastInput.failed = true;
    lastInput.failPayload = statePayload;
    failedInputs.push(lastInput);
    const { node, text, images } = lastInput;
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
    // Keep the failure frame's verbatim payload alongside the input's.
    attachRaw(node, statePayload, 'raw (failure)');
    return true;
  }

  function renderState(p) {
    let note = p.state || 'state';
    let bad = false;
    if (p.state === 'undelivered' || p.state === 'spawn_error') {
      const reason = p.state === 'undelivered' ? (p.reason || 'no harness connected') : ('spawn failed: ' + (p.error || 'unknown error'));
      if (failInput(reason, p)) return null;
      note = p.state === 'undelivered' ? 'not delivered: ' + reason : reason;
      bad = true;
    } else if (p.state === 'exit') {
      if (interruptDivider && !(p.code != null && p.code !== 0)) {
        // the Stop's SIGINT ended the process: expected, so it rides the divider
        attachRaw(interruptDivider, p, 'raw (exit)');
        interruptDivider = null;
        return null;
      }
      note = 'process exited (code ' + (p.code != null ? p.code : '?') + ')';
      bad = p.code != null && p.code !== 0;
    } else if (p.state === 'resume_failed') {
      note = p.reason || 'resume failed; starting fresh';
    } else if (p.state === 'spawned') {
      if (p.styles) noteStyles(p.styles);
      lastSessionDivider = divider(p.resumed ? 'session resumed' : 'session started', [], p, 'frame--session', 'raw (spawn)');
      return lastSessionDivider;
    }
    const node = bubble('status', 'state', el('div', 'frame-note', note), p);
    if (bad) node.classList.add('is-error');
    return node;
  }

  // --- streamed replies ---
  //
  // With --include-partial-messages Claude Code emits stream_event frames
  // (the API's message_start / content_block_start / content_block_delta /
  // content_block_stop / message_delta / message_stop) as a reply is written.
  // Acta relays them live without storing them. Each lane keeps one growing
  // "live" assistant frame built from the deltas; the complete assistant
  // frame that follows replaces it, so nothing streamed is ever the record.

  function liveFrame(lane, model) {
    if (lane.live) return lane.live;
    const wrap = el('div', 'frame frame--assistant is-streaming');
    const head = el('div', 'frame-head');
    head.appendChild(el('span', 'frame-kind', modelName(model || lane.model || curModel)));
    wrap.appendChild(head);
    const body = el('div', 'frame-body');
    wrap.appendChild(body);
    const stick = atBottom(lane.log);
    lane.log.insertBefore(wrap, lane.activity.isConnected && lane.activity.parentNode === lane.log ? lane.activity : null);
    if (stick) scroll(lane.log);
    lane.live = wrap;
    lane.liveBlocks = new Map(); // index -> {type, text, node, raf}
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
      const stick = atBottom(lane.log);
      if (b.type === 'text') { const fresh = mdRender(b.text); b.node.replaceWith(fresh); b.node = fresh; }
      if (stick) scroll(lane.log);
    });
  }
  function renderStreamEvent(p) {
    const ev = p.event || {};
    const lane = cur;
    if (ev.type === 'message_start') {
      dropLive(lane);
      const m = ev.message || {};
      if (m.model && !lane.model && lane !== mainLane) lane.model = m.model;
      liveFrame(lane, m.model);
      return null;
    }
    if (ev.type === 'message_stop') {
      // a live frame that never got visible content (thinking-only tail) is noise
      if (lane.live && !lane.live.querySelector('.frame-body').children.length) dropLive(lane);
      return null;
    }
    if (ev.type === 'content_block_start') {
      const cb = ev.content_block || {};
      if (cb.type === 'thinking') { setActivity('Thinking'); if (!lane.think) lane.think = { start: Date.now(), tokens: 0, last: null, frames: 0 }; liveThought(lane); if (lane.liveBlocks) lane.liveBlocks.set(ev.index, { type: 'thinking' }); return null; }
      if (!lane.live) liveFrame(lane);
      const b = { type: cb.type, text: cb.text || '', node: null, raf: 0, name: cb.name };
      const body = lane.live.querySelector('.frame-body');
      if (cb.type === 'text') { b.node = mdRender(b.text); body.appendChild(b.node); }
      else if (cb.type === 'tool_use') { b.node = el('div', 'msg-tool is-streaming'); b.node.appendChild(toolIcon(cb.name)); b.node.appendChild(el('span', 'tool-name', cb.name || 'tool')); b.node.appendChild(el('span', 'tool-arg', '…')); body.appendChild(b.node); setActivity('Preparing ' + (cb.name || 'tool')); }
      lane.liveBlocks.set(ev.index, b);
      return null;
    }
    if (ev.type === 'content_block_delta') {
      if (!lane.liveBlocks) return null;
      const b = lane.liveBlocks.get(ev.index);
      const d = ev.delta || {};
      if (!b || b.type === 'thinking') return null;
      if (d.type === 'text_delta') { b.text += d.text || ''; paintLiveBlock(lane, b); if (lane === mainLane || true) setActivity('Writing'); }
      else if (d.type === 'input_json_delta') { b.json = (b.json || '') + (d.partial_json || ''); const arg = /"(?:command|file_path|path|pattern|url|description|prompt|message)"\s*:\s*"((?:[^"\\]|\\.)*)/.exec(b.json); if (arg && b.node) b.node.querySelector('.tool-arg').textContent = arg[1].replace(/\\n/g, ' ').slice(0, 120); }
      return null;
    }
    return null; // content_block_stop / message_delta / message_stop: the assistant frame follows
  }

  // renderRewind draws the marker and folds the stretch it discarded into a
  // collapsed branch: everything from the target message to here, moved into
  // a <details> so the transcript reads as one conversation until opened.
  const REWIND_LABEL = { conversation: 'conversation rewound', files: 'files restored', both: 'conversation rewound and files restored', summarise: 'summarised and rewound' };

  function renderRewind(p) {
    const wrap = el('div', 'frame frame--rewind');
    const line = el('div', 'result-line');
    line.appendChild(el('span', 'result-label', REWIND_LABEL[p.mode] || 'rewound'));
    const bits = [];
    if (p.messages) bits.push(p.messages + (p.messages === 1 ? ' message' : ' messages'));
    if (p.files && p.files.changed && p.files.changed.length) bits.push(p.files.changed.length + (p.files.changed.length === 1 ? ' file' : ' files') + ' · +' + (p.files.insertions || 0) + '/-' + (p.files.deletions || 0));
    if (bits.length) line.appendChild(el('span', 'result-stats', bits.join(' · ')));
    wrap.appendChild(line);

    // move the discarded stretch inside
    const target = echoedNode(p.target_uuid);
    const log = cur.log;
    const kids = [...log.children];
    const from = target ? kids.indexOf(target.closest('.frame') || target) : -1;
    if (from >= 0) {
      const box = el('details', 'rewind-branch');
      const sum = el('summary', 'rewind-branch-sum', '');
      box.appendChild(sum);
      const body = el('div', 'rewind-branch-body');
      let moved = 0;
      for (const k of kids.slice(from)) {
        if (k === activityOf(cur)) continue;
        body.appendChild(k);
        moved++;
      }
      box.appendChild(body);
      sum.textContent = 'show what was discarded · ' + moved + (moved === 1 ? ' frame' : ' frames');
      wrap.appendChild(box);
      // the messages are gone from the live conversation
      const i = echoed.findIndex(e => e.uuid === p.target_uuid);
      if (i >= 0) echoed.splice(i);
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
    attachRaw(wrap, p, 'raw (rewind)');
    return wrap;
  }
  function echoedNode(uuid) { const e = echoed.find(x => x.uuid === uuid); return e ? e.node : null; }
  function activityOf(lane) { return lane.activity; }

  function renderFrame(kind, payload, at) {
    curAt = at || '';
    noteActivity(kind, payload, at);
    if (kind === 'rate_limit_event') noteRateLimit(payload);
    else if (kind === 'assistant') noteAssistant(payload);
    else if (kind === 'result') noteResult(payload);
    else if (kind === 'system' && payload && payload.subtype === 'compact_boundary') noteCompact(payload);
    if (kind === 'result' || (kind === 'state' && payload && (payload.state === 'exit' || payload.state === 'spawned'))) stalePendingPerms();
    if (kind === 'state' && payload && (payload.state === 'exit' || payload.state === 'spawn_error' || payload.state === 'undelivered')) { procAlive = false; cmdQueue.length = 0; }
    if ((kind === 'state' && payload && payload.state === 'spawned') || (kind === 'system' && payload && payload.subtype === 'init')) procAlive = true;
    if (kind === 'system' && payload && payload.subtype === 'init') {
      if (payload.permissionMode) setMode(payload.permissionMode);
      if (payload.model) curModel = payload.model;
      noteStyle(payload.output_style);
      noteFastState(payload);
      if (hydrated && !modelsAsked) requestModels();
    }
    if (interruptDivider && !(kind === 'state' || kind === 'rate_limit_event' || kind === 'system')) interruptDivider = null;
    if (lastModeMark && !(kind === 'control_response' || kind === 'system' || kind === 'rate_limit_event')) lastModeMark = null;
    if (pendingLifecycle && !(kind === 'user' || kind === 'command_lifecycle' || kind === 'system' || kind === 'rate_limit_event')) { foldIntoLast(pendingLifecycle, 'raw (lifecycle started)'); pendingLifecycle = null; }
    if (apiRetry && !(kind === 'system' || kind === 'rate_limit_event')) apiRetrySettle(kind, payload);
    if (compact) {
      const u = (kind === 'result' && payload && payload.usage) || null;
      const emptyResult = kind === 'result' && compact.done && (!u || !((u.input_tokens || 0) + (u.output_tokens || 0) + (u.cache_read_input_tokens || 0) + (u.cache_creation_input_tokens || 0)));
      if (emptyResult) { compactFold(payload, 'raw (result)'); compactClose(); return null; }
      const meta = kind === 'assistant' && payload && (payload.is_meta || (payload.message && payload.message.model === '<synthetic>'));
      if (!(kind === 'system' || kind === 'user' || kind === 'rate_limit_event' || kind === 'control_response' || meta)) compactClose();
    }
    if ((kind === 'assistant' || kind === 'result' || kind === 'state') && cur.live) dropLive(cur);
    if (kind === 'assistant') dropLiveThought(cur); // the real frame (thought chip or reply) takes over
    if (kind === 'stream_event') return renderStreamEvent(payload);
    switch (kind) {
      case 'input': return renderInput(payload);
      case 'control_request': return renderPermRequest(payload);
      case 'control': return renderControl(payload);
      case 'control_response': return renderControlResponse(payload);
      case 'assistant': return renderAssistant(payload, at);
      case 'user': return renderUser(payload);
      case 'result': return renderResult(payload);
      case 'system': return renderSystem(payload);
      case 'rate_limit_event': return renderRateLimit(payload);
      case 'state': return renderState(payload);
      case 'rewind': return renderRewind(payload);
      case 'task_output': return renderTaskOutput(payload);
      case 'conversation_reset': return renderReset(payload);
      case 'command_lifecycle': return renderLifecycle(payload);
      default:
        // Unknown kind — verbatim only, so nothing is lost.
        return bubble(kind || 'frame', 'unknown', null, payload);
    }
  }

  // laneFor picks the lane a frame belongs to: subagent frames carry their
  // parent's tool_use_id; task_* system frames link through task ids.
  function laneFor(kind, p) {
    if (!p) return mainLane;
    if ((kind === 'assistant' || kind === 'user' || kind === 'stream_event') && p.parent_tool_use_id) return laneByAgent(p.parent_tool_use_id, p);
    return mainLane;
  }

  function addFrame(seq, kind, payload, at) {
    if (seq && seen.has(seq)) return;
    if (seq) { seen.add(seq); if (seq > lastSeq) lastSeq = seq; }
    tabAlert(kind, payload);
    cur = laneFor(kind, payload);
    const node = renderFrame(kind, payload, at);
    if (cur !== mainLane && node) { cur.steps++; laneHeaderRefresh(cur); }
    // a prompt that attached to a pill (permission, question, plan,
    // elicitation) adds no frame of its own but still needs the modal
    if (!node) { placeActivity(); cur = mainLane; showNextPerm(); return; }
    const stick = atBottom(cur.log);
    // a settled frame lands above whatever is still streaming (the next
    // block's live frame or thinking chip), so a tool result follows its call
    const live = cur.log.querySelector(':scope > .is-streaming, :scope > .is-live');
    if (live) cur.log.insertBefore(node, live); else cur.log.appendChild(node);
    placeActivity();
    if (stick) scroll(cur.log);
    cur = mainLane;
    showNextPerm();
  }

  // Render the server-rendered frames sitting in the DOM as data attributes.
  function hydrate() {
    const nodes = Array.from(log.querySelectorAll('.frame[data-payload]'));
    for (const n of nodes) {
      const seq = parseInt(n.dataset.seq || '0', 10);
      let payload = {};
      try { payload = JSON.parse(n.dataset.payload); } catch (_) {}
      cur = laneFor(n.dataset.kind, payload);
      const rendered = renderFrame(n.dataset.kind, payload, n.dataset.at);
      if (seq) { seen.add(seq); if (seq > lastSeq) lastSeq = seq; }
      if (cur === mainLane) { if (rendered) n.replaceWith(rendered); else n.remove(); }
      else { n.remove(); if (rendered) { cur.log.appendChild(rendered); cur.steps++; laneHeaderRefresh(cur); } }
      cur = mainLane;
    }
    for (const l of lanes.values()) { placeActivity(l); scroll(l.log); }
    hydrated = true;
    paintModelSelect();
    showNextPerm();
    const m = /agent=([^&]+)/.exec(location.hash || '');
    if (m && lanes.has(decodeURIComponent(m[1]))) showLane(decodeURIComponent(m[1]));
  }

  // --- rename ---
  //
  // The header title is editable in place; saving posts the new title and the
  // server tells the backend too, so Claude Code's own session name matches.
  // The live channel repaints every other view (sidebar, list, other tabs).

  const nameBtn = stage.querySelector('[data-chat-rename]');
  const nameInput = stage.querySelector('[data-rename-input]');
  let openRename = null;
  // renameTo posts the new title; the server tells the backend too. "/rename
  // <name>" in the composer lands here rather than going to Claude Code, so
  // Acta's title and Claude's session name change together.
  async function renameTo(title) {
    if (!nameBtn || title === nameBtn.textContent.trim()) return;
    const body = new URLSearchParams({ title, csrf_token: stage.dataset.csrf || '' });
    try {
      const res = await fetch('/account/sessions/' + encodeURIComponent(sessionID) + '/title', {
        method: 'POST', body, headers: { 'X-Requested-With': 'fetch', 'X-CSRF-Token': stage.dataset.csrf || '' },
      });
      if (res.ok && title) { nameBtn.textContent = title; document.title = title + ' \u00b7 Acta'; }
    } catch (_) {}
  }
  if (nameBtn && nameInput) {
    const open = () => {
      nameInput.value = nameBtn.textContent.trim();
      nameBtn.hidden = true;
      nameInput.hidden = false;
      nameInput.focus();
      nameInput.select();
    };
    const close = () => { nameInput.hidden = true; nameBtn.hidden = false; };
    const save = () => { const title = nameInput.value.trim(); close(); renameTo(title); };
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
    const url = proto + '//' + location.host + '/account/sessions/' +
      encodeURIComponent(sessionID) + '/ws?after=' + lastSeq;
    setConn('connecting');
    ws = new WebSocket(url);
    ws.onopen = () => { setConn('connected'); reportFocus(); if (stage.dataset.running === '1' && !modelsAsked) requestModels(); };
    ws.onmessage = (e) => {
      let m;
      try { m = JSON.parse(e.data); } catch (_) { return; }
      addFrame(m.seq, m.kind, m.payload, m.at);
    };
    ws.onclose = () => { setConn('offline'); ws = null; setTimeout(connect, 1500); };
    ws.onerror = () => { try { ws.close(); } catch (_) {} };
  }

  // --- attention ---
  //
  // The tab tells the server whether it is visible and focused; while it is,
  // the server raises no alerts about this session (the owner is looking),
  // and coming back marks the session's notifications read. When the tab is
  // open but not looked at, and this device has no Web Push subscription
  // (which would already cover it), the page shows the alert itself.

  function tabFocused() { return document.visibilityState === 'visible' && document.hasFocus(); }
  function reportFocus() {
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'focus', on: tabFocused() }));
  }
  for (const evName of ['visibilitychange', 'focus', 'blur']) (evName === 'visibilitychange' ? document : window).addEventListener(evName, reportFocus);

  let pushHere = null; // does this device receive Web Push already?
  if ('serviceWorker' in navigator && 'PushManager' in window) {
    navigator.serviceWorker.getRegistration().then(reg => reg ? reg.pushManager.getSubscription() : null).then(sub => { pushHere = !!sub; }).catch(() => { pushHere = false; });
  } else pushHere = false;

  // alertText mirrors the server's classification for the tab-side notice.
  function alertText(kind, p) {
    if (!p) return '';
    if (kind === 'control_request' && p.request) {
      const r = p.request, inp = r.input || {};
      if (r.subtype === 'elicitation') return 'needs input for ' + (r.mcp_server_name || 'an MCP server');
      if (r.subtype !== 'can_use_tool') return '';
      if (r.tool_name === 'AskUserQuestion') return 'has a question' + (inp.questions && inp.questions[0] ? ': ' + inp.questions[0].question : '');
      if (r.tool_name === 'ExitPlanMode') return 'wants approval for a plan';
      const d = inp.command || inp.file_path || inp.description || r.description || '';
      return 'needs permission for ' + (r.display_name || r.tool_name) + (d ? ': ' + String(d).split('\n')[0].slice(0, 80) : '');
    }
    if (kind === 'result') return p.is_error || /^error/.test(p.subtype || '') ? 'stopped on an error' : 'finished a turn' + (p.result ? ': ' + String(p.result).split('\n')[0].slice(0, 100) : '');
    if (kind === 'state') { if (p.state === 'exit' && p.code) return 'exited with code ' + p.code; if (p.state === 'spawn_error') return "couldn't start"; if (p.state === 'resume_failed') return "couldn't resume the conversation"; }
    return '';
  }
  function tabAlert(kind, p) {
    if (!hydrated || tabFocused() || pushHere !== false) return;
    if (!('Notification' in window) || Notification.permission !== 'granted') return;
    const text = alertText(kind, p);
    if (!text) return;
    const title = (stage.querySelector('[data-chat-rename]') || {}).textContent || 'Claude session';
    const opts = { body: 'Claude ' + text, tag: 'session-' + sessionID, icon: '/static/icon-192.png' };
    try {
      const reg = navigator.serviceWorker && navigator.serviceWorker.controller ? navigator.serviceWorker.ready : null;
      if (reg) reg.then(r => r.showNotification(title.trim(), opts)).catch(() => {});
      else { const n = new Notification(title.trim(), opts); n.onclick = () => { window.focus(); n.close(); }; }
    } catch (_) {}
  }
  // The first message sent from a browser that has never been asked is the
  // moment to ask for notification permission: a user gesture, and the user
  // has just started something they may walk away from.
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
  //
  // Pictures come in by paste, drag-and-drop or the attach button, show as
  // thumbnails above the textarea, and go out with the message as base64
  // image blocks. Anything over 1568px on its long edge is scaled down first
  // (the model sees no more than that anyway) — PNG stays PNG so screenshots
  // keep crisp text, everything else re-encodes as JPEG.

  const attachRow = stage.querySelector('[data-chat-attach]');
  const attachFile = stage.querySelector('[data-attach-file]');
  const attachBtn = stage.querySelector('[data-attach-btn]');
  const lightbox = stage.querySelector('[data-lightbox]');
  const pendingImages = []; // { chip, media_type, data }

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
    if (pendingImages.some(i => !i.data)) return; // still encoding
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
  //
  // Typing "/" at the start of the box lists Claude Code's commands (from
  // the initialize control response: name, description, argument hint,
  // aliases; MCP prompts as "server:prompt"). Acta keeps a few for itself
  // (/rename) and hides the ones it renders elsewhere (/context and /usage
  // live under the gauges, /model and /effort in the model picker, and so
  // on). Pick a row to fill the box; a command that takes no argument sends
  // straight away. After "/config " the keys and their values complete too.

  const slashMenu = stage.querySelector('[data-slash-menu]');
  const hintEl = stage.querySelector('.chat-hint');
  const HINT_DEFAULT = hintEl ? hintEl.textContent : '';
  const HIDDEN_CMDS = new Set(['context', 'usage', 'cost', 'stats', 'model', 'effort', 'fast', 'autocompact', 'color', 'agents', 'extra-usage', '__remote-workflow', 'workflow-launch-exec', 'heapdump']);
  const ACTA_CMDS = [
    { cmd: 'clear', description: 'Start over with an empty context; what came before folds away', argumentHint: '' },
    { cmd: 'compact', description: 'Free up context by summarising the conversation so far', argumentHint: '[instructions for the summary]' },
    { cmd: 'recap', description: 'A one-line recap of the session so far', argumentHint: '' },
    { cmd: 'rename', description: 'Rename this session, here and in Claude Code', argumentHint: '<name>' },
    { cmd: 'goal', description: 'Set a goal: Claude keeps working until the condition is met', argumentHint: '<condition> | clear' },
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
  let slashPassive = false; // rows shown, but Enter sends the box as it is

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
  // slashOptions: the rows for the box's current text, and the hint line.
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
    // with no partial argument typed, Enter sends what stands: the rows
    // are only there for Tab or a click
    return { rows, hint, query: 'a:' + c.cmd + ':' + tok, passive: tok === '' };
  }
  function hintRefresh(hint) {
    if (!hintEl) return;
    hintEl.textContent = '';
    hintEl.classList.toggle('is-cmd', !!hint);
    if (!hint) { hintEl.textContent = HINT_DEFAULT; return; }
    hintEl.appendChild(el('code', null, hint.cmd));
    if (hint.desc) hintEl.appendChild(el('span', null, ' \u2014 ' + hint.desc));
    hintEl.title = hint.cmd + (hint.desc ? ' \u2014 ' + hint.desc : '');
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
      b.addEventListener('mousedown', (e) => e.preventDefault()); // keep the box focused
      b.addEventListener('click', () => slashPick(r, false));
      slashMenu.appendChild(b);
    });
    slashMenu.hidden = false;
    const act = slashMenu.children[slashIdx];
    if (act && act.scrollIntoView) act.scrollIntoView({ block: 'nearest' });
  }
  // slashPick fills the box with the row; `complete` (Tab) never sends.
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
      // the row is what was typed already: send it as it stands
      if (slashPassive || (r.kind === 'cmd' && box.value.trim().toLowerCase() === ('/' + r.c.cmd).toLowerCase())) { slashClose(); return false; }
      e.preventDefault(); slashPick(r, false); return true;
    }
    if (e.key === 'Escape') { e.preventDefault(); slashDismissed = box.value; slashClose(); return true; }
    return false;
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
    st.textContent = goal.state === 'met' ? 'met · the turn ended once this held' : goal.state === 'unmet' ? 'not met · Claude Code stopped holding the turn open' : 'active' + (goal.turns ? ' · ' + goal.turns + (goal.turns === 1 ? ' turn' : ' turns') : '') + ' · Claude keeps working until this holds';
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
  //
  // Click a gauge: a panel with Claude Code's own /context report (the
  // per-category table), the /usage report, and the auto-compact settings
  // (/autocompact <window>, /config autoCompact=…). The commands go through
  // the session like any other, one at a time while it is idle, and their
  // replies land here as well as in the transcript.
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
    // the input frame comes back and marks the turn active; until then
    // hold the queue so two commands never race into the same turn
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
    note.textContent = mainTurnActive ? 'turn in progress · refreshes when it ends' : cmdQueue.length || queuedNow ? 'refreshing…' : !procAlive ? 'session not running · refresh starts it' : at ? 'as of ' + fmtAgo(at) : '';
    const rf = gaugePop.querySelector('[data-gpop-refresh]');
    rf.classList.toggle('is-busy', !!(cmdQueue.length || queuedNow));
  }
  function gaugeRefresh(force) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
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

  // No hover on touch screens: a tap on a frame reveals its time + raw controls.
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

  hydrate();
  connect();
})();
