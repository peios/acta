package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/peios/acta/internal/agentsession/claude"
	"github.com/peios/acta/internal/agentsession/codex"
)

// cmdHarness runs the Acta harness: it connects out to Acta over a websocket,
// announces this machine and the sessions it holds, and supervises backend
// processes on demand. The harness knows nothing about any backend: Acta
// composes the command to run and every line written to it, and the harness
// pipes bytes both ways, tails files it is asked to tail, and reports exits.
// Acta never runs an agent itself — the harness holds the credentials and
// this machine's shell. See ACT-36 and ACT-37.
func cmdHarness(args []string) error {
	cfg := loadConfig()
	token := os.Getenv("ACTA_TOKEN")
	if token == "" {
		token = cfg.Token
	}
	base := os.Getenv("ACTA_URL")
	if base == "" {
		base = cfg.URL
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	if token == "" {
		return fmt.Errorf("not logged in — run `acta login <host>` (or set ACTA_TOKEN)")
	}

	h := &harness{
		base:  strings.TrimRight(base, "/"),
		token: token,
		label: hostLabel(),
		// Outbound frames outlive a connection: what a running process says
		// while the server is away queues here (up to the buffer) and is
		// flushed on reconnect instead of being lost with the socket.
		out:    make(chan []byte, 256),
		procs:  map[string]*proc{},
		tails:  map[string]chan struct{}{},
		stateP: harnessStatePath(),
	}
	h.sessions = loadHarnessState(h.stateP)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "acta harness: %s → %s\n", h.label, h.base)
	for {
		if err := h.connectAndServe(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "acta harness: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second): // reconnect
		}
	}
}

// protocolVersion is what the hello announces. Acta composes every process
// and every stdin line from version 2 on; the harness keeps no per-backend
// knowledge and no per-session state beyond which sessions it holds. From
// version 3 the harness reads and scans a backend's transcripts on request.
const protocolVersion = 3

type harness struct {
	base, token, label string
	stateP             string

	mu       sync.Mutex
	sessions map[string]bool // sessions this harness has run, by id (offered as resumable on hello)
	procs    map[string]*proc
	tails    map[string]chan struct{} // session/task id -> closed when the tail should stop

	conn *websocket.Conn
	out  chan []byte

	forgotten map[string]bool // sessions Acta deleted: nothing more is sent about them
}

type proc struct {
	session string
	cmd     *exec.Cmd
	stdin   *bufio.Writer
	stdinMu sync.Mutex
}

func hostLabel() string {
	host, _ := os.Hostname()
	u := os.Getenv("USER")
	if u == "" {
		u = "user"
	}
	if host == "" {
		return u
	}
	return u + "@" + host
}

// backendsAvailable lists the backends whose command is on this host's PATH.
// Acta decides how each backend runs; the harness only says which commands
// it could start.
func backendsAvailable() []string {
	var out []string
	for _, b := range []struct{ name, bin string }{{"claude-code", "claude"}, {"codex", "codex"}} {
		if _, err := exec.LookPath(b.bin); err == nil {
			out = append(out, b.name)
		}
	}
	return out
}

func (h *harness) connectAndServe(ctx context.Context) error {
	wsURL := strings.Replace(h.base, "http", "ws", 1) + "/api/v1/harness/ws"
	// The dial gets its own deadline: a proxy that accepts the TCP connection
	// while the server behind it is restarting can otherwise hold the
	// upgrade request open indefinitely.
	dctx, cancelDial := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + h.token}},
	})
	cancelDial()
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	conn.SetReadLimit(8 << 20)
	defer conn.CloseNow()

	h.conn = conn

	// hello
	h.mu.Lock()
	sessions := make([]string, 0, len(h.sessions))
	for s := range h.sessions {
		sessions = append(sessions, s)
	}
	running := make([]string, 0, len(h.procs))
	for s := range h.procs {
		running = append(running, s)
	}
	h.mu.Unlock()
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	hello, _ := json.Marshal(map[string]any{
		"t": "hello", "v": protocolVersion, "label": h.label, "backends": backendsAvailable(),
		"sessions": sessions, "running": running, "cwd": cwd, "home": home,
	})
	if err := conn.Write(ctx, websocket.MessageText, hello); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "acta harness: connected (%d known session(s))\n", len(sessions))

	// write pump; readDone ends it when the read side goes, so a dead
	// connection is left behind instead of waited on forever
	writerDone := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-readDone:
				return
			case b, ok := <-h.out:
				if !ok {
					return
				}
				if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
					return
				}
			}
		}
	}()

	// keepalive: a server that went away without closing the socket (a
	// restart behind a proxy, a laptop that slept) leaves the read blocked
	// for good; a failed ping closes the connection so the read fails and the
	// reconnect loop takes over.
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-writerDone:
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Ping(pctx)
				cancel()
				if err != nil {
					fmt.Fprintf(os.Stderr, "acta harness: keepalive failed: %v\n", err)
					conn.CloseNow()
					return
				}
			}
		}
	}()

	// read pump
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var f struct {
			T       string            `json:"t"`
			Session string            `json:"session"`
			Cwd     string            `json:"cwd"`
			Cmd     string            `json:"cmd"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Resume  bool              `json:"resume"`
			Styles  bool              `json:"styles"`
			Line    string            `json:"line"`
			ID      string            `json:"id"`
			Path    string            `json:"path"`
			Backend string            `json:"backend"`
			Key     string            `json:"key"`
			After   string            `json:"after"`
			Hold    bool              `json:"hold"`
			Catchup *readReq          `json:"catchup"`
		}
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.T {
		case "ls":
			go h.listDirs(f.ID, f.Path)
		case "scan":
			go h.scan(f.ID, f.Backend)
		case "read":
			go h.read(f.Session, f.ID, readReq{Path: f.Path, Key: f.Key, After: f.After}, f.Hold)
		case "spawn":
			h.spawn(f.Session, f.Cwd, f.Cmd, f.Args, f.Env, f.Resume, f.Styles, f.Catchup)
		case "write":
			h.write(f.Session, f.Line)
		case "stop":
			h.stopSession(f.Session)
		case "forget":
			h.forget(f.Session)
		case "tail":
			h.tail(f.Session, f.ID, f.Path)
		case "untail":
			h.untail(f.Session, f.ID)
		}
	}
	close(readDone)
	<-writerDone
	return fmt.Errorf("connection lost, reconnecting")
}

// listDirs answers a working-directory completion request from Acta: the
// directories under the typed path's parent whose names start with its last
// segment (~ expanded; hidden ones only when the prefix asks for them).
// Directories only, capped, so a wide home directory stays a short list.
func (h *harness) listDirs(reqID, typed string) {
	reply := map[string]any{"t": "ls_result", "id": reqID, "path": typed}
	defer func() { h.send(reply) }()
	home, _ := os.UserHomeDir()
	p := strings.TrimSpace(typed)
	if p == "" {
		p, _ = os.Getwd()
		p += "/"
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = home + p[1:]
	}
	dir, prefix := p, ""
	if !strings.HasSuffix(p, "/") {
		dir, prefix = filepath.Dir(p), filepath.Base(p)
	}
	if st, err := os.Stat(strings.TrimSuffix(p, "/")); err == nil && st.IsDir() && prefix == "" {
		reply["exists"] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		reply["error"] = err.Error()
		return
	}
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if !isDir {
			continue
		}
		dirs = append(dirs, filepath.Join(dir, name))
		if len(dirs) >= 40 {
			break
		}
	}
	// A typed path that is itself a directory also offers what is inside it,
	// so landing on a directory and pressing on works like a shell.
	if prefix != "" && len(dirs) < 40 {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			reply["exists"] = true
			if kids, err := os.ReadDir(p); err == nil {
				for _, e := range kids {
					if strings.HasPrefix(e.Name(), ".") || !e.IsDir() {
						continue
					}
					dirs = append(dirs, filepath.Join(p, e.Name()))
					if len(dirs) >= 40 {
						break
					}
				}
			}
		}
	}
	reply["dirs"] = dirs
}

func (h *harness) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case h.out <- b:
	default:
	}
}

// sendWait queues a frame that must not be dropped (a transcript's records:
// a gap breaks the chain), waiting for room; it gives up only when the
// outbound queue has been stuck for a long time (the server is gone).
func (h *harness) sendWait(v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case h.out <- b:
		return true
	case <-time.After(60 * time.Second):
		return false
	}
}

// --- transcripts ---
//
// A backend keeps its own record of each conversation on this host. Acta
// asks for it in two ways: a read (before a resume, or for an import) sends
// a JSONL file's lines up verbatim, all of them or those after a named
// line; a scan lists the transcripts a backend has here, for the import
// picker. What the records mean is Acta's business; how a backend lays its
// files out on this host is the harness's, like the output styles above.

// readReq names a transcript read: the file (a glob, ~ allowed), the field
// that identifies a line, and the value of the last line already held.
type readReq struct {
	Path  string `json:"path"`
	Key   string `json:"key"`
	After string `json:"after"`
}

const (
	readFileCap  = 128 << 20 // bytes of transcript read at most
	readBatchCap = 768 << 10 // bytes of lines per records frame
)

// read sends the lines of a transcript up in batches, then read_done. With
// After set, only the lines after the last one whose Key field equals it
// are sent, and none when no line does (found: false). hold records the
// session as one this harness can resume.
func (h *harness) read(session, id string, req readReq, hold bool) {
	if hold {
		h.mu.Lock()
		h.sessions[session] = true
		delete(h.forgotten, session)
		h.mu.Unlock()
		h.saveState()
	}
	done := map[string]any{"t": "read_done", "session": session, "id": id, "count": 0, "found": false}
	defer func() { h.sendWait(done) }()
	path := expandHome(req.Path)
	matches, _ := filepath.Glob(path)
	if len(matches) == 0 {
		if _, err := os.Stat(path); err != nil {
			return
		}
		matches = []string{path}
	}
	// the same id under two project directories is not expected; the
	// newest wins if it happens
	var newest string
	var newestAt time.Time
	for _, m := range matches {
		if st, err := os.Stat(m); err == nil && (newest == "" || st.ModTime().After(newestAt)) {
			newest, newestAt = m, st.ModTime()
		}
	}
	if newest == "" {
		return
	}
	st, err := os.Stat(newest)
	if err != nil {
		return
	}
	if st.Size() > readFileCap {
		done["error"] = fmt.Sprintf("transcript is %d MB, more than the %d MB limit", st.Size()>>20, readFileCap>>20)
		return
	}
	f, err := os.Open(newest)
	if err != nil {
		done["error"] = err.Error()
		return
	}
	defer f.Close()
	key := req.Key
	if key == "" {
		key = "uuid"
	}
	needle := []byte(`"` + req.After + `"`)
	var pending [][]byte // the lines since the last match (all lines when nothing is to be matched)
	found := req.After == ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if req.After != "" && strings.Contains(string(line), string(needle)) {
			if fieldString(line, key) == req.After {
				found = true
				pending = pending[:0]
				continue
			}
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		pending = append(pending, cp)
	}
	if err := sc.Err(); err != nil {
		done["error"] = err.Error()
		return
	}
	if !found {
		return
	}
	done["found"] = true
	count := 0
	var batch []json.RawMessage
	size := 0
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		ok := h.sendWait(map[string]any{"t": "records", "session": session, "id": id, "lines": batch})
		batch, size = nil, 0
		return ok
	}
	for _, line := range pending {
		if h.isForgotten(session) {
			done["error"] = "session deleted during the read"
			return
		}
		if size+len(line) > readBatchCap && !flush() {
			done["error"] = "connection stalled during the read"
			return
		}
		batch = append(batch, json.RawMessage(line))
		size += len(line)
		count++
	}
	if !flush() {
		done["error"] = "connection stalled during the read"
		return
	}
	done["count"] = count
}

// fieldString reads a string field of a JSON line by a dotted path
// ("payload.item.id"); "" when absent or not a string.
func fieldString(line []byte, path string) string {
	var cur json.RawMessage = line
	for _, k := range strings.Split(path, ".") {
		var m map[string]json.RawMessage
		if json.Unmarshal(cur, &m) != nil {
			return ""
		}
		next, ok := m[k]
		if !ok {
			return ""
		}
		cur = next
	}
	var v string
	if json.Unmarshal(cur, &v) != nil {
		return ""
	}
	return v
}

// scan lists the transcripts a backend keeps on this host.
func (h *harness) scan(reqID, backend string) {
	reply := map[string]any{"t": "scan_result", "id": reqID, "backend": backend}
	home, _ := os.UserHomeDir()
	switch backend {
	case "claude-code":
		items := claude.ScanTranscripts(home)
		if items == nil {
			items = []claude.Transcript{}
		}
		reply["items"] = items
	case "codex":
		items := codex.ScanTranscripts(home)
		if items == nil {
			items = []codex.Transcript{}
		}
		reply["items"] = items
	default:
		reply["error"] = "no transcripts known for backend " + backend
	}
	h.send(reply)
}

// --- processes ---

// spawn runs the process Acta composed for a session, in cwd, and streams
// its stdout lines back verbatim. Every line is one frame; Acta labels it.
func (h *harness) spawn(session, cwd, command string, args []string, env map[string]string, resume, styles bool, catchup *readReq) {
	h.mu.Lock()
	if _, running := h.procs[session]; running {
		h.mu.Unlock()
		return
	}
	h.sessions[session] = true
	delete(h.forgotten, session)
	h.mu.Unlock()
	h.saveState()

	// What the backend's own transcript holds beyond Acta's copy (turns taken
	// in a terminal) goes up first, so it lands ahead of the new process.
	if catchup != nil {
		h.read(session, "catchup", *catchup, false)
	}

	if command == "" {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": "no command to run"})
		return
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = expandHome(cwd)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return
	}
	// Keep stderr visible, but also remember the tail: Acta reads it on exit
	// (a resume of a session that never had a turn fails there).
	var errTail strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &limitedWriter{w: &errTail, n: 4 << 10})

	if err := cmd.Start(); err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return
	}

	p := &proc{session: session, cmd: cmd, stdin: bufio.NewWriter(stdinPipe)}
	h.mu.Lock()
	h.procs[session] = p
	h.mu.Unlock()
	spawned := map[string]any{"t": "spawned", "session": session, "resumed": resume}
	if styles {
		spawned["styles"] = outputStyles(cwd)
	}
	h.send(spawned)

	go func() {
		sc := bufio.NewScanner(stdoutPipe)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if len(strings.TrimSpace(string(line))) == 0 || h.isForgotten(session) {
				continue
			}
			if !json.Valid(line) {
				// a stray non-JSON line (a warning printed to stdout): keep it
				// as a note so nothing said is lost
				note, _ := json.Marshal(map[string]any{"state": "stdout", "text": string(line)})
				h.send(map[string]any{"t": "event", "session": session, "kind": "state", "payload": json.RawMessage(note)})
				continue
			}
			payload := make([]byte, len(line))
			copy(payload, line)
			h.send(map[string]any{"t": "event", "session": session, "payload": json.RawMessage(payload)})
		}
		err := cmd.Wait()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		h.mu.Lock()
		delete(h.procs, session)
		h.mu.Unlock()
		if h.isForgotten(session) {
			return // Acta deleted it; the exit is expected and unreported
		}
		h.send(map[string]any{"t": "exit", "session": session, "code": code, "stderr": errTail.String()})
	}()
}

// write sends one composed line to a session's process. A line for a session
// with no process is dropped: Acta spawns before it writes.
func (h *harness) write(session, line string) {
	h.mu.Lock()
	p := h.procs[session]
	h.mu.Unlock()
	if p == nil || line == "" {
		return
	}
	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()
	_, _ = p.stdin.WriteString(line)
	_ = p.stdin.WriteByte('\n')
	_ = p.stdin.Flush()
}

// stopSession interrupts a session's process. Acta prefers the backend's own
// interrupt line (written like any other); this is the fallback.
func (h *harness) stopSession(session string) {
	h.mu.Lock()
	p := h.procs[session]
	h.mu.Unlock()
	if p == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGINT)
}

// forget drops a session Acta has deleted: its process is killed and its
// record removed, so a later hello no longer offers it and nothing more is
// reported about it. The backend's own transcript on disk stays.
func (h *harness) forget(session string) {
	h.mu.Lock()
	p := h.procs[session]
	delete(h.sessions, session)
	if h.forgotten == nil {
		h.forgotten = map[string]bool{}
	}
	h.forgotten[session] = true
	for k, done := range h.tails {
		if strings.HasPrefix(k, session+"/") {
			close(done)
			delete(h.tails, k)
		}
	}
	h.mu.Unlock()
	h.saveState()
	if p != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (h *harness) isForgotten(session string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.forgotten[session]
}

// --- tailing files ---
//
// A backend that runs a command in the background writes its output to a
// file and only reads it back when asked. Acta recognises that and asks the
// harness to tail the file meanwhile, so the browser can watch the command
// run: live chunks are relayed as it grows, and when Acta says the task ended
// one frame with the whole output (tail-capped) is sent for the transcript.

const (
	tailLiveCap = 256 << 10 // bytes relayed live per task
	tailKeepCap = 128 << 10 // bytes kept in the final frame
)

func (h *harness) tail(session, id, path string) {
	if id == "" || path == "" {
		return
	}
	done := make(chan struct{})
	h.mu.Lock()
	if _, dup := h.tails[session+"/"+id]; dup {
		h.mu.Unlock()
		return
	}
	h.tails[session+"/"+id] = done
	h.mu.Unlock()
	go h.tailFile(session, id, path, done)
}

func (h *harness) untail(session, id string) {
	h.mu.Lock()
	done := h.tails[session+"/"+id]
	delete(h.tails, session+"/"+id)
	h.mu.Unlock()
	if done != nil {
		close(done)
	}
}

func (h *harness) tailFile(session, id, path string, done <-chan struct{}) {
	var offset int64
	var sent int
	send := func(text string, final bool) {
		msg := map[string]any{"task_id": id, "text": text}
		if final {
			msg["done"] = true
		}
		payload, _ := json.Marshal(msg)
		h.send(map[string]any{"t": "event", "session": session, "kind": "task_output", "payload": json.RawMessage(payload)})
	}
	read := func() {
		if sent >= tailLiveCap {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return
		}
		b, _ := io.ReadAll(io.LimitReader(f, int64(tailLiveCap-sent)))
		if len(b) == 0 {
			return
		}
		offset += int64(len(b))
		sent += len(b)
		send(string(b), false)
	}
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	deadline := time.After(2 * time.Hour)
	for {
		select {
		case <-t.C:
			read()
		case <-done:
			read()
			full, _ := os.ReadFile(path)
			if len(full) > tailKeepCap {
				full = append([]byte("… (earlier output dropped)\n"), full[len(full)-tailKeepCap:]...)
			}
			send(string(full), true)
			return
		case <-deadline:
			send("", true)
			h.untail(session, id)
			return
		}
	}
}

// outputStyles lists the output styles a session in cwd can use: the
// built-ins plus any custom ones in the user's and the project's
// .claude/output-styles directories (name and description from the front
// matter, else the file name). Reported when a spawn asks for it: the one
// piece of local Claude Code knowledge the harness keeps, because it is a
// question about this host's filesystem.
func outputStyles(cwd string) []map[string]string {
	out := []map[string]string{
		{"name": "default", "description": "Claude Code's normal engineering voice", "source": "built-in"},
		{"name": "Explanatory", "description": "adds short asides on the reasoning and trade-offs", "source": "built-in"},
		{"name": "Learning", "description": "hands small pieces of the work back to you to write", "source": "built-in"},
	}
	home, _ := os.UserHomeDir()
	for _, d := range []struct{ dir, source string }{{filepath.Join(home, ".claude", "output-styles"), "user"}, {filepath.Join(expandHome(cwd), ".claude", "output-styles"), "project"}} {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			desc := ""
			if b, err := os.ReadFile(filepath.Join(d.dir, e.Name())); err == nil {
				s := string(b)
				if strings.HasPrefix(s, "---") {
					if end := strings.Index(s[3:], "\n---"); end >= 0 {
						for _, l := range strings.Split(s[3:3+end], "\n") {
							k, v, ok := strings.Cut(l, ":")
							if !ok {
								continue
							}
							v = strings.Trim(strings.TrimSpace(v), `"'`)
							switch strings.TrimSpace(k) {
							case "name":
								if v != "" {
									name = v
								}
							case "description":
								desc = v
							}
						}
					}
				}
			}
			out = append(out, map[string]string{"name": name, "description": desc, "source": d.source})
		}
	}
	return out
}

// limitedWriter keeps at most n bytes, so a chatty process cannot grow the
// remembered stderr without bound.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		b := p
		if len(b) > l.n {
			b = b[:l.n]
		}
		_, _ = l.w.Write(b)
		l.n -= len(b)
	}
	return len(p), nil
}

// --- local state ---
//
// The only thing the harness remembers across runs is which sessions it has
// run, so the next hello can offer them as resumable. Everything a resume
// needs (working directory, options, the backend's conversation id) is
// Acta's, and comes down with the spawn.

func harnessStatePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "acta", "harness-sessions.json")
}

type harnessState struct {
	V        int      `json:"v"`
	Sessions []string `json:"sessions"`
}

func loadHarnessState(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var st harnessState
	if json.Unmarshal(data, &st) == nil && st.V >= 2 {
		for _, s := range st.Sessions {
			out[s] = true
		}
		return out
	}
	// Earlier files: a map of id -> per-session details (which Acta now
	// owns), or before that a bare list of ids. Keep the ids.
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) == nil {
		for s := range m {
			out[s] = true
		}
		return out
	}
	var ids []string
	if json.Unmarshal(data, &ids) == nil {
		for _, s := range ids {
			out[s] = true
		}
	}
	return out
}

func (h *harness) saveState() {
	h.mu.Lock()
	st := harnessState{V: protocolVersion, Sessions: make([]string, 0, len(h.sessions))}
	for s := range h.sessions {
		st.Sessions = append(st.Sessions, s)
	}
	h.mu.Unlock()
	data, _ := json.MarshalIndent(st, "", "  ")
	_ = os.MkdirAll(filepath.Dir(h.stateP), 0o700)
	_ = os.WriteFile(h.stateP, data, 0o600)
}

func expandHome(p string) string {
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
