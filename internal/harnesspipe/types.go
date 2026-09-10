// Package harnesspipe owns provider processes and raw transport, without
// interpreting provider protocols. The same engine serves embedded and detached
// operation; only the private local transport differs.
package harnesspipe

import (
	"context"
	"encoding/json"
	"time"
)

// MaxInput bounds one immutable provider input, including embedded images.
const MaxInput = 8 * 1024 * 1024

const MaxFrame = 16 * 1024 * 1024

// JSON string escaping and a derived text snapshot can expand a capture. Keep
// transport limits consistent with the capture limit, including worst-case escapes.
const MaxRecord = MaxFrame*8 + 1024*1024
const MaxWire = MaxFrame*16 + 1024*1024

type Spec struct {
	ThreadID   string   `json:"thread_id"`
	RunID      string   `json:"run_id"`
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	CWD        string   `json:"cwd"`
}
type Process struct {
	Spec  Spec   `json:"spec"`
	State string `json:"state"` // starting, running, exited, uncertain
	Error string `json:"error,omitempty"`
	PID   int    `json:"pid,omitempty"`
}
type RawFrame struct {
	Sequence   int64           `json:"sequence"`
	RunID      string          `json:"run_id"`
	ReceivedAt time.Time       `json:"received_at"`
	Stream     string          `json:"stream"`
	Data       json.RawMessage `json:"data"`
	Original   *string         `json:"original,omitempty"`
}
type WriteRequest struct {
	RunID string `json:"run_id"`
	ID    string `json:"id"`
	Data  string `json:"data"`
}
type ReadRequest struct {
	ThreadID string `json:"thread_id"`
	After    int64  `json:"after"`
}
type API interface {
	Spawn(context.Context, Spec) (Process, error)
	Write(context.Context, WriteRequest) error
	Kill(context.Context, string) error
	Processes(context.Context) ([]Process, error)
	Read(context.Context, ReadRequest) ([]RawFrame, error)
}

// Text retains exact capture bytes in new journals. Older detached pipes are
// readable during an upgrade; their JSON encoder already normalized stdout.
func (f RawFrame) Text() string {
	if f.Original != nil {
		return *f.Original
	}
	if f.Stream == "stderr" {
		var text string
		if json.Unmarshal(f.Data, &text) == nil {
			return text
		}
	}
	return string(f.Data)
}
