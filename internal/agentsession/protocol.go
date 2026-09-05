// Package agentsession runs Acta's browser-driven agent sessions: the session
// records and transcripts (over the store) and the live relay (the Hub) that
// carries a chat between a browser and a harness process on the owner's
// machine. Acta never runs the agent; the harness holds the Claude (or other
// backend) credentials and dials in. See ACT-36 for the design.
package agentsession

import "encoding/json"

// The wire protocol is deliberately thin: Acta forwards a backend's events
// through in an envelope and lets the common shape emerge once a second backend
// exists, rather than designing a neutral schema up front. All frames are
// newline-independent JSON objects with a "t" discriminator.

// Frame kinds sent by a harness to Acta.
const (
	// FrameHello is the first frame on a harness connection: the harness
	// announces its label and backends, the session ids it holds (so Acta can
	// mark them resumable again after a reconnect), and which of those have a
	// process running right now. Acta keys nothing on
	// harness identity — the label is only for a human choosing where to spawn.
	FrameHello = "hello"
	// FrameEvent carries one backend event verbatim (a Claude Code stream-json
	// message, wrapped) or a harness-authored notice, for a session.
	FrameEvent = "event"
	// FrameSpawned / FrameSpawnError report the outcome of a spawn request.
	FrameSpawned    = "spawned"
	FrameSpawnError = "spawn_error"
	// FrameExit reports that a session's process ended.
	FrameExit = "exit"
)

// Frame kinds sent by Acta to a harness.
const (
	// FrameSpawn asks the harness to run the process the Driver composed
	// (cmd, args, env, in cwd) for a session.
	FrameSpawn = "spawn"
	// FrameStop signals a session's process (the fallback when the backend
	// has no interrupt line of its own).
	FrameStop = "stop"
	// FrameForget tells the harness a session has been deleted: kill its
	// process and drop it from the harness's records. The backend's own
	// transcript on the host is left alone.
	FrameForget = "forget"
	// FrameWrite carries one stdin line, composed by Acta from the backend's
	// Driver, for the harness to write to the session's process unchanged.
	// The harness never composes a line itself.
	FrameWrite = "write"
	// FrameTail / FrameUntail ask the harness to stream a file on its host
	// as it grows (a background command's output) and to stop.
	FrameTail   = "tail"
	FrameUntail = "untail"
	// Ls asks the harness to complete a directory path on its host (the
	// working-directory picker); LsResult is the harness's answer.
	FrameLs       = "ls"
	FrameLsResult = "ls_result"
)

// Inbound is a frame read from a harness. Only the fields relevant to a kind
// are set; Payload holds a FrameEvent's verbatim body.
type Inbound struct {
	T        string          `json:"t"`
	V        int             `json:"v,omitempty"` // hello: protocol version (2)
	Session  string          `json:"session,omitempty"`
	Label    string          `json:"label,omitempty"`
	Backends []string        `json:"backends,omitempty"`
	Sessions []string        `json:"sessions,omitempty"`
	Running  []string        `json:"running,omitempty"` // hello: the subset of Sessions with a live process
	Cwd      string          `json:"cwd,omitempty"`     // hello: where the harness itself runs
	Home     string          `json:"home,omitempty"`    // hello: the harness user's home, for ~ expansion
	ID       string          `json:"id,omitempty"`      // ls_result: the request it answers
	Path     string          `json:"path,omitempty"`    // ls_result: the path that was listed
	Dirs     []string        `json:"dirs,omitempty"`    // ls_result: matching directories, absolute
	Exists   bool            `json:"exists,omitempty"`  // ls_result: the path itself is a directory
	Styles   json.RawMessage `json:"styles,omitempty"`  // spawned: the output styles the session can use
	Resumed  bool            `json:"resumed,omitempty"` // spawned: the process was resumed, not freshly started
	Kind     string          `json:"kind,omitempty"`    // event: set only for harness-authored notices (task_output); Acta labels backend lines
	Payload  json.RawMessage `json:"payload,omitempty"`
	Error    string          `json:"error,omitempty"`
	Code     *int            `json:"code,omitempty"`
	Stderr   string          `json:"stderr,omitempty"` // exit: the tail of the process's stderr
}

// Outbound is a frame written to a harness.
type Outbound struct {
	T       string `json:"t"`
	Session string `json:"session,omitempty"`
	Backend string `json:"backend,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	ID      string `json:"id,omitempty"`   // ls: request id echoed in the ls_result; tail: the task id
	Path    string `json:"path,omitempty"` // ls: the (partial) directory path to complete; tail: the file
	// spawn: the process to run, composed by the backend's Driver.
	Cmd    string            `json:"cmd,omitempty"`
	Args   []string          `json:"args,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Resume bool              `json:"resume,omitempty"` // the process continues an earlier conversation
	Styles bool              `json:"styles,omitempty"` // report the output styles available in cwd
	// write: one stdin line, without its newline.
	Line string `json:"line,omitempty"`
}

// BrowserOp is a browser-authored operation on the session, in neutral terms
// the backend's Driver turns into its own lines. It is recorded verbatim as a
// "control" frame so the transcript shows what was asked.
//
//	answer        id, outcome (allow|deny|accept|decline|cancel), message, input, permissions, answers, content
//	setting       id, key (permission_mode|model|output_style|effort|fast|personality|service_tier), value
//	catalog       id — ask for the models, commands and settings the pickers show
//	rewind        id, target (a user message id) — drop the conversation from there
//	rewind_files  id, target, dry_run — restore the files a message changed
//	side_question id, question — ask the model something off the record
type BrowserOp struct {
	Op          string          `json:"op"`
	ID          string          `json:"id,omitempty"`
	Kind        string          `json:"kind,omitempty"` // answer: the request's subtype, for a backend whose answers differ by kind
	Outcome     string          `json:"outcome,omitempty"`
	Message     string          `json:"message,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Permissions json.RawMessage `json:"permissions,omitempty"`
	Answers     json.RawMessage `json:"answers,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
	Key         string          `json:"key,omitempty"`
	Value       string          `json:"value,omitempty"`
	Target      string          `json:"target,omitempty"`
	DryRun      bool            `json:"dry_run,omitempty"`
	Question    string          `json:"question,omitempty"`
}

// BrowserIn is a frame read from a browser chat connection.
type BrowserIn struct {
	T       string          `json:"t"`                 // "input" | "stop" | "control" | "mark" | "focus"
	On      bool            `json:"on,omitempty"`      // for "focus": the tab is visible and focused
	Text    string          `json:"text"`              // for "input"
	Images  []ImageIn       `json:"images,omitempty"`  // for "input": pictures attached to the message
	Payload json.RawMessage `json:"payload,omitempty"` // for "control": the backend control message
}

// ImageIn is one picture attached to a message: base64 bytes plus their media
// type, exactly what the backend's image content block wants.
type ImageIn struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
