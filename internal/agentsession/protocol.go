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
	// FrameSpawn asks the harness to start a backend process for an
	// Acta-minted session id (passed to the backend as its own session id).
	FrameSpawn = "spawn"
	// FrameInput injects a user message into a running session.
	FrameInput = "input"
	// FrameStop ends a session's current turn / process.
	FrameStop = "stop"
	// FrameControl carries a control-protocol message for the backend verbatim
	// (a control_response answering a permission prompt, or a control_request
	// such as set_permission_mode). The harness writes it to the process as-is.
	FrameControl = "control"
	// Ls asks the harness to complete a directory path on its host (the
	// working-directory picker); LsResult is the harness's answer.
	FrameLs       = "ls"
	FrameLsResult = "ls_result"
)

// Inbound is a frame read from a harness. Only the fields relevant to a kind
// are set; Payload holds a FrameEvent's verbatim body.
type Inbound struct {
	T        string          `json:"t"`
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
	Kind     string          `json:"kind,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Error    string          `json:"error,omitempty"`
	Code     *int            `json:"code,omitempty"`
}

// Outbound is a frame written to a harness.
type Outbound struct {
	T       string          `json:"t"`
	Session string          `json:"session,omitempty"`
	Backend string          `json:"backend,omitempty"`
	Cwd     string          `json:"cwd,omitempty"`
	Options json.RawMessage `json:"options,omitempty"`
	Text    string          `json:"text,omitempty"`
	Images  []ImageIn       `json:"images,omitempty"`  // input: pictures sent with the text
	Payload json.RawMessage `json:"payload,omitempty"` // control: the backend message verbatim
	ID      string          `json:"id,omitempty"`      // ls: request id echoed in the ls_result
	Path    string          `json:"path,omitempty"`    // ls: the (partial) directory path to complete
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
