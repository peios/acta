// Package codex drives OpenAI's Codex CLI through its app-server protocol
// (JSON-RPC over stdio, the interface its own editor extension uses) and
// projects what comes back into the common event model. See ACT-37.
package codex

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// LaunchSpec is the codex command for a session.
type LaunchSpec struct {
	Cmd  string
	Args []string
	Env  map[string]string
}

// Launch composes the command. The app-server takes no per-session
// arguments: everything (working directory, sandbox, model) is said in the
// thread/start or thread/resume request that follows the handshake.
func Launch() LaunchSpec {
	return LaunchSpec{Cmd: "codex", Args: []string{"app-server", "--stdio"}}
}

// Mode is the Codex counterpart of a Claude Code permission mode: how eager
// it is to ask (approval policy), what the sandbox lets commands touch, and
// who reviews the asks. Codex cannot say "edits yes, commands ask" as sharply
// as Claude Code; the sandbox does the asking instead.
type Mode struct {
	ApprovalPolicy string
	Sandbox        string // read-only | workspace-write | danger-full-access
	Reviewer       string // user | auto_review
}

// ModeFor maps a permission mode name to Codex settings.
func ModeFor(mode string) Mode {
	switch mode {
	case "acceptEdits":
		return Mode{"on-request", "workspace-write", "user"}
	case "plan":
		return Mode{"on-request", "read-only", "user"}
	case "bypassPermissions", "dontAsk":
		return Mode{"never", "danger-full-access", "user"}
	case "auto":
		return Mode{"on-request", "workspace-write", "auto_review"}
	}
	return Mode{"untrusted", "workspace-write", "user"}
}

// ModeName reverses ModeFor from what a thread reports about itself.
func ModeName(approvalPolicy, sandboxType, reviewer string) string {
	switch {
	case reviewer == "auto_review" || reviewer == "guardian_subagent":
		return "auto"
	case approvalPolicy == "never" || sandboxType == "dangerFullAccess" || sandboxType == "danger-full-access":
		return "bypassPermissions"
	case sandboxType == "readOnly" || sandboxType == "read-only":
		return "plan"
	case approvalPolicy == "on-request":
		return "acceptEdits"
	}
	return "default"
}

// sandboxPolicy is the turn/start form of a sandbox mode.
func sandboxPolicy(mode string) map[string]any {
	switch mode {
	case "read-only":
		return map[string]any{"type": "readOnly"}
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	}
	return map[string]any{"type": "workspaceWrite"}
}

func opt(options map[string]any, k string) string {
	if options == nil {
		return ""
	}
	s, _ := options[k].(string)
	return strings.TrimSpace(s)
}

func line(v any) []byte { b, _ := json.Marshal(v); return b }

// request composes a JSON-RPC request. Ids are strings with a purpose
// prefix, so the projector knows what each response answers.
func request(id, method string, params any) []byte {
	m := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		m["params"] = params
	}
	return line(m)
}

func nonce() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

// StartLines are the handshake and the thread to open or resume. The thread
// id Codex minted is kept in the session's options ("conversation") by Notes.
func StartLines(sessionID string, options map[string]any, cwd string, resume bool) [][]byte {
	mode := ModeFor(opt(options, "permission_mode"))
	out := [][]byte{
		request("acta-init", "initialize", map[string]any{"clientInfo": map[string]any{"name": "acta", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}),
		line(map[string]any{"jsonrpc": "2.0", "method": "initialized"}),
	}
	params := map[string]any{
		"cwd": cwd, "sandbox": mode.Sandbox, "approvalPolicy": mode.ApprovalPolicy, "approvalsReviewer": mode.Reviewer,
	}
	if m := opt(options, "model"); m != "" {
		params["model"] = m
	}
	if p := opt(options, "personality"); p != "" {
		params["personality"] = p
	}
	if t := opt(options, "service_tier"); t != "" {
		params["serviceTier"] = t
	}
	if resume && opt(options, "conversation") != "" {
		params["threadId"] = opt(options, "conversation")
		// Acta holds its own copy of the conversation (stored frames, a
		// catch-up read of the rollout), so the app-server need not hand the
		// history back: hydrating a long rollout's turns takes minutes and
		// blocks every request behind it, the turn included.
		params["excludeTurns"] = true
		out = append(out, request("acta-thread-resume", "thread/resume", params))
	} else {
		params["ephemeral"] = false
		out = append(out, request("acta-thread-start", "thread/start", params))
	}
	return out
}

// Image is a picture attached to a message.
type Image struct {
	MediaType string
	Data      string
}

// InputLine delivers a message: a new turn when the thread is idle, steered
// into the running turn otherwise (Codex delivers it into the turn at its
// next step). A few of Acta's own slash commands map to thread requests.
func InputLine(options map[string]any, text string, images []Image) []byte {
	thread := opt(options, "conversation")
	t := strings.TrimSpace(text)
	switch {
	case t == "/compact":
		return request("acta-compact-"+nonce(), "thread/compact/start", map[string]any{"threadId": thread})
	case t == "/goal" || t == "/goal status":
		return request("acta-goal-get-"+nonce(), "thread/goal/get", map[string]any{"threadId": thread})
	case t == "/goal clear":
		return request("acta-goal-clear-"+nonce(), "thread/goal/clear", map[string]any{"threadId": thread})
	case strings.HasPrefix(t, "/goal "):
		return request("acta-goal-set-"+nonce(), "thread/goal/set", map[string]any{"threadId": thread, "objective": strings.TrimSpace(strings.TrimPrefix(t, "/goal "))})
	}
	input := make([]map[string]any, 0, len(images)+1)
	for _, im := range images {
		input = append(input, map[string]any{"type": "image", "url": "data:" + im.MediaType + ";base64," + im.Data})
	}
	if t != "" {
		input = append(input, map[string]any{"type": "text", "text": text})
	}
	if turn := opt(options, "turn"); turn != "" {
		return request("acta-steer-"+nonce(), "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": turn, "input": input})
	}
	params := map[string]any{"threadId": thread, "input": input}
	// settings chosen in the session apply from the next turn on
	if m := opt(options, "model"); m != "" {
		params["model"] = m
	}
	if e := opt(options, "effort"); e != "" {
		params["effort"] = e
	}
	if pm := opt(options, "permission_mode"); pm != "" {
		mode := ModeFor(pm)
		params["approvalPolicy"] = mode.ApprovalPolicy
		params["sandboxPolicy"] = sandboxPolicy(mode.Sandbox)
		params["approvalsReviewer"] = mode.Reviewer
	}
	if p := opt(options, "personality"); p != "" {
		params["personality"] = p
	}
	if st := opt(options, "service_tier"); st != "" {
		params["serviceTier"] = st
	}
	return request("acta-turn-"+nonce(), "turn/start", params)
}

// InterruptLine ends the running turn; the process (and the thread) stay.
func InterruptLine(options map[string]any) []byte {
	return request("acta-interrupt-"+nonce(), "turn/interrupt", map[string]any{"threadId": opt(options, "conversation"), "turnId": opt(options, "turn")})
}

// Op mirrors agentsession.BrowserOp.
type Op struct {
	Op, ID, Kind, Outcome, Message, Key, Value, Target, Question string
	Input, Permissions, Answers, Content                         json.RawMessage
	DryRun                                                       bool
}

// ControlLines turns a browser operation into JSON-RPC: an answer is the
// response to the server request it names (whose method the op carries as
// Kind); a catalogue request lists models and skills; a rewind reverts the
// thread to before a turn. Settings produce no line — they are remembered
// and applied on the next turn.
func ControlLines(options map[string]any, op Op) [][]byte {
	thread := opt(options, "conversation")
	rpcID := func(id string) any {
		if n, err := strconv.ParseInt(id, 10, 64); err == nil {
			return n
		}
		return id
	}
	switch op.Op {
	case "answer":
		var result any
		switch op.Kind {
		case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "execCommandApproval", "applyPatchApproval":
			decision := "decline"
			switch op.Outcome {
			case "allow", "accept":
				decision = "accept"
				if len(op.Permissions) > 0 && string(op.Permissions) != "null" && string(op.Permissions) != "[]" {
					decision = "acceptForSession"
				}
			case "cancel":
				decision = "cancel"
			}
			result = map[string]any{"decision": decision}
		case "item/tool/requestUserInput":
			var answers map[string]any
			_ = json.Unmarshal(op.Answers, &answers)
			out := map[string]any{}
			for k, v := range answers {
				switch a := v.(type) {
				case string:
					out[k] = map[string]any{"answers": strings.Split(a, ", ")}
				case []any:
					out[k] = map[string]any{"answers": a}
				}
			}
			result = map[string]any{"answers": out}
		case "mcpServer/elicitation/request":
			action := op.Outcome
			if action == "allow" {
				action = "accept"
			} else if action == "deny" {
				action = "decline"
			}
			r := map[string]any{"action": action}
			if action == "accept" && len(op.Content) > 0 {
				r["content"] = op.Content
			}
			result = r
		case "item/permissions/requestApproval":
			if op.Outcome == "allow" || op.Outcome == "accept" {
				var perms any
				_ = json.Unmarshal(op.Input, &perms)
				result = map[string]any{"permissions": perms, "scope": "turn"}
			} else {
				return [][]byte{line(map[string]any{"jsonrpc": "2.0", "id": rpcID(op.ID), "error": map[string]any{"code": -32000, "message": "declined by the user in Acta"}})}
			}
		default:
			return nil
		}
		return [][]byte{line(map[string]any{"jsonrpc": "2.0", "id": rpcID(op.ID), "result": result})}
	case "catalog":
		return [][]byte{
			request("models-"+op.ID, "model/list", map[string]any{}),
			request("skills-"+op.ID, "skills/list", map[string]any{}),
		}
	case "rewind":
		return [][]byte{request(op.ID, "thread/revert", map[string]any{"threadId": thread, "beforeTurnId": op.Target})}
	}
	return nil
}

// Kind labels a line for storage: a notification or server request by its
// method, a response to one of Acta's requests as "response".
func Kind(payload json.RawMessage) string {
	var m struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(payload, &m) == nil && m.Method != "" {
		return m.Method
	}
	return "response"
}

// Stored: everything but the per-token deltas and live output, which the
// item that completes carries whole.
func Stored(kind string, _ json.RawMessage) bool {
	switch kind {
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/reasoning/textDelta", "item/reasoning/summaryPartAdded",
		"item/commandExecution/outputDelta", "item/fileChange/outputDelta", "item/plan/delta", "command/exec/outputDelta", "process/outputDelta",
		"thread/realtime/transcript/delta", "thread/realtime/outputAudio/delta", "thread/realtime/item/transcript/delta":
		return false
	}
	return true
}

// Acknowledged: a turn starting proves the message was taken.
func Acknowledged(kind string, _ json.RawMessage) bool {
	return kind == "turn/started"
}

// Ready recognises the frame after which the app-server takes a turn: the
// thread/start or thread/resume result, or the thread/started notice,
// whichever comes first. The app-server handles requests concurrently, so
// a turn written before this is refused as "Not initialized".
func Ready(kind string, payload json.RawMessage) bool {
	if kind == "thread/started" {
		return true
	}
	if kind != "response" {
		return false
	}
	var m struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(payload, &m) != nil || len(m.Result) == 0 {
		return false
	}
	return m.ID == "acta-thread-resume" || m.ID == "acta-thread-start"
}

// Notes: the thread id (for resume) and the active turn (for steering and
// interrupting), from the frames that name them.
func Notes(kind string, payload json.RawMessage) map[string]string {
	var m struct {
		ID     any `json:"id"`
		Result struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		} `json:"result"`
		Params struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"params"`
	}
	if json.Unmarshal(payload, &m) != nil {
		return nil
	}
	switch kind {
	case "thread/started":
		if m.Params.Thread.ID != "" {
			return map[string]string{"conversation": m.Params.Thread.ID}
		}
	case "response":
		if id, _ := m.ID.(string); (id == "acta-thread-start" || id == "acta-thread-resume") && m.Result.Thread.ID != "" {
			return map[string]string{"conversation": m.Result.Thread.ID}
		}
	case "turn/started":
		if m.Params.Turn.ID != "" {
			return map[string]string{"turn": m.Params.Turn.ID}
		}
	case "turn/completed":
		return map[string]string{"turn": ""}
	}
	return nil
}

// Option: a setting the browser chose, remembered for the next turn.
func Option(kind string, payload json.RawMessage) (key, value string, ok bool) {
	if kind != "control" {
		return "", "", false
	}
	var op struct {
		Op, Key, Value string
	}
	if json.Unmarshal(payload, &op) != nil || op.Op != "setting" {
		return "", "", false
	}
	switch op.Key {
	case "permission_mode", "effort", "personality", "service_tier":
		return op.Key, op.Value, true
	case "model":
		if op.Value == "default" {
			return "model", "", true
		}
		return "model", op.Value, true
	case "output_style":
		if op.Value == "default" {
			return "personality", "", true
		}
		return "personality", op.Value, true
	case "fast":
		if op.Value == "on" {
			return "service_tier", "priority", true
		}
		return "service_tier", "", true
	}
	return "", "", false
}

// RenameLine gives the thread Acta's title.
func RenameLine(options map[string]any, title string) []byte {
	return request("acta-name-"+nonce(), "thread/name/set", map[string]any{"threadId": opt(options, "conversation"), "name": title})
}
