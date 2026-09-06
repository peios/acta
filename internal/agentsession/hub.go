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

	"github.com/peios/acta/internal/agentsession/model"
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

	mu         sync.Mutex
	harness    map[string]map[string]*harnessConn // ownerID -> connID -> conn
	browsers   map[string]map[string]*browserConn // sessionID -> connID -> conn
	titling    map[string]bool                    // sessions with a title request in flight
	pendingLs  map[string]chan Inbound            // ListDirs / ScanTranscripts requests awaiting a harness answer
	reads      map[string]*transcriptRead         // transcript reads in flight, by session/id
	projectors map[string]*sessionProjector       // live projectors by session
	histMu     sync.Mutex
	hist       map[string]histEntry // recent sessions' projected transcripts (see History)
}

// transcriptRead gathers the lines a harness sends for one read of a
// backend's transcript (see FrameRead) until the read_done that ends it:
// which lines are conversation, and where the live branch runs, is decided
// over the whole batch, not line by line.
type transcriptRead struct {
	source string // "catchup" (before a resume) or "import"
	conn   *harnessConn
	lines  []json.RawMessage
	bytes  int
	over   bool // the batch outgrew readCap and is dropped
}

// readCap bounds one transcript read held in memory. The harness keeps what
// it sends under its own cap (the newest turns of a transcript, trimmed of
// oversized strings), so this is a backstop against an older harness that
// sends the file whole.
const readCap = 96 << 20

// ErrNoHarness is returned when no connected harness can take a request.
var ErrNoHarness = errors.New("no such harness connected")

// ErrHarnessTooOld is returned when the harness asked cannot do what is
// asked: it runs an older acta than the server (restart it after updating).
var ErrHarnessTooOld = errors.New("the harness runs an older acta; update and restart it")

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

// maybeAlert raises an alert for a projected event that warrants one,
// unless the owner is watching. Session lookup and delivery run off the
// relay path.
func (h *Hub) maybeAlert(sessionID string, e model.Event) {
	if h.alert == nil {
		return
	}
	verb, summary, ok := alertFor(e)
	if !ok || h.focused(sessionID) {
		return
	}
	go func() {
		as, err := h.svc.store.AgentSessionByID(context.Background(), sessionID)
		if err != nil {
			return
		}
		h.alert(as.OwnerID, as, verb, summary)
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

// sendRename asks the backend to adopt the title: a running process takes
// it as a command; otherwise the harness holding the session (or any of
// the owner's) writes it into the backend's own record on the host, so the
// title, and the status marker in it, show in the backend's own picker
// after the machine has been off. Best effort either way.
func (h *Hub) sendRename(ownerID, sessionID, title string) {
	ctx := context.Background()
	c := h.harnessFor(ownerID, sessionID)
	if c != nil && c.isRunning(sessionID) {
		h.sendNameCommand(ctx, c, sessionID, title)
		return
	}
	if c == nil {
		c = h.harnessByLabel(ownerID, "")
	}
	if c == nil || c.v < 3 || strings.TrimSpace(title) == "" {
		return
	}
	as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
	if err != nil {
		return
	}
	d := DriverFor(as.Backend)
	if d == nil {
		return
	}
	cu, ok := d.Transcript(as, nil)
	if !ok {
		return
	}
	conv := as.ID
	if c, _ := as.Options["conversation"].(string); strings.TrimSpace(c) != "" {
		conv = strings.TrimSpace(c)
	}
	c.send(Outbound{T: FrameRetitle, Session: sessionID, Backend: as.Backend, Path: cu.Path, ID: conv, Title: strings.TrimSpace(title)})
}

// driverOf returns the driver for a session's backend, or nil.
func (h *Hub) driverOf(ctx context.Context, sessionID string) Driver {
	as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
	if err != nil {
		return nil
	}
	return DriverFor(as.Backend)
}

// sendNameCommand names the live backend process after the session, with
// whatever lines its driver says (for Claude Code: a rename control for the
// transcript's title and a "/rename" command for the name peers see). The
// driver may name an input text to record, so the transcript shows the
// rename like any other command.
func (h *Hub) sendNameCommand(ctx context.Context, c *harnessConn, sessionID, title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	d := h.driverOf(ctx, sessionID)
	if d == nil {
		return
	}
	as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
	if err != nil {
		return
	}
	lines, echo := d.Rename(as, title)
	if echo != "" {
		raw, _ := json.Marshal(map[string]any{"text": echo})
		h.record(ctx, sessionID, "input", raw)
	}
	for _, l := range lines {
		c.write(sessionID, l)
	}
}

// deliver hands a user message to the harness holding a session: Acta
// composes the backend's line, starting the process first when none is
// running.
func (h *Hub) deliver(ctx context.Context, c *harnessConn, sessionID, text string, images []ImageIn) {
	as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
	if err != nil {
		return
	}
	d := DriverFor(as.Backend)
	if d == nil {
		h.recordState(ctx, sessionID, map[string]any{"state": "undelivered", "reason": "no driver for backend " + as.Backend})
		return
	}
	if !c.isRunning(sessionID) {
		h.spawn(c, as, true)
	}
	line := d.InputLine(as, text, images)
	// Remember it until the process has clearly taken it: if this spawn turns
	// out to be a resume of a session with no stored conversation, the
	// process dies at once and the message must go to its replacement.
	c.mu.Lock()
	c.pending[sessionID] = line
	c.mu.Unlock()
	c.write(sessionID, line)
}

// spawn asks a harness to run a session's process, composed by its driver.
// resume continues the backend's own conversation.
func (h *Hub) spawn(c *harnessConn, as store.AgentSession, resume bool) {
	d := DriverFor(as.Backend)
	if d == nil {
		h.recordState(context.Background(), as.ID, map[string]any{"state": "spawn_error", "error": "no driver for backend " + as.Backend})
		return
	}
	l := d.Launch(as, resume)
	c.mu.Lock()
	c.sessions[as.ID] = true
	c.resuming[as.ID] = resume
	c.mu.Unlock()
	// A resume first catches up on what the backend's own record holds that
	// Acta does not (turns taken in a terminal): the harness reads the
	// transcript from the last message Acta has before it starts the process.
	var cu *Catchup
	if resume && c.v >= 3 { // an older harness does not read transcripts
		if evs, err := h.svc.Events(context.Background(), as.ID, 0, 0); err == nil {
			if x, ok := d.Transcript(as, evs); ok {
				x.Backend = as.Backend
				cu = &x
				h.openRead(as.ID, "catchup", "catchup", c)
			}
		}
	}
	c.send(Outbound{T: FrameSpawn, Session: as.ID, Backend: as.Backend, Cwd: as.Cwd, Cmd: l.Cmd, Args: l.Args, Env: l.Env, Resume: resume, Styles: l.Styles, Catchup: cu})
}

// openRead prepares to gather the lines of a transcript read from harness c.
func (h *Hub) openRead(sessionID, reqID, source string, c *harnessConn) {
	h.mu.Lock()
	if h.reads == nil {
		h.reads = map[string]*transcriptRead{}
	}
	h.reads[sessionID+"/"+reqID] = &transcriptRead{source: source, conn: c}
	h.mu.Unlock()
}

// dropReads fails every read in flight from harness c: what arrived is let
// go, and the session says why nothing came of it, instead of holding a
// partial transcript in memory for good.
func (h *Hub) dropReads(ctx context.Context, c *harnessConn) {
	h.mu.Lock()
	var gone []string
	var sources []string
	for key, r := range h.reads {
		if r.conn == c {
			gone = append(gone, key)
			sources = append(sources, r.source)
			delete(h.reads, key)
		}
	}
	h.mu.Unlock()
	for i, key := range gone {
		session := key[:strings.LastIndex(key, "/")]
		h.recordState(ctx, session, map[string]any{"state": sources[i] + "_failed", "reason": "the harness disconnected during the read"})
		slog.Warn("transcript read dropped", "session", session, "source", sources[i])
	}
}

// gatherRead adds a batch of lines to a read in flight.
func (h *Hub) gatherRead(sessionID, reqID string, lines []json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.reads[sessionID+"/"+reqID]
	if r == nil || r.over {
		return
	}
	for _, l := range lines {
		r.bytes += len(l)
	}
	if r.bytes > readCap {
		r.over = true
		r.lines = nil
		return
	}
	r.lines = append(r.lines, lines...)
}

// finishRead stores what a completed transcript read found: a divider
// saying where the records came from, then the records themselves, each at
// the moment it was written, and projects them for anyone watching.
func (h *Hub) finishRead(ctx context.Context, c *harnessConn, in Inbound) {
	h.mu.Lock()
	r := h.reads[in.Session+"/"+in.ID]
	delete(h.reads, in.Session+"/"+in.ID)
	h.mu.Unlock()
	if r == nil {
		return
	}
	if r.source == "import" {
		c.mu.Lock()
		c.sessions[in.Session] = true
		c.mu.Unlock()
		h.notify(c.ownerID, in.Session)
	}
	if in.Error != "" || r.over {
		reason := in.Error
		if r.over {
			reason = "the transcript is too large to read"
		}
		h.recordState(ctx, in.Session, map[string]any{"state": r.source + "_failed", "reason": reason})
		slog.Warn("transcript read failed", "session", in.Session, "source", r.source, "reason", reason)
		return
	}
	d := h.driverOf(ctx, in.Session)
	if d == nil || !in.Found || len(r.lines) == 0 {
		return
	}
	recs := d.TranscriptRecords(r.lines)
	if len(recs) == 0 {
		return
	}
	first, last := recs[0].At, recs[len(recs)-1].At
	for _, rec := range recs {
		if !rec.At.IsZero() && (first.IsZero() || rec.At.Before(first)) {
			first = rec.At
		}
		if rec.At.After(last) {
			last = rec.At
		}
	}
	divider := map[string]any{"state": r.source, "count": len(recs)}
	if !first.IsZero() {
		divider["from"] = first.UTC().Format(time.RFC3339)
		divider["to"] = last.UTC().Format(time.RFC3339)
	}
	if in.Skipped > 0 {
		divider["skipped"] = in.Skipped
	}
	raw, _ := json.Marshal(divider)
	batch := make([]store.AgentSessionEvent, 0, len(recs)+1)
	batch = append(batch, store.AgentSessionEvent{SessionID: in.Session, Kind: "state", Payload: raw, CreatedAt: first})
	for _, rec := range recs {
		batch = append(batch, store.AgentSessionEvent{SessionID: in.Session, Kind: TranscriptKind, Payload: rec.Payload, CreatedAt: rec.At})
	}
	stored, err := h.svc.AppendBatch(ctx, batch)
	if err != nil {
		slog.Error("agent session append batch", "session", in.Session, "err", err)
		h.recordState(ctx, in.Session, map[string]any{"state": r.source + "_failed", "reason": "the records could not be stored: " + err.Error()})
		return
	}
	for _, ev := range stored {
		h.emit(ctx, in.Session, ev, true)
	}
	if r.source == "import" && !last.IsZero() {
		// the session is as old as its last message, not as new as the import
		if as, err := h.svc.store.AgentSessionByID(ctx, in.Session); err == nil {
			_, _ = h.svc.store.UpdateAgentSessionOptions(ctx, as.ID, as.Options, last)
		}
	}
}

// TranscriptKind labels a stored frame read off a backend's own transcript
// rather than heard from a live process; the backend's projector knows its
// records.
const TranscriptKind = "transcript"

// ScanTranscripts asks a harness for the transcripts a backend keeps on its
// host and waits for the list, in the backend's own shape.
func (h *Hub) ScanTranscripts(ctx context.Context, ownerID, harnessID, backend string) (json.RawMessage, error) {
	c := h.harnessByID(ownerID, harnessID)
	if c == nil {
		return nil, ErrNoHarness
	}
	if c.v < 3 {
		return nil, ErrHarnessTooOld
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
	c.send(Outbound{T: FrameScan, ID: reqID, Backend: backend})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case r := <-ch:
		if r.Error != "" {
			return nil, errors.New(r.Error)
		}
		return r.Items, nil
	case <-c.Closed():
		return nil, ErrNoHarness
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Import has a harness read a whole transcript on its host into a session
// Acta has just recorded under the transcript's own id, and hold the session
// so it can be resumed there. The records arrive as the read runs.
func (h *Hub) Import(ownerID, harnessID string, as store.AgentSession) error {
	c := h.harnessByID(ownerID, harnessID)
	if c == nil {
		return ErrNoHarness
	}
	if c.v < 3 {
		return ErrHarnessTooOld
	}
	d := DriverFor(as.Backend)
	if d == nil {
		return errors.New("no driver for backend " + as.Backend)
	}
	cu, ok := d.Transcript(as, nil)
	if !ok {
		return errors.New("backend " + as.Backend + " keeps no transcript to import")
	}
	h.openRead(as.ID, "import", "import", c)
	c.send(Outbound{T: FrameRead, Session: as.ID, ID: "import", Backend: as.Backend, Path: cu.Path, Key: cu.Key, Hold: true})
	return nil
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
	d := h.driverOf(ctx, sessionID)
	if d == nil {
		return
	}
	line := d.TitleRequest(titleRequestPrefix+strconv.FormatInt(time.Now().UnixNano(), 36), desc)
	if line == nil {
		h.mu.Lock()
		delete(h.titling, sessionID)
		h.mu.Unlock()
		return
	}
	c.write(sessionID, line)
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
func (h *Hub) applyTitleAnswer(ctx context.Context, d Driver, ownerID, sessionID, kind string, payload json.RawMessage) bool {
	if d == nil {
		return false
	}
	rid, title, ok := d.TitleAnswer(kind, payload)
	if !ok {
		return false
	}
	if strings.HasPrefix(rid, titleRequestPrefix) {
		h.mu.Lock()
		delete(h.titling, sessionID)
		h.mu.Unlock()
		if title != "" {
			if _, err := h.svc.SetTitle(ctx, sessionID, ownerID, title); err == nil {
				h.pushRename(ownerID, sessionID, title)
			}
		}
	}
	return true
}

// Delete removes a session and tells every harness holding it to stop its
// process and forget it, so it is neither offered on the next hello nor left
// running unowned. Claude Code's own transcript on the host is left alone.
func (h *Hub) Delete(ctx context.Context, ownerID, sessionID string) error {
	if err := h.svc.Delete(ctx, sessionID, ownerID); err != nil {
		return err
	}
	h.dropProjector(sessionID)
	h.mu.Lock()
	var conns []*harnessConn
	for _, c := range h.harness[ownerID] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		if !c.holds(sessionID) {
			continue
		}
		c.drop(sessionID)
		c.send(Outbound{T: FrameForget, Session: sessionID})
	}
	h.notify(ownerID, sessionID)
	return nil
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
	v        int // protocol version from the hello (2: writes what Acta composes)
	out      chan []byte
	closed   chan struct{}
	once     sync.Once

	mu       sync.Mutex
	sessions map[string]bool   // session ids this harness holds (can resume)
	running  map[string]bool   // the subset with a live process right now
	resuming map[string]bool   // sessions whose current process is a resume (an early exit may mean "nothing to resume")
	pending  map[string][]byte // the last input line per session until the backend has clearly taken it
	retried  map[string]bool   // sessions whose pending line was already re-sent once after an exit
}

// write sends one composed stdin line to the harness for a session.
func (c *harnessConn) write(session string, line []byte) {
	if len(line) == 0 {
		return
	}
	c.send(Outbound{T: FrameWrite, Session: session, Line: string(line)})
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

// drop forgets a session on this connection (both held and running).
func (c *harnessConn) drop(session string) {
	c.mu.Lock()
	delete(c.sessions, session)
	delete(c.running, session)
	delete(c.resuming, session)
	delete(c.pending, session)
	delete(c.retried, session)
	c.mu.Unlock()
}

func (c *harnessConn) isRunning(session string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running[session]
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
		v:        in.V,
		out:      make(chan []byte, sendQueue),
		closed:   make(chan struct{}),
		sessions: map[string]bool{},
		running:  map[string]bool{},
		resuming: map[string]bool{},
		pending:  map[string][]byte{},
		retried:  map[string]bool{},
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
	slog.Info("harness attached", "owner", ownerID, "label", c.label, "v", c.v, "backends", c.backends, "sessions", len(c.sessions))
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
	h.dropReads(context.Background(), c)
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
		d := h.driverOf(ctx, in.Session)
		kind := in.Kind // a harness-authored notice names its own kind (task_output)
		if kind == "" && d != nil {
			kind = d.Kind(in.Payload)
		}
		if kind == "" {
			kind = "event"
		}
		if h.applyTitleAnswer(ctx, d, c.ownerID, in.Session, kind, in.Payload) {
			return // our own naming traffic, not part of the conversation
		}
		if kind == "result" {
			go h.maybeAutoTitle(context.Background(), c.ownerID, in.Session)
		}
		stored := true
		if d != nil {
			stored = d.Stored(kind, in.Payload)
		} else {
			stored = !(kind == "stream_event" || (kind == "task_output" && !taskOutputDone(in.Payload)))
		}
		if !stored {
			// Streamed deltas and a background command's output as it runs:
			// relayed live so the screen moves, not stored — the settled
			// frame that follows carries everything they said.
			h.emit(ctx, in.Session, store.AgentSessionEvent{SessionID: in.Session, Kind: kind, Payload: in.Payload, CreatedAt: time.Now()}, false)
			return
		}
		h.record(ctx, in.Session, kind, in.Payload)
		if d != nil {
			h.afterFrame(ctx, c, d, in.Session, kind, in.Payload)
		}
	case FrameSpawned:
		if c.setRunning(in.Session, true) {
			h.notify(c.ownerID, in.Session)
		}
		// a catch-up the harness was asked for has finished by now
		h.mu.Lock()
		delete(h.reads, in.Session+"/catchup")
		h.mu.Unlock()
		c.mu.Lock()
		if !in.Resumed {
			delete(c.resuming, in.Session)
		}
		c.mu.Unlock()
		// the backend's opening lines (a handshake, the thread to open or
		// resume) go in before anything else
		if d := h.driverOf(ctx, in.Session); d != nil {
			if as, err := h.svc.store.AgentSessionByID(ctx, in.Session); err == nil {
				for _, l := range d.StartLines(as, in.Resumed) {
					c.write(in.Session, l)
				}
			}
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
	case FrameRecords:
		h.gatherRead(in.Session, in.ID, in.Lines)
	case FrameReadDone:
		h.finishRead(ctx, c, in)
	case FrameLsResult, FrameScanResult:
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
	case FrameExit:
		code := 0
		if in.Code != nil {
			code = *in.Code
		}
		if c.setRunning(in.Session, false) {
			h.notify(c.ownerID, in.Session)
		}
		c.mu.Lock()
		wasResume := c.resuming[in.Session]
		delete(c.resuming, in.Session)
		held := c.pending[in.Session]
		retried := c.retried[in.Session]
		c.mu.Unlock()
		if wasResume {
			// A backend that has nothing to resume (a session spawned but never
			// used) dies at once: start it fresh under the same id instead of
			// leaving it unusable, and hand it the message that was waiting.
			if d := h.driverOf(ctx, in.Session); d != nil && d.ResumeFailed(code, in.Stderr) {
				h.recordState(ctx, in.Session, map[string]any{"state": "resume_failed", "reason": "no stored conversation; starting fresh under the same id"})
				if as, err := h.svc.store.AgentSessionByID(ctx, in.Session); err == nil {
					h.spawn(c, as, false)
					if len(held) > 0 {
						c.write(in.Session, held)
					}
				}
				return
			}
		}
		h.recordState(ctx, in.Session, map[string]any{"state": "exit", "code": code})
		if len(held) > 0 && !retried {
			// The process died holding a message it never answered (it was
			// written in the moment between the death and its report): start
			// the conversation again and hand the message over, once — a
			// process that dies on every attempt is not retried forever.
			if as, err := h.svc.store.AgentSessionByID(ctx, in.Session); err == nil {
				c.mu.Lock()
				c.retried[in.Session] = true
				c.mu.Unlock()
				h.spawn(c, as, true)
				c.write(in.Session, held)
			}
		}
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
	if d := h.driverOf(ctx, sessionID); d != nil {
		if k, v, ok := d.Option("input", raw); ok {
			_ = h.svc.SetOption(ctx, sessionID, k, v)
		}
	}
	if c := h.harnessFor(ownerID, sessionID); c != nil {
		h.deliver(ctx, c, sessionID, text, images)
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

// Control persists a browser operation (an approval answer, a setting
// change, a catalogue request, a rewind step) and forwards the lines the
// session's driver makes of it to the harness holding the session.
func (h *Hub) Control(ctx context.Context, ownerID, sessionID string, op BrowserOp, payload json.RawMessage) {
	h.record(ctx, sessionID, "control", payload)
	d := h.driverOf(ctx, sessionID)
	if d != nil {
		if k, v, ok := d.Option("control", payload); ok {
			_ = h.svc.SetOption(ctx, sessionID, k, v)
		}
	}
	c := h.harnessFor(ownerID, sessionID)
	if c == nil {
		h.recordState(ctx, sessionID, map[string]any{"state": "undelivered", "reason": "no harness connected"})
		return
	}
	// an operation for a session with no process (an answer to a prompt
	// that died with it) has nothing to reach; a setting is still remembered
	if d == nil || !c.isRunning(sessionID) {
		return
	}
	as, err := h.svc.store.AgentSessionByID(ctx, sessionID)
	if err != nil {
		return
	}
	for _, l := range d.ControlLines(as, op) {
		c.write(sessionID, l)
	}
}

// Stop ends the session's current turn: the backend's own interrupt when it
// has one (the process, and its warm context, survive), else a signal.
func (h *Hub) Stop(ownerID, sessionID string) {
	c := h.harnessFor(ownerID, sessionID)
	if c == nil {
		return
	}
	if d := h.driverOf(context.Background(), sessionID); d != nil && c.isRunning(sessionID) {
		if as, err := h.svc.store.AgentSessionByID(context.Background(), sessionID); err == nil {
			if line := d.InterruptLine(as); line != nil {
				c.write(sessionID, line)
				return
			}
		}
	}
	c.send(Outbound{T: FrameStop, Session: sessionID})
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
	h.spawn(c, as, false)
	h.notify(as.OwnerID, as.ID)
	return true
}

// afterFrame applies the driver's rules to a stored frame: remember the
// backend's conversation id for resume, drop the held message once taken,
// and have the harness tail (or stop tailing) a background task's output.
func (h *Hub) afterFrame(ctx context.Context, c *harnessConn, d Driver, session, kind string, payload json.RawMessage) {
	for k, v := range d.Notes(session, kind, payload) {
		_ = h.svc.SetOption(ctx, session, k, v)
	}
	if d.Acknowledged(kind, payload) {
		c.mu.Lock()
		delete(c.pending, session)
		delete(c.resuming, session)
		delete(c.retried, session)
		c.mu.Unlock()
	}
	if id, path, ok := d.BackgroundTask(kind, payload); ok {
		c.send(Outbound{T: FrameTail, Session: session, ID: id, Path: path})
	}
	if id, ok := d.TaskEnded(kind, payload); ok {
		c.send(Outbound{T: FrameUntail, Session: session, ID: id})
	}
}

// taskOutputDone reports whether a task_output frame is the final one.
func taskOutputDone(payload json.RawMessage) bool {
	var p struct {
		Done bool `json:"done"`
	}
	return json.Unmarshal(payload, &p) == nil && p.Done
}

// record persists a transcript frame and fans its projection out to the
// session's browsers.
func (h *Hub) record(ctx context.Context, sessionID, kind string, payload json.RawMessage) {
	ev, err := h.svc.Append(ctx, sessionID, kind, payload)
	if err != nil {
		slog.Error("agent session append", "session", sessionID, "err", err)
		return
	}
	h.emit(ctx, sessionID, ev, true)
}

func (h *Hub) recordState(ctx context.Context, sessionID string, m map[string]any) {
	raw, _ := json.Marshal(m)
	h.record(ctx, sessionID, "state", raw)
}

// fanout delivers one model event to every browser watching the session.
func (h *Hub) fanout(sessionID string, e model.Event) {
	b, err := json.Marshal(e.Wire())
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
