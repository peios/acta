package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
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

// knownBackends are the backends Acta has drivers for, as the new-session
// form offers them.
var knownBackends = func() []string { b := agentsession.Backends(); sort.Strings(b); return b }()

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
	Frames  int  // stored frames
	Bytes   int64
	Status  string // from the title's marker (see agentsession.SplitStatus)
	Bare    string // the title without it
}

// Size words the stored size of a session's transcript.
func (r agentSessionRow) Size() string { return fmtBytes(r.Bytes) }

// fmtBytes words a byte count the way a file manager would.
func fmtBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
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
	sizes, err := h.agentSessions.Sizes(r.Context(), p.ID)
	if err != nil {
		return chrome{}, nil, nil, err
	}
	// what is under way first, then what is waiting, then the rest, then
	// what is finished; most recently touched first within each
	sort.SliceStable(list, func(i, j int) bool {
		si, _ := agentsession.SplitStatus(list[i].Title)
		sj, _ := agentsession.SplitStatus(list[j].Title)
		return statusRank(si) < statusRank(sj)
	})
	rows := make([]agentSessionRow, 0, len(list))
	nav := make([]agentSessionNav, 0, len(list))
	for _, as := range list {
		sz := sizes[as.ID]
		status, bare := agentsession.SplitStatus(as.Title)
		rows = append(rows, agentSessionRow{AgentSession: as, Live: liveIDs[as.ID], Running: runIDs[as.ID], Frames: sz.Frames, Bytes: sz.Bytes, Status: status, Bare: bare})
		nav = append(nav, agentSessionNav{ID: as.ID, Title: sessionLabel(as), Status: status, Backend: as.Backend, Live: liveIDs[as.ID], Running: runIDs[as.ID]})
	}
	ch.AgentMode = true
	ch.AgentSessions = nav
	ch.ActiveSession = activeID
	return ch, rows, presences, nil
}

// statusRank orders sessions by status: in progress, to do, none, done.
func statusRank(status string) int {
	switch status {
	case agentsession.StatusInProgress:
		return 0
	case agentsession.StatusTodo:
		return 1
	case agentsession.StatusDone:
		return 3
	}
	return 2
}

// sessionLabel is what a session is called in lists: its title, or a
// backend-plus-directory fallback for untitled ones.
func sessionLabel(as store.AgentSession) string {
	if _, bare := agentsession.SplitStatus(as.Title); bare != "" {
		return bare
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

// transcriptItem is one conversation a harness found on its host, as the
// import picker lists it: the backend's own listing plus whether Acta
// already holds a session under that id.
type transcriptItem struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Cwd     string    `json:"cwd"`
	Title   string    `json:"title"`
	First   string    `json:"first"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	Size    int64     `json:"size"`
	Mode    string    `json:"permission_mode"`
	Model   string    `json:"model"`
	Held    bool      `json:"held"`
}

// scanTranscripts asks a harness for a backend's transcripts and marks the
// ones this principal already has a session for.
func (h *handlers) scanTranscripts(ctx context.Context, ownerID, harnessID, backend string) ([]transcriptItem, error) {
	raw, err := h.agentHub.ScanTranscripts(ctx, ownerID, harnessID, backend)
	if err != nil {
		return nil, err
	}
	var items []transcriptItem
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	}
	have := map[string]bool{}
	if list, err := h.agentSessions.List(ctx, ownerID); err == nil {
		for _, as := range list {
			have[as.ID] = true
		}
	}
	for i := range items {
		items[i].Held = have[items[i].ID]
	}
	return items, nil
}

// agentHarnessTranscripts lists the transcripts a backend keeps on a
// harness's host, for the import picker.
func (h *handlers) agentHarnessTranscripts(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	backend := strings.TrimSpace(r.URL.Query().Get("backend"))
	if backend == "" {
		backend = "claude-code"
	}
	w.Header().Set("Content-Type", "application/json")
	if !knownBackend(backend) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []transcriptItem{}, "error": "unknown backend"})
		return
	}
	items, err := h.scanTranscripts(r.Context(), p.ID, r.PathValue("id"), backend)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []transcriptItem{}, "error": err.Error()})
		return
	}
	if items == nil {
		items = []transcriptItem{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
}

var transcriptIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// agentSessionImport records a session for each chosen transcript, under
// the transcript's own id, and has the harness read it in. The harness is
// asked again which transcripts it has, so the file read is one it listed,
// never a path the browser named.
func (h *handlers) agentSessionImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	harnessID := strings.TrimSpace(r.PostFormValue("harness"))
	backend := strings.TrimSpace(r.PostFormValue("backend"))
	if backend == "" {
		backend = "claude-code"
	}
	if !knownBackend(backend) {
		http.Redirect(w, r, "/account/sessions?err=bad_backend", http.StatusSeeOther)
		return
	}
	want := map[string]bool{}
	for _, id := range r.PostForm["transcript"] {
		if transcriptIDRe.MatchString(id) {
			want[id] = true
		}
	}
	if len(want) == 0 {
		http.Redirect(w, r, "/account/sessions", http.StatusSeeOther)
		return
	}
	items, err := h.scanTranscripts(r.Context(), p.ID, harnessID, backend)
	if errors.Is(err, agentsession.ErrHarnessTooOld) {
		http.Redirect(w, r, "/account/sessions?err=old_harness", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/account/sessions?err=no_harness", http.StatusSeeOther)
		return
	}
	var created []string
	for _, it := range items {
		if !want[it.ID] || it.Held {
			continue
		}
		options := map[string]any{"permission_mode": "default"}
		switch backend {
		case "claude-code":
			switch it.Mode {
			case "default", "acceptEdits", "plan", "bypassPermissions", "auto":
				options["permission_mode"] = it.Mode
			}
		case "codex":
			// the thread is resumed by its own id; the mode is Acta's default
			options["permission_mode"] = "auto"
			options["conversation"] = it.ID
			if it.Model != "" {
				options["model"] = it.Model
			}
		}
		title := it.Title
		if title == "" {
			title = clipTitle(it.First, 80)
		}
		as, err := h.agentSessions.CreateWithID(r.Context(), it.ID, p.ID, backend, it.Cwd, title, options)
		if err != nil {
			continue
		}
		if err := h.agentHub.Import(p.ID, harnessID, as); err != nil {
			_, _ = h.agentSessions.Append(r.Context(), as.ID, "state",
				json.RawMessage(`{"state":"import_failed","reason":"no harness connected"}`))
		}
		created = append(created, as.ID)
	}
	if len(created) == 1 {
		http.Redirect(w, r, "/account/sessions/"+created[0], http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/account/sessions", http.StatusSeeOther)
}

// pageTurns is how many turns the session page opens with; chunkTurns how
// many each scroll fetch adds. Turns are the unit of reading, but an
// autonomous session runs a hundred frames a turn, so a frame budget bounds
// what the browser has to build at once (see agentsession.Tail).
const (
	pageTurns   = 40
	chunkTurns  = 20
	pageFrames  = 300
	chunkFrames = 150
)

// agentSessionEvents returns a window of a session's projected events:
// ?before=<seq> for the turns ending before that event, ?after=<seq> for
// the turns starting after it, ?tail=1 for the last ones. The projection
// runs from the start regardless (an event's shape depends on what came
// before), only the window crosses the wire.
func (h *handlers) agentSessionEvents(w http.ResponseWriter, r *http.Request) {
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
	all, last, err := h.agentHub.History(r.Context(), as, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	turns := chunkTurns
	if n, err := strconv.Atoi(q.Get("turns")); err == nil && n > 0 && n <= 200 {
		turns = n
	}
	var win agentsession.Window
	switch {
	case q.Get("before") != "":
		seq, _ := strconv.ParseInt(q.Get("before"), 10, 64)
		win = agentsession.Before(all, seq, turns, chunkFrames)
	case q.Get("after") != "":
		seq, _ := strconv.ParseInt(q.Get("after"), 10, 64)
		win = agentsession.After(all, seq, turns, chunkFrames)
	default:
		win = agentsession.Tail(all, pageTurns, pageFrames)
	}
	evs := win.Events
	if evs == nil {
		evs = []model.Event{}
	}
	for i := range evs {
		evs[i] = evs[i].Wire()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"events": evs, "more": win.More, "last_seq": last, "lanes": agentsession.Lanes(all, win.Events)})
}

// agentSessionFrames returns stored frames by seq (?seq=1,2,3), verbatim,
// for the raw panels: the page carries events without their payloads.
func (h *handlers) agentSessionFrames(w http.ResponseWriter, r *http.Request) {
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
	type frame struct {
		Seq     int64           `json:"seq"`
		Kind    string          `json:"kind"`
		At      time.Time       `json:"at"`
		Payload json.RawMessage `json:"payload"`
	}
	out := []frame{}
	seen := map[int64]bool{}
	for _, part := range strings.Split(r.URL.Query().Get("seq"), ",") {
		seq, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || seq <= 0 || seen[seq] || len(out) >= 200 {
			continue
		}
		seen[seq] = true
		evs, err := h.agentSessions.Events(r.Context(), as.ID, seq-1, 1)
		if err != nil || len(evs) == 0 || evs[0].Seq != seq {
			continue
		}
		out = append(out, frame{Seq: evs[0].Seq, Kind: evs[0].Kind, At: evs[0].CreatedAt, Payload: json.RawMessage(evs[0].Payload)})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_ = json.NewEncoder(w).Encode(map[string]any{"frames": out})
}

// clipTitle keeps the first line of s, at most n runes.
func clipTitle(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func agentSessionErr(code string) string {
	switch code {
	case "no_harness":
		return "No harness is connected. Run `acta harness` on the machine you want to work on."
	case "bad_backend":
		return "Unknown backend."
	case "old_harness":
		return "That harness runs an older acta. Update it and restart `acta harness`."
	case "running":
		return "The session is running. Stop it before re-reading its transcript."
	case "nothing":
		return "Nothing was chosen to prune."
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

	// a session started here is in progress until its owner says otherwise
	as, err := h.agentSessions.Create(r.Context(), p.ID, backend, cwd, agentsession.WithDefaultStatus(title), options)
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

// agentSessionStorage answers the session's storage dialog: how much the
// transcript holds before compression, what each prune category would save,
// and which harness a re-read would use.
func (h *handlers) agentSessionStorage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id := r.PathValue("id")
	ests, total, err := h.agentSessions.PruneEstimates(r.Context(), id, p.ID)
	if errors.Is(err, agentsession.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	label, running, held := h.agentHub.Holder(p.ID, id)
	if !held {
		label = h.agentHub.AnyHarness(p.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"bytes": total, "categories": ests, "harness": label, "held": held, "running": running})
}

// agentSessionPrune rewrites the transcript with the chosen categories
// taken out, then sends the reader back to the page, whose size shows it.
func (h *handlers) agentSessionPrune(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	id := r.PathValue("id")
	cats := map[string]bool{}
	for _, c := range r.PostForm["cat"] {
		for _, known := range model.PruneCategories {
			if c == known {
				cats[c] = true
			}
		}
	}
	_, err := h.agentSessions.Prune(r.Context(), id, p.ID, cats)
	switch {
	case errors.Is(err, agentsession.ErrNothingToPrune):
		http.Redirect(w, r, "/account/sessions/"+id+"?err=nothing", http.StatusSeeOther)
		return
	case errors.Is(err, agentsession.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.agentHub.Invalidate(id)
	http.Redirect(w, r, "/account/sessions/"+id, http.StatusSeeOther)
}

// agentSessionReimport replaces the stored transcript with a fresh read of
// the backend's own record on the harness.
func (h *handlers) agentSessionReimport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p := principalFrom(r.Context())
	id := r.PathValue("id")
	as, err := h.agentSessions.Get(r.Context(), id, p.ID)
	if errors.Is(err, agentsession.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	err = h.agentHub.Reimport(r.Context(), p.ID, as, r.PostFormValue("full") == "1")
	switch {
	case errors.Is(err, agentsession.ErrNoHarness):
		http.Redirect(w, r, "/account/sessions/"+id+"?err=no_harness", http.StatusSeeOther)
	case errors.Is(err, agentsession.ErrHarnessTooOld):
		http.Redirect(w, r, "/account/sessions/"+id+"?err=old_harness", http.StatusSeeOther)
	case errors.Is(err, agentsession.ErrSessionRunning):
		http.Redirect(w, r, "/account/sessions/"+id+"?err=running", http.StatusSeeOther)
	case err != nil:
		http.Error(w, "internal error", http.StatusInternalServerError)
	default:
		http.Redirect(w, r, "/account/sessions/"+id, http.StatusSeeOther)
	}
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
	LanesJSON  template.JS // the lanes those events belong to (see agentsession.Lanes)
	LastSeq    int64
	Earlier    bool // turns before the ones in the page exist
	Live       bool // held by a connected harness
	Running    bool // process running right now
	Frames     int  // stored frames
	Bytes      int64
	Status     string // from the title's marker
	Bare       string // the title without it
	Err        string
}

// Size words the stored size of the session's transcript.
func (d agentSessionData) Size() string { return fmtBytes(d.Bytes) }

// StatusOptions lists the statuses the header's picker offers.
func (agentSessionData) StatusOptions() []statusOption { return statusOptions() }

type statusOption struct{ Value, Label string }

func statusOptions() []statusOption {
	out := []statusOption{{"", agentsession.StatusLabel("")}}
	for _, s := range agentsession.Statuses {
		out = append(out, statusOption{s, agentsession.StatusLabel(s)})
	}
	return out
}

// StatusOptions lists the statuses a row's picker offers.
func (agentSessionRow) StatusOptions() []statusOption { return statusOptions() }

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
	all, last, err := h.agentHub.History(r.Context(), as, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// the page opens on the last turns; earlier ones arrive as the reader
	// scrolls up (agentSessionEvents)
	win := agentsession.Tail(all, pageTurns, pageFrames)
	evs := win.Events
	if evs == nil {
		evs = []model.Event{}
	}
	for i := range evs {
		evs[i] = evs[i].Wire()
	}
	// json.Marshal escapes <, > and & so the array is safe inside a script
	// element; template.JS keeps html/template from escaping it again.
	ej, err := json.Marshal(evs)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// the lanes the window's events belong to, for the tabs of subagents
	// that started before it
	lj, _ := json.Marshal(agentsession.Lanes(all, win.Events))
	live, running := false, false
	var frames int
	var bytes int64
	for _, row := range rows {
		if row.ID == as.ID {
			live, running = row.Live, row.Running
			frames, bytes = row.Frames, row.Bytes
		}
	}
	status, bare := agentsession.SplitStatus(as.Title)
	render(w, http.StatusOK, "agent_session.html", agentSessionData{
		chrome:     ch,
		Principal:  p,
		Session:    as,
		EventsJSON: template.JS(ej),
		LanesJSON:  template.JS(lj),
		LastSeq:    last,
		Earlier:    win.More,
		Live:       live,
		Running:    running,
		Frames:     frames,
		Bytes:      bytes,
		Status:     status,
		Bare:       bare,
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
			b, _ := json.Marshal(e.Wire())
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
			// A browser operation: must be a small JSON object naming an op.
			if len(in.Payload) > 0 && len(in.Payload) < 64<<10 {
				var op agentsession.BrowserOp
				if json.Unmarshal(in.Payload, &op) == nil && op.Op != "" {
					h.agentHub.Control(context.Background(), p.ID, as.ID, op, in.Payload)
				}
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
