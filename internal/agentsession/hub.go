package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peios/acta/internal/id"
	"github.com/peios/acta/internal/store"
)

// Hub is the in-process live relay. It holds the currently-connected harnesses
// (keyed by owner, never by a stable harness identity — Acta keys nothing on
// which harness is which) and the browser chat connections (keyed by session),
// and routes frames between them, persisting every one through the Service.
//
// Like internal/live it is deliberately the simplest thing that works for a
// single instance: sends are best-effort onto a buffered channel, and a
// connection whose buffer fills is closed rather than allowed to stall a
// sender — a dropped browser reconnects and replays the transcript from the
// store, so nothing is lost.
type Hub struct {
	svc *Service

	// presence, when set, is told each time a session's held/running state
	// may have changed, so the web layer can push it to the owner's browsers.
	presence func(ownerID, sessionID string, held, running bool)

	// renamed, when set, is told when a session's title changes, so the web
	// layer can push it to the owner's browsers.
	renamed func(ownerID, sessionID, title string)

	// alert, when set, is told when a session needs its owner (or stopped)
	// and no browser is looking at it, so the web layer can notify them.
	alert func(ownerID string, as store.AgentSession, verb, summary string)

	mu        sync.Mutex
	harness   map[string]map[string]*harnessConn // ownerID -> connID -> conn
	browsers  map[string]map[string]*browserConn // sessionID -> connID -> conn
	titling   map[string]bool                    // sessions with a title request in flight
	pendingLs map[string]chan Inbound            // ListDirs requests awaiting a harness answer
}

// ErrNoHarness is returned when no connected harness can take a request.
var ErrNoHarness = errors.New("no such harness connected")

// sendQueue is the per-connection outbound buffer. A chat is low-rate, so this
// only ever absorbs short bursts; a full buffer means the socket is wedged.
const sendQueue = 256

func NewHub(svc *Service) *Hub {
	return &Hub{
		svc:      svc,
		harness:  map[string]map[string]*harnessConn{},
		browsers: map[string]map[string]*browserConn{},
	}
}

// SetPresenceNotifier registers the callback that receives presence changes.
func (h *Hub) SetPresenceNotifier(fn func(ownerID, sessionID string, held, running bool)) {
	h.presence = fn
}

// SetRenameNotifier registers the callback used to push title changes.
// SetAlertNotifier installs the out-of-band channel for moments a session
// needs its owner: a permission, a question, a plan, an MCP elicitation, or
// the session stopping. It is not called while a browser has the session
// focused — the owner is already looking.
func (h *Hub) SetAlertNotifier(fn func(ownerID string, as store.AgentSession, verb, summary string)) {
	h.alert = fn
}

// SetFocus records whether a browser connection has its tab visible and
// focused, which is what decides if an alert is worth raising.
func (h *Hub) SetFocus(c *browserConn, on bool) {
	c.focused.Store(on)
}

// focused reports whether any browser watching the session is focused on it.
func (h *Hub) focused(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.browsers[sessionID] {
		if c.focused.Load() {
			return true
		}
	}
	return false
}

// maybeAlert raises an alert for a frame that warrants one, unless the owner
// is watching. Session lookup and delivery run off the relay path.
func (h *Hub) maybeAlert(ownerID, sessionID, kind string, payload json.RawMessage) {
	if h.alert == nil {
		return
	}
	verb, summary, ok := alertFor(kind, payload)
	if !ok || h.focused(sessionID) {
		return
	}
	go func() {
		as, err := h.svc.Get(context.Background(), sessionID, ownerID)
		if err != nil {
			return
		}
		h.alert(ownerID, as, verb, summary)
	}()
}

func (h *Hub) SetRenameNotifier(fn func(ownerID, sessionID, title string)) {
	h.renamed = fn
}

// titleRequestPrefix marks the control requests Acta itself issues to name a
// session, so their answers can be recognised on the way back.
const titleRequestPrefix = "acta-title-"

// Rename sets a session's title and tells the backend holding it, so Claude
// Code's own session name (its resume picker, window title) matches Acta's.
func (h *Hub) Rename(ctx context.Context, ownerID, sessionID, title string) (store.AgentSession, error) {
	as, err := h.svc.SetTitle(ctx, sessionID, ownerID, title)
	if err != nil {
		return as, err
	}
	h.pushRename(ownerID, sessionID, title)
	h.sendRename(ownerID, sessionID, title)
	return as, nil
}

func (h *Hub) pushRename(ownerID, sessionID, title string) {
	if h.renamed != nil {
		h.renamed(ownerID, sessionID, title)
	}
}

// sendRename asks the backend to adopt the title (best effort: a session with
// no live process simply has nothing to rename).
func (h *Hub) sendRename(ownerID, sessionID, title string) {
	c := h.harnessFor(ownerID, sessionID)
	if c == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "acta-rename-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		"request":    map[string]any{"subtype": "rename_session", "title": title},
	})
	if err != nil {
		return
	}
	c.send(Outbound{T: FrameControl, Session: sessionID, Payload: payload})
	c.mu.Lock()
	running := c.running[sessionID]
	c.mu.Unlock()
	if running {
		h.sendNameCommand(context.Background(), c, sessionID, title)
	}
}

// sendNameCommand names the live Claude Code process after the session. The
// rename_session control only retitles the transcript; the "/rename" command
// also sets the name peers see (ListAgents, SendMessage), so a message
// addressed by Acta title reaches the right session. It goes in as an input
// frame, so the transcript shows the rename like any other command.
func (h *Hub) sendNameCommand(ctx context.Context, c *harnessConn, sessionID, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	text := "/rename " + title
	raw, _ := json.Marshal(map[string]any{"text": text})
	h.record(ctx, sessionID, "input", raw)
	c.send(Outbound{T: FrameInput, Session: sessionID, Text: text})
}

// maybeAutoTitle names a session that has never been named, once its first
// turn has produced something to name it after. Claude Code answers a
// generate_session_title control request with a short title; the answer comes
// back through HarnessFrame.
func (h *Hub) maybeAutoTitle(ctx context.Context, ownerID, sessionID string) {
	as, err := h.svc.Get(ctx, sessionID, ownerID)
	if err != nil || strings.TrimSpace(as.Title) != "" {
		return
	}
	h.mu.Lock()
	if h.titling == nil {
		h.titling = map[string]bool{}
	}
	if h.titling[sessionID] {
		h.mu.Unlock()
		return
	}
	h.titling[sessionID] = true
	h.mu.Unlock()

	desc := h.firstInputText(ctx, sessionID)
	if desc == "" {
		h.mu.Lock()
		delete(h.titling, sessionID)
		h.mu.Unlock()
		return
	}
	c := h.harnessFor(ownerID, sessionID)
	if c == nil {
		h.mu.Lock()
		delete(h.titling, sessionID)
		h.mu.Unlock()
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": titleRequestPrefix + strconv.FormatInt(time.Now().UnixNano(), 36),
		"request":    map[string]any{"subtype": "generate_session_title", "description": desc, "persist": true},
	})
	if err != nil {
		return
	}
	c.send(Outbound{T: FrameControl, Session: sessionID, Payload: payload})
}

// firstInputText is the text of the session's first message, which is what a
// title should describe.
func (h *Hub) firstInputText(ctx context.Context, sessionID string) string {
	evs, err := h.svc.Events(ctx, sessionID, 0, 200)
	if err != nil {
		return ""
	}
	for _, ev := range evs {
		if ev.Kind != "input" {
			continue
		}
		var m struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(ev.Payload, &m) == nil && strings.TrimSpace(m.Text) != "" {
			return m.Text
		}
	}
	return ""
}

// applyTitleAnswer takes the title from an answer to our own request. It
// reports whether the frame was ours (and so should not reach the transcript).
func (h *Hub) applyTitleAnswer(ctx context.Context, ownerID, sessionID string, payload json.RawMessage) bool {
	var m struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Subtype   string `json:"subtype"`
			Response  struct {
				Title string `json:"title"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &m) != nil || m.Type != "control_response" {
		return false
	}
	rid := m.Response.RequestID
	if !strings.HasPrefix(rid, titleRequestPrefix) && !strings.HasPrefix(rid, "acta-rename-") {
		return false
	}
	if strings.HasPrefix(rid, titleRequestPrefix) {
		h.mu.Lock()
		delete(h.titling, sessionID)
		h.mu.Unlock()
		title := strings.TrimSpace(m.Response.Response.Title)
		if title != "" {
			if _, err := h.svc.SetTitle(ctx, sessionID, ownerID, title); err == nil {
				h.pushRename(ownerID, sessionID, title)
			}
		}
	}
	return true
}

// presenceOf computes a session's presence across all of an owner's connected
// harnesses: held if any holds it, running if any has a process for it.
func (h *Hub) presenceOf(ownerID, sessionID string) (held, running bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.harness[ownerID] {
		c.mu.Lock()
		if c.sessions[sessionID] {
			held = true
		}
		if c.running[sessionID] {
			running = true
		}
		c.mu.Unlock()
	}
	return held, running
}

// notify publishes a session's current presence, if anyone is listening.
func (h *Hub) notify(ownerID, sessionID string) {
	if h.presence == nil {
		return
	}
	held, running := h.presenceOf(ownerID, sessionID)
	h.presence(ownerID, sessionID, held, running)
}

type harnessConn struct {
	id       string
	ownerID  string
	label    string
	backends []string
	cwd      string
	home     string
	out      chan []byte
	closed   chan struct{}
	once     sync.Once

	mu       sync.Mutex
	sessions map[string]bool // session ids this harness holds (can resume)
	running  map[string]bool // the subset with a live process right now
}

func (c *harnessConn) Close() { c.once.Do(func() { close(c.closed) }) }

// Out is the channel a harness connection's write pump drains.
func (c *harnessConn) Out() <-chan []byte { return c.out }

// Closed is signalled when the hub wants the connection torn down.
func (c *harnessConn) Closed() <-chan struct{} { return c.closed }

func (c *harnessConn) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.out <- b:
	default:
		c.Close() // wedged; reap it
	}
}

func (c *harnessConn) holds(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[session]
}

func (c *harnessConn) add(session string) {
	c.mu.Lock()
	c.sessions[session] = true
	c.mu.Unlock()
}

// setRunning marks a session held and sets its running flag, reporting
// whether anything actually changed (so callers can skip a no-op publish).
func (c *harnessConn) setRunning(session string, on bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := !c.sessions[session] || c.running[session] != on
	c.sessions[session] = true
	if on {
		c.running[session] = true
	} else {
		delete(c.running, session)
	}
	return changed
}

func (c *harnessConn) heldIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.sessions))
	for s := range c.sessions {
		out = append(out, s)
	}
	return out
}

type browserConn struct {
	id        string
	sessionID string
	out       chan []byte
	closed    chan struct{}
	once      sync.Once
	focused   atomic.Bool // tab visible and focused, per the browser's own report
}

func (c *browserConn) Close()                  { c.once.Do(func() { close(c.closed) }) }
func (c *browserConn) Out() <-chan []byte      { return c.out }
func (c *browserConn) Closed() <-chan struct{} { return c.closed }

func (c *browserConn) send(b []byte) {
	select {
	case c.out <- b:
	default:
		c.Close()
	}
}

// --- harness lifecycle ---

// HarnessPresence is the non-secret view of a connected harness for the
// sessions page: what to show and what backends can be spawned on it.
type HarnessPresence struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Backends []string `json:"backends"`
	Cwd      string   `json:"cwd"`      // where `acta harness` runs on its host
	Home     string   `json:"home"`     // that user's home directory
	Sessions []string `json:"sessions"` // held: resumable on this harness
	Running  []string `json:"running"`  // subset with a live process
}

// AttachHarness registers a newly-connected harness after its hello frame and
// returns its connection handle. The caller runs the read and write pumps.
func (h *Hub) AttachHarness(ownerID string, in Inbound) *harnessConn {
	c := &harnessConn{
		id:       id.New(),
		ownerID:  ownerID,
		label:    in.Label,
		backends: in.Backends,
		cwd:      in.Cwd,
		home:     in.Home,
		out:      make(chan []byte, sendQueue),
		closed:   make(chan struct{}),
		sessions: map[string]bool{},
		running:  map[string]bool{},
	}
	for _, s := range in.Sessions {
		c.sessions[s] = true
	}
	for _, s := range in.Running {
		c.sessions[s] = true
		c.running[s] = true
	}
	h.mu.Lock()
	if h.harness[ownerID] == nil {
		h.harness[ownerID] = map[string]*harnessConn{}
	}
	h.harness[ownerID][c.id] = c
	h.mu.Unlock()
	slog.Info("harness attached", "owner", ownerID, "label", c.label, "backends", c.backends, "sessions", len(c.sessions))
	for _, id := range c.heldIDs() {
		h.notify(ownerID, id)
	}
	return c
}

// DetachHarness removes a harness connection.
func (h *Hub) DetachHarness(c *harnessConn) {
	h.mu.Lock()
	if m := h.harness[c.ownerID]; m != nil {
		delete(m, c.id)
		if len(m) == 0 {
			delete(h.harness, c.ownerID)
		}
	}
	h.mu.Unlock()
	c.Close()
	slog.Info("harness detached", "owner", c.ownerID, "label", c.label)
	for _, id := range c.heldIDs() {
		h.notify(c.ownerID, id)
	}
}

// Harnesses returns the owner's currently-connected harnesses.
func (h *Hub) Harnesses(ownerID string) []HarnessPresence {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []HarnessPresence
	for _, c := range h.harness[ownerID] {
		c.mu.Lock()
		sess := make([]string, 0, len(c.sessions))
		for s := range c.sessions {
			sess = append(sess, s)
		}
		run := make([]string, 0, len(c.running))
		for s := range c.running {
			run = append(run, s)
		}
		c.mu.Unlock()
		out = append(out, HarnessPresence{ID: c.id, Label: c.label, Backends: c.backends, Cwd: c.cwd, Home: c.home, Sessions: sess, Running: run})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Label < out[j].Label || (out[i].Label == out[j].Label && out[i].ID < out[j].ID)
	})
	return out
}

// harnessFor returns a connected harness of ownerID that holds session, or nil.
func (h *Hub) harnessFor(ownerID, session string) *harnessConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.harness[ownerID] {
		if c.holds(session) {
			return c
		}
	}
	return nil
}

// harnessByLabel returns a connected harness of ownerID with the given label
// (any, if label is empty), or nil.
func (h *Hub) harnessByLabel(ownerID, label string) *harnessConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.harness[ownerID] {
		if label == "" || c.label == label {
			return c
		}
	}
	return nil
}

// harnessByID returns the owner's connected harness with that connection id.
func (h *Hub) harnessByID(ownerID, id string) *harnessConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c, ok := h.harness[ownerID][id]; ok {
		return c
	}
	return nil
}

// ListDirs asks a harness to complete a directory path on its host and waits
// for the answer: the directories matching the typed prefix, and whether the
// path itself is a directory. Backs the working-directory picker.
func (h *Hub) ListDirs(ctx context.Context, ownerID, harnessID, path string) (dirs []string, exists bool, err error) {
	c := h.harnessByID(ownerID, harnessID)
	if c == nil {
		return nil, false, ErrNoHarness
	}
	reqID := id.New()
	ch := make(chan Inbound, 1)
	h.mu.Lock()
	if h.pendingLs == nil {
		h.pendingLs = map[string]chan Inbound{}
	}
	h.pendingLs[reqID] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pendingLs, reqID)
		h.mu.Unlock()
	}()
	c.send(Outbound{T: FrameLs, ID: reqID, Path: path})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case r := <-ch:
		if r.Error != "" {
			return nil, false, errors.New(r.Error)
		}
		return r.Dirs, r.Exists, nil
	case <-c.Closed():
		return nil, false, ErrNoHarness
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// HarnessFrame handles one frame read from a harness connection.
func (h *Hub) HarnessFrame(ctx context.Context, c *harnessConn, in Inbound) {
	switch in.T {
	case FrameEvent:
		if in.Session == "" {
			return
		}
		if c.setRunning(in.Session, true) { // an event means the process is alive
			h.notify(c.ownerID, in.Session)
		}
		kind := in.Kind
		if kind == "" {
			kind = "event"
		}
		if kind == "control_response" && h.applyTitleAnswer(ctx, c.ownerID, in.Session, in.Payload) {
			return // our own naming traffic, not part of the conversation
		}
		if kind == "result" {
			go h.maybeAutoTitle(context.Background(), c.ownerID, in.Session)
		}
		if kind == "task_output" && !taskOutputDone(in.Payload) {
			// A background shell's output as it runs: relayed live, not
			// stored — the frame marked done carries the whole output.
			h.fanout(in.Session, store.AgentSessionEvent{SessionID: in.Session, Kind: kind, Payload: in.Payload, CreatedAt: time.Now()})
			return
		}
		if kind == "stream_event" {
			// Partial-message deltas: relayed live so a reply can grow on
			// screen, but not stored — the assistant frame that follows
			// carries the complete message, so nothing is lost.
			h.fanout(in.Session, store.AgentSessionEvent{SessionID: in.Session, Kind: kind, Payload: in.Payload, CreatedAt: time.Now()})
			return
		}
		h.record(ctx, in.Session, kind, in.Payload)
		h.maybeAlert(c.ownerID, in.Session, kind, in.Payload)
	case FrameSpawned:
		if c.setRunning(in.Session, true) {
			h.notify(c.ownerID, in.Session)
		}
		spawned := map[string]any{"state": "spawned", "resumed": in.Resumed}
		if len(in.Styles) > 0 {
			spawned["styles"] = in.Styles
		}
		h.recordState(ctx, in.Session, spawned)
		// a fresh process has a derived peer name: give it the session's title
		if as, err := h.svc.Get(ctx, in.Session, c.ownerID); err == nil && as.Title != "" {
			h.sendNameCommand(ctx, c, in.Session, as.Title)
		}
	case FrameLsResult:
		h.mu.Lock()
		ch := h.pendingLs[in.ID]
		h.mu.Unlock()
		if ch != nil {
			select {
			case ch <- in:
			default:
			}
		}
	case FrameSpawnError:
		h.recordState(ctx, in.Session, map[string]any{"state": "spawn_error", "error": in.Error})
		raw, _ := json.Marshal(map[string]any{"state": "spawn_error", "error": in.Error})
		h.maybeAlert(c.ownerID, in.Session, "state", raw)
	case FrameExit:
		code := 0
		if in.Code != nil {
			code = *in.Code
		}
		if c.setRunning(in.Session, false) {
			h.notify(c.ownerID, in.Session)
		}
		h.recordState(ctx, in.Session, map[string]any{"state": "exit", "code": code})
		raw, _ := json.Marshal(map[string]any{"state": "exit", "code": code})
		h.maybeAlert(c.ownerID, in.Session, "state", raw)
	default:
		// Unknown harness frame — record it verbatim so nothing is lost.
		raw, _ := json.Marshal(in)
		if in.Session != "" {
			h.record(ctx, in.Session, "unknown", raw)
		}
	}
}

// --- browser lifecycle ---

// AttachBrowser registers a browser chat connection for a session.
func (h *Hub) AttachBrowser(sessionID string) *browserConn {
	c := &browserConn{
		id:        id.New(),
		sessionID: sessionID,
		out:       make(chan []byte, sendQueue),
		closed:    make(chan struct{}),
	}
	h.mu.Lock()
	if h.browsers[sessionID] == nil {
		h.browsers[sessionID] = map[string]*browserConn{}
	}
	h.browsers[sessionID][c.id] = c
	h.mu.Unlock()
	return c
}

// DetachBrowser removes a browser chat connection.
func (h *Hub) DetachBrowser(c *browserConn) {
	h.mu.Lock()
	if m := h.browsers[c.sessionID]; m != nil {
		delete(m, c.id)
		if len(m) == 0 {
			delete(h.browsers, c.sessionID)
		}
	}
	h.mu.Unlock()
	c.Close()
}

// --- routing ---

// Input persists a user message and forwards it to the harness holding the
// session. When no harness holds it, the transcript records that the message
// couldn't be delivered rather than dropping it silently.
func (h *Hub) Input(ctx context.Context, ownerID, sessionID, text string, images []ImageIn) error {
	m := map[string]any{"text": text}
	if len(images) > 0 {
		m["images"] = images
	}
	raw, _ := json.Marshal(m)
	h.record(ctx, sessionID, "input", raw)
	if c := h.harnessFor(ownerID, sessionID); c != nil {
		c.send(Outbound{T: FrameInput, Session: sessionID, Text: text, Images: images})
		return nil
	}
	h.recordState(ctx, sessionID, map[string]any{"state": "undelivered", "reason": "no harness connected"})
	return nil
}

// Mark records a browser-authored transcript marker (a rewind, say). Unlike
// Control it is never forwarded to the backend: it describes something the
// browser did, for the benefit of every other reader of the transcript.
func (h *Hub) Mark(ctx context.Context, sessionID, kind string, payload json.RawMessage) {
	h.record(ctx, sessionID, kind, payload)
}

// Control persists a browser-authored control message (a permission answer or
// a mode change) and forwards it to the harness holding the session.
func (h *Hub) Control(ctx context.Context, ownerID, sessionID string, payload json.RawMessage) {
	h.record(ctx, sessionID, "control", payload)
	if c := h.harnessFor(ownerID, sessionID); c != nil {
		c.send(Outbound{T: FrameControl, Session: sessionID, Payload: payload})
		return
	}
	h.recordState(ctx, sessionID, map[string]any{"state": "undelivered", "reason": "no harness connected"})
}

// Stop asks the harness to end the session's current turn or process.
func (h *Hub) Stop(ownerID, sessionID string) {
	if c := h.harnessFor(ownerID, sessionID); c != nil {
		c.send(Outbound{T: FrameStop, Session: sessionID})
	}
}

// Spawn routes a spawn request to a connected harness of the session's owner,
// chosen by label. It returns false when no matching harness is connected.
// Spawn starts the session on a connected harness: the one whose connection
// id is target, else the first whose label is target, else (target empty) any.
func (h *Hub) Spawn(as store.AgentSession, target string) bool {
	c := h.harnessByID(as.OwnerID, target)
	if c == nil {
		c = h.harnessByLabel(as.OwnerID, target)
	}
	if c == nil {
		return false
	}
	opts, _ := json.Marshal(as.Options)
	c.add(as.ID)
	c.send(Outbound{T: FrameSpawn, Session: as.ID, Backend: as.Backend, Cwd: as.Cwd, Options: opts})
	h.notify(as.OwnerID, as.ID)
	return true
}

// taskOutputDone reports whether a task_output frame is the final one.
func taskOutputDone(payload json.RawMessage) bool {
	var p struct {
		Done bool `json:"done"`
	}
	return json.Unmarshal(payload, &p) == nil && p.Done
}

// record persists a transcript frame and fans it out to the session's browsers.
func (h *Hub) record(ctx context.Context, sessionID, kind string, payload json.RawMessage) {
	ev, err := h.svc.Append(ctx, sessionID, kind, payload)
	if err != nil {
		slog.Error("agent session append", "session", sessionID, "err", err)
		return
	}
	h.fanout(sessionID, ev)
}

func (h *Hub) recordState(ctx context.Context, sessionID string, m map[string]any) {
	raw, _ := json.Marshal(m)
	h.record(ctx, sessionID, "state", raw)
}

// fanout marshals a stored frame into the browser wire shape and delivers it to
// every browser watching the session.
func (h *Hub) fanout(sessionID string, ev store.AgentSessionEvent) {
	b, err := json.Marshal(browserFrameOf(ev))
	if err != nil {
		return
	}
	h.mu.Lock()
	conns := make([]*browserConn, 0, len(h.browsers[sessionID]))
	for _, c := range h.browsers[sessionID] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.send(b)
	}
}

// browserFrame is the shape the chat view renders from — one transcript frame,
// with its store sequence for ordering and its verbatim payload.
type browserFrame struct {
	Seq     int64           `json:"seq"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	At      string          `json:"at"`
}

func browserFrameOf(ev store.AgentSessionEvent) browserFrame {
	return browserFrame{Seq: ev.Seq, Kind: ev.Kind, Payload: json.RawMessage(ev.Payload), At: ev.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z")}
}
