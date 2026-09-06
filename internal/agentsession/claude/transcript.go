package claude

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/peios/acta/internal/agentsession/model"
)

// Claude Code keeps its own record of every conversation on the host, at
// ~/.claude/projects/<cwd slug>/<session id>.jsonl: one JSON record per line,
// the messages chained by uuid/parentUuid (a rewind starts a new branch; a
// compaction starts a new root whose logicalParentUuid points back). The
// assistant and user records there wrap the same API message objects, with
// the same uuids, as the stream-json frames Acta stores from a live process.
//
// That record is the source of truth for what was said: a session Acta
// started can be continued in a terminal and brought back, and a
// conversation that never touched Acta can be imported. Both are the same
// operation — read the transcript from the last message Acta holds (or from
// the start), keep the live branch, store the records verbatim under the
// "transcript" kind — and the projector turns those records into the same
// events as live frames, synthesising the turn boundaries the stream carries
// as init/result frames. See ACT-38.

// TranscriptKind labels a stored frame that is a record read off the
// transcript on the host rather than a line the process wrote to Acta.
const TranscriptKind = "transcript"

// TranscriptGlob is where the transcript of a session lives on the host, as
// a glob: the cwd slug in the path is Claude Code's own encoding, so the
// harness finds the file by id instead of Acta guessing the directory. After
// a /clear the process moved to a fresh transcript, which the session's
// options name.
func TranscriptGlob(sessionID string, options map[string]any) string {
	id := sessionID
	if options != nil {
		if c, _ := options["conversation"].(string); strings.TrimSpace(c) != "" {
			id = strings.TrimSpace(c)
		}
	}
	return "~/.claude/projects/*/" + id + ".jsonl"
}

// Leaf is the transcript uuid a stored frame carries, when it is one of the
// message records the transcript also holds (an assistant message, a user
// message or tool result, or an imported record). The last of these across a
// session's frames is where a catch-up read starts.
func Leaf(kind string, payload json.RawMessage) string {
	switch kind {
	case "assistant", "user", TranscriptKind:
	default:
		return ""
	}
	var m struct {
		Type        string `json:"type"`
		UUID        string `json:"uuid"`
		IsSidechain bool   `json:"isSidechain"`
	}
	if json.Unmarshal(payload, &m) != nil || m.IsSidechain {
		return ""
	}
	if m.Type != "assistant" && m.Type != "user" {
		return ""
	}
	return m.UUID
}

// TranscriptRecord is a line of the transcript worth storing, with the
// moment it was written.
type TranscriptRecord struct {
	Payload json.RawMessage
	At      time.Time
}

// ChainRecords picks, from lines read off a transcript, the records to
// store: the message chain that ends at the last record written (the live
// branch — what a rewind abandoned is dropped), followed back through
// parentUuid and, across a compaction, logicalParentUuid, until a parent the
// lines do not hold (the record Acta already has, for a catch-up). Only the
// user, assistant and system records are kept, in file order; the bookkeeping
// records around them (queue operations, titles, snapshots) are not
// conversation. Leading system records, and trailing ones down to the
// duration that closes the last turn, are trimmed so that a read that finds
// nothing new stores nothing.
func ChainRecords(lines []json.RawMessage) []TranscriptRecord {
	type rec struct {
		raw               json.RawMessage
		typ, uuid, parent string
		logical           string
		at                time.Time
		idx               int
	}
	var recs []*rec
	byID := map[string]*rec{}
	for i, raw := range lines {
		var m struct {
			Type        string `json:"type"`
			UUID        string `json:"uuid"`
			Parent      string `json:"parentUuid"`
			Logical     string `json:"logicalParentUuid"`
			Timestamp   string `json:"timestamp"`
			IsSidechain bool   `json:"isSidechain"`
		}
		if json.Unmarshal(raw, &m) != nil || m.UUID == "" || m.IsSidechain {
			continue
		}
		r := &rec{raw: raw, typ: m.Type, uuid: m.UUID, parent: m.Parent, logical: m.Logical, idx: i}
		if m.Timestamp != "" {
			r.at, _ = time.Parse(time.RFC3339Nano, m.Timestamp)
		}
		recs = append(recs, r)
		byID[m.UUID] = r
	}
	if len(recs) == 0 {
		return nil
	}
	live := map[string]bool{}
	for cur := recs[len(recs)-1]; cur != nil && !live[cur.uuid]; {
		live[cur.uuid] = true
		next := byID[cur.parent]
		if next == nil && cur.logical != "" {
			next = byID[cur.logical]
		}
		cur = next
	}
	var out []TranscriptRecord
	for _, r := range recs {
		if !live[r.uuid] {
			continue
		}
		switch r.typ {
		case "user", "assistant", "system":
		default:
			continue
		}
		out = append(out, TranscriptRecord{Payload: r.raw, At: r.at})
	}
	isMsg := func(r TranscriptRecord) bool {
		var m struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(r.Payload, &m)
		return m.Type != "system"
	}
	subtype := func(r TranscriptRecord) string {
		var m struct {
			Subtype string `json:"subtype"`
		}
		_ = json.Unmarshal(r.Payload, &m)
		return m.Subtype
	}
	for len(out) > 0 && !isMsg(out[0]) {
		out = out[1:]
	}
	// trailing system records go, but the one that closes the last turn
	// (its duration) stays, so the turn ends on the page
	for len(out) > 0 && !isMsg(out[len(out)-1]) && subtype(out[len(out)-1]) != "turn_duration" {
		out = out[:len(out)-1]
	}
	return out
}

// --- the picker's listing ---

// Transcript is one conversation on the host, as the import picker shows it.
type Transcript struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Cwd     string    `json:"cwd"`
	Title   string    `json:"title"`   // the name Claude Code shows in its own picker, if any
	First   string    `json:"first"`   // the first thing the user said
	Started time.Time `json:"started"` // the first record
	Updated time.Time `json:"updated"` // the last record
	Size    int64     `json:"size"`
	Version string    `json:"version"`         // the Claude Code version that wrote it
	Mode    string    `json:"permission_mode"` // the permission mode it last ran in
}

const (
	scanHead = 256 << 10 // bytes read from the start of each file
	scanTail = 64 << 10  // bytes read from the end
)

// ScanTranscripts lists the conversations Claude Code keeps under home, most
// recently written first. It reads only the head and tail of each file (the
// first prompt and the directory come early; the title, mode and last
// timestamp are rewritten near the end), so a machine with hundreds of
// transcripts answers in well under a second. Conversations with no user
// prompt in their head (an aborted start) are left out.
func ScanTranscripts(home string) []Transcript {
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	var out []Transcript
	for _, path := range files {
		if strings.HasPrefix(filepath.Base(path), "agent-") {
			continue // a subagent's sidechain, not a conversation of its own
		}
		if t, ok := scanTranscript(path); ok {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

func scanTranscript(path string) (Transcript, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return Transcript{}, false
	}
	t := Transcript{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path, Size: st.Size(), Updated: st.ModTime()}
	head := make([]byte, min64(st.Size(), scanHead))
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	var tail []byte
	if st.Size() > scanHead {
		start := st.Size() - scanTail
		if start < scanHead {
			start = scanHead
		}
		tail = make([]byte, st.Size()-start)
		if _, err := f.ReadAt(tail, start); err != nil && err != io.EOF {
			tail = nil
		}
		// the tail starts mid-line: drop the fragment
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		} else {
			tail = nil
		}
	}
	// the head ends mid-line unless it is the whole file
	if int64(len(head)) < st.Size() {
		if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
	}
	lines := bytes.Split(head, []byte("\n"))
	if len(tail) > 0 {
		lines = append(lines, bytes.Split(tail, []byte("\n"))...)
	}
	var aiTitle, customTitle, firstCmd string
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch str(m, "type") {
		case "custom-title":
			customTitle = str(m, "customTitle")
		case "ai-title":
			aiTitle = str(m, "aiTitle")
		case "permission-mode":
			t.Mode = str(m, "permissionMode")
		}
		if ts := str(m, "timestamp"); ts != "" {
			if at, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				if t.Started.IsZero() || at.Before(t.Started) {
					t.Started = at
				}
				if at.After(t.Updated) || t.Updated.Equal(st.ModTime()) {
					t.Updated = at
				}
			}
		}
		if t.Cwd == "" && str(m, "cwd") != "" {
			t.Cwd = str(m, "cwd")
		}
		if t.Version == "" && str(m, "version") != "" {
			t.Version = str(m, "version")
		}
		if t.First == "" && str(m, "type") == "user" && !boolean(m, "isMeta") && !boolean(m, "isCompactSummary") && !boolean(m, "isSidechain") {
			text, _ := echoContent(sub(m, "message")["content"])
			text = promptText(text)
			switch {
			case text == "":
			case strings.HasPrefix(text, "/"):
				// a command opens many sessions (/clear, a skill); the first
				// thing said in words names it better, if there is one
				if firstCmd == "" {
					firstCmd = text
				}
			default:
				t.First = clipRunes(text, 200)
			}
		}
	}
	if t.First == "" {
		t.First = clipRunes(firstCmd, 200)
	}
	if t.First == "" {
		return Transcript{}, false
	}
	// a title set by hand also lives beside the transcript, whole
	if b, err := os.ReadFile(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "custom-title.json")); err == nil {
		var m struct {
			CustomTitle string `json:"customTitle"`
		}
		if json.Unmarshal(b, &m) == nil && m.CustomTitle != "" {
			customTitle = m.CustomTitle
		}
	}
	t.Title = firstStr(customTitle, aiTitle)
	if t.Started.IsZero() {
		t.Started = t.Updated
	}
	return t, true
}

var commandArgsRe = regexp.MustCompile(`<command-args>([\s\S]*?)</command-args>`)

// promptText is what the user typed, from a transcript user record: a local
// command's expansion (<command-name>…) collapses back to the "/command
// args" that was typed; hidden system text is dropped.
func promptText(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "<command-name>") || strings.HasPrefix(t, "<command-message>") {
		name := ""
		if cn := cmdNameRe.FindStringSubmatch(t); cn != nil {
			name = cn[1]
		}
		if name == "" {
			return ""
		}
		args := ""
		if am := commandArgsRe.FindStringSubmatch(t); am != nil {
			args = strings.TrimSpace(am[1])
		}
		if args != "" {
			return "/" + name + " " + args
		}
		return "/" + name
	}
	if strings.HasPrefix(t, "<local-command-") || strings.HasPrefix(t, "<system-reminder>") {
		return ""
	}
	return t
}

func clipRunes(s string, n int) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// --- projection ---

// transcript projects one record read off the host's transcript. The
// message records take the same path as live frames (they are the same
// objects); the boundaries a live stream marks with init and result frames
// are synthesised here: a user prompt opens a turn, a turn_duration record
// (or the next prompt, or the end of the read) closes it.
func (p *Projector) transcript(f model.Frame) []model.Event {
	m := obj(f.Payload)
	switch str(m, "type") {
	case "assistant":
		return p.assistant(f)
	case "user":
		if boolean(m, "isMeta") {
			return []model.Event{model.FoldTo(f, "", "meta")}
		}
		msg := sub(m, "message")
		content := msg["content"]
		if s, ok := content.(string); ok {
			if boolean(m, "isCompactSummary") {
				return p.user(f)
			}
			return p.transcriptPrompt(f, s, nil)
		}
		blocks := arr(msg, "content")
		results := false
		for _, b := range blocks {
			if bm, _ := b.(map[string]any); str(bm, "type") == "tool_result" {
				results = true
			}
		}
		if !results {
			text, images := echoContent(content)
			return p.transcriptPrompt(f, text, images)
		}
		// the stream spells the tool's structured result in snake case
		if tur, ok := m["toolUseResult"]; ok {
			m2 := cloneMap(m)
			m2["tool_use_result"] = tur
			return p.synth(f, "user", m2, p.user)
		}
		return p.user(f)
	case "system":
		switch st := str(m, "subtype"); st {
		case "compact_boundary":
			md := sub(m, "compactMetadata")
			s := map[string]any{"type": "system", "subtype": "compact_boundary", "compact_metadata": map[string]any{
				"pre_tokens": num(md, "preTokens"), "post_tokens": num(md, "postTokens"), "duration_ms": num(md, "durationMs"), "trigger": str(md, "trigger"),
			}}
			return p.synth(f, "system", s, p.system)
		case "local_command":
			// a local command's output: the stream replays it as a user message
			s := map[string]any{"type": "user", "isReplay": true, "message": map[string]any{"role": "user", "content": str(m, "content")}}
			return p.synth(f, "user", s, p.user)
		case "turn_duration":
			if !p.importTurn {
				return []model.Event{model.FoldTo(f, "", "turn duration")}
			}
			return p.transcriptResult(f, int64(num(m, "durationMs")), true)
		case "model_refusal_fallback", "api_error":
			e := model.New(model.SessionState, f).Set("text", firstStr(str(m, "content"), strings.ReplaceAll(st, "_", " ")))
			if st == "api_error" {
				e.Set("error", true)
			}
			return []model.Event{e}
		default:
			return []model.Event{model.FoldTo(f, "", firstStr(st, "system"))}
		}
	}
	return []model.Event{model.FoldTo(f, "", firstStr(str(m, "type"), "record"))}
}

// transcriptPrompt is a user prompt from the transcript: the turn before it
// closes (a transcript from before turn_duration records has no other end),
// and it opens the next as an input would.
func (p *Projector) transcriptPrompt(f model.Frame, text string, images []any) []model.Event {
	var out []model.Event
	if p.importTurn {
		out = append(out, p.transcriptResult(f, 0, false)...)
	}
	if strings.HasPrefix(strings.TrimSpace(text), "<task-notification>") {
		// not a prompt: the note that a background agent finished, which the
		// live stream carries as an echo and which ends the agent's lane
		m := obj(f.Payload)
		echo := map[string]any{"type": "user", "isReplay": true, "uuid": str(m, "uuid"), "message": map[string]any{"role": "user", "content": text}}
		return append(out, p.synth(f, "user", echo, p.user)...)
	}
	text = promptText(text)
	if text == "" && len(images) == 0 {
		return append(out, model.FoldTo(f, "", "prompt"))
	}
	in := map[string]any{"text": text}
	if len(images) > 0 {
		in["images"] = images
	}
	// a failure to start the process lands on the message Acta was asked
	// to deliver, which is never a prompt read off the transcript
	keep := p.lastInput
	evs := p.synth(f, "input", in, p.input)
	p.lastInput = keep
	p.importTurn = true
	for i := range evs {
		if evs[i].T == model.Input {
			evs[i].Set("transcript", true)
		}
	}
	out = append(out, evs...)
	if !strings.HasPrefix(text, "/") {
		// a live process echoes the message back (--replay-user-messages),
		// which is what settles the bubble and names it for a rewind; the
		// record on disk is that echo, so the same follows the input here
		m := obj(f.Payload)
		echo := map[string]any{"type": "user", "isReplay": true, "uuid": str(m, "uuid"), "message": map[string]any{"role": "user", "content": sub(m, "message")["content"]}}
		for _, e := range p.synth(f, "user", echo, p.user) {
			e.Raw = nil
			out = append(out, e)
		}
	}
	return out
}

// transcriptResult closes the turn a transcript prompt opened, as the
// stream's result frame would. withRaw says the frame at hand is this
// event's own record (a turn_duration); otherwise the record belongs to the
// event that follows and the result carries no raw of its own.
func (p *Projector) transcriptResult(f model.Frame, durationMs int64, withRaw bool) []model.Event {
	if !p.importTurn {
		return nil
	}
	p.importTurn = false
	turns := 1
	if len(p.cmds) > 0 {
		turns = 0 // a local command's empty turn
	}
	r := map[string]any{"type": "result", "subtype": "success", "num_turns": turns, "is_error": false}
	if durationMs > 0 {
		r["duration_ms"] = durationMs
	}
	evs := p.synth(f, "result", r, p.result)
	if !withRaw {
		for i := range evs {
			evs[i].Raw = nil
		}
	}
	return evs
}

// synth runs a handler over a frame Acta composed in the stream's shape from
// a transcript record, then puts the record itself in the events' raw
// panels, so what is shown as evidence is what was on disk.
func (p *Projector) synth(f model.Frame, kind string, payload map[string]any, fn func(model.Frame) []model.Event) []model.Event {
	raw, _ := json.Marshal(payload)
	sf := model.Frame{Seq: f.Seq, Kind: kind, Payload: raw, At: f.At, Stored: f.Stored}
	evs := fn(sf)
	for i := range evs {
		for j := range evs[i].Raw {
			if evs[i].Raw[j].Kind == kind && string(evs[i].Raw[j].Payload) == string(raw) {
				evs[i].Raw[j].Kind = f.Kind
				evs[i].Raw[j].Payload = f.Payload
			}
		}
	}
	return evs
}

// transcriptState is the divider a catch-up or import puts before the
// records it stores: where they came from and how many.
func (p *Projector) transcriptState(f model.Frame, m map[string]any) []model.Event {
	e := model.NewLabelled(model.SessionCatchup, f, str(m, "state")).Set("source", str(m, "state")).Set("count", int(num(m, "count"))).Set("from", str(m, "from")).Set("to", str(m, "to"))
	e.Ref = ref("catchup", f)
	return []model.Event{e}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
