package codex

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/agentsession/model"
)

// Projector turns a Codex app-server transcript (JSON-RPC lines, plus Acta's
// own input/control/state frames) into model events. One per session; frames
// arrive in order.
type Projector struct {
	thread    string
	turn      string // the active turn
	model     string
	ctxWindow int64
	lastTotal int64 // tokens in the window at the last usage report

	items       map[string]*item     // item id -> what it is and where it shows
	approvals   map[string]*approval // request id (or review id) -> pending approval
	autoRef     string               // the latest automatic review, for the warning that explains it
	cmds        []*cmd               // /compact, /goal awaiting their reply
	lastInput   string
	sessionRef  string
	turnRef     string // ref of the last input that started a turn (its init/turn frames fold there)
	compact     *compaction
	lastDiff    string
	goal        *goal
	goalRef     string            // the /goal marker the next goal notification folds into
	settingRef  map[string]string // key -> the browser's setting marker awaiting the thread's confirmation
	effort      string
	personality string
	tier        string
	initDone    bool
	initRef     string // where the thread's opening frames fold
	turnHasEcho bool
}

type item struct {
	typ, ref string
}
type approval struct {
	ref, kind, method, callID string
	done                      bool
	auto                      bool
}
type cmd struct {
	ref, name, args, kind string
}
type compaction struct {
	ref string
	pre int64
}
type goal struct {
	cond, state string
}

// New returns a projector for one Codex session.
func New() *Projector {
	return &Projector{items: map[string]*item{}, approvals: map[string]*approval{}, settingRef: map[string]string{}}
}

func obj(raw json.RawMessage) map[string]any {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return map[string]any{}
	}
	return m
}
func str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}
func sub(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	s, _ := m[k].(map[string]any)
	return s
}
func arr(m map[string]any, k string) []any {
	if m == nil {
		return nil
	}
	a, _ := m[k].([]any)
	return a
}
func num(m map[string]any, k string) float64 {
	if m == nil {
		return 0
	}
	f, _ := m[k].(float64)
	return f
}
func boolean(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}
func anyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case nil:
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func ref(kind string, f model.Frame) string { return kind + ":" + strconv.FormatInt(f.Seq, 10) }
func at(f model.Frame) string               { return f.At.UTC().Format(model.Fmt) }

// bare makes an event with no raw frame of its own (a side effect of a frame
// another event carries).
func bare(t string, f model.Frame) model.Event {
	return model.Event{T: t, Seq: f.Seq, At: at(f), Data: map[string]any{}}
}

// Project maps one frame to its events.
func (p *Projector) Project(f model.Frame) []model.Event {
	var out []model.Event
	switch f.Kind {
	case "input":
		out = p.input(f)
	case "control":
		out = p.control(f)
	case "state":
		out = p.state(f)
	case "response":
		out = p.response(f)
	case TranscriptKind:
		out = p.transcript(f)
	default:
		m := obj(f.Payload)
		if _, isReq := m["id"]; isReq && str(m, "method") != "" {
			out = p.serverRequest(f, m)
		} else {
			out = p.notification(f, m)
		}
	}
	for i := range out {
		out[i].Sub = i
	}
	return out
}

// --- browser input ---

var slashRe = regexp.MustCompile(`^/([\w:-]+)(?:\s+([\s\S]*))?$`)

func (p *Projector) input(f model.Frame) []model.Event {
	m := obj(f.Payload)
	text := str(m, "text")
	e := model.New(model.Input, f).Set("text", text)
	if imgs := arr(m, "images"); len(imgs) > 0 {
		e.Set("images", imgs)
	}
	e.Ref = ref("input", f)
	p.lastInput = e.Ref
	if sm := slashRe.FindStringSubmatch(strings.TrimSpace(text)); sm != nil && len(arr(m, "images")) == 0 {
		name, args := sm[1], strings.TrimSpace(sm[2])
		switch name {
		case "compact", "goal":
			c := &cmd{ref: ref("cmd", f), name: name, args: args, kind: name}
			p.cmds = append(p.cmds, c)
			e.Ref = c.ref
			p.lastInput = c.ref
			e.Set("cmd", map[string]any{"name": name, "args": args, "kind": name})
			return []model.Event{e}
		}
	}
	p.turnRef = e.Ref
	return []model.Event{e}
}

func (p *Projector) takeCmd(kind string) *cmd {
	for i, c := range p.cmds {
		if c.kind == kind {
			p.cmds = append(p.cmds[:i], p.cmds[i+1:]...)
			return c
		}
	}
	return nil
}

// --- browser operations ---

func (p *Projector) control(f model.Frame) []model.Event {
	m := obj(f.Payload)
	op := str(m, "op")
	id := str(m, "id")
	switch op {
	case "answer":
		a := p.approvals[id]
		outcome := str(m, "outcome")
		switch outcome {
		case "allow", "accept":
			if sub(m, "answers") != nil {
				outcome = "answered"
			} else if a != nil && a.kind == "elicitation" {
				outcome = "answered"
			} else {
				outcome = "allowed"
			}
		case "deny":
			if a != nil && a.kind == "question" {
				outcome = "skipped"
			} else {
				outcome = "denied"
			}
		case "decline":
			outcome = "declined"
		case "cancel":
			outcome = "cancelled"
		}
		e := model.New(model.ApprovalAnswer, f).Set("id", id).Set("mine", true).Set("outcome", outcome)
		if ans := sub(m, "answers"); ans != nil {
			e.Set("answers", ans)
		}
		if msg := str(m, "message"); msg != "" {
			e.Set("message", msg)
		}
		if c, ok := m["content"]; ok {
			e.Set("content", c)
		}
		if a != nil {
			if a.done {
				return []model.Event{model.FoldTo(f, a.ref, "answer")}
			}
			a.done = true
			e.To = a.ref
			if a.kind == "question" && sub(m, "answers") != nil {
				q := bare(model.QuestionAnswer, f).Set("call_id", a.callID).Set("answers", sub(m, "answers"))
				q.To = "tool:" + a.callID
				return []model.Event{e, q}
			}
		}
		return []model.Event{e}
	case "setting":
		key, val := str(m, "key"), str(m, "value")
		if key == "output_style" {
			key = "personality"
		}
		if key == "fast" {
			e := model.New(model.Fast, f).Set("on", val == "on")
			e.Ref = ref("setting", f)
			return []model.Event{e}
		}
		if key == "effort" {
			e := model.New(model.Effort, f).Set("value", val)
			e.Ref = ref("setting", f)
			return []model.Event{e}
		}
		// applied from the next turn; the thread reports its settings then,
		// which confirms (or corrects) the marker
		e := model.New(model.Setting, f).Set("key", key).Set("value", val).Set("requested", true)
		e.Ref = ref("setting", f)
		p.settingRef[key] = e.Ref
		if key == "model" && val != "" {
			p.model = val
		}
		return []model.Event{e}
	case "catalog", "rewind", "rewind_files", "side_question":
		return []model.Event{model.FoldTo(f, "", strings.ReplaceAll(op, "_", " "))}
	}
	return []model.Event{model.New(model.Unknown, f).Set("kind", "control").Set("text", "browser operation: "+op)}
}

// --- harness state ---

func (p *Projector) state(f model.Frame) []model.Event {
	m := obj(f.Payload)
	st := str(m, "state")
	idle := bare(model.TurnIdle, f)
	switch st {
	case "catchup", "import":
		return p.transcriptState(f, m)
	case "spawned":
		for _, a := range p.approvals {
			a.done = true
		}
		e := model.NewLabelled(model.SessionSpawned, f, "spawn").Set("resumed", boolean(m, "resumed"))
		e.Ref = ref("session", f)
		p.sessionRef = e.Ref
		p.initDone = false
		p.turn = ""
		return []model.Event{e, idle}
	case "exit":
		for _, a := range p.approvals {
			a.done = true
		}
		p.turn = ""
		e := model.NewLabelled(model.SessionExit, f, "exit").Set("code", int(num(m, "code")))
		return []model.Event{e, idle}
	case "spawn_error":
		e := model.New(model.SessionSpawnError, f).Set("error", str(m, "error"))
		if p.lastInput != "" {
			e.To = p.lastInput
		}
		return []model.Event{e}
	case "undelivered":
		e := model.New(model.SessionUndelivered, f).Set("reason", str(m, "reason"))
		if p.lastInput != "" {
			e.To = p.lastInput
		}
		return []model.Event{e}
	case "resume_failed":
		return []model.Event{model.New(model.SessionResumeFail, f).Set("reason", str(m, "reason"))}
	case "stdout":
		return []model.Event{model.New(model.SessionState, f).Set("text", str(m, "text"))}
	}
	return []model.Event{model.New(model.SessionState, f).Set("text", st)}
}

// --- responses to Acta's own requests ---

func (p *Projector) response(f model.Frame) []model.Event {
	m := obj(f.Payload)
	id := anyStr(m["id"])
	result := sub(m, "result")
	rpcErr := sub(m, "error")
	errText := str(rpcErr, "message")
	switch {
	case id == "acta-init":
		return []model.Event{model.FoldTo(f, p.sessionRef, "initialize")}
	case id == "acta-thread-start" || id == "acta-thread-resume":
		if rpcErr != nil {
			e := model.New(model.Notice, f).Set("level", "error").Set("text", "could not open the thread: "+errText).Set("subtype", "thread")
			return []model.Event{e}
		}
		return p.threadOpened(f, result, sub(result, "thread"), id == "acta-thread-resume")
	case strings.HasPrefix(id, "acta-turn-") || strings.HasPrefix(id, "acta-steer-"):
		if rpcErr != nil {
			e := model.New(model.Notice, f).Set("level", "error").Set("text", errText).Set("subtype", "turn")
			if p.lastInput != "" {
				e.To = p.lastInput
			}
			return []model.Event{e, bare(model.TurnIdle, f)}
		}
		if t := sub(result, "turn"); t != nil && str(t, "id") != "" {
			p.turn = str(t, "id")
		}
		return []model.Event{model.FoldTo(f, p.lastInput, "turn started")}
	case strings.HasPrefix(id, "acta-interrupt-"):
		return []model.Event{model.FoldTo(f, "", "interrupt")}
	case strings.HasPrefix(id, "acta-name-"):
		return []model.Event{model.FoldTo(f, "", "rename")}
	case strings.HasPrefix(id, "models-"):
		e := model.NewLabelled(model.SessionCatalog, f, "models")
		var models []map[string]any
		for _, x := range arr(result, "data") {
			xm, _ := x.(map[string]any)
			if boolean(xm, "hidden") {
				continue
			}
			var efforts []string
			for _, ef := range arr(xm, "supportedReasoningEfforts") {
				em, _ := ef.(map[string]any)
				if e := str(em, "reasoningEffort"); e != "" {
					efforts = append(efforts, e)
				}
			}
			models = append(models, map[string]any{
				"value": str(xm, "id"), "resolvedModel": str(xm, "id"), "displayName": str(xm, "displayName"), "description": str(xm, "description"),
				"supportedEffortLevels": efforts, "supportsFastMode": len(arr(xm, "serviceTiers")) > 0, "defaultEffort": str(xm, "defaultReasoningEffort"),
			})
		}
		e.Set("models", models)
		e.Set("output_styles", []map[string]any{
			{"name": "default", "description": "Codex's usual voice", "source": "built-in"},
			{"name": "friendly", "description": "warmer, more conversational", "source": "built-in"},
			{"name": "pragmatic", "description": "terse and to the point", "source": "built-in"},
		})
		return []model.Event{e}
	case strings.HasPrefix(id, "skills-"):
		e := model.NewLabelled(model.SessionCatalog, f, "skills")
		var cmds []map[string]any
		for _, entry := range arr(result, "data") {
			em, _ := entry.(map[string]any)
			for _, sk := range arr(em, "skills") {
				sm, _ := sk.(map[string]any)
				if !boolean(sm, "enabled") && sm["enabled"] != nil {
					continue
				}
				desc := str(sm, "shortDescription")
				if iface := sub(sm, "interface"); iface != nil && str(iface, "shortDescription") != "" {
					desc = str(iface, "shortDescription")
				}
				if desc == "" {
					desc = str(sm, "description")
				}
				cmds = append(cmds, map[string]any{"name": str(sm, "name"), "description": desc, "argumentHint": "", "skill": true})
			}
		}
		e.Set("commands", cmds)
		return []model.Event{e}
	case strings.HasPrefix(id, "acta-goal-"):
		if c := p.takeCmd("goal"); c != nil {
			if rpcErr != nil {
				e := model.NewLabelled(model.CmdReply, f, "reply").Set("text", errText).Set("kind", "goal").Set("name", "goal").Set("error", true)
				e.To = c.ref
				return []model.Event{e}
			}
			var out []model.Event
			text := ""
			p.goalRef = c.ref
			if g := sub(result, "goal"); g != nil {
				p.goal = &goal{cond: str(g, "objective"), state: goalState(str(g, "status"))}
				text = "Goal " + str(g, "status") + ": " + str(g, "objective")
			} else if boolean(result, "cleared") {
				p.goal = nil
				text = "Goal cleared"
			} else {
				p.goal = nil
				text = "No goal set"
			}
			e := model.NewLabelled(model.CmdReply, f, "reply").Set("text", text).Set("kind", "goal").Set("name", "goal")
			e.To = c.ref
			out = append(out, e)
			g := bare(model.Goal, f)
			for k, v := range p.goalData() {
				g.Data[k] = v
			}
			out = append(out, g)
			return out
		}
		return []model.Event{model.FoldTo(f, "", "goal")}
	case strings.HasPrefix(id, "acta-compact-"):
		if rpcErr != nil {
			e := model.New(model.Notice, f).Set("level", "error").Set("text", "compaction: "+errText).Set("subtype", "compact")
			return []model.Event{e}
		}
		if p.compact == nil {
			return []model.Event{p.compactStart(f, "compact requested")}
		}
		return []model.Event{model.FoldTo(f, p.compact.ref, "compact requested")}
	case strings.HasPrefix(id, "rw-"):
		e := model.New(model.Reply, f).Set("id", id)
		if rpcErr != nil {
			e.Set("error", errText)
		} else {
			e.Set("response", map[string]any{"rewound": true})
		}
		return []model.Event{e}
	}
	if rpcErr != nil {
		return []model.Event{model.New(model.Notice, f).Set("level", "error").Set("text", errText).Set("subtype", "rpc")}
	}
	return []model.Event{model.FoldTo(f, "", "response "+id)}
}

func goalState(status string) string {
	switch status {
	case "complete":
		return "met"
	case "active", "":
		return "active"
	}
	return "active" // paused, blocked, usageLimited, budgetLimited: still a goal
}

func (p *Projector) goalData() map[string]any {
	if p.goal == nil {
		return map[string]any{"state": "cleared"}
	}
	return map[string]any{"cond": p.goal.cond, "state": p.goal.state}
}

// threadOpened: the thread/start or thread/resume result (or the
// thread/started notification, whichever comes first) is the session's init.
func (p *Projector) threadOpened(f model.Frame, result, thread map[string]any, resumed bool) []model.Event {
	if p.initDone {
		return []model.Event{model.FoldTo(f, p.initRef, "thread")}
	}
	p.initDone = true
	if id := str(thread, "id"); id != "" {
		p.thread = id
	}
	mdl := str(result, "model")
	if mdl == "" {
		mdl = str(thread, "model")
	}
	p.model = mdl
	p.effort = firstStr(str(result, "reasoningEffort"), str(thread, "reasoningEffort"))
	p.tier = str(result, "serviceTier")
	sb := sub(result, "sandbox")
	mode := ModeName(str(result, "approvalPolicy"), str(sb, "type"), str(result, "approvalsReviewer"))
	e := model.NewLabelled(model.SessionInit, f, "thread").Set("model", mdl).Set("permission_mode", mode).Set("cwd", firstStr(str(result, "cwd"), str(thread, "cwd"))).Set("conversation", p.thread).Set("effort", firstStr(str(result, "reasoningEffort"), str(thread, "reasoningEffort")))
	if st := str(result, "serviceTier"); st != "" {
		if st == "priority" {
			e.Set("fast_mode", "on")
		} else {
			e.Set("fast_mode", "off")
		}
	}
	if p.sessionRef != "" {
		e.To = p.sessionRef
		p.initRef = p.sessionRef
		p.sessionRef = ""
	} else {
		e.Ref = ref("session", f)
		p.initRef = e.Ref
	}
	return []model.Event{e}
}

func firstStr(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// --- server requests (approvals, questions) ---

func (p *Projector) serverRequest(f model.Frame, m map[string]any) []model.Event {
	id := anyStr(m["id"])
	method := str(m, "method")
	params := sub(m, "params")
	e := model.New(model.ApprovalRequest, f).Set("id", id).Set("subtype", method)
	e.Ref = "approval:" + id
	a := &approval{ref: e.Ref, method: method}
	callID := str(params, "itemId")
	a.callID = callID
	if callID != "" {
		e.Set("call_id", callID)
		if it := p.items[callID]; it != nil {
			e.To = it.ref
		}
	}
	switch method {
	case "item/commandExecution/requestApproval", "execCommandApproval":
		a.kind = "tool"
		input := map[string]any{"command": str(params, "command"), "cwd": str(params, "cwd")}
		if r := str(params, "reason"); r != "" {
			input["reason"] = r
		}
		if nc := sub(params, "networkApprovalContext"); nc != nil {
			input["network"] = nc
		}
		e.Set("kind", "tool").Set("tool", "Bash").Set("display", "Shell").Set("description", str(params, "reason")).Set("input", input)
		if amend := arr(params, "proposedExecpolicyAmendment"); len(amend) > 0 {
			e.Set("suggestions", []map[string]any{{"type": "acceptForSession", "behavior": "allow", "rules": []map[string]any{{"toolName": "Bash", "ruleContent": strings.Join(strs(amend), " ")}}}})
		}
	case "item/fileChange/requestApproval", "applyPatchApproval":
		a.kind = "tool"
		input := map[string]any{}
		if r := str(params, "reason"); r != "" {
			input["reason"] = r
		}
		if g := str(params, "grantRoot"); g != "" {
			input["grant_root"] = g
		}
		e.Set("kind", "tool").Set("tool", "apply_patch").Set("display", "Edit files").Set("description", str(params, "reason")).Set("input", input)
	case "item/tool/requestUserInput":
		a.kind = "question"
		var qs []map[string]any
		for _, q := range arr(params, "questions") {
			qm, _ := q.(map[string]any)
			var opts []map[string]any
			for _, o := range arr(qm, "options") {
				om, _ := o.(map[string]any)
				opts = append(opts, map[string]any{"label": str(om, "label"), "description": str(om, "description")})
			}
			qs = append(qs, map[string]any{"id": str(qm, "id"), "header": str(qm, "header"), "question": str(qm, "question"), "options": opts, "other": boolean(qm, "isOther"), "secret": boolean(qm, "isSecret")})
		}
		e.Set("kind", "question").Set("tool", "request_user_input").Set("questions", qs)
	case "mcpServer/elicitation/request":
		a.kind = "elicitation"
		e.Set("kind", "elicitation").Set("server", str(params, "serverName")).Set("message", str(params, "message")).Set("mode", "form")
		if s := params["requestedSchema"]; s != nil {
			e.Set("schema", s)
		}
	case "item/permissions/requestApproval":
		a.kind = "tool"
		e.Set("kind", "tool").Set("tool", "permissions").Set("display", "Permissions").Set("description", str(params, "reason")).Set("input", map[string]any{"permissions": params["permissions"], "cwd": str(params, "cwd"), "reason": str(params, "reason")})
	default:
		a.kind = "other"
		e.Set("kind", "other")
	}
	p.approvals[id] = a
	return []model.Event{e}
}

func strs(xs []any) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, anyStr(x))
	}
	return out
}

// --- notifications ---

func (p *Projector) notification(f model.Frame, m map[string]any) []model.Event {
	method := str(m, "method")
	params := sub(m, "params")
	switch method {
	case "thread/started":
		return p.threadOpened(f, nil, sub(params, "thread"), false)
	case "turn/started":
		if t := sub(params, "turn"); t != nil {
			p.turn = str(t, "id")
		}
		p.turnHasEcho = false
		if p.compact != nil && p.compact.pre == 0 {
			// a compaction runs as a turn of its own
			return []model.Event{model.FoldTo(f, p.compact.ref, "turn")}
		}
		return []model.Event{model.FoldTo(f, p.lastInput, "turn started")}
	case "turn/completed":
		p.turn = ""
		turn := sub(params, "turn")
		idle := bare(model.TurnIdle, f)
		if p.compact != nil {
			// the compaction's own turn
			c := p.compact
			p.compact = nil
			return []model.Event{model.FoldTo(f, c.ref, "turn completed"), idle}
		}
		if c := p.takeCmd("goal"); c != nil && len(arr(turn, "items")) == 0 && str(turn, "status") != "completed" {
			// a goal cleared mid-turn interrupts the goal's turn
			return []model.Event{model.FoldTo(f, c.ref, "turn"), idle}
		}
		status := str(turn, "status")
		e := model.New(model.TurnEnd, f).Set("ok", status == "completed").Set("interrupted", status == "interrupted")
		e.Ref = ref("turn", f)
		if te := sub(turn, "error"); te != nil {
			e.Set("error", str(te, "message"))
			if d := str(te, "additionalDetails"); d != "" {
				e.Set("errors", []string{d})
			}
		} else if status == "failed" {
			e.Set("error", "failed")
		}
		if d := num(turn, "durationMs"); d > 0 {
			e.Set("duration_ms", int64(d))
		}
		if p.lastTotal > 0 {
			e.Set("tokens", p.lastTotal)
		}
		if p.ctxWindow > 0 {
			e.Set("context_window", p.ctxWindow)
		}
		for _, it := range arr(turn, "items") {
			im, _ := it.(map[string]any)
			if str(im, "type") == "agentMessage" {
				e.Set("result", str(im, "text"))
			}
		}
		var out []model.Event
		if p.goal != nil && p.goal.state == "active" && status == "completed" {
			// Codex reports the goal's state itself (thread/goal/updated); a
			// completed turn with the goal still active means it is still working
		}
		out = append(out, e, idle)
		return out
	case "item/started":
		return p.itemStarted(f, params)
	case "item/completed":
		return p.itemCompleted(f, params)
	case "item/agentMessage/delta":
		e := model.Event{T: model.StreamDelta, Seq: f.Seq, At: at(f), Live: true, Data: map[string]any{"index": 0, "text": str(params, "delta")}}
		return []model.Event{e}
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta", "item/reasoning/summaryPartAdded":
		e := model.Event{T: model.Thinking, Seq: f.Seq, At: at(f), Live: true, Data: map[string]any{"text": str(params, "delta")}}
		return []model.Event{e}
	case "item/commandExecution/outputDelta":
		e := model.Event{T: model.ToolOutput, Seq: f.Seq, At: at(f), Live: true, Data: map[string]any{"task_id": str(params, "itemId"), "text": str(params, "delta")}}
		e.To = "tool:" + str(params, "itemId")
		return []model.Event{e}
	case "item/plan/delta", "item/fileChange/outputDelta", "item/fileChange/patchUpdated", "item/mcpToolCall/progress":
		if it := p.items[str(params, "itemId")]; it != nil {
			return []model.Event{model.FoldTo(f, it.ref, strings.TrimPrefix(method, "item/"))}
		}
		return []model.Event{model.FoldTo(f, "", method)}
	case "turn/diff/updated":
		diff := str(params, "diff")
		if diff == p.lastDiff {
			return []model.Event{model.FoldTo(f, "", "diff (unchanged)")}
		}
		p.lastDiff = diff
		e := model.New(model.TurnDiff, f).Set("text", diff).Set("turn", str(params, "turnId"))
		return []model.Event{e}
	case "turn/plan/updated":
		var list []map[string]any
		done, total := 0, 0
		for i, s := range arr(params, "plan") {
			sm, _ := s.(map[string]any)
			status := map[string]string{"pending": "pending", "inProgress": "in_progress", "completed": "completed"}[str(sm, "status")]
			if status == "" {
				status = "pending"
			}
			total++
			if status == "completed" {
				done++
			}
			list = append(list, map[string]any{"id": strconv.Itoa(i + 1), "subject": str(sm, "step"), "status": status})
		}
		e := model.New(model.Tasks, f).Set("list", list).Set("done", done).Set("total", total)
		if ex := str(params, "explanation"); ex != "" {
			e.Set("explanation", ex)
		}
		e.To = "tasks"
		return []model.Event{e}
	case "thread/tokenUsage/updated":
		tu := sub(params, "tokenUsage")
		last := sub(tu, "last")
		total := sub(tu, "total")
		if w := num(tu, "modelContextWindow"); w > 0 {
			p.ctxWindow = int64(w)
		}
		p.lastTotal = int64(num(total, "totalTokens"))
		e := model.New(model.UsageContext, f).Set("used", int64(num(last, "inputTokens")+num(last, "outputTokens")))
		if p.ctxWindow > 0 {
			e.Set("window", p.ctxWindow)
		}
		return []model.Event{e}
	case "account/rateLimits/updated":
		rl := sub(params, "rateLimits")
		windows := map[string]any{}
		name := func(w map[string]any, fallback string) string {
			switch int(num(w, "windowDurationMins")) {
			case 10080:
				return "weekly"
			case 300:
				return "5h"
			case 60:
				return "hourly"
			}
			return fallback
		}
		if w := sub(rl, "primary"); w != nil {
			windows[name(w, "primary")] = map[string]any{"utilization": num(w, "usedPercent") / 100, "resets_at": int64(num(w, "resetsAt")), "key": "primary"}
		}
		if w := sub(rl, "secondary"); w != nil {
			windows[name(w, "secondary")] = map[string]any{"utilization": num(w, "usedPercent") / 100, "resets_at": int64(num(w, "resetsAt")), "key": "secondary"}
		}
		e := model.New(model.UsageLimits, f).Set("windows", windows).Set("plan", str(rl, "planType"))
		if c := sub(rl, "credits"); c != nil && boolean(c, "hasCredits") {
			e.Set("credits", str(c, "balance"))
		}
		if r := str(rl, "rateLimitReachedType"); r != "" {
			e.Set("status", r)
		}
		return []model.Event{e}
	case "thread/goal/updated":
		g := sub(params, "goal")
		p.goal = &goal{cond: str(g, "objective"), state: goalState(str(g, "status"))}
		e := model.New(model.Goal, f)
		for k, v := range p.goalData() {
			e.Data[k] = v
		}
		e.Set("text", "Goal "+str(g, "status")+": "+str(g, "objective")).Set("status", str(g, "status"))
		if c := p.takeCmd("goal"); c != nil {
			p.goalRef = c.ref
		}
		e.To = p.goalRef
		return []model.Event{e}
	case "thread/goal/cleared":
		p.goal = nil
		e := model.New(model.Goal, f).Set("state", "cleared").Set("text", "Goal cleared")
		if c := p.takeCmd("goal"); c != nil {
			p.goalRef = c.ref
		}
		e.To = p.goalRef
		return []model.Event{e}
	case "item/autoApprovalReview/started":
		id := str(params, "reviewId")
		action := sub(params, "action")
		e := model.NewLabelled(model.ApprovalRequest, f, "auto review started").Set("id", id).Set("kind", "tool").Set("auto", true).Set("subtype", method)
		e.Ref = "approval:" + id
		tool, input := actionSummary(action)
		e.Set("tool", tool).Set("display", tool).Set("input", input)
		if target := str(params, "targetItemId"); target != "" {
			e.Set("call_id", target)
			if it := p.items[target]; it != nil {
				e.To = it.ref
			}
		}
		p.approvals[id] = &approval{ref: e.Ref, kind: "tool", method: method, auto: true, callID: str(params, "targetItemId")}
		p.autoRef = e.Ref
		return []model.Event{e}
	case "item/autoApprovalReview/completed":
		id := str(params, "reviewId")
		review := sub(params, "review")
		outcome := "denied"
		if str(review, "status") == "approved" {
			outcome = "allowed"
		}
		e := model.NewLabelled(model.ApprovalAnswer, f, "auto review completed").Set("id", id).Set("outcome", outcome).Set("auto", true).Set("by", str(params, "decisionSource"))
		if r := str(review, "rationale"); r != "" {
			e.Set("message", r)
		}
		if r := str(review, "riskLevel"); r != "" {
			e.Set("risk", r)
		}
		if a := p.approvals[id]; a != nil {
			a.done = true
			e.To = a.ref
		}
		return []model.Event{e}
	case "guardianWarning":
		if p.autoRef != "" {
			return []model.Event{model.FoldTo(f, p.autoRef, "auto review note")}
		}
		return []model.Event{model.New(model.Notice, f).Set("level", "info").Set("text", str(params, "message")).Set("subtype", method)}
	case "serverRequest/resolved":
		id := anyStr(params["requestId"])
		if a := p.approvals[id]; a != nil {
			return []model.Event{model.FoldTo(f, a.ref, "resolved")}
		}
		return []model.Event{model.FoldTo(f, "", "request resolved")}
	case "error":
		te := sub(params, "error")
		text := str(te, "message")
		if boolean(params, "willRetry") {
			e := model.New(model.APIRetry, f).Set("error", text)
			e.Ref = ref("apiretry", f)
			return []model.Event{e}
		}
		return []model.Event{model.New(model.Notice, f).Set("level", "error").Set("text", text).Set("subtype", "error")}
	case "warning":
		return []model.Event{model.New(model.Notice, f).Set("level", "warning").Set("text", str(params, "message")).Set("subtype", method)}
	case "model/rerouted":
		p.model = str(params, "toModel")
		return []model.Event{model.New(model.Notice, f).Set("level", "info").Set("text", "model rerouted from "+str(params, "fromModel")+" to "+str(params, "toModel")).Set("model", p.model).Set("subtype", method)}
	case "thread/settings/updated":
		return p.settingsUpdated(f, sub(params, "threadSettings"))
	case "thread/name/updated", "thread/status/changed", "mcpServer/startupStatus/updated", "remoteControl/status/changed",
		"deprecationNotice", "configWarning", "thread/closed", "thread/queue/changed", "skills/changed", "account/updated", "app/list/updated", "thread/environment/connected", "thread/environment/disconnected":
		return []model.Event{model.FoldTo(f, "", strings.TrimPrefix(method, "thread/"))}
	case "thread/compacted":
		if p.compact != nil {
			return []model.Event{model.FoldTo(f, p.compact.ref, "compacted")}
		}
		return []model.Event{model.FoldTo(f, "", "compacted")}
	}
	return []model.Event{model.New(model.Unknown, f).Set("kind", method).Set("text", method)}
}

// settingsUpdated: the thread reporting its settings (after every turn
// start). Each changed one becomes a setting event: confirming the marker
// the browser drew when it asked, or standing alone when the change came
// from elsewhere. The first report (before anything changed) folds away.
func (p *Projector) settingsUpdated(f model.Frame, ts map[string]any) []model.Event {
	sb := sub(ts, "sandboxPolicy")
	cur := map[string]string{
		"model":           str(ts, "model"),
		"effort":          str(ts, "effort"),
		"personality":     str(ts, "personality"),
		"service_tier":    str(ts, "serviceTier"),
		"permission_mode": ModeName(str(ts, "approvalPolicy"), str(sb, "type"), str(ts, "approvalsReviewer")),
	}
	prev := map[string]string{"model": p.model, "effort": p.effort, "personality": p.personality, "service_tier": p.tier}
	first := p.effort == "" && p.personality == "" && p.tier == ""
	p.model, p.effort, p.personality, p.tier = cur["model"], cur["effort"], cur["personality"], cur["service_tier"]
	var out []model.Event
	carried := false
	for _, key := range []string{"model", "effort", "personality", "service_tier", "permission_mode"} {
		val := cur[key]
		to := p.settingRef[key]
		changed := key != "permission_mode" && val != "" && val != prev[key]
		if to == "" && (!changed || first) {
			continue
		}
		delete(p.settingRef, key)
		var e model.Event
		switch key {
		case "effort":
			e = model.New(model.Effort, f).Set("value", val)
		case "service_tier":
			e = model.New(model.Fast, f).Set("on", val == "priority")
		default:
			e = model.New(model.Setting, f).Set("key", key).Set("value", val)
		}
		if carried {
			e.Raw = nil // the frame rides the first event
		}
		carried = true
		if to != "" {
			e.To = to
		} else {
			e.Ref = ref("setting", f)
		}
		out = append(out, e)
	}
	if !carried {
		return []model.Event{model.FoldTo(f, "", "settings")}
	}
	return out
}

// actionSummary names the tool and input of an automatic review's target.
func actionSummary(action map[string]any) (string, map[string]any) {
	switch str(action, "type") {
	case "applyPatch":
		return "apply_patch", map[string]any{"files": action["files"], "cwd": str(action, "cwd")}
	case "command", "execCommand":
		return "Bash", map[string]any{"command": firstStr(str(action, "command"), anyStr(action["command"])), "cwd": str(action, "cwd")}
	}
	if t := str(action, "type"); t != "" {
		return t, action
	}
	return "tool", action
}

// --- items ---

func (p *Projector) itemStarted(f model.Frame, params map[string]any) []model.Event {
	it := sub(params, "item")
	id := str(it, "id")
	typ := str(it, "type")
	switch typ {
	case "userMessage":
		text, images := userContent(arr(it, "content"))
		e := model.New(model.UserMessage, f).Set("id", str(params, "turnId")).Set("text", text).Set("steer", p.turnHasEcho)
		if len(images) > 0 {
			e.Set("images", images)
		}
		p.turnHasEcho = true
		e.Ref = ref("user", f)
		return []model.Event{e}
	case "agentMessage", "reasoning":
		return []model.Event{model.FoldTo(f, p.lastInput, typ+" started")}
	case "commandExecution":
		p.items[id] = &item{typ: typ, ref: "tool:" + id}
		e := model.New(model.ToolCall, f).Set("id", id).Set("name", "Bash").Set("input", map[string]any{"command": commandText(it), "cwd": str(it, "cwd")})
		e.Ref = "tool:" + id
		return []model.Event{e}
	case "fileChange":
		p.items[id] = &item{typ: typ, ref: "tool:" + id}
		var files []string
		for _, c := range arr(it, "changes") {
			cm, _ := c.(map[string]any)
			files = append(files, str(cm, "path"))
		}
		e := model.New(model.ToolCall, f).Set("id", id).Set("name", "apply_patch").Set("input", map[string]any{"files": files, "file_path": strings.Join(files, ", ")})
		e.Ref = "tool:" + id
		return []model.Event{e}
	case "mcpToolCall":
		p.items[id] = &item{typ: typ, ref: "tool:" + id}
		name := "mcp__" + str(it, "server") + "__" + str(it, "tool")
		e := model.New(model.ToolCall, f).Set("id", id).Set("name", name).Set("input", it["arguments"])
		if str(it, "server") == "acta" {
			e.Set("role", "acta")
		}
		e.Ref = "tool:" + id
		return []model.Event{e}
	case "webSearch":
		p.items[id] = &item{typ: typ, ref: "tool:" + id}
		e := model.New(model.ToolCall, f).Set("id", id).Set("name", "WebSearch").Set("input", map[string]any{"query": str(it, "query")})
		e.Ref = "tool:" + id
		return []model.Event{e}
	case "dynamicToolCall":
		p.items[id] = &item{typ: typ, ref: "tool:" + id}
		e := model.New(model.ToolCall, f).Set("id", id).Set("name", str(it, "tool")).Set("input", it["arguments"])
		e.Ref = "tool:" + id
		return []model.Event{e}
	case "plan":
		key := "plan:" + str(params, "turnId")
		p.items[id] = &item{typ: typ, ref: key}
		e := model.New(model.PlanUpdate, f).Set("key", key).Set("text", str(it, "text")).Set("state", "drafting")
		return []model.Event{e}
	case "contextCompaction":
		if p.compact == nil {
			return []model.Event{p.compactStart(f, "compaction started")}
		}
		p.compact.pre = p.lastTotal
		return []model.Event{model.FoldTo(f, p.compact.ref, "compaction started")}
	case "subAgentActivity", "collabAgentToolCall":
		p.items[id] = &item{typ: typ, ref: ref("agent", f)}
		e := model.New(model.SessionState, f).Set("text", "subagent "+firstStr(str(it, "kind"), str(it, "status"), "activity")+" · "+firstStr(str(it, "agentPath"), str(it, "agentThreadId"), strings.Join(strs(arr(it, "receiverThreadIds")), ", ")))
		e.Ref = p.items[id].ref
		return []model.Event{e}
	}
	e := model.New(model.SessionState, f).Set("text", "item "+typ)
	return []model.Event{e}
}

func (p *Projector) itemCompleted(f model.Frame, params map[string]any) []model.Event {
	it := sub(params, "item")
	id := str(it, "id")
	typ := str(it, "type")
	known := p.items[id]
	switch typ {
	case "userMessage":
		return []model.Event{model.FoldTo(f, "", "message")}
	case "agentMessage":
		text := str(it, "text")
		e := model.New(model.Assistant, f).Set("model", p.model).Set("blocks", []map[string]any{{"type": "text", "text": text}}).Set("phase", str(it, "phase"))
		if qs := arr(it, "questions"); len(qs) > 0 {
			e.Set("questions", qs)
		}
		return []model.Event{e}
	case "reasoning":
		summary := strings.TrimSpace(strings.Join(strs(arr(it, "summary")), "\n\n"))
		content := strings.TrimSpace(strings.Join(strs(arr(it, "content")), "\n\n"))
		e := model.New(model.Thought, f)
		if text := firstStr(summary, content); text != "" {
			e.Set("text", text)
		}
		return []model.Event{e}
	case "commandExecution":
		status := str(it, "status")
		e := model.New(model.ToolResult, f).Set("call_id", id).Set("name", "Bash").Set("text", str(it, "aggregatedOutput")).Set("error", status == "failed" || status == "declined").Set("status", status)
		if it["exitCode"] != nil {
			e.Set("exit_code", int(num(it, "exitCode")))
		}
		if d := num(it, "durationMs"); d > 0 {
			e.Set("duration_ms", int64(d))
		}
		if known != nil {
			e.To = known.ref
		}
		return []model.Event{e}
	case "fileChange":
		status := str(it, "status")
		var diffs []map[string]any
		for _, c := range arr(it, "changes") {
			cm, _ := c.(map[string]any)
			kind := str(sub(cm, "kind"), "type")
			d := map[string]any{"file": str(cm, "path"), "kind": "unified", "text": str(cm, "diff")}
			if kind == "add" {
				d["kind"] = "create"
				d["content"] = str(cm, "diff")
			} else if kind == "delete" {
				d["deleted"] = true
			}
			if mv := str(sub(cm, "kind"), "move_path"); mv != "" {
				d["moved_to"] = mv
			}
			diffs = append(diffs, d)
		}
		e := model.New(model.ToolResult, f).Set("call_id", id).Set("name", "apply_patch").Set("error", status == "failed" || status == "declined").Set("status", status).Set("diffs", diffs).Set("text", status)
		if len(diffs) == 1 {
			e.Set("diff", diffs[0])
		}
		if known != nil {
			e.To = known.ref
		}
		return []model.Event{e}
	case "mcpToolCall":
		name := "mcp__" + str(it, "server") + "__" + str(it, "tool")
		res := sub(it, "result")
		var sb strings.Builder
		for _, c := range arr(res, "content") {
			cm, _ := c.(map[string]any)
			sb.WriteString(str(cm, "text"))
		}
		txt := sb.String()
		errObj := sub(it, "error")
		e := model.New(model.ToolResult, f).Set("call_id", id).Set("name", name).Set("text", txt).Set("error", errObj != nil || str(it, "status") == "failed")
		if str(it, "server") == "acta" {
			e.Set("role", "acta")
		}
		if sc := res["structuredContent"]; sc != nil {
			e.Set("data", sc)
		} else {
			var data any
			if json.Unmarshal([]byte(strings.TrimSpace(txt)), &data) == nil && data != nil {
				e.Set("data", data)
			}
		}
		if errObj != nil && txt == "" {
			e.Set("text", firstStr(str(errObj, "message"), anyStr(errObj)))
		}
		if known != nil {
			e.To = known.ref
		}
		return []model.Event{e}
	case "webSearch":
		var lines []string
		for _, r := range arr(it, "results") {
			rm, _ := r.(map[string]any)
			lines = append(lines, firstStr(str(rm, "title"), anyStr(r))+" "+str(rm, "url"))
		}
		e := model.New(model.ToolResult, f).Set("call_id", id).Set("name", "WebSearch").Set("text", strings.Join(lines, "\n"))
		if known != nil {
			e.To = known.ref
		}
		return []model.Event{e}
	case "dynamicToolCall":
		e := model.New(model.ToolResult, f).Set("call_id", id).Set("name", str(it, "tool")).Set("text", anyStr(it["contentItems"])).Set("error", it["success"] == false)
		if known != nil {
			e.To = known.ref
		}
		return []model.Event{e}
	case "plan":
		key := "plan:" + str(params, "turnId")
		e := model.New(model.PlanUpdate, f).Set("key", key).Set("text", str(it, "text")).Set("state", "drafting")
		e.To = key
		return []model.Event{e}
	case "contextCompaction":
		if p.compact == nil {
			return []model.Event{model.FoldTo(f, "", "compaction")}
		}
		e := model.NewLabelled(model.CompactEnd, f, "compaction completed").Set("ok", true)
		if p.compact.pre > 0 {
			e.Set("pre", p.compact.pre)
		}
		e.To = p.compact.ref
		// the turn/completed that follows closes it (see notification)
		return []model.Event{e}
	}
	if known != nil {
		return []model.Event{model.FoldTo(f, known.ref, typ+" completed")}
	}
	return []model.Event{model.FoldTo(f, "", typ+" completed")}
}

func (p *Projector) compactStart(f model.Frame, label string) model.Event {
	e := model.NewLabelled(model.CompactStart, f, label)
	e.Ref = ref("compact", f)
	p.compact = &compaction{ref: e.Ref, pre: p.lastTotal}
	if c := p.takeCmd("compact"); c != nil {
		e.To = c.ref
		e.Set("args", c.args)
	}
	return e
}

// commandText strips the shell wrapper Codex runs commands through.
func commandText(it map[string]any) string {
	cmd := str(it, "command")
	for _, a := range arr(it, "commandActions") {
		am, _ := a.(map[string]any)
		if c := str(am, "command"); c != "" {
			return c
		}
	}
	if m := regexp.MustCompile(`^\S*bash -lc '(.*)'$`).FindStringSubmatch(cmd); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`^\S*bash -lc (.*)$`).FindStringSubmatch(cmd); m != nil {
		return m[1]
	}
	return cmd
}

var dataURLRe = regexp.MustCompile(`^data:([^;]+);base64,(.*)$`)

// userContent splits a user item's content into text and images.
func userContent(content []any) (string, []any) {
	var texts []string
	var images []any
	for _, c := range content {
		cm, _ := c.(map[string]any)
		switch str(cm, "type") {
		case "text":
			texts = append(texts, str(cm, "text"))
		case "image":
			if m := dataURLRe.FindStringSubmatch(str(cm, "url")); m != nil {
				images = append(images, map[string]any{"media_type": m[1], "data": m[2]})
			} else {
				texts = append(texts, fmt.Sprintf("[image %s]", str(cm, "url")))
			}
		case "localImage":
			texts = append(texts, fmt.Sprintf("[image %s]", str(cm, "path")))
		case "skill":
			texts = append(texts, "/"+str(cm, "name"))
		}
	}
	return strings.Join(texts, "\n"), images
}
