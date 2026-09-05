package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/peios/acta/internal/agentsession"
	"github.com/peios/acta/internal/agentsession/model"
	"github.com/peios/acta/internal/identity"
	"github.com/peios/acta/internal/store"
)

// knownBackends are the backends the harness can spawn. Claude Code is the only
// one today; the list is here so the new-session form and the harness advertise
// the same vocabulary.
var knownBackends = []string{"claude-code"}

type agentSessionsData struct {
	chrome
	Principal *identity.Principal
	Sessions  []agentSessionRow
	Harnesses []agentsession.HarnessPresence
	Picks     []harnessPick
	PicksJSON string
	Backends  []string
	Err       string
}

// agentSessionRow is one session in the list, plus whether a harness currently
// holds it (so the UI can show live vs offline).
type agentSessionRow struct {
	store.AgentSession
	Live    bool // held by a connected harness (resumable)
	Running bool // a process is running right now
}

// agentChrome builds the page chrome in agent mode: the sidebar lists the
// user's sessions instead of the workspace nav, with activeID highlighted. It
// returns the session rows and harness presences too, since the list page
// renders the same data in its body.
func (h *handlers) agentChrome(r *http.Request, activeID string) (chrome, []agentSessionRow, []agentsession.HarnessPresence, error) {
	ch, err := h.chromeFor(r, "agents", nil)
	if err != nil {
		return chrome{}, nil, nil, err
	}
	p := principalFrom(r.Context())
	list, err := h.agentSessions.List(r.Context(), p.ID)
	if err != nil {
		return chrome{}, nil, nil, err
	}
	presences := h.agentHub.Harnesses(p.ID)
	liveIDs := map[string]bool{}
	runIDs := map[string]bool{}
	for _, pr := range presences {
		for _, s := range pr.Sessions {
			liveIDs[s] = true
		}
		for _, s := range pr.Running {
			runIDs[s] = true
		}
	}
	rows := make([]agentSessionRow, 0, len(list))
	nav := make([]agentSessionNav, 0, len(list))
	for _, as := range list {
		rows = append(rows, agentSessionRow{AgentSession: as, Live: liveIDs[as.ID], Running: runIDs[as.ID]})
		nav = append(nav, agentSessionNav{ID: as.ID, Title: sessionLabel(as), Live: liveIDs[as.ID], Running: runIDs[as.ID]})
	}
	ch.AgentMode = true
	ch.AgentSessions = nav
	ch.ActiveSession = activeID
	return ch, rows, presences, nil
}

// sessionLabel is what a session is called in lists: its title, or a
// backend-plus-directory fallback for untitled ones.
func sessionLabel(as store.AgentSession) string {
	if as.Title != "" {
		return as.Title
	}
	if as.Cwd != "" {
		parts := strings.Split(strings.TrimRight(as.Cwd, "/"), "/")
		return as.Backend + " · " + parts[len(parts)-1]
	}
	return as.Backend + " session"
}

func (h *handlers) agentSessionsPage(w http.ResponseWriter, r *http.Request) {
	ch, rows, presences, err := h.agentChrome(r, "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p := principalFrom(r.Context())
	picks := harnessPicks(presences, rows)
	pj, _ := json.Marshal(picks)
	render(w, http.StatusOK, "agent_sessions.html", agentSessionsData{
		chrome:    ch,
		Principal: p,
		Sessions:  rows,
		Harnesses: presences,
		Picks:     picks,
		PicksJSON: string(pj),
		Backends:  knownBackends,
		Err:       agentSessionErr(r.URL.Query().Get("err")),
	})
}

// harnessPick is what the new-session form knows about one connected
// harness: enough to default and complete the working directory.
type harnessPick struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Backends []string `json:"backends"`
	Cwd      string   `json:"cwd"`
	Home     string   `json:"home"`
	Cwds     []string `json:"cwds"` // working directories of sessions this harness holds, most recent first
	Running  int      `json:"running"`
	Held     int      `json:"held"`
	Recent   bool     `json:"recent"` // the default pick: holds the most recently active session
}

// harnessPicks pairs each connected harness with the working directories of
// the sessions it holds and marks the most recently used one as the default.
func harnessPicks(presences []agentsession.HarnessPresence, rows []agentSessionRow) []harnessPick {
	byID := map[string]agentSessionRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]harnessPick, 0, len(presences))
	best, bestAt := -1, time.Time{}
	for i, pr := range presences {
		pick := harnessPick{ID: pr.ID, Label: pr.Label, Backends: pr.Backends, Cwd: pr.Cwd, Home: pr.Home, Running: len(pr.Running), Held: len(pr.Sessions)}
		var held []agentSessionRow
		for _, sid := range pr.Sessions {
			if r, ok := byID[sid]; ok {
				held = append(held, r)
			}
		}
		sort.Slice(held, func(a, b int) bool { return held[a].UpdatedAt.After(held[b].UpdatedAt) })
		seen := map[string]bool{}
		for _, r := range held {
			if r.Cwd != "" && !seen[r.Cwd] {
				seen[r.Cwd] = true
				pick.Cwds = append(pick.Cwds, r.Cwd)
			}
		}
		if len(held) > 0 && held[0].UpdatedAt.After(bestAt) {
			best, bestAt = i, held[0].UpdatedAt
		}
		out = append(out, pick)
	}
	if best < 0 && len(out) > 0 {
		best = 0
	}
	if best >= 0 {
		out[best].Recent = true
	}
	return out
}

// agentHarnessDirs completes a working-directory path on a harness host for
// the new-session form: GET /account/harnesses/{id}/dirs?path=…
// agentSessionLookup finds the caller's session with a given title (case-
// insensitively), so a peer message from a session named after an Acta title
// can link to it even when the sidebar predates that session.
func (h *handlers) agentSessionLookup(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	exclude := r.URL.Query().Get("exclude")
	w.Header().Set("Content-Type", "application/json")
	if title == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{})
		return
	}
	list, err := h.agentSessions.List(r.Context(), p.ID)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{})
		return
	}
	for _, as := range list {
		if as.ID != exclude && strings.EqualFold(strings.TrimSpace(as.Title), title) {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": as.ID, "title": as.Title})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func (h *handlers) agentHarnessDirs(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	path := r.URL.Query().Get("path")
	if len(path) > 4096 {
		http.Error(w, "path too long", http.StatusBadRequest)
		return
	}
	dirs, exists, err := h.agentHub.ListDirs(r.Context(), p.ID, r.PathValue("id"), path)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"dirs": []string{}, "error": err.Error()})
		return
	}
	if dirs == nil {
		dirs = []string{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"dirs": dirs, "exists": exists})
}

func agentSessionErr(code string) string {
	switch code {
	case "no_harness":
		return "No harness is connected. Run `acta harness` on the machine you want to work on."
	case "bad_backend":
		return "Unknown backend."
	case "":
		return ""
	default:
		return "Something went wrong."
	}
}

func (h *handlers) agentSessionCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	backend := strings.TrimSpace(r.PostFormValue("backend"))
	if backend == "" {
		backend = "claude-code"
	}
	if !knownBackend(backend) {
		http.Redirect(w, r, "/account/sessions?err=bad_backend", http.StatusSeeOther)
		return
	}
	cwd := strings.TrimSpace(r.PostFormValue("cwd"))
	// The form names the harness by connection id; a label (the older API
	// shape) still works, and neither means any connected harness.
	target := strings.TrimSpace(r.PostFormValue("harness"))
	if target == "" {
		target = strings.TrimSpace(r.PostFormValue("label"))
	}
	title := strings.TrimSpace(r.PostFormValue("title"))

	// Permission mode is chosen in the session itself; a session starts in
	// auto unless the caller says otherwise.
	options := map[string]any{"permission_mode": "auto"}
	if pm := strings.TrimSpace(r.PostFormValue("permission_mode")); pm != "" {
		options["permission_mode"] = pm
	}
	// A model or effort named at creation starts the session on it (the
	// picker in the session changes it later).
	if m := strings.TrimSpace(r.PostFormValue("model")); m != "" && len(m) < 80 {
		options["model"] = m
	}
	if e := strings.TrimSpace(r.PostFormValue("effort")); e != "" && len(e) < 20 {
		options["effort"] = e
	}

	as, err := h.agentSessions.Create(r.Context(), p.ID, backend, cwd, title, options)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !h.agentHub.Spawn(as, target) {
		// Keep the record (its transcript will note the failure once a harness
		// picks it up), but tell the user why nothing started.
		_, _ = h.agentSessions.Append(r.Context(), as.ID, "state",
			json.RawMessage(`{"state":"undelivered","reason":"no harness connected at creation"}`))
		http.Redirect(w, r, "/account/sessions/"+as.ID+"?err=no_harness", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/account/sessions/"+as.ID, http.StatusSeeOther)
}

func knownBackend(b string) bool {
	for _, k := range knownBackends {
		if k == b {
			return true
		}
	}
	return false
}

func (h *handlers) agentSessionDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	err := h.agentHub.Delete(r.Context(), p.ID, r.PathValue("id"))
	if err != nil && !errors.Is(err, agentsession.ErrNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/account/sessions", http.StatusSeeOther)
}

type agentSessionData struct {
	chrome
	Principal *identity.Principal
	Session   store.AgentSession
	// EventsJSON is the session's transcript projected into the common event
	// model, as a JSON array the page hydrates from — the same shape the
	// websocket then streams, so one client renderer serves both.
	EventsJSON template.JS
	LastSeq    int64
	Live       bool // held by a connected harness
	Running    bool // process running right now
	Err        string
}

func (h *handlers) agentSessionPage(w http.ResponseWriter, r *http.Request) {
	if p := principalFrom(r.Context()); p != nil {
		// opening the session reads everything the bell said about it
		_ = h.board.MarkItemNotificationsRead(r.Context(), p.ID, r.PathValue("id"))
	}
	ch, rows, _, err := h.agentChrome(r, r.PathValue("id"))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	p := principalFrom(r.Context())
	as, err := h.agentSessions.Get(r.Context(), r.PathValue("id"), p.ID)
	if errors.Is(err, agentsession.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	evs, last, err := h.agentHub.History(r.Context(), as, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if evs == nil {
		evs = []model.Event{}
	}
	// json.Marshal escapes <, > and & so the array is safe inside a script
	// element; template.JS keeps html/template from escaping it again.
	ej, err := json.Marshal(evs)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	live, running := false, false
	for _, row := range rows {
		if row.ID == as.ID {
			live, running = row.Live, row.Running
		}
	}
	render(w, http.StatusOK, "agent_session.html", agentSessionData{
		chrome:     ch,
		Principal:  p,
		Session:    as,
		EventsJSON: template.JS(ej),
		LastSeq:    last,
		Live:       live,
		Running:    running,
		Err:        agentSessionErr(r.URL.Query().Get("err")),
	})
}

// --- websockets ---

// agentSessionBrowserWS is the browser's chat connection to one session. It is
// cookie-authed UI; coder/websocket enforces same-origin on Accept, which is
// the CSRF equivalent for a socket. The browser sends {"t":"input","text":...}
// and receives transcript frames from the seq it names in ?after.
func (h *handlers) agentSessionBrowserWS(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	as, err := h.agentSessions.Get(r.Context(), r.PathValue("id"), p.ID)
	if errors.Is(err, agentsession.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	// The default read limit is 32 KB; a message carrying images is larger, and
	// exceeding it kills the connection rather than the frame.
	c.SetReadLimit(8 << 20)

	bc := h.agentHub.AttachBrowser(as.ID)
	defer h.agentHub.DetachBrowser(bc)
	go keepalive(c, bc.Closed())

	// Replay the events of any frames the page doesn't already have
	// (?after=<seq>); the projection runs from the start regardless, since an
	// event's shape depends on what came before it.
	after := int64(0)
	if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	if evs, _, err := h.agentHub.History(r.Context(), as, after); err == nil {
		for _, e := range evs {
			b, _ := json.Marshal(e)
			_ = c.Write(r.Context(), websocket.MessageText, b)
		}
	}

	// Write pump: hub frames -> socket.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-bc.Closed():
				return
			case b := <-bc.Out():
				if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
					return
				}
			}
		}
	}()

	// Read pump: browser input -> hub.
	for {
		_, data, err := c.Read(context.Background())
		if err != nil {
			break
		}
		var in agentsession.BrowserIn
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		switch in.T {
		case "input":
			imgs, ok := validImages(in.Images)
			if ok && (strings.TrimSpace(in.Text) != "" || len(imgs) > 0) {
				_ = h.agentHub.Input(context.Background(), p.ID, as.ID, in.Text, imgs)
			}
		case "stop":
			h.agentHub.Stop(p.ID, as.ID)
		case "focus":
			// The tab reports whether it is visible and focused; while it is,
			// alerts about this session are not raised, and coming back to
			// it reads whatever the bell collected meanwhile.
			h.agentHub.SetFocus(bc, in.On)
			if in.On {
				_ = h.board.MarkItemNotificationsRead(context.Background(), p.ID, as.ID)
				if n, err := h.board.UnreadCount(context.Background(), p.ID); err == nil {
					h.publishLive(userTopic(p.ID), "notif.count", "", map[string]any{"count": n})
				}
			}
		case "mark":
			// A note about something the browser did (a rewind). Recorded so
			// every reader sees it; never sent to the backend.
			if len(in.Payload) > 0 && len(in.Payload) < 64<<10 && json.Valid(in.Payload) && strings.HasPrefix(strings.TrimSpace(string(in.Payload)), "{") {
				h.agentHub.Mark(context.Background(), as.ID, "rewind", in.Payload)
			}
		case "control":
			// Must be a JSON object, and small: a permission answer or a mode
			// change, never a bulk payload.
			if len(in.Payload) > 0 && len(in.Payload) < 64<<10 && json.Valid(in.Payload) && strings.HasPrefix(strings.TrimSpace(string(in.Payload)), "{") {
				h.agentHub.Control(context.Background(), p.ID, as.ID, in.Payload)
			}
		}
	}
	bc.Close()
	<-writerDone
}

// harnessWS is the harness relay endpoint. It is Bearer-authed (the api mux is
// wrapped with requireToken), so the principal is already in context. The first
// frame must be a hello naming the harness label, backends, and the session ids
// it holds. The connection then carries events up and spawn/input/stop down.
func (h *handlers) harnessWS(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// A harness is not a browser and sends no Origin; the Bearer token is the
	// auth, so skip the same-origin check here.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(8 << 20) // stream-json events (a full transcript compaction) can be large

	// First frame: hello.
	_, data, err := c.Read(context.Background())
	if err != nil {
		return
	}
	var hello agentsession.Inbound
	if json.Unmarshal(data, &hello) != nil || hello.T != agentsession.FrameHello {
		_ = c.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}
	// Only announce sessions this principal actually owns — a harness can't
	// claim someone else's session id.
	hello.Sessions = h.ownedSessions(r.Context(), p.ID, hello.Sessions)

	hc := h.agentHub.AttachHarness(p.ID, hello)
	defer h.agentHub.DetachHarness(hc)
	go keepalive(c, hc.Closed())

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-hc.Closed():
				return
			case b := <-hc.Out():
				if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := c.Read(context.Background())
		if err != nil {
			break
		}
		var in agentsession.Inbound
		if json.Unmarshal(data, &in) != nil {
			continue
		}
		// A harness may only touch sessions the principal owns.
		if in.Session != "" {
			if owner, err := h.agentSessions.OwnerOf(r.Context(), in.Session); err != nil || owner != p.ID {
				continue
			}
		}
		h.agentHub.HarnessFrame(context.Background(), hc, in)
	}
	hc.Close()
	<-writerDone
}

// ownedSessions filters ids down to those actually owned by ownerID.
func (h *handlers) ownedSessions(ctx context.Context, ownerID string, ids []string) []string {
	out := ids[:0]
	for _, id := range ids {
		if owner, err := h.agentSessions.OwnerOf(ctx, id); err == nil && owner == ownerID {
			out = append(out, id)
		}
	}
	return out
}

// agentSessionRename sets a session's title, and tells the backend so Claude
// Code's own session name matches. Answers 204 for the chat view's inline
// edit; a form post (no fetch) falls back to a redirect.
func (h *handlers) agentSessionRename(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id := r.PathValue("id")
	title := strings.TrimSpace(r.FormValue("title"))
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	if _, err := h.agentHub.Rename(r.Context(), p.ID, id, title); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/account/sessions/"+id, http.StatusSeeOther)
}

// validImages accepts the pictures attached to a message: a handful of
// png/jpeg/webp/gif images, base64-encoded, small enough to relay (the browser
// downsizes before sending; this is the backstop).
func validImages(in []agentsession.ImageIn) ([]agentsession.ImageIn, bool) {
	if len(in) == 0 {
		return nil, true
	}
	if len(in) > 8 {
		return nil, false
	}
	total := 0
	out := make([]agentsession.ImageIn, 0, len(in))
	for _, im := range in {
		switch im.MediaType {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
		default:
			return nil, false
		}
		if im.Data == "" || len(im.Data) > 4<<20 {
			return nil, false
		}
		if _, err := base64.StdEncoding.DecodeString(im.Data); err != nil {
			return nil, false
		}
		total += len(im.Data)
		if total > 7<<20 {
			return nil, false
		}
		out = append(out, agentsession.ImageIn{MediaType: im.MediaType, Data: im.Data})
	}
	return out, true
}

// keepalive pings the peer until done closes or a ping fails, closing the
// connection on failure so the read loop ends and the hub drops the entry.
// Without it a peer that vanished without closing (a sleeping laptop, a tab
// killed behind a proxy) stays attached — and a browser would keep counting
// as looking at its session, silencing every alert about it.
func keepalive(c *websocket.Conn, done <-chan struct{}) {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := c.Ping(ctx)
			cancel()
			if err != nil {
				c.CloseNow()
				return
			}
		}
	}
}
