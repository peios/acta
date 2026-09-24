// Package codethreads defines host-owned Acta Code Threads and CAT frames.
package codethreads

import "time"

const (
	StartMethod     = "threads.start"
	ListMethod      = "threads.list"
	ReadMethod      = "threads.read"
	SendMethod      = "threads.send"
	ReprocessMethod = "threads.reprocess"
	MaxFrameBytes   = 64 * 1024
	MaxMessageBytes = 4 * 1024
)

type Start struct {
	Provider   string `json:"provider"`
	ID         string `json:"id"`
	CodebaseID string `json:"codebase_id"`
	RootID     string `json:"root_id"`
}
type Query struct {
	CodebaseID string `json:"codebase_id"`
}
type Input struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Delivery string `json:"delivery"`
}
type Send struct {
	Input
	CodebaseID string `json:"codebase_id"`
	ThreadID   string `json:"thread_id"`
}
type Read struct {
	CodebaseID string `json:"codebase_id"`
	ThreadID   string `json:"thread_id"`
	After      uint64 `json:"after"`
	Revision   uint64 `json:"revision"`
}
type Reprocess struct {
	CodebaseID string `json:"codebase_id"`
	ThreadID   string `json:"thread_id"`
	Revision   uint64 `json:"revision"`
}
type Thread struct {
	ID              string    `json:"id"`
	CodebaseID      string    `json:"codebase_id"`
	RootID          string    `json:"root_id"`
	Provider        string    `json:"provider"`
	Name            string    `json:"name"`
	Directory       string    `json:"directory"`
	State           string    `json:"state"`
	Busy            bool      `json:"busy"`
	Revision        uint64    `json:"revision"`
	SourceTruncated bool      `json:"source_truncated"`
	Error           string    `json:"error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// Payload is produced by an adapter. The host assigns ordering and timestamps.
// Raw holds a single output line, without the line terminator.
type Payload struct {
	Type      string `json:"type"`
	Provider  string `json:"provider"`
	Stream    string `json:"stream,omitempty"`
	Raw       string `json:"raw,omitempty"`
	ID        string `json:"id,omitempty"`
	InputID   string `json:"input_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Delivery  string `json:"delivery,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	Error     string `json:"error,omitempty"`
	Level     string `json:"level,omitempty"`
	Message   string `json:"message,omitempty"`
	Target    string `json:"target,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Hook      *Hook  `json:"hook,omitempty"`
}

// Hook is a complete display snapshot of one provider hook execution.
type Hook struct {
	Context       *HookContext `json:"context,omitempty"`
	ID            string       `json:"id"`
	Event         string       `json:"event"`
	Name          string       `json:"name,omitempty"`
	Status        string       `json:"status"`
	Outputs       []HookOutput `json:"outputs"`
	ExitCode      *int         `json:"exit_code,omitempty"`
	StatusMessage string       `json:"status_message,omitempty"`
	HandlerType   string       `json:"handler_type,omitempty"`
	ExecutionMode string       `json:"execution_mode,omitempty"`
	Scope         string       `json:"scope,omitempty"`
	Source        string       `json:"source,omitempty"`
	SourcePath    string       `json:"source_path,omitempty"`
	StartedAt     *int64       `json:"started_at,omitempty"`
	CompletedAt   *int64       `json:"completed_at,omitempty"`
	DurationMS    *int64       `json:"duration_ms,omitempty"`
}
type HookContext struct {
	Texts  []string `json:"texts"`
	Source string   `json:"source"` // hook_response or context_item
}
type HookOutput struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}
type Frame struct {
	Payload
	Seq          uint64    `json:"seq"`
	SourceSeq    uint64    `json:"source_seq"`
	At           time.Time `json:"at"`
	ChangeSeq    uint64    `json:"change_seq"`
	Supersedes   string    `json:"supersedes,omitempty"`
	SupersededBy string    `json:"superseded_by,omitempty"`
}
type Page struct {
	Thread Thread  `json:"thread"`
	Frames []Frame `json:"frames"`
	Next   uint64  `json:"next"`
	First  uint64  `json:"first"`
	More   bool    `json:"more"`
	Reset  bool    `json:"reset"`
}
