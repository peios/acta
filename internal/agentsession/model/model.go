// Package model is the common event model for agent sessions: the one shape
// the browser renders and the rest of Acta reasons about, whichever backend
// (Claude Code, Codex) produced the transcript.
//
// The model is a projection, never a record. What Acta stores is each
// backend's own output verbatim (one agent_session_events row per line); a
// Projector turns that stream into Events, on ingest for the live fan-out and
// again on read for history, from the same code. That keeps the stored data
// independent of this model: when the model grows a field, every old session
// renders the new way on its next load, with no migration.
//
// Vocabulary is minted on demand — an event type or field exists because
// something in the browser draws it or something on the server acts on it —
// and every Event carries the verbatim frames it was projected from, so
// anything the model does not (yet) express is still on screen behind the
// frame's raw toggle. See ACT-37 for the design.
package model

import (
	"encoding/json"
	"time"
)

// Event is one thing that happened in a session, in the model's terms. T
// names the kind (dotted, e.g. "tool.call"); Data holds its fields, whose
// keys are the contract with the browser (documented per kind below).
//
// Ref names the transcript node this event creates, so a later event can
// address it: "tool:<call id>", "approval:<request id>", "cmd:<seq>",
// "hook:<hook id>", "lane:<agent call id>", "plan:<key>", "compact", "tasks",
// "input:<seq>", "turn:<seq>", "session:<seq>", "setting:<request id>",
// "peer:<seq>". To names the node an event attaches to instead of (or as well
// as) creating its own: an answer lands on its request, a tool's result on
// its call, a folded frame on whatever it belongs to. An event with T "fold"
// has nothing to show — it exists to carry raw frames to the node named by To
// (or the last visible node when To is empty).
type Event struct {
	T    string         `json:"t"`
	Seq  int64          `json:"seq"`           // the source frame's store sequence
	Sub  int            `json:"sub,omitempty"` // index among events from the same frame
	At   string         `json:"at"`            // RFC3339 with milliseconds, UTC
	Lane string         `json:"lane,omitempty"`
	Ref  string         `json:"ref,omitempty"`
	To   string         `json:"to,omitempty"`
	Data map[string]any `json:"d,omitempty"`
	Raw  []Raw          `json:"raw,omitempty"`
	// Live marks an event relayed as it happens but never stored (a streamed
	// delta, a background command's output as it runs): the browser draws it
	// but knows a settled event will follow and replace it.
	Live bool `json:"live,omitempty"`
}

// Raw is one verbatim backend frame an Event was projected from, with the
// label the browser shows it under in the raw panel.
type Raw struct {
	Label   string          `json:"label,omitempty"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// Frame is a stored (or live, unstored) backend frame as a Projector sees it.
type Frame struct {
	Seq     int64
	Kind    string
	Payload json.RawMessage
	At      time.Time
	Stored  bool // false for live-only frames (stream deltas, task output chunks)
}

// Projector turns a session's frames into Events, in order, keeping whatever
// state the mapping needs between frames (which tool call a result answers,
// which command a reply belongs to, whether a compaction is under way). One
// Projector serves one session; a fresh one replaying the stored frames from
// the start reaches the same state as one that saw them live.
type Projector interface {
	Project(f Frame) []Event
}

// Event kinds. Data keys are listed with each; a key absent from Data means
// the backend did not say.
const (
	// Session lifecycle.
	SessionSpawned     = "session.spawned"       // resumed bool, styles []style
	SessionInit        = "session.init"          // model, permission_mode, output_style, fast_mode, fast_reason, cwd, tools []string, mcp []{name,status}, agents, slash_commands
	SessionExit        = "session.exit"          // code int, expected bool (an exit that followed a Stop)
	SessionSpawnError  = "session.spawn_error"   // error
	SessionUndelivered = "session.undelivered"   // reason
	SessionResumeFail  = "session.resume_failed" // reason
	SessionReset       = "session.reset"         // conversation id
	SessionCatalog     = "session.catalog"       // models []model, commands []command, styles, fast_mode, fast_reason, default_effort
	SessionState       = "session.state"         // a harness state note with no better home: text, error bool

	// The user's side.
	Input       = "input"        // text, images []{media_type,data}, cmd {name,args} when the text is a slash command
	UserMessage = "user.message" // id, text, images, steer bool (sent into a running turn)
	PeerMessage = "peer.message" // from, name, mode, text

	// The assistant's side.
	Assistant   = "assistant"    // model, blocks []block; block = {type:text,text} | {type:tool_use,id,name,input,role,...} | {type:thinking}
	Thought     = "thought"      // tokens, secs, text (when the backend ships it)
	Thinking    = "thinking"     // live: tokens (a running estimate while the model thinks)
	StreamStart = "stream.start" // live: model
	StreamBlock = "stream.block" // live: index, type, name, text
	StreamDelta = "stream.delta" // live: index, text | json
	StreamStop  = "stream.stop"  // live
	CmdReply    = "cmd.reply"    // to cmd:<seq>: text, error bool, kind (the command's kind)
	Notice      = "notice"       // level info|warning|error, text, subtype
	APIRetry    = "api.retry"    // attempt, max, delay_ms, error, status
	APIError    = "api.error"    // attempts, error

	// Tools.
	ToolCall       = "tool.call"       // (only for a call outside an assistant event; normally a block) id, name, input
	ToolResult     = "tool.result"     // to tool:<id>: call_id, name, text, error bool, diff {file,kind,hunks|content,replace_all}, data (parsed JSON when the result is JSON), role
	ToolDenied     = "tool.denied"     // to tool:<id>: call_id, reason, message
	ToolBackground = "tool.background" // to tool:<id>: call_id, task_id, status
	ToolOutput     = "tool.output"     // task_id, text, done (live chunks, then one stored final)
	QuestionAnswer = "question.answer" // to tool:<id>: call_id, answers {q: a}, error text
	PeerDelivery   = "peer.delivery"   // to tool:<id>: call_id, ok, text

	// Approvals: something the backend cannot continue without.
	ApprovalRequest = "approval.request" // id, kind tool|question|plan|elicitation|other, tool, display, description, input, suggestions, call_id, questions, server, message, mode, url, schema, subtype
	ApprovalAnswer  = "approval.answer"  // to approval:<id>: id, outcome allowed|denied|answered|skipped|declined|cancelled, answers, message, content, mine bool (the browser's own answer, not the backend's echo)

	// Turns.
	TurnEnd = "turn.end" // ok bool, error, interrupted bool, calls, duration_ms, tokens, denials, errors []string, context_window, result text
	// TurnIdle says the backend is idle again — after any result, a spawn or
	// an exit — so pending approvals are void and queued commands may run.
	// It draws nothing.
	TurnIdle = "turn.idle"

	// Settings the session runs under.
	Setting      = "setting"       // key permission_mode|model|output_style|effort|fast, value, requested bool (asked for, not yet confirmed), reason
	Effort       = "effort"        // value
	Fast         = "fast"          // on bool, reason, unavailable bool
	Goal         = "goal"          // cond, state active|met|unmet|cleared, turns, last, text
	Report       = "report"        // key context|usage|autocompact, text
	AutoCompact  = "autocompact"   // enabled *bool, window
	UsageLimits  = "usage.limits"  // windows {name:{utilization,resets_at}}, status, overage, plan
	UsageContext = "usage.context" // used, window

	// Hooks.
	HookStart = "hook.start" // id, name, event
	HookEnd   = "hook.end"   // to hook:<id>: id, ok, outcome, exit_code, stdout, stderr, injected {md|text|json, extra}

	// Subagents. lane is the agent call id; the parent lane's events carry it.
	AgentStart    = "agent.start"    // to tool:<id>: id, type, description, task_id
	AgentProgress = "agent.progress" // to lane:<id>: id, last, type
	AgentEnd      = "agent.end"      // id, status, summary, ended_at, usage

	// Tasks (the checklist the model keeps).
	Tasks = "tasks" // list []{id,subject,description,status,active_form}, done, total, all_done bool (just became so)

	// Plans.
	PlanUpdate  = "plan.update"  // key, text, revisions
	PlanSubmit  = "plan.submit"  // key, text, call_id
	PlanVerdict = "plan.verdict" // key, state approved|rejected|stale, feedback

	// Compaction, reset, rewind.
	CompactStart   = "compact.start"   // for cmd:<seq> (args)
	CompactEnd     = "compact.end"     // pre, post, duration_ms, trigger, ok bool, error
	CompactSummary = "compact.summary" // text
	Rewind         = "rewind"          // mode, target, messages, files {changed,insertions,deletions}, summary

	// Replies to the browser's own queries (a rewind step, a side question).
	Reply = "reply" // id, response, error

	// A frame with nothing to show: raws only, attached to To or the last node.
	Fold = "fold"
	// A frame the projector does not know. Shown as such, with its raw.
	Unknown = "unknown"
)

// Fmt is the timestamp format events use.
const Fmt = "2006-01-02T15:04:05.000Z"

// New makes an Event for a frame.
func New(t string, f Frame) Event {
	return Event{T: t, Seq: f.Seq, At: f.At.UTC().Format(Fmt), Data: map[string]any{}, Raw: []Raw{{Kind: f.Kind, Payload: f.Payload}}}
}

// NewLabelled makes an Event whose raw frame carries a label.
func NewLabelled(t string, f Frame, label string) Event {
	e := New(t, f)
	e.Raw[0].Label = label
	return e
}

// FoldTo makes a fold event carrying f's raw frame to the node named ref.
func FoldTo(f Frame, ref, label string) Event {
	e := NewLabelled(Fold, f, label)
	e.To = ref
	e.Data = nil
	return e
}

// Set adds a field and returns the event, for chaining.
func (e Event) Set(k string, v any) Event {
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	e.Data[k] = v
	return e
}

// With sets Ref / To / Lane in one go (empty strings leave the field alone).
func (e Event) With(ref, to, lane string) Event {
	if ref != "" {
		e.Ref = ref
	}
	if to != "" {
		e.To = to
	}
	if lane != "" {
		e.Lane = lane
	}
	return e
}
