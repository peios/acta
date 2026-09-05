package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// cmdHarness runs the Acta harness: it connects out to Acta over a websocket,
// announces this machine and the sessions it holds, and spawns/drives Claude
// Code processes on demand. Acta never runs Claude — the harness holds the
// credentials and this machine's shell. See ACT-36.
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
		out:     make(chan []byte, 256),
		procs:   map[string]*claudeProc{},
		pending: map[string][]byte{},
		stateP:  harnessStatePath(),
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

// sessionState is what the harness must remember to resume a session it
// spawned earlier: where it ran and how. The id is the map key (and Claude's
// own session id, so `--resume <id>` finds the transcript).
type sessionState struct {
	Backend string          `json:"backend"`
	Cwd     string          `json:"cwd"`
	Options json.RawMessage `json:"options,omitempty"`
	// Conversation is Claude's own id for the transcript when it differs from
	// the session id: /clear starts a fresh conversation, and the frames that
	// follow carry its session_id (the transcript file's name, which is not
	// the reset's new_conversation_id). A later resume must continue that
	// one, not the transcript from before the clear.
	Conversation string `json:"conversation,omitempty"`
}

type harness struct {
	base, token, label string
	stateP             string

	mu       sync.Mutex
	sessions map[string]sessionState // sessions this harness has spawned, by id
	procs    map[string]*claudeProc
	pending  map[string][]byte // last message per session, until the process answers

	conn *websocket.Conn
	out  chan []byte

	tails map[string]chan struct{} // session/task_id -> closed when the background task ends

	forgotten map[string]bool // sessions Acta deleted: nothing more is sent about them
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
		"t": "hello", "label": h.label, "backends": []string{"claude-code"},
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
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
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
			T       string          `json:"t"`
			Session string          `json:"session"`
			Backend string          `json:"backend"`
			Cwd     string          `json:"cwd"`
			Options json.RawMessage `json:"options"`
			Text    string          `json:"text"`
			Images  []harnessImage  `json:"images"`
			Payload json.RawMessage `json:"payload"`
			ID      string          `json:"id"`
			Path    string          `json:"path"`
		}
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		switch f.T {
		case "ls":
			go h.listDirs(f.ID, f.Path)
		case "spawn":
			h.spawn(ctx, f.Session, f.Backend, f.Cwd, f.Options)
		case "input":
			h.input(f.Session, f.Text, f.Images)
		case "stop":
			h.stopSession(f.Session)
		case "forget":
			h.forget(f.Session)
		case "control":
			h.control(f.Session, f.Payload)
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

// --- claude code adapter ---

type claudeProc struct {
	session string
	cmd     *exec.Cmd
	stdin   *bufio.Writer
	stdinMu sync.Mutex
}

func (h *harness) spawn(ctx context.Context, session, backend, cwd string, options json.RawMessage) {
	if backend == "" {
		backend = "claude-code"
	}
	if backend != "claude-code" {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": "unsupported backend " + backend})
		return
	}
	st := sessionState{Backend: backend, Cwd: cwd, Options: options}
	h.mu.Lock()
	h.sessions[session] = st
	h.mu.Unlock()
	h.saveState()
	h.startProc(session, st, false)
}

// startProc launches Claude Code for a session. resume=false starts a fresh
// session under Acta's id (--session-id); resume=true continues the transcript
// Claude already has for that id (--resume). Either way the process is the
// same shape: stream-json in and out, kept alive by an open stdin.
func (h *harness) startProc(session string, st sessionState, resume bool) bool {
	h.mu.Lock()
	if _, running := h.procs[session]; running {
		h.mu.Unlock()
		return true
	}
	h.mu.Unlock()

	var opts struct {
		PermissionMode string `json:"permission_mode"`
		Model          string `json:"model"`
		Effort         string `json:"effort"`
	}
	_ = json.Unmarshal(st.Options, &opts)

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--replay-user-messages",
		// Route permission prompts over the stream as control_request frames,
		// so Acta can show them as a modal and answer with a control_response.
		"--permission-prompt-tool", "stdio",
		// Stream text as it is written (stream_event frames) so the browser can
		// show a reply growing instead of waiting for the whole message.
		"--include-partial-messages",
		// Fast mode is refused in SDK/print mode unless the flag settings opt
		// in; with this it becomes a per-session /fast toggle (still subject to
		// the account's own availability, reported in init.fast_mode_state).
		"--settings", `{"fastMode":true}`,
	}
	if resume {
		if st.Conversation != "" {
			args = append(args, "--resume", st.Conversation)
		} else {
			args = append(args, "--resume", session)
		}
	} else {
		args = append(args, "--session-id", session)
	}
	if opts.PermissionMode != "" {
		args = append(args, "--permission-mode", opts.PermissionMode)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}

	cmd := exec.Command("claude", args...)
	cmd.Dir = expandHome(st.Cwd)
	// File checkpointing is off by default outside the TUI; with it on, a
	// rewind can restore the files a turn changed.
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=1")
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return false
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return false
	}
	// Keep stderr visible, but also remember the tail: a resume of a session
	// that never had a turn fails there with "No conversation found".
	var errTail strings.Builder
	cmd.Stderr = io.MultiWriter(os.Stderr, &limitedWriter{w: &errTail, n: 4 << 10})

	if err := cmd.Start(); err != nil {
		h.send(map[string]any{"t": "spawn_error", "session": session, "error": err.Error()})
		return false
	}

	proc := &claudeProc{session: session, cmd: cmd, stdin: bufio.NewWriter(stdinPipe)}
	h.mu.Lock()
	h.procs[session] = proc
	h.mu.Unlock()
	h.send(map[string]any{"t": "spawned", "session": session, "resumed": resume, "styles": outputStyles(st.Cwd)})

	// stdout reader: each line is one stream-json message; forward verbatim,
	// tagged with its Claude "type" as the frame kind.
	go func() {
		sc := bufio.NewScanner(stdoutPipe)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			line := sc.Bytes()
			if len(strings.TrimSpace(string(line))) == 0 || h.isForgotten(session) {
				continue
			}
			kind := messageType(line)
			// Only a real answer proves the message landed: a resume that finds
			// no conversation still emits an empty result before dying.
			if kind == "assistant" {
				h.mu.Lock()
				delete(h.pending, session)
				h.mu.Unlock()
			}
			h.noteBackgroundTask(session, kind, line)
			if kind == "system" {
				h.noteConversationID(session, line)
			}
			payload := make([]byte, len(line))
			copy(payload, line)
			h.send(map[string]any{"t": "event", "session": session, "kind": kind, "payload": json.RawMessage(payload)})
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
		// Claude Code writes a session's conversation only once it has taken a
		// turn, so resuming one that was spawned but never used fails outright.
		// Start it fresh under the same id instead of leaving it unusable.
		if resume && code != 0 && strings.Contains(errTail.String(), "No conversation found") {
			h.send(map[string]any{"t": "event", "session": session, "kind": "state",
				"payload": json.RawMessage(`{"state":"resume_failed","reason":"no stored conversation; starting fresh under the same id"}`)})
			if h.startProc(session, st, false) {
				h.mu.Lock()
				msg, ok := h.pending[session]
				fresh := h.procs[session]
				h.mu.Unlock()
				if ok && fresh != nil {
					h.writeStdin(fresh, msg)
				}
				return
			}
		}
		h.send(map[string]any{"t": "exit", "session": session, "code": code})
	}()
	return true
}

// --- background shell output ---
//
// A Bash call run in the background answers at once with the task id and the
// file its output is written to; Claude only reads that file when it asks.
// The harness tails it meanwhile so the browser can watch the command run:
// live chunks are relayed (not stored), and when the task ends one frame
// with the whole output (tail-capped) is sent for the transcript.

var bgTaskRe = regexp.MustCompile(`Command running in background with ID: (\S+)\. Output is being written to: (\S+?)\.?(?:\s|$)`)

const (
	bgLiveCap = 256 << 10 // bytes relayed live per task
	bgKeepCap = 128 << 10 // bytes kept in the stored final frame
)

func (h *harness) noteBackgroundTask(session, kind string, line []byte) {
	switch kind {
	case "user":
		if !bytes.Contains(line, []byte("Command running in background with ID")) {
			return
		}
		m := bgTaskRe.FindSubmatch(line)
		if m == nil {
			return
		}
		taskID, path := string(m[1]), string(m[2])
		done := make(chan struct{})
		h.mu.Lock()
		if h.tails == nil {
			h.tails = map[string]chan struct{}{}
		}
		if _, dup := h.tails[session+"/"+taskID]; dup {
			h.mu.Unlock()
			return
		}
		h.tails[session+"/"+taskID] = done
		h.mu.Unlock()
		go h.tailTask(session, taskID, path, done)
	case "system":
		var m struct {
			Subtype string `json:"subtype"`
			TaskID  string `json:"task_id"`
			Status  string `json:"status"`
			Patch   struct {
				Status string `json:"status"`
			} `json:"patch"`
		}
		if json.Unmarshal(line, &m) != nil || m.TaskID == "" {
			return
		}
		ended := m.Subtype == "task_notification" || (m.Subtype == "task_updated" && m.Patch.Status != "" && m.Patch.Status != "running")
		if !ended {
			return
		}
		h.mu.Lock()
		done := h.tails[session+"/"+m.TaskID]
		delete(h.tails, session+"/"+m.TaskID)
		h.mu.Unlock()
		if done != nil {
			close(done)
		}
	}
}

func (h *harness) tailTask(session, taskID, path string, done <-chan struct{}) {
	var offset int64
	var sent int
	send := func(text string, final bool) {
		msg := map[string]any{"task_id": taskID, "text": text}
		if final {
			msg["done"] = true
		}
		payload, _ := json.Marshal(msg)
		h.send(map[string]any{"t": "event", "session": session, "kind": "task_output", "payload": json.RawMessage(payload)})
	}
	read := func() {
		if sent >= bgLiveCap {
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
		b, _ := io.ReadAll(io.LimitReader(f, int64(bgLiveCap-sent)))
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
			if len(full) > bgKeepCap {
				full = append([]byte("… (earlier output dropped)\n"), full[len(full)-bgKeepCap:]...)
			}
			send(string(full), true)
			return
		case <-deadline:
			send("", true)
			return
		}
	}
}

// outputStyles lists the output styles a session in cwd can use: the
// built-ins plus any custom ones in the user's and the project's
// .claude/output-styles directories (name and description from the front
// matter, else the file name).
func outputStyles(cwd string) []map[string]string {
	out := []map[string]string{
		{"name": "default", "description": "Claude Code's normal engineering voice", "source": "built-in"},
		{"name": "Explanatory", "description": "adds short asides on the reasoning and trade-offs", "source": "built-in"},
		{"name": "Learning", "description": "hands small pieces of the work back to you to write", "source": "built-in"},
	}
	home, _ := os.UserHomeDir()
	for _, d := range []struct{ dir, source string }{{filepath.Join(home, ".claude", "output-styles"), "user"}, {filepath.Join(cwd, ".claude", "output-styles"), "project"}} {
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

func messageType(line []byte) string {
	var m struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &m) == nil && m.Type != "" {
		return m.Type
	}
	return "event"
}

// harnessImage is a picture attached to a message, relayed as-is into the
// backend's image content block.
type harnessImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// userContent builds the message content: plain text when there are no
// pictures (what every consumer of the transcript expects), else the block
// array with the images first and the text after.
func userContent(text string, images []harnessImage) any {
	if len(images) == 0 {
		return text
	}
	blocks := make([]map[string]any, 0, len(images)+1)
	for _, im := range images {
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": im.MediaType, "data": im.Data}})
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	return blocks
}

func (h *harness) input(session, text string, images []harnessImage) {
	// "/effort <level>" is a Claude Code local command that only lasts the
	// process; remember it so a resume relaunches with --effort.
	if lvl := effortCommand(text); lvl != "" {
		h.rememberOption(session, "effort", lvl)
	}
	h.mu.Lock()
	proc := h.procs[session]
	st, known := h.sessions[session]
	h.mu.Unlock()
	if proc == nil {
		// No process: resume it if this harness spawned it before, otherwise say
		// so in the transcript rather than dropping the message silently.
		if !known {
			h.send(map[string]any{"t": "event", "session": session, "kind": "state",
				"payload": map[string]any{"state": "undelivered", "reason": "this harness has no record of the session"}})
			return
		}
		if !h.startProc(session, st, true) {
			return
		}
		h.mu.Lock()
		proc = h.procs[session]
		h.mu.Unlock()
		if proc == nil {
			return
		}
	}
	msg, _ := json.Marshal(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": userContent(text, images)},
		"parent_tool_use_id": nil,
	})
	// Remember it until the process has clearly taken it: if this spawn turns
	// out to be a resume of a session with no stored conversation, the process
	// dies immediately and the message must go to its replacement.
	h.mu.Lock()
	h.pending[session] = msg
	h.mu.Unlock()
	h.writeStdin(proc, msg)
}

// writeStdin sends one already-marshalled stream-json line to a process.
func (h *harness) writeStdin(proc *claudeProc, msg []byte) {
	proc.stdinMu.Lock()
	defer proc.stdinMu.Unlock()
	_, _ = proc.stdin.Write(append(msg, '\n'))
	_ = proc.stdin.Flush()
}

// control writes a control-protocol message to the session's process verbatim.
// A set_permission_mode or set_model request is also folded into the
// remembered options so a later resume starts in the chosen mode / on the
// chosen model; a control_response for a session with no process is
// meaningless and dropped.
func (h *harness) control(session string, payload json.RawMessage) {
	var probe struct {
		Type    string `json:"type"`
		Request struct {
			Subtype string `json:"subtype"`
			Mode    string `json:"mode"`
			Model   string `json:"model"`
		} `json:"request"`
	}
	_ = json.Unmarshal(payload, &probe)
	var key, val string
	switch {
	case probe.Type != "control_request":
	case probe.Request.Subtype == "set_permission_mode" && probe.Request.Mode != "":
		key, val = "permission_mode", probe.Request.Mode
	case probe.Request.Subtype == "set_model" && probe.Request.Model != "":
		key, val = "model", probe.Request.Model
	}
	if key != "" {
		h.rememberOption(session, key, val)
	}
	h.mu.Lock()
	proc := h.procs[session]
	h.mu.Unlock()
	if proc == nil {
		return
	}
	proc.stdinMu.Lock()
	defer proc.stdinMu.Unlock()
	_, _ = proc.stdin.Write(append(append([]byte{}, payload...), '\n'))
	_ = proc.stdin.Flush()
}

// rememberOption folds a per-session choice (permission mode, model, effort)
// into the state file so a later resume starts with it.
func (h *harness) rememberOption(session, key, val string) {
	h.mu.Lock()
	st, ok := h.sessions[session]
	if ok {
		var opts map[string]any
		_ = json.Unmarshal(st.Options, &opts)
		if opts == nil {
			opts = map[string]any{}
		}
		if val == "default" && key == "model" {
			delete(opts, key)
		} else {
			opts[key] = val
		}
		st.Options, _ = json.Marshal(opts)
		h.sessions[session] = st
	}
	h.mu.Unlock()
	if ok {
		h.saveState()
	}
}

// effortCommand returns the level named by a "/effort <level>" message, or "".
func effortCommand(text string) string {
	f := strings.Fields(strings.TrimSpace(text))
	if len(f) != 2 || f[0] != "/effort" {
		return ""
	}
	switch f[1] {
	case "low", "medium", "high", "xhigh", "max":
		return f[1]
	}
	return ""
}

func (h *harness) stopSession(session string) {
	h.mu.Lock()
	proc := h.procs[session]
	h.mu.Unlock()
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	// An interrupt control_request ends the current turn but keeps the process
	// (and its warm context) alive, so the next message needs no resume. SIGINT
	// is the fallback if stdin is gone; it ends the turn but exits the process.
	if proc.stdin != nil {
		line := fmt.Sprintf(`{"type":"control_request","request_id":"interrupt-%d","request":{"subtype":"interrupt"}}`+"\n", time.Now().UnixMilli())
		proc.stdinMu.Lock()
		_, err := proc.stdin.WriteString(line)
		if err == nil {
			err = proc.stdin.Flush()
		}
		proc.stdinMu.Unlock()
		if err == nil {
			return
		}
	}
	_ = proc.cmd.Process.Signal(syscall.SIGINT)
}

// forget drops a session Acta has deleted: its process is killed and its
// record removed, so a later hello no longer offers it and nothing more is
// reported about it. Claude Code's own transcript on disk stays.
func (h *harness) forget(session string) {
	h.mu.Lock()
	proc := h.procs[session]
	delete(h.sessions, session)
	delete(h.pending, session)
	if h.forgotten == nil {
		h.forgotten = map[string]bool{}
	}
	h.forgotten[session] = true
	h.mu.Unlock()
	h.saveState()
	if proc != nil && proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}
}

func (h *harness) isForgotten(session string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.forgotten[session]
}

// --- local state ---

func harnessStatePath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "acta", "harness-sessions.json")
}

func loadHarnessState(path string) map[string]sessionState {
	out := map[string]sessionState{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if json.Unmarshal(data, &out) == nil {
		return out
	}
	// Older files held a bare list of ids; keep them resumable with unknown cwd.
	var ids []string
	if json.Unmarshal(data, &ids) == nil {
		for _, s := range ids {
			out[s] = sessionState{Backend: "claude-code"}
		}
	}
	return out
}

// noteConversationID records the session id Claude reports in an init frame
// when it is not the one this session was spawned under: after a /clear the
// process moves to a fresh transcript, and a resume must name that one.
func (h *harness) noteConversationID(session string, line []byte) {
	var m struct {
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(line, &m) != nil || m.Subtype != "init" || m.SessionID == "" || m.SessionID == session {
		return
	}
	h.mu.Lock()
	st, ok := h.sessions[session]
	changed := ok && st.Conversation != m.SessionID
	if changed {
		st.Conversation = m.SessionID
		h.sessions[session] = st
	}
	h.mu.Unlock()
	if changed {
		h.saveState()
	}
}

func (h *harness) saveState() {
	h.mu.Lock()
	snapshot := make(map[string]sessionState, len(h.sessions))
	for k, v := range h.sessions {
		snapshot[k] = v
	}
	h.mu.Unlock()
	data, _ := json.MarshalIndent(snapshot, "", "  ")
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
