package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/peios/acta/internal/agentsession/model"
)

// Codex keeps its own record of every thread on the host, at
// ~/.codex/sessions/<yyyy>/<mm>/<dd>/rollout-<stamp>-<thread id>.jsonl: one
// JSON record per line ({timestamp, ordinal, type, payload}), linear. The
// event_msg records mirror what the app-server tells a client — a turn
// starting and completing, an item completing — but the items are spelled
// in Codex's core shape (UserMessage with content blocks, CommandExecution
// with a command array, snake_case fields) rather than the app-server's v2
// shape (userMessage, commandExecution, camelCase) the projector renders.
// So a record read off the transcript is stored verbatim under the
// "transcript" kind and projected by translating it into the notification
// the app-server would have sent, which then takes the ordinary path. See
// ACT-38; the Claude Code counterpart is in the claude package.

// TranscriptKind labels a stored frame read off the transcript on the host.
const TranscriptKind = "transcript"

// TranscriptGlob is where a thread's rollout lives on the host, as a glob:
// the date directories and the stamp in the name are Codex's, the thread id
// (the session's conversation option; the session id itself for one
// imported under the thread's id) is what finds it.
func TranscriptGlob(sessionID string, options map[string]any) string {
	id := sessionID
	if c := opt(options, "conversation"); c != "" {
		id = c
	}
	return "~/.codex/sessions/*/*/*/rollout-*-" + id + ".jsonl"
}

// LeafKey is the field of a transcript line that names it for a catch-up:
// the completed item's id, which Acta also holds on the live item/completed
// notification.
const LeafKey = "payload.item.id"

// Leaf is the item id a stored frame carries, when it is a completed item
// (live or read off the transcript); the last across a session's frames is
// where a catch-up read starts.
func Leaf(kind string, payload json.RawMessage) string {
	switch kind {
	case "item/completed":
		var m struct {
			Params struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"params"`
		}
		_ = json.Unmarshal(payload, &m)
		return m.Params.Item.ID
	case TranscriptKind:
		var m struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"payload"`
		}
		if json.Unmarshal(payload, &m) == nil && m.Type == "event_msg" && m.Payload.Type == "item_completed" {
			return m.Payload.Item.ID
		}
	}
	return ""
}

// TranscriptRecord is a line of the transcript worth storing, with the
// moment it was written.
type TranscriptRecord struct {
	Payload json.RawMessage
	At      time.Time
}

// ChainRecords picks, from lines read off a rollout, the records to store:
// the event_msg records that are conversation (turns, items, token counts,
// settings) and the turn contexts that name the model, in order. The raw
// model exchange (response_item), the compaction's replacement history and
// the bookkeeping around them are not kept. Rollouts written before items
// were recorded carry user_message/agent_message events instead; those are
// kept only when no item records exist, so nothing renders twice. Leading
// bookkeeping is trimmed, and trailing bookkeeping down to the record that
// closes the last turn, so a catch-up that finds nothing new stores nothing.
func ChainRecords(lines []json.RawMessage) []TranscriptRecord {
	type rec struct {
		raw   json.RawMessage
		typ   string // record type
		ev    string // event_msg payload type
		at    time.Time
		isMsg bool
	}
	var recs []rec
	items := false
	for _, raw := range lines {
		var m struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		r := rec{raw: raw, typ: m.Type, ev: m.Payload.Type}
		if m.Timestamp != "" {
			r.at, _ = time.Parse(time.RFC3339Nano, m.Timestamp)
		}
		switch m.Type {
		case "event_msg":
			switch m.Payload.Type {
			case "item_completed":
				items = true
				r.isMsg = true
			case "user_message", "agent_message":
				r.isMsg = true
			case "task_started", "task_complete", "turn_aborted", "token_count", "thread_settings_applied", "thread_rolled_back":
			default:
				continue
			}
		case "turn_context":
		default:
			continue
		}
		recs = append(recs, r)
	}
	var kept []rec
	for _, r := range recs {
		if items && (r.ev == "user_message" || r.ev == "agent_message") {
			continue
		}
		kept = append(kept, r)
	}
	for len(kept) > 0 && !kept[0].isMsg {
		kept = kept[1:]
	}
	// trailing bookkeeping goes, but the record that closes the last turn
	// (task_complete, turn_aborted) stays, so the turn ends on the page
	for len(kept) > 0 && !kept[len(kept)-1].isMsg {
		if ev := kept[len(kept)-1].ev; ev == "task_complete" || ev == "turn_aborted" {
			break
		}
		kept = kept[:len(kept)-1]
	}
	out := make([]TranscriptRecord, 0, len(kept))
	for _, r := range kept {
		out = append(out, TranscriptRecord{Payload: r.raw, At: r.at})
	}
	return out
}

// --- the picker's listing ---

// Transcript is one thread on the host, as the import picker shows it.
type Transcript struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Cwd     string    `json:"cwd"`
	Title   string    `json:"title"` // the thread's name in Codex's own picker, if any
	First   string    `json:"first"` // the first thing the user said
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	Size    int64     `json:"size"`
	Version string    `json:"version"` // the Codex version that wrote it
	Model   string    `json:"model"`   // the model it last ran
}

const (
	scanHead = 256 << 10
	scanTail = 64 << 10
)

// ScanTranscripts lists the threads Codex keeps under home, most recently
// written first, reading only the head and tail of each rollout (the
// directory and first prompt come early; the model and last timestamp are
// rewritten as it goes) plus the thread names Codex keeps in
// session_index.jsonl. Threads with no user prompt in their head (an
// aborted start, a subagent's own rollout) are left out.
func ScanTranscripts(home string) []Transcript {
	names := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(home, ".codex", "session_index.jsonl")); err == nil {
		for _, line := range bytes.Split(b, []byte("\n")) {
			var m struct {
				ID   string `json:"id"`
				Name string `json:"thread_name"`
			}
			if json.Unmarshal(line, &m) == nil && m.ID != "" && m.Name != "" {
				names[m.ID] = m.Name // later lines win: the latest name
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", "*", "*", "*", "rollout-*.jsonl"))
	var out []Transcript
	for _, path := range files {
		if t, ok := scanTranscript(path); ok {
			t.Title = names[t.ID]
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
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	t := Transcript{Path: path, Size: st.Size(), Updated: st.ModTime()}
	// rollout-<yyyy>-<mm>-<dd>T<hh>-<mm>-<ss>-<thread id>: the id follows
	// the sixth dash (session_meta names it too, and wins)
	if parts := strings.SplitN(base, "-", 7); len(parts) == 7 {
		t.ID = parts[6]
	}
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
		if i := bytes.IndexByte(tail, '\n'); i >= 0 {
			tail = tail[i+1:]
		} else {
			tail = nil
		}
	}
	if int64(len(head)) < st.Size() {
		if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
			head = head[:i]
		}
	}
	lines := bytes.Split(head, []byte("\n"))
	if len(tail) > 0 {
		lines = append(lines, bytes.Split(tail, []byte("\n"))...)
	}
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		p := sub(m, "payload")
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
		switch str(m, "type") {
		case "session_meta":
			if id := str(p, "id"); id != "" {
				t.ID = id
			}
			t.Cwd = firstStr(str(p, "cwd"), t.Cwd)
			t.Version = firstStr(str(p, "cli_version"), t.Version)
			if sub(p, "source")["subagent"] != nil || strings.Contains(str(p, "thread_source"), "subagent") || strings.Contains(str(p, "thread_source"), "guardian") {
				return Transcript{}, false // a subagent's (or the reviewer's) own thread
			}
		case "turn_context":
			if mdl := str(p, "model"); mdl != "" {
				t.Model = mdl
			}
			if t.Cwd == "" {
				t.Cwd = str(p, "cwd")
			}
		case "event_msg":
			switch str(p, "type") {
			case "thread_settings_applied":
				if mdl := str(sub(p, "thread_settings"), "model"); mdl != "" {
					t.Model = mdl
				}
			case "item_completed":
				if it := sub(p, "item"); t.First == "" && str(it, "type") == "UserMessage" {
					text, _ := userContent(coreUserContent(arr(it, "content")))
					if text = promptText(text); text != "" {
						t.First = clipRunes(text, 200)
					}
				}
			case "user_message":
				if t.First == "" {
					if text := promptText(str(p, "message")); text != "" {
						t.First = clipRunes(text, 200)
					}
				}
			}
		}
	}
	if t.First == "" || t.ID == "" {
		return Transcript{}, false
	}
	if t.Started.IsZero() {
		t.Started = t.Updated
	}
	return t, true
}

// promptText is what the user typed: the context Codex prepends in tags
// (environment, plugins, instructions) is not a prompt.
func promptText(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "<") {
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

// transcript projects one record read off the rollout by translating it
// into the app-server notification it corresponds to and projecting that;
// the record itself is what the events show as evidence.
func (p *Projector) transcript(f model.Frame) []model.Event {
	m := obj(f.Payload)
	pl := sub(m, "payload")
	switch str(m, "type") {
	case "turn_context":
		if str(pl, "model") == "" {
			return []model.Event{model.FoldTo(f, "", "turn context")}
		}
		ts := map[string]any{"model": str(pl, "model"), "effort": str(pl, "effort"), "personality": str(pl, "personality"),
			"approvalPolicy": str(pl, "approval_policy"), "approvalsReviewer": str(pl, "approvals_reviewer"), "sandboxPolicy": map[string]any{"type": str(sub(pl, "sandbox_policy"), "type")}}
		return p.synth(f, "thread/settings/updated", map[string]any{"threadSettings": ts})
	case "event_msg":
		thread := str(pl, "thread_id")
		switch st := str(pl, "type"); st {
		case "task_started":
			turn := map[string]any{"id": str(pl, "turn_id"), "status": "inProgress", "startedAt": num(pl, "started_at")}
			if w := num(pl, "model_context_window"); w > 0 {
				p.ctxWindow = int64(w)
			}
			return p.synth(f, "turn/started", map[string]any{"threadId": thread, "turn": turn})
		case "task_complete":
			turn := map[string]any{"id": str(pl, "turn_id"), "status": "completed", "durationMs": num(pl, "duration_ms"), "items": []any{}}
			if s := str(pl, "last_agent_message"); s != "" {
				turn["items"] = []any{map[string]any{"type": "agentMessage", "text": s}}
			}
			return p.synth(f, "turn/completed", map[string]any{"threadId": thread, "turn": turn})
		case "turn_aborted":
			turn := map[string]any{"id": str(pl, "turn_id"), "status": "interrupted", "durationMs": num(pl, "duration_ms"), "items": []any{}}
			return p.synth(f, "turn/completed", map[string]any{"threadId": thread, "turn": turn})
		case "token_count":
			info := sub(pl, "info")
			usage := func(u map[string]any) map[string]any {
				return map[string]any{"inputTokens": num(u, "input_tokens"), "outputTokens": num(u, "output_tokens"), "totalTokens": num(u, "total_tokens"),
					"cachedInputTokens": num(u, "cached_input_tokens"), "reasoningOutputTokens": num(u, "reasoning_output_tokens")}
			}
			tu := map[string]any{"last": usage(sub(info, "last_token_usage")), "total": usage(sub(info, "total_token_usage")), "modelContextWindow": num(info, "model_context_window")}
			return p.synth(f, "thread/tokenUsage/updated", map[string]any{"threadId": thread, "tokenUsage": tu})
		case "thread_settings_applied":
			s := sub(pl, "thread_settings")
			ts := map[string]any{"model": str(s, "model"), "effort": str(s, "effort"), "personality": str(s, "personality"), "serviceTier": str(s, "service_tier"),
				"approvalPolicy": str(s, "approval_policy"), "approvalsReviewer": str(s, "approvals_reviewer"), "sandboxPolicy": map[string]any{"type": str(sub(s, "sandbox_policy"), "type")}}
			return p.synth(f, "thread/settings/updated", map[string]any{"threadId": thread, "threadSettings": ts})
		case "thread_rolled_back":
			n := int(num(pl, "num_turns"))
			text := "conversation rolled back"
			if n == 1 {
				text += " one turn"
			} else if n > 1 {
				text += " " + itoa(n) + " turns"
			}
			return []model.Event{model.New(model.SessionState, f).Set("text", text)}
		case "user_message":
			// a rollout from before items were recorded
			it := map[string]any{"type": "userMessage", "id": "", "content": []any{map[string]any{"type": "text", "text": str(pl, "message")}}}
			return p.synth(f, "item/started", map[string]any{"threadId": thread, "item": it})
		case "agent_message":
			it := map[string]any{"type": "agentMessage", "id": "", "text": str(pl, "message"), "phase": str(pl, "phase")}
			return p.synth(f, "item/completed", map[string]any{"threadId": thread, "item": it})
		case "item_completed":
			it := v2Item(sub(pl, "item"))
			if it == nil {
				return []model.Event{model.FoldTo(f, "", "item")}
			}
			params := map[string]any{"threadId": thread, "turnId": str(pl, "turn_id"), "item": it}
			switch str(it, "type") {
			case "userMessage":
				// the started notification is the one that renders the message
				return p.synth(f, "item/started", params)
			case "agentMessage", "reasoning":
				return p.synth(f, "item/completed", params)
			}
			// a tool: the start registers the call, the completion its result;
			// the record is shown once, on the call
			out := p.synth(f, "item/started", params)
			for _, e := range p.synthRaw(f, "item/completed", params, false) {
				out = append(out, e)
			}
			return out
		default:
			return []model.Event{model.FoldTo(f, "", strings.ReplaceAll(st, "_", " "))}
		}
	}
	return []model.Event{model.FoldTo(f, "", firstStr(str(m, "type"), "record"))}
}

// synth projects the notification Acta composed for a transcript record,
// then puts the record itself in the events' raw panels.
func (p *Projector) synth(f model.Frame, method string, params map[string]any) []model.Event {
	return p.synthRaw(f, method, params, true)
}

func (p *Projector) synthRaw(f model.Frame, method string, params map[string]any, keepRaw bool) []model.Event {
	raw, _ := json.Marshal(map[string]any{"method": method, "params": params})
	sf := model.Frame{Seq: f.Seq, Kind: method, Payload: raw, At: f.At, Stored: f.Stored}
	evs := p.notification(sf, obj(raw))
	for i := range evs {
		if !keepRaw {
			evs[i].Raw = nil
			continue
		}
		for j := range evs[i].Raw {
			if evs[i].Raw[j].Kind == method && string(evs[i].Raw[j].Payload) == string(raw) {
				evs[i].Raw[j].Kind = f.Kind
				evs[i].Raw[j].Payload = f.Payload
				evs[i].Raw[j].Seq = model.RawOf(f).Seq
			}
		}
	}
	return evs
}

// transcriptState is the divider a catch-up or import puts before the
// records it stores.
func (p *Projector) transcriptState(f model.Frame, m map[string]any) []model.Event {
	e := model.NewLabelled(model.SessionCatchup, f, str(m, "state")).Set("source", str(m, "state")).Set("count", int(num(m, "count"))).Set("from", str(m, "from")).Set("to", str(m, "to"))
	if sk := num(m, "skipped"); sk > 0 {
		e.Set("skipped", int64(sk))
	}
	e.Ref = ref("catchup", f)
	return []model.Event{e}
}

// WriteTitle gives a thread the name Codex's own picker shows, the way
// Codex records one: a line appended to ~/.codex/session_index.jsonl, the
// last line for a thread id winning.
func WriteTitle(home, threadID, title string) error {
	b, err := json.Marshal(map[string]string{"id": threadID, "thread_name": title, "updated_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "session_index.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// KeepLine says whether a rollout line can be part of the conversation at
// all (ChainRecords keeps event_msg and turn_context records), so a reader
// can drop the rest, response items, compaction snapshots and token
// bookkeeping, which is most of a long rollout, before holding it.
func KeepLine(line []byte) bool {
	var r struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	return r.Type == "event_msg" || r.Type == "turn_context"
}

// TurnStart says whether a rollout line begins a turn (the task_started
// event), the place a reader may cut a long rollout without splitting one.
func TurnStart(line []byte) bool {
	var r struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &r) != nil {
		return false
	}
	return r.Type == "event_msg" && r.Payload.Type == "task_started"
}

// v2Item spells a core item (as the rollout records it) the way the
// app-server's v2 API does, which is what the item handlers read. Nil for a
// kind the app-server would not have sent as an item.
func v2Item(it map[string]any) map[string]any {
	id := str(it, "id")
	switch str(it, "type") {
	case "UserMessage":
		return map[string]any{"type": "userMessage", "id": id, "content": coreUserContent(arr(it, "content"))}
	case "AgentMessage":
		var texts []string
		for _, c := range arr(it, "content") {
			cm, _ := c.(map[string]any)
			if s := str(cm, "text"); s != "" {
				texts = append(texts, s)
			}
		}
		return map[string]any{"type": "agentMessage", "id": id, "text": strings.Join(texts, "\n"), "phase": str(it, "phase")}
	case "Reasoning":
		return map[string]any{"type": "reasoning", "id": id, "summary": arr(it, "summary_text"), "content": arr(it, "raw_content")}
	case "CommandExecution":
		cmd := ""
		if parts := arr(it, "command"); len(parts) > 0 {
			cmd = strings.Join(strs(parts), " ")
		} else {
			cmd = str(it, "command")
		}
		var actions []any
		for _, pc := range arr(it, "parsed_cmd") {
			pm, _ := pc.(map[string]any)
			if c := str(pm, "cmd"); c != "" {
				actions = append(actions, map[string]any{"type": str(pm, "type"), "command": c, "path": pm["path"]})
			}
		}
		out := map[string]any{"type": "commandExecution", "id": id, "command": cmd, "cwd": strings.TrimPrefix(str(it, "cwd"), "file://"),
			"status": v2Status(str(it, "status")), "aggregatedOutput": firstStr(str(it, "aggregated_output"), str(it, "formatted_output"), str(it, "stdout")+str(it, "stderr")), "commandActions": actions}
		if it["exit_code"] != nil {
			out["exitCode"] = num(it, "exit_code")
		}
		if d := durationMs(it["duration"]); d > 0 {
			out["durationMs"] = d
		}
		return out
	case "FileChange":
		var changes []any
		if cm, ok := it["changes"].(map[string]any); ok {
			paths := make([]string, 0, len(cm))
			for path := range cm {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			for _, path := range paths {
				ch, _ := cm[path].(map[string]any)
				kind := map[string]any{"type": str(ch, "type")}
				if mv := str(ch, "move_path"); mv != "" {
					kind["move_path"] = mv
				}
				changes = append(changes, map[string]any{"path": path, "kind": kind, "diff": firstStr(str(ch, "unified_diff"), str(ch, "diff"), str(ch, "content"))})
			}
		}
		return map[string]any{"type": "fileChange", "id": id, "status": v2Status(str(it, "status")), "changes": changes}
	case "McpToolCall":
		out := map[string]any{"type": "mcpToolCall", "id": id, "server": str(it, "server"), "tool": str(it, "tool"), "arguments": it["arguments"], "status": v2Status(str(it, "status")), "result": it["result"], "error": it["error"]}
		if d := durationMs(it["duration"]); d > 0 {
			out["durationMs"] = d
		}
		return out
	case "Extension":
		if str(it, "kind") == "web.search" {
			return map[string]any{"type": "webSearch", "id": id, "query": str(it, "query"), "action": it["action"], "results": it["results"]}
		}
		return map[string]any{"type": str(it, "kind"), "id": id}
	case "ContextCompaction":
		return map[string]any{"type": "contextCompaction", "id": id}
	case "ImageView":
		return map[string]any{"type": "imageView", "id": id, "path": strings.TrimPrefix(str(it, "path"), "file://")}
	case "":
		return nil
	}
	t := str(it, "type")
	return map[string]any{"type": strings.ToLower(t[:1]) + t[1:], "id": id}
}

// coreUserContent spells a user message's blocks as the v2 API does.
func coreUserContent(blocks []any) []any {
	out := make([]any, 0, len(blocks))
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		switch str(bm, "type") {
		case "text", "Text", "input_text":
			out = append(out, map[string]any{"type": "text", "text": str(bm, "text")})
		case "image", "Image", "input_image":
			out = append(out, map[string]any{"type": "image", "url": firstStr(str(bm, "image_url"), str(bm, "url"))})
		case "local_image", "LocalImage":
			out = append(out, map[string]any{"type": "localImage", "path": str(bm, "path")})
		case "skill", "Skill":
			out = append(out, map[string]any{"type": "skill", "name": str(bm, "name")})
		}
	}
	return out
}

func v2Status(s string) string {
	switch s {
	case "in_progress":
		return "inProgress"
	}
	return s
}

// durationMs reads a duration Codex records as {secs, nanos} (or a number
// of milliseconds).
func durationMs(v any) float64 {
	switch d := v.(type) {
	case float64:
		return d
	case map[string]any:
		return num(d, "secs")*1000 + num(d, "nanos")/1e6
	}
	return 0
}

func itoa(n int) string { return strconv.Itoa(n) }
