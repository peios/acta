// Package claude projects Claude Code's stream-json transcript into the common
// event model. Everything the browser used to infer from frame order — which
// synthetic message answers which local command, when a compaction starts and
// ends, which tool_result belongs to which call, which user turn is a peer
// message — is decided here, once, in Go.
package claude

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/peios/acta/internal/agentsession/model"
)

// Projector is the per-session state machine. It is not safe for concurrent
// use; the hub serialises frames per session.
type Projector struct {
	seq int64 // last stored frame seen, for refs minted from a frame

	toolNames map[string]string // call id -> tool name
	toolRoles map[string]string // call id -> role (agent, question, peer, acta, task, plan)
	denied    map[string]bool   // call ids Claude Code refused itself

	// local commands ("/x") awaiting their reply and closing result, oldest
	// first. Claude Code answers in the order it reads them, which need not be
	// the order they were sent, so replies match by shape first, then age.
	cmds []*cmd

	// approvals outstanding, by request id, so an answer (ours or Claude's
	// echo of it) resolves once and later ones fold.
	approvals map[string]*approval
	askCalls  map[string]string // request id -> call id for AskUserQuestion prompts

	compact    *compaction
	interrupt  *model.Frame      // the "[Request interrupted]" user frame awaiting its result
	afterStop  string            // ref of the interrupted turn divider, for the exit that follows
	lastInput  string            // ref of the latest input bubble (for a delivery failure, or a turn's init)
	sessionRef string            // ref of the session started/resumed divider awaiting its init
	lastMode   string            // ref of the latest setting marker awaiting its confirmation
	settingIDs map[string]string // control request id -> setting ref

	// The current turn: has a message entered it (later echoes are steers)?
	turnHasEcho bool
	turnActive  bool
	importTurn  bool // a turn opened by a transcript record, closed by synthesis (see transcript.go)

	hooks            map[string]bool // hook ids seen (a duplicate response folds)
	peerRef          string          // latest peer bubble, for the lifecycle frame that closes it
	pendingLifecycle *model.Frame

	plans    map[string]*plan
	planCall map[string]*plan // call id -> plan
	curPlan  *plan

	tasks     map[string]*task
	taskCalls map[string]*taskCall
	tasksDone bool

	lanes    map[string]*lane  // agent call id -> lane
	taskLane map[string]*lane  // task id -> lane
	taskPill map[string]string // shell task id -> call id

	apiRetry *apiRetry

	goal *goal

	// context accounting: the last prompt size and the window, per lane
	ctxWindow int64

	model string // the model the session runs on, from init
}

type cmd struct {
	ref, name, args, kind string
	replied, silent       bool
}

type approval struct {
	ref, kind, callID string
	done              bool
	plan              *plan
}

type compaction struct {
	ref     string
	done    bool
	summary bool
	cmdRef  string
}

type plan struct {
	key, ref, exitID, reqID, feedback, state, text string
	revisions                                      int
}

type task struct {
	id, subject, description, status, activeForm string
}
type taskCall struct {
	name, tmp string
	input     map[string]any
}

type lane struct {
	id, taskID, status, typ, desc string
	ref, doneRef                  string
	started                       bool
}

type apiRetry struct {
	ref   string
	count int
}

type goal struct {
	cond, state, last string
	turns             int
	overridden        bool
}

// New returns a projector for one Claude Code session.
func New() *Projector {
	return &Projector{
		toolNames: map[string]string{}, toolRoles: map[string]string{}, denied: map[string]bool{},
		approvals: map[string]*approval{}, askCalls: map[string]string{}, settingIDs: map[string]string{},
		hooks: map[string]bool{}, plans: map[string]*plan{}, planCall: map[string]*plan{},
		tasks: map[string]*task{}, taskCalls: map[string]*taskCall{},
		lanes: map[string]*lane{}, taskLane: map[string]*lane{}, taskPill: map[string]string{},
	}
}

// --- helpers over raw JSON ---

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
	switch v := m[k].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}
func has(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	_, ok := m[k]
	return ok
}
func boolean(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}

// blockText joins the text of a content array (or returns a string as is).
func blockText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, b := range v {
			if m, ok := b.(map[string]any); ok && str(m, "type") == "text" {
				sb.WriteString(str(m, "text"))
			}
		}
		return sb.String()
	}
	return ""
}

func ref(kind string, f model.Frame) string { return kind + ":" + strconv.FormatInt(f.Seq, 10) }

func (p *Projector) lastCmdRef() string {
	if len(p.cmds) == 0 {
		return ""
	}
	return p.cmds[len(p.cmds)-1].ref
}

// --- the projection ---

// Project maps one frame to its events.
func (p *Projector) Project(f model.Frame) []model.Event {
	if f.Stored {
		p.seq = f.Seq
	}
	var out []model.Event
	switch f.Kind {
	case "input":
		out = p.input(f)
	case "control":
		out = p.control(f)
	case "control_response":
		out = p.controlResponse(f)
	case "control_request":
		out = p.controlRequest(f)
	case "assistant":
		out = p.assistant(f)
	case "user":
		out = p.user(f)
	case "result":
		out = p.result(f)
	case "system":
		out = p.system(f)
	case "rate_limit_event":
		out = p.rateLimit(f)
	case "state":
		out = p.state(f)
	case "rewind":
		e := model.New(model.Rewind, f)
		e.Data = obj(f.Payload)
		out = []model.Event{e}
	case "task_output":
		m := obj(f.Payload)
		e := model.New(model.ToolOutput, f).Set("task_id", str(m, "task_id")).Set("text", str(m, "text")).Set("done", boolean(m, "done"))
		if id := p.taskPill[str(m, "task_id")]; id != "" {
			e.To = "tool:" + id
		}
		e.Live = !boolean(m, "done")
		out = []model.Event{e}
	case "conversation_reset":
		out = p.reset(f)
	case "command_lifecycle":
		out = p.lifecycle(f)
	case "tool_progress":
		out = p.toolProgress(f)
	case "stream_event":
		out = p.stream(f)
	case TranscriptKind:
		out = p.transcript(f)
	default:
		e := model.New(model.Unknown, f).Set("kind", f.Kind)
		out = []model.Event{e}
	}
	for i := range out {
		out[i].Sub = i
	}
	return out
}

// --- input (the browser's message) ---

var slashRe = regexp.MustCompile(`^/([\w:-]+)(?:\s+([\s\S]*))?$`)

func cmdKind(name string) string {
	switch name {
	case "clear", "compact", "goal", "effort", "fast":
		return name
	case "context", "usage", "cost", "stats", "autocompact":
		return "report"
	}
	return "cmd"
}

// reportKey is the panel key a report command feeds.
func reportKey(name string) string {
	switch name {
	case "context":
		return "context"
	case "usage", "cost", "stats":
		return "usage"
	case "autocompact":
		return "autocompact"
	}
	return ""
}

func (p *Projector) input(f model.Frame) []model.Event {
	m := obj(f.Payload)
	text := str(m, "text")
	e := model.New(model.Input, f).Set("text", text)
	if imgs := arr(m, "images"); len(imgs) > 0 {
		e.Set("images", imgs)
	}
	e.Ref = ref("input", f)
	p.turnActive = true
	// whichever node this input becomes (a bubble, a command marker, or the
	// host a silent report folded into) is where the turn's init lands
	defer func() { p.lastInput = e.Ref }()
	if sm := slashRe.FindStringSubmatch(strings.TrimSpace(text)); sm != nil && len(arr(m, "images")) == 0 {
		name, args := sm[1], strings.TrimSpace(sm[2])
		kind := cmdKind(name)
		c := &cmd{ref: ref("cmd", f), name: name, args: args, kind: kind}
		e.Ref = c.ref
		e.Set("cmd", map[string]any{"name": name, "args": args, "kind": kind})
		if kind == "report" {
			// a report the context panel renders takes no room in the
			// transcript: its input, reply and result fold into the frame above
			c.silent = true
			e.T = model.Fold
			e.Raw[0].Label = "/" + name
			e.To = ""
			e.Data = map[string]any{"cmd": map[string]any{"name": name, "args": args, "kind": kind}}
		}
		p.cmds = append(p.cmds, c)
		if kind == "effort" {
			if lvl := strings.Fields(args); len(lvl) == 1 {
				side := model.Event{T: model.Effort, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), To: c.ref, Data: map[string]any{"value": lvl[0]}}
				return []model.Event{e, side}
			}
		}
		return []model.Event{e}
	}
	return []model.Event{e}
}

// --- control (browser-authored) ---

func (p *Projector) control(f model.Frame) []model.Event {
	m := obj(f.Payload)
	if op := str(m, "op"); op != "" {
		return p.browserOp(f, m, op)
	}
	// older transcripts hold Claude Code's own control shapes
	switch str(m, "type") {
	case "control_response":
		resp := sub(m, "response")
		rid := str(resp, "request_id")
		r := sub(resp, "response")
		return p.approvalAnswer(f, rid, r, true)
	case "control_request":
		req := sub(m, "request")
		rid := str(m, "request_id")
		st := str(req, "subtype")
		switch st {
		case "set_permission_mode":
			e := model.New(model.Setting, f).Set("key", "permission_mode").Set("value", str(req, "mode")).Set("requested", true)
			e.Ref = ref("setting", f)
			p.settingIDs[rid] = e.Ref
			p.lastMode = e.Ref
			return []model.Event{e}
		case "set_model":
			e := model.New(model.Setting, f).Set("key", "model").Set("value", str(req, "model")).Set("requested", true)
			e.Ref = ref("setting", f)
			p.settingIDs[rid] = e.Ref
			p.lastMode = e.Ref
			return []model.Event{e}
		case "update_settings":
			if s := sub(req, "settings"); s != nil && str(s, "outputStyle") != "" {
				e := model.New(model.Setting, f).Set("key", "output_style").Set("value", str(s, "outputStyle")).Set("requested", true)
				e.Ref = ref("setting", f)
				p.settingIDs[rid] = e.Ref
				p.lastMode = e.Ref
				return []model.Event{e}
			}
		case "list_models", "get_settings", "initialize", "rewind_conversation", "rewind_files", "side_question", "interrupt":
			return []model.Event{model.FoldTo(f, "", strings.ReplaceAll(st, "_", " "))}
		}
		e := model.New(model.Unknown, f).Set("kind", "control").Set("text", "control request: "+strings.ReplaceAll(st, "_", " "))
		return []model.Event{e}
	}
	return []model.Event{model.New(model.Unknown, f).Set("kind", "control")}
}

// browserOp projects a neutral browser operation (see agentsession.BrowserOp).
func (p *Projector) browserOp(f model.Frame, m map[string]any, op string) []model.Event {
	id := str(m, "id")
	switch op {
	case "answer":
		// the same resolution the driver's control_response gets
		r := map[string]any{}
		switch str(m, "outcome") {
		case "accept", "decline", "cancel":
			r["action"] = str(m, "outcome")
			if c, ok := m["content"]; ok {
				r["content"] = c
			}
		case "deny":
			r["behavior"] = "deny"
			r["message"] = str(m, "message")
		default:
			r["behavior"] = "allow"
			in := sub(m, "input")
			if in == nil {
				in = map[string]any{}
			}
			if a := sub(m, "answers"); a != nil {
				in["answers"] = a
			}
			r["updatedInput"] = in
			if perms, ok := m["permissions"]; ok {
				r["updatedPermissions"] = perms
			}
		}
		return p.approvalAnswer(f, id, r, true)
	case "setting":
		key, val := str(m, "key"), str(m, "value")
		switch key {
		case "effort":
			return []model.Event{model.FoldTo(f, "", "effort")}
		case "fast":
			return []model.Event{model.FoldTo(f, "", "fast")}
		}
		e := model.New(model.Setting, f).Set("key", key).Set("value", val).Set("requested", true)
		e.Ref = ref("setting", f)
		if id != "" {
			p.settingIDs[id] = e.Ref
		}
		p.lastMode = e.Ref
		return []model.Event{e}
	case "catalog", "rewind", "rewind_files", "side_question":
		return []model.Event{model.FoldTo(f, "", strings.ReplaceAll(op, "_", " "))}
	}
	return []model.Event{model.New(model.Unknown, f).Set("kind", "control").Set("text", "browser operation: "+op)}
}

// approvalAnswer resolves an approval from an answer: ours (mine=true) or
// Claude Code's echo. The first resolution wins; later ones fold.
func (p *Projector) approvalAnswer(f model.Frame, rid string, r map[string]any, mine bool) []model.Event {
	a := p.approvals[rid]
	if a == nil {
		if rid == "" {
			return []model.Event{model.New(model.Unknown, f).Set("kind", "control").Set("text", "control response")}
		}
		// an answer to an approval we never saw (a replay starting late)
		e := model.New(model.ApprovalAnswer, f).Set("id", rid).Set("mine", mine)
		fillOutcome(e, r, "", false)
		return []model.Event{e}
	}
	if a.done {
		return []model.Event{model.FoldTo(f, a.ref, "answer")}
	}
	a.done = true
	e := model.New(model.ApprovalAnswer, f).Set("id", rid).Set("mine", mine)
	e.To = a.ref
	fillOutcome(e, r, a.kind, a.plan != nil)
	if a.plan != nil {
		out, _ := e.Data["outcome"].(string)
		st := "stale"
		if out == "allowed" {
			st = "approved"
		} else if out == "denied" {
			st = "rejected"
		}
		a.plan.state = st
		a.plan.reqID = ""
		if st == "rejected" {
			if msg := str(r, "message"); msg != "" {
				a.plan.feedback = msg
			}
		}
		v := model.New(model.PlanVerdict, f).Set("key", a.plan.key).Set("state", st).Set("feedback", a.plan.feedback)
		v.To = a.plan.ref
		v.Raw = nil
		return []model.Event{e, v}
	}
	if a.kind == "question" {
		if ans := sub(sub(r, "updatedInput"), "answers"); ans != nil {
			q := model.New(model.QuestionAnswer, f).Set("call_id", a.callID).Set("answers", ans)
			q.To = "tool:" + a.callID
			q.Raw = nil
			return []model.Event{e, q}
		}
	}
	return []model.Event{e}
}

func str2(v any) string { s, _ := v.(string); return s }

// fillOutcome sets outcome/answers/message/content on an approval.answer.
func fillOutcome(e model.Event, r map[string]any, kind string, isPlan bool) {
	action := str2(r["action"])
	behavior := str2(r["behavior"])
	answers := sub(sub(r, "updatedInput"), "answers")
	out := ""
	switch {
	case action == "accept":
		out = "answered"
	case action == "decline":
		out = "declined"
	case action == "cancel":
		out = "cancelled"
	case behavior == "allow" && answers != nil:
		out = "answered"
	case behavior == "allow":
		out = "allowed"
	case behavior == "deny" && kind == "question":
		out = "skipped"
	case behavior == "deny":
		out = "denied"
	default:
		out = "answered"
	}
	e.Set("outcome", out)
	if answers != nil {
		e.Set("answers", answers)
	}
	if msg := str2(r["message"]); msg != "" {
		e.Set("message", msg)
	}
	if c, ok := r["content"]; ok {
		e.Set("content", c)
	}
	if perms, ok := r["updatedPermissions"]; ok {
		e.Set("permissions", perms)
	}
}

// --- control_response (from Claude Code) ---

func (p *Projector) controlResponse(f model.Frame) []model.Event {
	m := obj(f.Payload)
	resp := sub(m, "response")
	rid := str(resp, "request_id")
	r := sub(resp, "response")
	isErr := str(resp, "subtype") == "error"
	switch {
	case strings.HasPrefix(rid, "rw-"):
		e := model.New(model.Reply, f).Set("id", rid)
		if isErr {
			e.Set("error", str(resp, "error"))
		} else {
			e.Set("response", r)
		}
		e.To = ""
		return []model.Event{e}
	case strings.HasPrefix(rid, "init-"):
		e := model.New(model.SessionCatalog, f)
		if r != nil {
			if v, ok := r["models"]; ok {
				e.Set("models", v)
			}
			if v, ok := r["commands"]; ok {
				e.Set("commands", v)
			}
			if v, ok := r["fast_mode_state"]; ok {
				e.Set("fast_mode", v)
			}
			if v, ok := r["fast_mode_disabled_reason"]; ok {
				e.Set("fast_reason", v)
			}
			if v, ok := r["output_style"]; ok {
				e.Set("output_style", v)
			}
			if v, ok := r["available_output_styles"]; ok {
				e.Set("output_styles", v)
			}
			if v, ok := r["current_permission_mode"]; ok {
				e.Set("permission_mode", v)
			}
		}
		e.Raw[0].Label = "initialize"
		return []model.Event{e}
	case strings.HasPrefix(rid, "models-") || (r != nil && r["models"] != nil):
		e := model.New(model.SessionCatalog, f).Set("models", r["models"])
		e.Raw[0].Label = "models"
		return []model.Event{e}
	case strings.HasPrefix(rid, "settings-"):
		e := model.New(model.SessionCatalog, f)
		if eff := sub(r, "effective"); eff != nil {
			e.Set("default_effort", str(eff, "effortLevel"))
		}
		e.Raw[0].Label = "settings"
		return []model.Event{e}
	case strings.HasPrefix(rid, "interrupt-"):
		return []model.Event{model.FoldTo(f, "", "interrupt response")}
	}
	if sref := p.settingIDs[rid]; sref != "" {
		// the answer to a mode / model / style change: confirms it
		e := model.New(model.Setting, f)
		e.To = sref
		if r != nil && str(r, "mode") != "" {
			e.Set("key", "permission_mode").Set("value", str(r, "mode"))
		} else {
			e.T = model.Fold
			e.Raw[0].Label = "response"
			e.Data = nil
		}
		if isErr {
			e.T = model.Setting
			e.Set("error", str(resp, "error"))
		}
		return []model.Event{e}
	}
	if r != nil && str(r, "mode") != "" {
		e := model.New(model.Setting, f).Set("key", "permission_mode").Set("value", str(r, "mode"))
		e.Ref = ref("setting", f)
		p.lastMode = e.Ref
		return []model.Event{e}
	}
	if rid != "" && p.approvals[rid] != nil {
		return p.approvalAnswer(f, rid, r, false)
	}
	return []model.Event{model.New(model.Unknown, f).Set("kind", "control_response").Set("text", "control response")}
}

// --- control_request (from Claude Code: permissions, questions, elicitation) ---

func (p *Projector) controlRequest(f model.Frame) []model.Event {
	m := obj(f.Payload)
	req := sub(m, "request")
	rid := str(m, "request_id")
	st := str(req, "subtype")
	e := model.New(model.ApprovalRequest, f).Set("id", rid).Set("subtype", st)
	e.Ref = "approval:" + rid
	a := &approval{ref: e.Ref}
	switch st {
	case "elicitation":
		a.kind = "elicitation"
		e.Set("kind", "elicitation").Set("server", str(req, "mcp_server_name")).Set("message", str(req, "message")).Set("mode", str(req, "mode")).Set("url", str(req, "url"))
		if s := req["requested_schema"]; s != nil {
			e.Set("schema", s)
		}
	case "can_use_tool":
		tool := str(req, "tool_name")
		input := sub(req, "input")
		callID := str(req, "tool_use_id")
		a.callID = callID
		e.Set("tool", tool).Set("display", str(req, "display_name")).Set("description", str(req, "description")).Set("call_id", callID)
		if input != nil {
			e.Set("input", input)
		}
		if s := req["permission_suggestions"]; s != nil {
			e.Set("suggestions", s)
		}
		switch tool {
		case "AskUserQuestion":
			a.kind = "question"
			e.Set("kind", "question")
			if q := arr(input, "questions"); q != nil {
				e.Set("questions", q)
			}
			p.askCalls[rid] = callID
			if callID != "" && p.toolNames[callID] != "" {
				e.To = "tool:" + callID
			}
		case "ExitPlanMode":
			a.kind = "plan"
			e.Set("kind", "plan")
			pl := p.planCall[callID]
			if pl == nil {
				if fp := str(input, "planFilePath"); fp != "" {
					pl = p.planFor(fp)
				} else if p.curPlan != nil {
					pl = p.curPlan
				} else {
					pl = p.planFor(callID)
				}
			}
			if t := str(input, "plan"); t != "" {
				pl.text = t
			}
			if pl.exitID == "" {
				pl.exitID = callID
			}
			pl.state = "pending"
			pl.reqID = rid
			p.curPlan = pl
			a.plan = pl
			e.Set("plan_key", pl.key).Set("plan_text", pl.text)
			if pl.ref == "" {
				pl.ref = "plan:" + pl.key
				e.Set("plan_new", true)
			} else {
				e.To = pl.ref
			}
		default:
			a.kind = "tool"
			e.Set("kind", "tool")
			if callID != "" && p.toolNames[callID] != "" {
				e.To = "tool:" + callID
			}
		}
	default:
		a.kind = "other"
		e.Set("kind", "other")
	}
	p.approvals[rid] = a
	return []model.Event{e}
}

// --- assistant ---

var (
	effortReplyRe = regexp.MustCompile(`^Set effort level to (\w+)`)
	fastReplyRe   = regexp.MustCompile(`(?i)^Fast mode (enabled|disabled|unavailable|on|off)`)
	goalSetRe     = regexp.MustCompile(`^Goal set:\s*([\s\S]+)$`)
	goalActiveRe  = regexp.MustCompile(`^Goal active:\s*([\s\S]*?)\s*\((\d+) turns?\)\s*(?:Last check:\s*([\s\S]*))?$`)
	goalMetRe     = regexp.MustCompile(`(?i)^Goal (?:met|reached|completed?|satisfied|achieved|done)\b[:\s]*([\s\S]*)$`)
	noGoalRe      = regexp.MustCompile(`(?i)^(No goal set|Goal cleared)`)
	acWindowRe    = regexp.MustCompile(`(?i)Auto-compact window(?: set to|:)\s*([\w.,]+)`)
	acSetRe       = regexp.MustCompile(`(?i)^Set Auto-compact to (true|false)`)
	cmdErrRe      = regexp.MustCompile(`(?i)isn't available in this environment|^Unknown command|^Usage:`)
)

var replyRe = map[string]*regexp.Regexp{
	"rename": regexp.MustCompile(`^Session renamed`), "name": regexp.MustCompile(`^Session renamed`),
	"model": regexp.MustCompile(`^(Set model|Current model)`), "effort": regexp.MustCompile(`^Set effort`), "fast": regexp.MustCompile(`^Fast mode`),
	"context": regexp.MustCompile(`Context Usage`), "usage": regexp.MustCompile(`(?i)(subscription|usage|limits)`), "cost": regexp.MustCompile(`(?i)(subscription|usage|limits)`), "stats": regexp.MustCompile(`(?i)(subscription|usage|limits)`),
	"autocompact": regexp.MustCompile(`^Auto-compact window`), "config": regexp.MustCompile(`^(Set |Usage: /config)`), "goal": regexp.MustCompile(`^(Goal |No goal)`), "compact": regexp.MustCompile(`(?i)compact`), "color": regexp.MustCompile(`^Session color`), "mcp": regexp.MustCompile(`MCP server`),
}

// cmdPick matches a synthetic reply to the command it answers.
func (p *Projector) cmdPick(txt, kindHint string) *cmd {
	var open []*cmd
	for _, c := range p.cmds {
		if !c.replied {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return nil
	}
	if kindHint != "" {
		for _, c := range open {
			if c.kind == kindHint {
				return c
			}
		}
		return nil
	}
	for _, c := range open {
		if re := replyRe[c.name]; re != nil && re.MatchString(txt) {
			return c
		}
	}
	for _, c := range open {
		if replyRe[c.name] == nil {
			return c
		}
	}
	return open[0]
}

func (p *Projector) cmdForget(c *cmd) {
	for i, x := range p.cmds {
		if x == c {
			p.cmds = append(p.cmds[:i], p.cmds[i+1:]...)
			return
		}
	}
}

func isSynthetic(m map[string]any) bool {
	return boolean(m, "is_meta") || str(sub(m, "message"), "model") == "<synthetic>"
}

func (p *Projector) assistant(f model.Frame) []model.Event {
	m := obj(f.Payload)
	msg := sub(m, "message")
	content := arr(msg, "content")
	synthetic := isSynthetic(m)
	laneID := str(m, "parent_tool_use_id")
	allText := len(content) > 0
	for _, b := range content {
		if str(b.(map[string]any), "type") != "text" {
			allText = false
		}
	}
	if !synthetic && len(p.cmds) > 0 {
		// a real model turn (a skill, or a goal's work) ends the local-command
		// run: the result that follows belongs to the turn, not to a marker
		p.cmds = nil
	}
	if synthetic && (p.compact == nil || !p.compact.done) && allText && p.compact == nil {
		txt := strings.TrimSpace(blockText(content))
		if em := effortReplyRe.FindStringSubmatch(txt); em != nil {
			c := p.cmdPick(txt, "effort")
			e := model.New(model.Effort, f).Set("value", em[1])
			if c != nil {
				c.replied = true
				e.To = c.ref
			} else {
				// asked through the picker (no input marker): the reply is the
				// marker, and the empty result that follows folds into it
				e.Ref = ref("cmd", f)
				p.cmds = append(p.cmds, &cmd{ref: e.Ref, name: "effort", kind: "effort", replied: true})
			}
			return []model.Event{e}
		}
		if fm := fastReplyRe.FindStringSubmatch(txt); fm != nil {
			c := p.cmdPick(txt, "fast")
			on := regexp.MustCompile(`(?i)enabled|on\b`).MatchString(fm[1])
			un := strings.EqualFold(fm[1], "unavailable")
			e := model.New(model.Fast, f).Set("on", on && !un).Set("unavailable", un)
			if un {
				e.Set("reason", strings.TrimSpace(strings.TrimPrefix(txt, "Fast mode unavailable:")))
			}
			if c != nil {
				c.replied = true
				e.To = c.ref
			} else {
				e.Ref = ref("cmd", f)
				p.cmds = append(p.cmds, &cmd{ref: e.Ref, name: "fast", kind: "fast", replied: true})
			}
			return []model.Event{e}
		}
		if c := p.cmdPick(txt, ""); c != nil {
			return p.cmdReply(f, c, txt)
		}
		// a reply whose command is no longer open (it ran after a real turn
		// cleared the queue, e.g. the rename Acta sends at spawn): still a
		// command's answer, shown as a marker the closing result folds into
		for name, re := range replyRe {
			if re.MatchString(txt) && name != "usage" && name != "cost" && name != "stats" && name != "compact" {
				c := &cmd{ref: ref("cmd", f), name: name, kind: cmdKind(name), replied: true}
				p.cmds = append(p.cmds, c)
				e := model.NewLabelled(model.CmdReply, f, "reply").Set("text", txt).Set("kind", c.kind).Set("name", name).Set("error", cmdErrRe.MatchString(txt)).Set("standalone", true)
				e.Ref = c.ref
				if name == "goal" {
					g := p.goalEvent(f, txt, "")
					g.Raw = nil
					return []model.Event{e, g}
				}
				return []model.Event{e}
			}
		}
		if goalSetRe.MatchString(txt) || goalActiveRe.MatchString(txt) || noGoalRe.MatchString(txt) || goalMetRe.MatchString(txt) {
			return []model.Event{p.goalEvent(f, txt, "")}
		}
	}
	if p.compact != nil && p.compact.done && synthetic {
		txt := strings.TrimSpace(blockText(content))
		label := "replayed command output"
		if len(txt) <= 200 && !strings.Contains(txt, "\n") {
			label = "message"
		}
		return []model.Event{model.FoldTo(f, p.compact.ref, label)}
	}
	// context accounting: the prompt size of a real message
	var extra []model.Event
	if !synthetic {
		if u := sub(msg, "usage"); u != nil {
			used := int64(num(u, "input_tokens") + num(u, "cache_read_input_tokens") + num(u, "cache_creation_input_tokens"))
			if used > 0 {
				ce := model.Event{T: model.UsageContext, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Lane: laneID, Data: map[string]any{"used": used}}
				if p.ctxWindow > 0 {
					ce.Data["window"] = p.ctxWindow
				}
				extra = append(extra, ce)
			}
		}
	}
	if allText == false && len(content) > 0 {
		onlyThinking := true
		for _, b := range content {
			if str(b.(map[string]any), "type") != "thinking" {
				onlyThinking = false
			}
		}
		if onlyThinking {
			b := content[0].(map[string]any)
			e := model.New(model.Thought, f).With("", "", laneID)
			if t := str(b, "thinking"); t != "" {
				e.Set("text", t)
			}
			return append([]model.Event{e}, extra...)
		}
	}
	e := model.New(model.Assistant, f).With("", "", laneID)
	if !synthetic {
		e.Set("model", str(msg, "model"))
	} else {
		e.Set("synthetic", true)
	}
	var blocks []map[string]any
	var more []model.Event
	for _, raw := range content {
		b, _ := raw.(map[string]any)
		switch str(b, "type") {
		case "text":
			blocks = append(blocks, map[string]any{"type": "text", "text": str(b, "text")})
		case "thinking":
			blk := map[string]any{"type": "thinking"}
			if t := str(b, "thinking"); t != "" {
				blk["text"] = t
			}
			blocks = append(blocks, blk)
		case "tool_use":
			blk, side := p.toolUse(f, b, laneID)
			if blk != nil {
				blocks = append(blocks, blk)
			}
			more = append(more, side...)
		default:
			blocks = append(blocks, map[string]any{"type": str(b, "type")})
		}
	}
	e.Set("blocks", blocks)
	if len(blocks) == 0 && len(more) > 0 {
		// every block folded into an existing card: the frame is only its payload
		more[0].Raw = append(more[0].Raw, e.Raw...)
		return append(more, extra...)
	}
	return append(append([]model.Event{e}, more...), extra...)
}

var planPathRe = regexp.MustCompile(`[\\/]\.claude[\\/]plans[\\/][^\\/]+\.md$`)
var taskToolRe = regexp.MustCompile(`^(TaskCreate|TaskUpdate|TaskList|TaskGet|TodoWrite)$`)

// toolUse classifies a tool_use block: the block the assistant event carries
// (nil when it folds into an existing card) plus side events.
func (p *Projector) toolUse(f model.Frame, b map[string]any, laneID string) (map[string]any, []model.Event) {
	id := str(b, "id")
	name := str(b, "name")
	input := sub(b, "input")
	if input == nil {
		input = map[string]any{}
	}
	p.toolNames[id] = name
	blk := map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
	role := ""
	switch {
	case taskToolRe.MatchString(name):
		role = "task"
		ev := p.taskBlock(f, id, name, input)
		blk["role"] = role
		return blk, ev
	case (name == "Write" || name == "Edit") && planPathRe.MatchString(str(input, "file_path")):
		role = "plan"
		pl := p.planFor(str(input, "file_path"))
		p.planCall[id] = pl
		fresh := pl.ref == ""
		if fresh {
			pl.ref = "plan:" + pl.key
		}
		before := pl.text
		if name == "Edit" {
			o, n := str(input, "old_string"), str(input, "new_string")
			if pl.text == "" {
				pl.text = n
			} else if boolean(input, "replace_all") {
				pl.text = strings.ReplaceAll(pl.text, o, n)
			} else {
				pl.text = strings.Replace(pl.text, o, n, 1)
			}
		} else {
			pl.text = str(input, "content")
		}
		if pl.text != before {
			pl.revisions++
		}
		if pl.state == "approved" || pl.state == "rejected" || pl.state == "stale" || pl.state == "" {
			pl.state = "drafting"
		}
		p.curPlan = pl
		up := model.Event{T: model.PlanUpdate, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Lane: laneID, Data: map[string]any{"key": pl.key, "text": pl.text, "revisions": pl.revisions, "state": pl.state}}
		if fresh {
			blk["role"] = role
			blk["plan_key"] = pl.key
			return blk, []model.Event{up}
		}
		up.To = pl.ref
		return nil, []model.Event{up}
	case name == "ToolSearch" && strings.Contains(str(input, "query"), "ExitPlanMode") && p.curPlan != nil && p.curPlan.ref != "":
		p.planCall[id] = p.curPlan
		return nil, []model.Event{{T: model.Fold, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), To: p.curPlan.ref, Raw: []model.Raw{{Label: "tool search"}}}}
	case name == "ExitPlanMode":
		role = "plan"
		var pl *plan
		if fp := str(input, "planFilePath"); fp != "" {
			pl = p.planFor(fp)
		} else if p.curPlan != nil {
			pl = p.curPlan
		} else {
			pl = p.planFor(id)
		}
		p.planCall[id] = pl
		fresh := pl.ref == ""
		if fresh {
			pl.ref = "plan:" + pl.key
		}
		if t, ok := input["plan"].(string); ok {
			if t != pl.text {
				pl.revisions++
			}
			pl.text = t
		}
		pl.exitID = id
		pl.state = "pending"
		p.curPlan = pl
		sb := model.Event{T: model.PlanSubmit, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Lane: laneID, Data: map[string]any{"key": pl.key, "text": pl.text, "call_id": id, "revisions": pl.revisions}}
		if fresh {
			blk["role"] = role
			blk["plan_key"] = pl.key
			return blk, []model.Event{sb}
		}
		sb.To = pl.ref
		return nil, []model.Event{sb}
	case name == "AskUserQuestion" && arr(input, "questions") != nil:
		role = "question"
	case (name == "Agent" || name == "Task") && (str(input, "prompt") != "" || str(input, "description") != ""):
		role = "agent"
		l := p.laneFor(id)
		l.typ = str(input, "subagent_type")
		l.desc = str(input, "description")
		l.ref = "tool:" + id
		l.status = "running"
	case name == "SendMessage" && (str(input, "message") != "" || str(input, "content") != ""):
		role = "peer"
	case strings.HasPrefix(name, "mcp__acta__"):
		role = "acta"
	}
	if role != "" {
		blk["role"] = role
		p.toolRoles[id] = role
	}
	return blk, nil
}

func (p *Projector) planFor(key string) *plan {
	pl := p.plans[key]
	if pl == nil {
		pl = &plan{key: key, state: "drafting"}
		p.plans[key] = pl
	}
	return pl
}

func (p *Projector) laneFor(id string) *lane {
	l := p.lanes[id]
	if l == nil {
		l = &lane{id: id, status: "running"}
		p.lanes[id] = l
	}
	return l
}

// --- tasks ---

func (p *Projector) taskEntry(id string) *task {
	t := p.tasks[id]
	if t == nil {
		t = &task{id: id, status: "pending"}
		p.tasks[id] = t
	}
	return t
}

func (p *Projector) tasksEvent(f model.Frame, to string) model.Event {
	var list []map[string]any
	done, total := 0, 0
	for _, t := range p.tasks {
		if t.status == "deleted" {
			continue
		}
		total++
		if t.status == "completed" {
			done++
		}
		list = append(list, map[string]any{"id": t.id, "subject": t.subject, "description": t.description, "status": t.status, "active_form": t.activeForm})
	}
	// stable order by numeric id
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, _ := strconv.Atoi(strings.TrimPrefix(list[j-1]["id"].(string), "new-"))
			b, _ := strconv.Atoi(strings.TrimPrefix(list[j]["id"].(string), "new-"))
			if a > b {
				list[j-1], list[j] = list[j], list[j-1]
			}
		}
	}
	e := model.Event{T: model.Tasks, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), To: to, Data: map[string]any{"list": list, "done": done, "total": total}}
	allDone := total > 0 && done == total
	if allDone && !p.tasksDone {
		e.Data["all_done"] = true
	}
	p.tasksDone = allDone
	return e
}

func (p *Projector) taskBlock(f model.Frame, id, name string, input map[string]any) []model.Event {
	call := &taskCall{name: name, input: input}
	p.taskCalls[id] = call
	switch name {
	case "TaskCreate":
		call.tmp = "new-" + id
		t := p.taskEntry(call.tmp)
		t.subject, t.description, t.activeForm, t.status = str(input, "subject"), str(input, "description"), str(input, "activeForm"), "pending"
	case "TaskUpdate":
		if tid, ok := input["taskId"]; ok {
			t := p.taskEntry(anyStr(tid))
			if s := str(input, "status"); s != "" {
				t.status = s
			}
			if s := str(input, "subject"); s != "" {
				t.subject = s
			}
			if s := str(input, "description"); s != "" {
				t.description = s
			}
			if s := str(input, "activeForm"); s != "" {
				t.activeForm = s
			}
		}
	case "TodoWrite":
		if todos := arr(input, "todos"); todos != nil {
			p.tasks = map[string]*task{}
			for i, td := range todos {
				tm, _ := td.(map[string]any)
				tid := anyStr(tm["id"])
				if tid == "" {
					tid = strconv.Itoa(i + 1)
				}
				t := p.taskEntry(tid)
				t.subject = firstStr(str(tm, "content"), str(tm, "subject"))
				t.status = firstStr(str(tm, "status"), "pending")
				t.activeForm = str(tm, "activeForm")
			}
		}
	}
	return []model.Event{p.tasksEvent(f, "")}
}

func anyStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	}
	return ""
}
func firstStr(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func (p *Projector) taskResult(f model.Frame, callID string, call *taskCall, tur map[string]any) []model.Event {
	switch {
	case call.name == "TaskCreate" && sub(tur, "task") != nil && call.tmp != "":
		t := p.tasks[call.tmp]
		delete(p.tasks, call.tmp)
		if t != nil {
			t.id = anyStr(sub(tur, "task")["id"])
			if s := str(sub(tur, "task"), "subject"); s != "" {
				t.subject = s
			}
			p.tasks[t.id] = t
		}
	case call.name == "TaskUpdate" && sub(tur, "statusChange") != nil && has(tur, "taskId"):
		t := p.taskEntry(anyStr(tur["taskId"]))
		if s := str(sub(tur, "statusChange"), "to"); s != "" {
			t.status = s
		}
	case (call.name == "TaskList" || call.name == "TaskGet") && arr(tur, "tasks") != nil:
		for _, x := range arr(tur, "tasks") {
			xm, _ := x.(map[string]any)
			t := p.taskEntry(anyStr(xm["id"]))
			if s := str(xm, "subject"); s != "" {
				t.subject = s
			}
			if s := str(xm, "status"); s != "" {
				t.status = s
			}
			if s := str(xm, "description"); s != "" {
				t.description = s
			}
			if s := str(xm, "activeForm"); s != "" {
				t.activeForm = s
			}
		}
	case call.name == "TaskGet" && sub(tur, "task") != nil:
		xm := sub(tur, "task")
		t := p.taskEntry(anyStr(xm["id"]))
		if s := str(xm, "subject"); s != "" {
			t.subject = s
		}
		if s := str(xm, "status"); s != "" {
			t.status = s
		}
		if s := str(xm, "description"); s != "" {
			t.description = s
		}
	}
	e := p.tasksEvent(f, "tasks")
	e.Raw = []model.Raw{{Label: strings.ToLower(strings.Replace(call.name, "Task", "task ", 1)) + " result", Kind: f.Kind, Payload: f.Payload}}
	return []model.Event{e}
}

// --- user (echoes and tool results) ---

var (
	peerRe    = regexp.MustCompile(`^Another Claude session sent a message:\s*<cross-session-message\s+([^>]*)>([\s\S]*?)</cross-session-message>`)
	attrRe    = regexp.MustCompile(`([\w-]+)="([^"]*)"`)
	cmdNameRe = regexp.MustCompile(`<command-name>/([\w:-]+)</command-name>`)
	answerRe  = regexp.MustCompile(`"([^"]+)"="([^"]*)"`)
)

// echoContent splits an echoed user message into text and images.
func echoContent(c any) (string, []any) {
	switch v := c.(type) {
	case string:
		return v, nil
	case []any:
		var texts []string
		var images []any
		for _, b := range v {
			bm, _ := b.(map[string]any)
			switch str(bm, "type") {
			case "text":
				texts = append(texts, str(bm, "text"))
			case "image":
				if s := sub(bm, "source"); s != nil && str(s, "data") != "" {
					images = append(images, map[string]any{"media_type": firstStr(str(s, "media_type"), "image/png"), "data": str(s, "data")})
				}
			}
		}
		return strings.Join(texts, "\n"), images
	}
	return "", nil
}

func (p *Projector) user(f model.Frame) []model.Event {
	m := obj(f.Payload)
	msg := sub(m, "message")
	content := msg["content"]
	laneID := str(m, "parent_tool_use_id")
	if p.compact != nil && p.compact.done {
		if s, ok := content.(string); ok && !boolean(m, "isReplay") && !p.compact.summary {
			p.compact.summary = true
			e := model.NewLabelled(model.CompactSummary, f, "summary").Set("text", s)
			e.To = p.compact.ref
			return []model.Event{e}
		}
		if boolean(m, "isReplay") {
			return []model.Event{model.FoldTo(f, p.compact.ref, "echo")}
		}
	}
	if boolean(m, "isReplay") {
		return p.echo(f, m, content)
	}
	if _, ok := content.(string); ok {
		return []model.Event{model.FoldTo(f, "", "echo")}
	}
	blocks := arr(msg, "content")
	if len(blocks) == 0 {
		return []model.Event{model.FoldTo(f, "", "user")}
	}
	allText := true
	allDenied := true
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		if str(bm, "type") != "text" {
			allText = false
		}
		if !(str(bm, "type") == "tool_result" && boolean(bm, "is_error") && p.denied[str(bm, "tool_use_id")]) {
			allDenied = false
		}
	}
	if allDenied {
		bm, _ := blocks[0].(map[string]any)
		return []model.Event{model.FoldTo(f, "tool:"+str(bm, "tool_use_id"), "denied result")}
	}
	if allText {
		txt := strings.TrimSpace(blockText(blocks))
		if strings.HasPrefix(strings.ToLower(txt), "[request interrupted") {
			ff := f
			p.interrupt = &ff
			return nil
		}
		e := model.New(model.UserMessage, f).Set("text", txt).Set("system", true).With("", "", laneID)
		return []model.Event{e}
	}
	var out []model.Event
	tur := sub(m, "tool_use_result")
	for _, raw := range blocks {
		b, _ := raw.(map[string]any)
		if str(b, "type") != "tool_result" {
			out = append(out, model.New(model.Unknown, f).Set("kind", "user").Set("text", "unrendered block: "+str(b, "type")))
			continue
		}
		callID := str(b, "tool_use_id")
		txt := resultText(b["content"])
		isErr := boolean(b, "is_error")
		name := p.toolNames[callID]
		role := p.toolRoles[callID]
		// a subagent's result: its lane finishes here
		if l := p.lanes[callID]; l != nil && l.ref != "" {
			if strings.HasPrefix(strings.TrimSpace(txt), "Async agent launched") {
				out = append(out, model.FoldTo(f, l.ref, "launch"))
				continue
			}
			status := "completed"
			if isErr {
				status = "failed"
			}
			l.status = status
			e := model.NewLabelled(model.AgentEnd, f, "agent result").Set("id", callID).Set("status", status).Set("summary", txt).With("", l.ref, laneID)
			out = append(out, e)
			continue
		}
		if tc := p.taskCalls[callID]; tc != nil {
			out = append(out, p.taskResult(f, callID, tc, tur)...)
			continue
		}
		if pl := p.planCall[callID]; pl != nil {
			out = append(out, p.planResult(f, pl, callID, txt, isErr)...)
			continue
		}
		if role == "question" {
			q := model.NewLabelled(model.QuestionAnswer, f, "answer result").Set("call_id", callID)
			q.To = "tool:" + callID
			parsed := map[string]any{}
			for _, am := range answerRe.FindAllStringSubmatch(txt, -1) {
				parsed[am[1]] = am[2]
			}
			if len(parsed) > 0 {
				q.Set("answers", parsed)
			} else if isErr {
				q.Set("error", txt)
			}
			out = append(out, q)
			continue
		}
		if role == "peer" {
			ok := !isErr
			text := txt
			var sent map[string]any
			if json.Unmarshal([]byte(txt), &sent) == nil && sent != nil {
				if v, has := sent["success"]; has {
					ok, _ = v.(bool)
				}
				if ok {
					text = firstStr(str(sent, "message"), "delivered")
				} else {
					text = firstStr(str(sent, "error"), str(sent, "message"), "not delivered")
				}
			} else if text == "" {
				if ok {
					text = "delivered"
				} else {
					text = "not delivered"
				}
			}
			e := model.NewLabelled(model.PeerDelivery, f, "delivery").Set("call_id", callID).Set("ok", ok).Set("text", text)
			e.To = "tool:" + callID
			out = append(out, e)
			continue
		}
		e := model.New(model.ToolResult, f).Set("call_id", callID).Set("name", name).Set("text", txt).Set("error", isErr).With("", "", laneID)
		if callID != "" && name != "" {
			e.To = "tool:" + callID
		}
		if role != "" {
			e.Set("role", role)
		}
		if role == "acta" || strings.HasPrefix(name, "mcp__") {
			var data any
			if json.Unmarshal([]byte(strings.TrimSpace(txt)), &data) == nil && data != nil {
				e.Set("data", data)
			} else if tur != nil {
				e.Set("data", tur)
			}
			if strings.HasPrefix(strings.TrimSpace(txt), "Error:") {
				e.Set("error", true)
			}
		}
		if !isErr && diffable(tur) {
			e.Set("diff", diffOf(tur))
		}
		out = append(out, e)
	}
	return out
}

func resultText(c any) string {
	switch v := c.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, x := range v {
			if xm, ok := x.(map[string]any); ok {
				sb.WriteString(str(xm, "text"))
			}
		}
		return sb.String()
	}
	b, _ := json.Marshal(c)
	return string(b)
}

func diffable(tur map[string]any) bool {
	if tur == nil {
		return false
	}
	if sp := arr(tur, "structuredPatch"); len(sp) > 0 {
		return true
	}
	if str(tur, "type") == "create" {
		if _, ok := tur["content"].(string); ok {
			return true
		}
	}
	return false
}

func diffOf(tur map[string]any) map[string]any {
	d := map[string]any{"file": str(tur, "filePath")}
	if str(tur, "type") == "create" {
		d["kind"] = "create"
		d["content"] = str(tur, "content")
		return d
	}
	d["kind"] = "patch"
	d["hunks"] = arr(tur, "structuredPatch")
	if boolean(tur, "replaceAll") {
		d["replace_all"] = true
	}
	return d
}

func (p *Projector) planResult(f model.Frame, pl *plan, callID, txt string, isErr bool) []model.Event {
	if callID == pl.exitID {
		head := txt
		if len(head) > 200 {
			head = head[:200]
		}
		if isErr || regexp.MustCompile(`(?i)rejected|not approved|denied|changes`).MatchString(head) {
			if pl.state != "rejected" {
				pl.state = "rejected"
				if pl.feedback == "" {
					fb := strings.TrimSpace(txt)
					if len(fb) > 600 {
						fb = fb[:600]
					}
					pl.feedback = fb
				}
			}
		} else if regexp.MustCompile(`(?i)approved`).MatchString(head) && pl.state != "approved" {
			pl.state = "approved"
		}
		e := model.NewLabelled(model.PlanVerdict, f, "approval result").Set("key", pl.key).Set("state", pl.state).Set("feedback", pl.feedback).Set("marker", pl.state == "approved" || pl.state == "rejected")
		e.To = pl.ref
		return []model.Event{e}
	}
	label := "plan write result"
	if p.toolNames[callID] == "ToolSearch" {
		label = "tool search result"
	}
	return []model.Event{model.FoldTo(f, pl.ref, label)}
}

// echo: Claude Code's replay of a message of ours (or a peer's).
func (p *Projector) echo(f model.Frame, m map[string]any, content any) []model.Event {
	text, images := echoContent(content)
	t := strings.TrimSpace(text)
	if pm := peerRe.FindStringSubmatch(t); pm != nil {
		p.turnHasEcho = true
		attrs := map[string]string{}
		for _, am := range attrRe.FindAllStringSubmatch(pm[1], -1) {
			attrs[am[1]] = am[2]
		}
		name := attrs["from-name"]
		if name == "" {
			name = peerShort(attrs["from"])
		}
		e := model.New(model.PeerMessage, f).Set("from", attrs["from"]).Set("name", name).Set("mode", attrs["from-mode"]).Set("text", strings.TrimSpace(pm[2]))
		e.Ref = ref("peer", f)
		if p.pendingLifecycle != nil {
			e.Raw = append(e.Raw, model.Raw{Label: "lifecycle started", Kind: p.pendingLifecycle.Kind, Payload: p.pendingLifecycle.Payload})
			p.pendingLifecycle = nil
		}
		p.peerRef = e.Ref
		return []model.Event{e}
	}
	if strings.HasPrefix(t, "<command-message>") || strings.HasPrefix(t, "<command-name>") {
		// a skill's expansion of our "/command": the marker already shows it
		to := p.lastCmdRef()
		if cn := cmdNameRe.FindStringSubmatch(t); cn != nil {
			for _, c := range p.cmds {
				if c.name == cn[1] {
					to = c.ref
				}
			}
		}
		return []model.Event{model.FoldTo(f, to, "echo")}
	}
	if strings.HasPrefix(t, "<task-notification>") {
		return p.taskNotice(f, t)
	}
	if strings.HasPrefix(t, "<local-command-stdout>") {
		to := ""
		for _, c := range p.cmds {
			if c.replied {
				to = c.ref
				break
			}
		}
		if to == "" {
			to = p.lastCmdRef()
		}
		return []model.Event{model.FoldTo(f, to, "echo")}
	}
	e := model.New(model.UserMessage, f).Set("text", text)
	if len(images) > 0 {
		e.Set("images", images)
	}
	if uuid := str(m, "uuid"); uuid != "" {
		e.Set("id", uuid)
		// only a message that starts a turn is a rewind target: one steered
		// into a running turn is stored as an attachment Claude Code cannot
		// rewind to
		e.Set("steer", p.turnHasEcho)
	}
	p.turnHasEcho = true
	e.Ref = ref("user", f)
	return []model.Event{e}
}

func peerShort(from string) string {
	if from == "" {
		return "another session"
	}
	s := from
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(s, ".sock")
}

// --- result (a turn ending) ---

var resultErrors = map[string]string{"error_max_turns": "max turns reached", "error_max_budget_usd": "budget exhausted", "error_max_structured_output_retries": "structured output failed", "error_during_execution": "error during execution"}

func (p *Projector) result(f model.Frame) []model.Event {
	m := obj(f.Payload)
	var out []model.Event
	// the model's context window, from the per-model usage
	if mu := sub(m, "modelUsage"); mu != nil {
		for _, v := range mu {
			if vm, ok := v.(map[string]any); ok && num(vm, "contextWindow") > 0 {
				p.ctxWindow = int64(num(vm, "contextWindow"))
				break
			}
		}
	}
	p.turnActive = false
	p.turnHasEcho = false
	stale := model.Event{T: model.TurnIdle, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt)}
	for _, a := range p.approvals {
		a.done = true
	}
	// compaction's closing empty result
	if p.compact != nil && p.compact.done {
		u := sub(m, "usage")
		if u == nil || num(u, "input_tokens")+num(u, "output_tokens")+num(u, "cache_read_input_tokens")+num(u, "cache_creation_input_tokens") == 0 {
			fe := model.FoldTo(f, p.compact.ref, "result")
			p.compact = nil
			return append(out, fe, stale)
		}
	}
	p.compact = nil
	// the empty turn that closes a local command
	var done *cmd
	for _, c := range p.cmds {
		if c.replied {
			done = c
			break
		}
	}
	if done == nil && !(num(m, "num_turns") > 0) && len(p.cmds) > 0 {
		done = p.cmds[0]
	}
	if done != nil {
		p.cmdForget(done)
		label := "result"
		if done.silent {
			label = "/" + done.name + " result"
		}
		return append(out, model.FoldTo(f, done.ref, label), stale)
	}
	// goal: a real turn ending while a goal is active means the Stop hook
	// let it through — the condition held — unless Claude Code gave up on it
	var goalEv *model.Event
	if p.goal != nil && p.goal.state == "active" {
		if p.goal.overridden {
			p.goal.state = "unmet"
		} else {
			p.goal.state = "met"
		}
		p.goal.overridden = false
		g := model.Event{T: model.Goal, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Data: p.goalData()}
		goalEv = &g
	}
	interrupted := p.interrupt != nil || str(m, "terminal_reason") == "aborted_streaming"
	subtype := str(m, "subtype")
	failed := boolean(m, "is_error") || (subtype != "" && subtype != "success")
	e := model.New(model.TurnEnd, f).Set("ok", !failed && !interrupted).Set("interrupted", interrupted)
	e.Ref = ref("turn", f)
	if !interrupted && subtype != "" && subtype != "success" {
		if s, ok := resultErrors[subtype]; ok {
			e.Set("error", s)
		} else {
			e.Set("error", strings.ReplaceAll(subtype, "_", " "))
		}
	}
	if has(m, "num_turns") {
		e.Set("calls", int(num(m, "num_turns")))
	}
	if has(m, "duration_ms") {
		e.Set("duration_ms", int64(num(m, "duration_ms")))
	}
	if u := sub(m, "usage"); u != nil {
		tok := int64(num(u, "input_tokens") + num(u, "cache_read_input_tokens") + num(u, "cache_creation_input_tokens") + num(u, "output_tokens"))
		if tok > 0 {
			e.Set("tokens", tok)
		}
	}
	if d := arr(m, "permission_denials"); len(d) > 0 {
		e.Set("denials", len(d))
	}
	if r, ok := m["result"].(string); ok && r != "" {
		e.Set("result", r)
	}
	if p.ctxWindow > 0 {
		e.Set("context_window", p.ctxWindow)
	}
	var msgs []string
	for _, x := range arr(m, "errors") {
		s := strings.TrimSpace(anyStr(x))
		if s != "" && !strings.HasPrefix(s, "[ede_diagnostic]") {
			msgs = append(msgs, s)
		}
	}
	if failed && len(msgs) > 0 {
		e.Set("errors", msgs)
	}
	if p.interrupt != nil {
		e.Raw = append(e.Raw, model.Raw{Label: "interrupt", Kind: p.interrupt.Kind, Payload: p.interrupt.Payload})
		p.interrupt = nil
	}
	if interrupted {
		p.afterStop = e.Ref
	}
	if p.apiRetry != nil {
		ar := model.Event{T: model.APIError, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), To: p.apiRetry.ref, Data: map[string]any{"attempts": p.apiRetry.count, "settled": true, "ok": !failed}}
		p.apiRetry = nil
		out = append(out, ar)
	}
	out = append(out, e, stale)
	if goalEv != nil {
		out = append(out, *goalEv)
	}
	return out
}

// --- system ---

func (p *Projector) system(f model.Frame) []model.Event {
	m := obj(f.Payload)
	st := str(m, "subtype")
	laneID := str(m, "parent_tool_use_id")
	switch st {
	case "thinking_tokens":
		e := model.Event{T: model.Thinking, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Lane: laneID, Data: map[string]any{"tokens": int64(num(m, "estimated_tokens"))}, Raw: []model.Raw{{Kind: f.Kind, Payload: f.Payload}}}
		return []model.Event{e}
	case "hook_started":
		id := str(m, "hook_id")
		p.hooks[id] = false
		e := model.NewLabelled(model.HookStart, f, "started").Set("id", id).Set("name", firstStr(str(m, "hook_name"), str(m, "hook_event"), id)).Set("event", str(m, "hook_event")).With("hook:"+id, "", laneID)
		return []model.Event{e}
	case "hook_response":
		id := str(m, "hook_id")
		answered, seen := p.hooks[id]
		if seen && answered {
			return []model.Event{model.FoldTo(f, "hook:"+id, "duplicate response")}
		}
		p.hooks[id] = true
		ok := (str(m, "outcome") == "" || str(m, "outcome") == "success") && num(m, "exit_code") == 0
		e := model.NewLabelled(model.HookEnd, f, "response").Set("id", id).Set("ok", ok).Set("outcome", str(m, "outcome")).Set("exit_code", int(num(m, "exit_code"))).With("", "", laneID)
		if !seen {
			e.Ref = "hook:" + id
			e.Set("name", firstStr(str(m, "hook_name"), str(m, "hook_event"), id)).Set("event", str(m, "hook_event"))
		} else {
			e.To = "hook:" + id
		}
		if s := str(m, "stdout"); s != "" {
			e.Set("stdout", s)
		}
		if s := str(m, "stderr"); s != "" {
			e.Set("stderr", s)
		}
		if inj := hookInjected(m); inj != nil {
			e.Set("injected", inj)
		}
		return []model.Event{e}
	case "status":
		if str(m, "status") == "compacting" {
			if p.compact != nil && !p.compact.done {
				return []model.Event{model.FoldTo(f, p.compact.ref, "status")}
			}
			return []model.Event{p.compactStart(f, "status")}
		}
		if cr := str(m, "compact_result"); cr != "" && p.compact != nil {
			if cr != "success" {
				p.compact.done = true
				e := model.NewLabelled(model.CompactEnd, f, "status").Set("ok", false).Set("error", firstStr(str(m, "compact_error"), strings.ReplaceAll(cr, "_", " ")))
				e.To = p.compact.ref
				return []model.Event{e}
			}
			return []model.Event{model.FoldTo(f, p.compact.ref, "status")}
		}
		if pm := str(m, "permissionMode"); pm != "" {
			e := model.NewLabelled(model.Setting, f, "status").Set("key", "permission_mode").Set("value", pm)
			if p.lastMode != "" {
				e.To = p.lastMode
				p.lastMode = ""
			} else {
				e.Ref = ref("setting", f)
			}
			return []model.Event{e}
		}
		keys := 0
		for k := range m {
			switch k {
			case "type", "subtype", "uuid", "session_id", "status":
			default:
				keys++
			}
		}
		if s, ok := m["status"].(string); ok && keys == 0 {
			return []model.Event{model.FoldTo(f, "", "status "+s)}
		}
		var bits []string
		for k, v := range m {
			switch k {
			case "type", "subtype", "uuid", "session_id":
				continue
			}
			if v == nil {
				continue
			}
			b, _ := json.Marshal(v)
			bits = append(bits, k+" "+strings.Trim(string(b), `"`))
		}
		note := "status update"
		if s := str(m, "status"); s != "" {
			note = "status: " + s
		} else if len(bits) > 0 {
			note = "status · " + strings.Join(bits, " · ")
		}
		return []model.Event{model.New(model.SessionState, f).Set("text", note).With("", "", laneID)}
	case "compact_boundary":
		md := sub(m, "compact_metadata")
		fresh := p.compact == nil
		var out []model.Event
		if fresh {
			out = append(out, p.compactStart(f, "boundary"))
		}
		p.compact.done = true
		e := model.NewLabelled(model.CompactEnd, f, "boundary").Set("ok", true).Set("pre", int64(num(md, "pre_tokens"))).Set("post", int64(num(md, "post_tokens"))).Set("duration_ms", int64(num(md, "duration_ms"))).Set("trigger", str(md, "trigger"))
		e.To = p.compact.ref
		if fresh {
			e.Raw = nil
		}
		out = append(out, e)
		if post := int64(num(md, "post_tokens")); post > 0 {
			ce := model.Event{T: model.UsageContext, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Data: map[string]any{"used": post}}
			if p.ctxWindow > 0 {
				ce.Data["window"] = p.ctxWindow
			}
			out = append(out, ce)
		}
		return out
	case "permission_denied":
		id := str(m, "tool_use_id")
		p.denied[id] = true
		e := model.NewLabelled(model.ToolDenied, f, "permission denied").Set("call_id", id).Set("reason", str(m, "decision_reason")).Set("message", str(m, "message")).With("", "", laneID)
		if p.toolNames[id] != "" {
			e.To = "tool:" + id
		}
		return []model.Event{e}
	case "task_started", "task_progress", "task_updated", "task_notification", "background_tasks_changed":
		return p.taskFrame(f, m, st, laneID)
	case "api_retry", "api_error":
		return p.apiRetryEvent(f, m, st)
	case "mirror_error":
		return []model.Event{model.New(model.Notice, f).Set("level", "error").Set("text", "mirror error · "+str(m, "error")).Set("subtype", st)}
	case "init":
		if p.compact != nil {
			return []model.Event{model.FoldTo(f, p.compact.ref, "init")}
		}
		p.model = str(m, "model")
		e := model.NewLabelled(model.SessionInit, f, "init").Set("model", str(m, "model")).Set("permission_mode", str(m, "permissionMode")).Set("output_style", str(m, "output_style")).Set("cwd", str(m, "cwd")).Set("conversation", str(m, "session_id"))
		if v, ok := m["fast_mode_state"]; ok {
			e.Set("fast_mode", v)
		}
		if v, ok := m["fast_mode_disabled_reason"]; ok {
			e.Set("fast_reason", v)
		}
		if v, ok := m["tools"]; ok {
			e.Set("tools", v)
		}
		if v, ok := m["mcp_servers"]; ok {
			e.Set("mcp", v)
		}
		if v, ok := m["slash_commands"]; ok {
			e.Set("slash_commands", v)
		}
		if v, ok := m["agents"]; ok {
			e.Set("agents", v)
		}
		// folded into the session started/resumed divider, else the message
		// that started the turn; neither present, it stands alone
		if p.sessionRef != "" {
			e.To = p.sessionRef
			p.sessionRef = ""
		} else if p.lastInput != "" {
			e.To = p.lastInput
		} else {
			e.Ref = ref("session", f)
		}
		return []model.Event{e}
	}
	if c, ok := m["content"].(string); ok && c != "" {
		if regexp.MustCompile(`(?i)hook blocked the turn from ending`).MatchString(c) && regexp.MustCompile(`(?i)overriding`).MatchString(c) && p.goal != nil {
			p.goal.overridden = true
		}
		lvl := str(m, "level")
		if lvl != "" || regexp.MustCompile(`fallback|refusal|informational|permission_retry|bridge_status|notification`).MatchString(st) {
			e := model.New(model.Notice, f).Set("level", firstStr(lvl, "info")).Set("text", c).Set("subtype", st).With("", "", laneID)
			if regexp.MustCompile(`^model_.*fallback$`).MatchString(st) && str(m, "fallbackModel") != "" {
				e.Set("model", str(m, "fallbackModel"))
			}
			return []model.Event{e}
		}
	}
	note := firstStr(st, "system")
	if st == "api_retry" {
		note = "API retry " + anyStr(m["attempt"]) + ": " + str(m, "error")
	}
	return []model.Event{model.New(model.SessionState, f).Set("text", note).With("", "", laneID)}
}

func hookInjected(m map[string]any) map[string]any {
	out := firstStr(str(m, "output"), str(m, "stdout"))
	if strings.TrimSpace(out) == "" {
		return nil
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) == nil && parsed != nil {
		hso := sub(parsed, "hookSpecificOutput")
		ctx := firstStr(str(hso, "additionalContext"), str(parsed, "additionalContext"), str(parsed, "systemMessage"))
		if strings.TrimSpace(ctx) != "" {
			rest := map[string]any{}
			for k, v := range parsed {
				rest[k] = v
			}
			if hso != nil {
				h2 := map[string]any{}
				for k, v := range hso {
					if k != "additionalContext" {
						h2[k] = v
					}
				}
				if len(h2) <= 1 {
					delete(rest, "hookSpecificOutput")
				} else {
					rest["hookSpecificOutput"] = h2
				}
			}
			delete(rest, "additionalContext")
			delete(rest, "systemMessage")
			r := map[string]any{"md": ctx}
			if len(rest) > 0 {
				r["extra"] = rest
			}
			return r
		}
		return map[string]any{"json": parsed}
	}
	return map[string]any{"text": out}
}

func (p *Projector) compactStart(f model.Frame, label string) model.Event {
	e := model.NewLabelled(model.CompactStart, f, label)
	e.Ref = ref("compact", f)
	p.compact = &compaction{ref: e.Ref}
	// a /compact marker folds into the block
	for _, c := range p.cmds {
		if c.kind == "compact" {
			e.To = c.ref
			e.Set("args", c.args)
			p.compact.cmdRef = c.ref
			p.cmdForget(c)
			break
		}
	}
	return e
}

func (p *Projector) apiRetryEvent(f model.Frame, m map[string]any, st string) []model.Event {
	errText := str(m, "error")
	if errText == "unknown" {
		errText = ""
	}
	if st == "api_error" {
		e := model.New(model.APIError, f).Set("error", errText)
		if p.apiRetry != nil {
			e.To = p.apiRetry.ref
			e.Set("attempts", p.apiRetry.count)
			p.apiRetry = nil
		} else {
			e.Ref = ref("apiretry", f)
		}
		return []model.Event{e}
	}
	fresh := p.apiRetry == nil
	if fresh {
		p.apiRetry = &apiRetry{ref: ref("apiretry", f)}
	}
	if a := int(num(m, "attempt")); a > 0 {
		p.apiRetry.count = a
	} else {
		p.apiRetry.count++
	}
	e := model.NewLabelled(model.APIRetry, f, st).Set("attempt", p.apiRetry.count).Set("max", int(num(m, "max_retries"))).Set("delay_ms", int64(num(m, "retry_delay_ms"))).Set("error", errText).Set("status", anyStr(m["error_status"]))
	if fresh {
		e.Ref = p.apiRetry.ref
	} else {
		e.To = p.apiRetry.ref
	}
	return []model.Event{e}
}

// taskFrame: the task_* system frames around subagents and background shells.
func (p *Projector) taskFrame(f model.Frame, m map[string]any, st, laneID string) []model.Event {
	callID := str(m, "tool_use_id")
	taskID := str(m, "task_id")
	label := strings.ReplaceAll(st, "_", " ")
	// a shell task folds into the Bash pill it belongs to
	isShell := st != "background_tasks_changed" && (str(m, "task_type") == "local_bash" || (callID != "" && p.lanes[callID] == nil && p.toolNames[callID] != "") || p.taskPill[taskID] != "")
	if isShell {
		pill := ""
		if callID != "" && p.lanes[callID] == nil && p.toolNames[callID] != "" {
			pill = callID
		}
		if pill == "" {
			pill = p.taskPill[taskID]
		}
		if pill != "" {
			if taskID != "" {
				p.taskPill[taskID] = pill
			}
			e := model.NewLabelled(model.ToolBackground, f, label).Set("call_id", pill).Set("task_id", taskID)
			e.To = "tool:" + pill
			if st == "task_started" && boolean(m, "is_backgrounded") {
				e.Set("status", "background")
			} else if st == "task_notification" {
				e.Set("status", firstStr(str(m, "status"), "done"))
			} else if st == "task_updated" {
				if ps := str(sub(m, "patch"), "status"); ps != "" && ps != "running" {
					e.Set("status", ps)
				} else {
					e.T = model.Fold
					e.Data = nil
				}
			} else {
				e.T = model.Fold
				e.Data = nil
			}
			return []model.Event{e}
		}
	}
	var l *lane
	if st == "background_tasks_changed" {
		tasks := arr(m, "tasks")
		// only shells (or nothing at all) changed: the pill tells that story
		anyAgent := false
		for _, t := range tasks {
			tm, _ := t.(map[string]any)
			if str(tm, "task_type") != "local_bash" || p.taskLane[str(tm, "task_id")] != nil {
				anyAgent = true
			}
			if lt := p.taskLane[str(tm, "task_id")]; lt != nil && str(tm, "description") != "" && lt.desc == "" {
				lt.desc = str(tm, "description")
			}
		}
		if !anyAgent {
			for _, t := range tasks {
				tm, _ := t.(map[string]any)
				if pill := p.taskPill[str(tm, "task_id")]; pill != "" {
					return []model.Event{model.FoldTo(f, "tool:"+pill, label)}
				}
			}
			return []model.Event{model.FoldTo(f, "", label)}
		}
		for _, t := range tasks {
			tm, _ := t.(map[string]any)
			if lt := p.taskLane[str(tm, "task_id")]; lt != nil {
				l = lt
				break
			}
		}
		if l == nil {
			l = p.newestLane(true)
		}
		if l == nil {
			l = p.newestLane(false)
		}
	} else {
		l = p.laneForTask(callID, taskID)
	}
	if l == nil || l.ref == "" {
		desc := str(m, "description")
		note := label
		if desc != "" {
			note += " · " + desc
		}
		return []model.Event{model.New(model.SessionState, f).Set("text", note).With("", "", laneID)}
	}
	switch st {
	case "task_started":
		l.started = true
		if d := str(m, "description"); d != "" && l.desc == "" {
			l.desc = d
		}
		e := model.NewLabelled(model.AgentStart, f, label).Set("id", l.id).Set("type", l.typ).Set("description", l.desc).Set("task_id", taskID)
		e.To = l.ref
		return []model.Event{e}
	case "task_progress":
		last := str(m, "description")
		if last == "" && str(m, "last_tool_name") != "" {
			last = "Running " + str(m, "last_tool_name")
		}
		if t := str(m, "subagent_type"); t != "" && l.typ == "" {
			l.typ = t
		}
		e := model.NewLabelled(model.AgentProgress, f, label).Set("id", l.id).Set("last", last).Set("type", l.typ)
		e.To = l.ref
		return []model.Event{e}
	case "task_updated", "task_notification":
		patch := sub(m, "patch")
		status := str(m, "status")
		if st == "task_updated" {
			status = str(patch, "status")
		} else if status == "" {
			status = "completed"
		}
		if status != "" && status != "running" {
			l.status = status
			e := model.NewLabelled(model.AgentEnd, f, label).Set("id", l.id).Set("status", status).Set("type", l.typ).Set("description", l.desc)
			if st == "task_updated" && num(patch, "end_time") > 0 {
				e.Set("ended_at", int64(num(patch, "end_time")))
			}
			if s := str(m, "summary"); s != "" {
				e.Set("summary", s)
			}
			if u := sub(m, "usage"); u != nil {
				e.Set("usage", u)
			}
			if l.doneRef == "" {
				l.doneRef = "lane-done:" + l.id
				e.Ref = l.doneRef
				e.Set("card", true)
			} else {
				e.To = l.doneRef
			}
			return []model.Event{e}
		}
	}
	return []model.Event{model.FoldTo(f, l.ref, label)}
}

var taskNoticeRe = regexp.MustCompile(`<tool-use-id>([^<]+)</tool-use-id>[\s\S]*?<status>([^<]+)</status>(?:[\s\S]*?<summary>([\s\S]*?)</summary>)?`)

// taskNotice is the text Claude Code hands the model when a background
// agent finishes (a user message beginning <task-notification>). Live, the
// system task_notification frame before it has already ended the lane and
// this folds into that; read off the transcript, where only the message
// exists, it ends the lane itself.
func (p *Projector) taskNotice(f model.Frame, t string) []model.Event {
	m := taskNoticeRe.FindStringSubmatch(t)
	if m == nil {
		return []model.Event{model.FoldTo(f, "", "task notification")}
	}
	l := p.lanes[strings.TrimSpace(m[1])]
	if l == nil {
		return []model.Event{model.FoldTo(f, "", "task notification")}
	}
	if l.doneRef != "" {
		return []model.Event{model.FoldTo(f, l.doneRef, "task notification")}
	}
	status := strings.TrimSpace(m[2])
	if status == "" || status == "running" {
		return []model.Event{model.FoldTo(f, l.ref, "task notification")}
	}
	l.status = status
	l.doneRef = "lane-done:" + l.id
	e := model.NewLabelled(model.AgentEnd, f, "task notification").Set("id", l.id).Set("status", status).Set("type", l.typ).Set("description", l.desc).Set("card", true)
	e.Ref = l.doneRef
	if sm := strings.TrimSpace(m[3]); sm != "" {
		e.Set("summary", sm)
	}
	return []model.Event{e}
}

func (p *Projector) newestLane(runningOnly bool) *lane {
	var best *lane
	for _, l := range p.lanes {
		if runningOnly && l.status != "running" {
			continue
		}
		if best == nil || l.id > best.id {
			best = l
		}
	}
	return best
}

func (p *Projector) laneForTask(callID, taskID string) *lane {
	if callID != "" {
		if l := p.lanes[callID]; l != nil {
			if taskID != "" && l.taskID == "" {
				l.taskID = taskID
				p.taskLane[taskID] = l
			}
			return l
		}
	}
	if taskID != "" {
		if l := p.taskLane[taskID]; l != nil {
			return l
		}
	}
	var free *lane
	for _, l := range p.lanes {
		if l.taskID == "" && l.status == "running" && (free == nil || l.id > free.id) {
			free = l
		}
	}
	if free != nil && taskID != "" {
		free.taskID = taskID
		p.taskLane[taskID] = free
	}
	return free
}

// --- rate limits ---

func (p *Projector) rateLimit(f model.Frame) []model.Event {
	m := obj(f.Payload)
	info := sub(m, "rate_limit_info")
	e := model.New(model.UsageLimits, f)
	windows := map[string]any{}
	names := map[string]string{"five_hour": "5h", "seven_day": "weekly"}
	for k, v := range sub(info, "unifiedWindows") {
		vm, _ := v.(map[string]any)
		if vm == nil || !has(vm, "utilization") {
			continue
		}
		name := names[k]
		if name == "" {
			name = k
		}
		windows[name] = map[string]any{"utilization": num(vm, "utilization"), "resets_at": int64(num(vm, "resetsAt")), "key": k}
	}
	e.Set("windows", windows).Set("status", str(info, "status")).Set("overage", str(info, "overageStatus")).Set("overage_reason", str(info, "overageDisabledReason"))
	return []model.Event{e}
}

// --- state (harness notes) ---

func (p *Projector) state(f model.Frame) []model.Event {
	m := obj(f.Payload)
	st := str(m, "state")
	// a turn a transcript record opened has no result of its own: the
	// process starting or ending, or another read, closes it
	var pre []model.Event
	if p.importTurn {
		switch st {
		case "spawned", "exit", "catchup", "import":
			pre = p.transcriptResult(f, 0, false)
		}
	}
	return append(pre, p.stateEvents(f, m, st)...)
}

func (p *Projector) stateEvents(f model.Frame, m map[string]any, st string) []model.Event {
	stale := model.Event{T: model.TurnIdle, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt)}
	switch st {
	case "catchup", "import":
		return p.transcriptState(f, m)
	case "spawned":
		for _, a := range p.approvals {
			a.done = true
		}
		e := model.NewLabelled(model.SessionSpawned, f, "spawn").Set("resumed", boolean(m, "resumed"))
		if s := m["styles"]; s != nil {
			e.Set("styles", s)
		}
		e.Ref = ref("session", f)
		p.sessionRef = e.Ref
		return []model.Event{e, stale}
	case "exit":
		for _, a := range p.approvals {
			a.done = true
		}
		code := int(num(m, "code"))
		e := model.NewLabelled(model.SessionExit, f, "exit").Set("code", code)
		if p.afterStop != "" && code == 0 {
			e.To = p.afterStop
			e.Set("expected", true)
			p.afterStop = ""
		}
		return []model.Event{e, stale}
	case "spawn_error":
		e := model.New(model.SessionSpawnError, f).Set("error", str(m, "error"))
		if p.lastInput != "" {
			e.To = p.lastInput
		}
		return []model.Event{e}
	case "undelivered":
		e := model.New(model.SessionUndelivered, f).Set("reason", firstStr(str(m, "reason"), "no harness connected"))
		if p.lastInput != "" {
			e.To = p.lastInput
		}
		return []model.Event{e}
	case "resume_failed":
		return []model.Event{model.New(model.SessionResumeFail, f).Set("reason", firstStr(str(m, "reason"), "resume failed; starting fresh"))}
	}
	return []model.Event{model.New(model.SessionState, f).Set("text", firstStr(st, "state"))}
}

// --- reset, lifecycle, goal, reports ---

func (p *Projector) reset(f model.Frame) []model.Event {
	m := obj(f.Payload)
	e := model.NewLabelled(model.SessionReset, f, "reset").Set("conversation", str(m, "new_conversation_id"))
	e.Ref = ref("session", f)
	for _, c := range p.cmds {
		if c.kind == "clear" && !c.replied {
			e.To = c.ref
			c.replied = true
			c.ref = e.Ref
			break
		}
	}
	if e.To == "" {
		p.cmds = append(p.cmds, &cmd{ref: e.Ref, name: "clear", kind: "clear", replied: true})
	}
	p.turnHasEcho = false
	p.sessionRef = e.Ref
	return []model.Event{e}
}

// toolProgress: a heartbeat Claude Code sends while a tool runs long (every
// 30 s for a Bash command), naming the call in parent_tool_use_id — the
// call, not a subagent lane. It folds into the call's card with how long
// the tool has been at it.
func (p *Projector) toolProgress(f model.Frame) []model.Event {
	m := obj(f.Payload)
	call := str(m, "parent_tool_use_id")
	if call == "" || p.toolNames[call] == "" {
		// the heartbeat's own id is the call's with "-heartbeat-N" appended
		call = str(m, "tool_use_id")
		if i := strings.Index(call, "-heartbeat-"); i >= 0 {
			call = call[:i]
		}
	}
	label := "progress"
	if s := num(m, "elapsed_time_seconds"); s > 0 {
		label = "running · " + strconv.Itoa(int(s)) + "s"
	}
	if call != "" && p.toolNames[call] != "" {
		return []model.Event{model.FoldTo(f, "tool:"+call, label)}
	}
	return []model.Event{model.FoldTo(f, "", label)}
}

func (p *Projector) lifecycle(f model.Frame) []model.Event {
	m := obj(f.Payload)
	st := str(m, "state")
	if st == "started" {
		ff := f
		p.pendingLifecycle = &ff
		return nil
	}
	if p.peerRef != "" {
		to := p.peerRef
		if st == "completed" {
			p.peerRef = ""
		}
		return []model.Event{model.FoldTo(f, to, "lifecycle "+st)}
	}
	return []model.Event{model.FoldTo(f, "", "lifecycle "+st)}
}

func (p *Projector) goalData() map[string]any {
	if p.goal == nil {
		return map[string]any{"state": "cleared"}
	}
	return map[string]any{"cond": p.goal.cond, "state": p.goal.state, "turns": p.goal.turns, "last": p.goal.last}
}

// goalEvent parses a goal reply ("Goal set: …", "Goal active: … (N turns)
// Last check: …", "No goal set", "Goal met") into the goal state.
func (p *Projector) goalEvent(f model.Frame, txt, clearArgs string) model.Event {
	t := strings.TrimSpace(txt)
	switch {
	case goalSetRe.MatchString(t):
		p.goal = &goal{cond: strings.TrimSpace(goalSetRe.FindStringSubmatch(t)[1]), state: "active"}
	case goalActiveRe.MatchString(t):
		gm := goalActiveRe.FindStringSubmatch(t)
		n, _ := strconv.Atoi(gm[2])
		p.goal = &goal{cond: strings.TrimSpace(gm[1]), state: "active", turns: n, last: strings.TrimSpace(gm[3])}
	case noGoalRe.MatchString(t):
		// definitive: whatever the pill showed, there is no goal now
		p.goal = nil
	case goalMetRe.MatchString(t):
		gm := goalMetRe.FindStringSubmatch(t)
		g := &goal{state: "met", last: strings.TrimSpace(gm[1])}
		if p.goal != nil {
			g.cond, g.turns = p.goal.cond, p.goal.turns
		}
		p.goal = g
	}
	e := model.New(model.Goal, f).Set("text", t)
	for k, v := range p.goalData() {
		e.Data[k] = v
	}
	return e
}

// cmdReply folds Claude Code's answer to a local command into its marker,
// and feeds the panels the reply is really for.
func (p *Projector) cmdReply(f model.Frame, c *cmd, txt string) []model.Event {
	c.replied = true
	var out []model.Event
	if c.silent {
		e := model.FoldTo(f, c.ref, "/"+c.name+" reply")
		out = append(out, e)
		if key := reportKey(c.name); key != "" {
			out = append(out, p.reportEvent(f, key, txt))
		}
		return out
	}
	e := model.NewLabelled(model.CmdReply, f, "reply").Set("text", txt).Set("kind", c.kind).Set("name", c.name).Set("error", cmdErrRe.MatchString(txt))
	e.To = c.ref
	out = append(out, e)
	if c.kind == "goal" {
		g := p.goalEvent(f, txt, c.args)
		g.Raw = nil
		out = append(out, g)
	}
	if am := acSetRe.FindStringSubmatch(txt); am != nil {
		out = append(out, model.Event{T: model.AutoCompact, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Data: map[string]any{"enabled": strings.EqualFold(am[1], "true")}})
	}
	if key := reportKey(c.name); key != "" {
		out = append(out, p.reportEvent(f, key, txt))
	}
	return out
}

func (p *Projector) reportEvent(f model.Frame, key, txt string) model.Event {
	e := model.Event{T: model.Report, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Data: map[string]any{"key": key, "text": txt}}
	if key == "autocompact" {
		if wm := acWindowRe.FindStringSubmatch(txt); wm != nil {
			w := wm[1]
			if strings.EqualFold(w, "auto") {
				w = "auto"
			}
			e.Data["window"] = w
		}
	}
	return e
}

// --- streamed replies (live, never stored) ---

func (p *Projector) stream(f model.Frame) []model.Event {
	m := obj(f.Payload)
	ev := sub(m, "event")
	laneID := str(m, "parent_tool_use_id")
	mk := func(t string) model.Event {
		return model.Event{T: t, Seq: f.Seq, At: f.At.UTC().Format(model.Fmt), Lane: laneID, Live: true, Data: map[string]any{}}
	}
	switch str(ev, "type") {
	case "message_start":
		e := mk(model.StreamStart)
		e.Set("model", str(sub(ev, "message"), "model"))
		return []model.Event{e}
	case "message_stop":
		return []model.Event{mk(model.StreamStop)}
	case "content_block_start":
		cb := sub(ev, "content_block")
		e := mk(model.StreamBlock).Set("index", int(num(ev, "index"))).Set("type", str(cb, "type"))
		if n := str(cb, "name"); n != "" {
			e.Set("name", n)
		}
		if t := str(cb, "text"); t != "" {
			e.Set("text", t)
		}
		return []model.Event{e}
	case "content_block_delta":
		d := sub(ev, "delta")
		e := mk(model.StreamDelta).Set("index", int(num(ev, "index")))
		switch str(d, "type") {
		case "text_delta":
			e.Set("text", str(d, "text"))
		case "input_json_delta":
			e.Set("json", str(d, "partial_json"))
		case "thinking_delta":
			e.Set("thinking", str(d, "thinking"))
		default:
			return nil
		}
		return []model.Event{e}
	}
	return nil
}
