package agentsession

import (
	"encoding/json"

	"github.com/peios/acta/internal/agentsession/claude"
	"github.com/peios/acta/internal/agentsession/codex"
	"github.com/peios/acta/internal/agentsession/model"
	"github.com/peios/acta/internal/store"
)

// A Driver is everything Acta knows about one backend besides projecting its
// frames: how to launch it, what to write to its stdin, what its output lines
// are called. The harness is a supervisor with no backend knowledge of its
// own — it runs the command a spawn names, pipes bytes both ways, tails files
// it is asked to tail, and reports exits — so adding a backend is a Driver
// and a Projector here, never a harness change. See ACT-37.
type Driver interface {
	// Launch composes the process a harness runs for a session, fresh or
	// resuming the backend's own conversation.
	Launch(as store.AgentSession, resume bool) Launch
	// Kind labels one stdout line for storage (the transcript's coarse
	// filter) and for the rules below.
	Kind(payload json.RawMessage) string
	// Stored reports whether a line of that kind is part of the record.
	// Streamed deltas are relayed live and not stored: the settled frame
	// that follows carries everything they said.
	Stored(kind string, payload json.RawMessage) bool
	// Acknowledged reports whether a line proves the backend took the last
	// message, so a message held back for a failed resume can be dropped.
	Acknowledged(kind string, payload json.RawMessage) bool
	// StartLines are written as soon as the process is up: a handshake, a
	// thread to open or resume. Nil for a backend that needs none.
	StartLines(as store.AgentSession, resume bool) [][]byte
	// InputLine composes the stdin line that delivers a user message (the
	// session's options say which conversation and turn it belongs to).
	InputLine(as store.AgentSession, text string, images []ImageIn) []byte
	// InterruptLine composes the stdin line that ends the current turn while
	// keeping the process; nil when the backend has none (signal instead).
	InterruptLine(as store.AgentSession) []byte
	// ControlLines composes the stdin lines for a browser operation (see
	// BrowserOp): an approval answer, a setting change, a catalogue request,
	// a rewind step. Nil drops it.
	ControlLines(as store.AgentSession, op BrowserOp) [][]byte
	// Notes extracts, from a stored frame, what the session must remember
	// for later lines: the backend's own conversation id (for a resume), the
	// active turn (for an interrupt). Keys go into the session's options; an
	// empty value removes the key.
	Notes(sessionID, kind string, payload json.RawMessage) map[string]string
	// Option extracts a setting change from an outgoing line (a mode or
	// model control, an /effort message) to remember for the next resume.
	Option(kind string, payload json.RawMessage) (key, value string, ok bool)
	// BackgroundTask recognises a frame that starts a background task whose
	// output lands in a file on the harness host, for the harness to tail.
	BackgroundTask(kind string, payload json.RawMessage) (id, path string, ok bool)
	// TaskEnded recognises the frame that ends such a task.
	TaskEnded(kind string, payload json.RawMessage) (id string, ok bool)
	// ResumeFailed reports whether an exit means the resume found no
	// conversation to continue, so the session should start fresh instead.
	ResumeFailed(code int, stderr string) bool
	// Rename composes the lines that give the backend's own session Acta's
	// title, plus the input text to record in the transcript for them ("" to
	// record nothing).
	Rename(as store.AgentSession, title string) (lines [][]byte, echo string)
	// TitleRequest composes a line asking the backend to name the session
	// after its first message; nil when the backend cannot. TitleAnswer
	// recognises the reply (by the request id), so it never reaches the
	// transcript.
	TitleRequest(requestID, description string) []byte
	TitleAnswer(kind string, payload json.RawMessage) (requestID, title string, ok bool)
	// Transcript is the backend's own record of a session on the host, for
	// the harness to read before a resume: the file (a glob) and the last
	// line Acta already holds, found among the session's stored frames. ok
	// is false for a backend without such a record.
	Transcript(as store.AgentSession, events []store.AgentSessionEvent) (Catchup, bool)
	// TranscriptRecords picks, from lines read off that record, the ones
	// worth storing (the live branch of the conversation), in order.
	TranscriptRecords(lines []json.RawMessage) []TranscriptRecord
	// Projector returns a fresh projector for the backend's frames.
	Projector() model.Projector
}

// Launch is the process a harness runs: command, arguments, extra
// environment, and whether to report the output styles available in the
// working directory (a Claude Code notion the picker needs).
type Launch struct {
	Cmd    string            `json:"cmd"`
	Args   []string          `json:"args"`
	Env    map[string]string `json:"env,omitempty"`
	Styles bool              `json:"styles,omitempty"`
}

// drivers is the registry of backends Acta can run.
var drivers = map[string]Driver{
	"claude-code": claudeDriver{},
	"codex":       codexDriver{},
}

// DriverFor returns the driver for a backend name, or nil.
func DriverFor(backend string) Driver { return drivers[backend] }

// Backends lists the backend names Acta has drivers for.
func Backends() []string {
	out := make([]string, 0, len(drivers))
	for k := range drivers {
		out = append(out, k)
	}
	return out
}

// claudeDriver adapts the claude package to the Driver interface (the
// package itself stays free of the hub's types).
type claudeDriver struct{}

func (claudeDriver) Launch(as store.AgentSession, resume bool) Launch {
	l := claude.Launch(as.ID, as.Options, resume)
	return Launch{Cmd: l.Cmd, Args: l.Args, Env: l.Env, Styles: true}
}
func (claudeDriver) Kind(p json.RawMessage) string              { return claude.Kind(p) }
func (claudeDriver) Stored(kind string, p json.RawMessage) bool { return claude.Stored(kind, p) }
func (claudeDriver) Acknowledged(kind string, _ json.RawMessage) bool {
	return kind == "assistant"
}
func (claudeDriver) StartLines(store.AgentSession, bool) [][]byte { return nil }
func (claudeDriver) InputLine(_ store.AgentSession, text string, images []ImageIn) []byte {
	imgs := make([]claude.Image, 0, len(images))
	for _, im := range images {
		imgs = append(imgs, claude.Image{MediaType: im.MediaType, Data: im.Data})
	}
	return claude.InputLine(text, imgs)
}
func (claudeDriver) InterruptLine(store.AgentSession) []byte { return claude.InterruptLine() }
func (claudeDriver) ControlLines(_ store.AgentSession, op BrowserOp) [][]byte {
	return claude.ControlLines(claude.Op{Op: op.Op, ID: op.ID, Outcome: op.Outcome, Message: op.Message, Input: op.Input, Permissions: op.Permissions, Answers: op.Answers, Content: op.Content, Key: op.Key, Value: op.Value, Target: op.Target, DryRun: op.DryRun, Question: op.Question})
}
func (claudeDriver) Notes(id, kind string, p json.RawMessage) map[string]string {
	if c := claude.Conversation(id, kind, p); c != "" {
		return map[string]string{"conversation": c}
	}
	return nil
}
func (claudeDriver) Option(kind string, p json.RawMessage) (string, string, bool) {
	return claude.Option(kind, p)
}
func (claudeDriver) BackgroundTask(kind string, p json.RawMessage) (string, string, bool) {
	return claude.BackgroundTask(kind, p)
}
func (claudeDriver) TaskEnded(kind string, p json.RawMessage) (string, bool) {
	return claude.TaskEnded(kind, p)
}
func (claudeDriver) ResumeFailed(code int, stderr string) bool {
	return claude.ResumeFailed(code, stderr)
}
func (claudeDriver) Rename(_ store.AgentSession, title string) ([][]byte, string) {
	return [][]byte{claude.RenameLine(title), claude.InputLine("/rename "+title, nil)}, "/rename " + title
}
func (claudeDriver) TitleRequest(id, desc string) []byte { return claude.TitleRequestLine(id, desc) }
func (claudeDriver) TitleAnswer(kind string, p json.RawMessage) (string, string, bool) {
	return claude.TitleAnswer(kind, p)
}
func (claudeDriver) Transcript(as store.AgentSession, events []store.AgentSessionEvent) (Catchup, bool) {
	after := ""
	for i := len(events) - 1; i >= 0 && after == ""; i-- {
		after = claude.Leaf(events[i].Kind, events[i].Payload)
	}
	return Catchup{Path: claude.TranscriptGlob(as.ID, as.Options), Key: "uuid", After: after}, true
}
func (claudeDriver) TranscriptRecords(lines []json.RawMessage) []TranscriptRecord {
	recs := claude.ChainRecords(lines)
	out := make([]TranscriptRecord, 0, len(recs))
	for _, r := range recs {
		out = append(out, TranscriptRecord{Payload: r.Payload, At: r.At})
	}
	return out
}
func (claudeDriver) Projector() model.Projector { return claude.New() }

// codexDriver adapts the codex package to the Driver interface.
type codexDriver struct{}

func (codexDriver) Launch(as store.AgentSession, _ bool) Launch {
	l := codex.Launch()
	return Launch{Cmd: l.Cmd, Args: l.Args, Env: l.Env}
}
func (codexDriver) Kind(p json.RawMessage) string              { return codex.Kind(p) }
func (codexDriver) Stored(kind string, p json.RawMessage) bool { return codex.Stored(kind, p) }
func (codexDriver) Acknowledged(kind string, p json.RawMessage) bool {
	return codex.Acknowledged(kind, p)
}
func (codexDriver) StartLines(as store.AgentSession, resume bool) [][]byte {
	return codex.StartLines(as.ID, as.Options, as.Cwd, resume)
}
func (codexDriver) InputLine(as store.AgentSession, text string, images []ImageIn) []byte {
	imgs := make([]codex.Image, 0, len(images))
	for _, im := range images {
		imgs = append(imgs, codex.Image{MediaType: im.MediaType, Data: im.Data})
	}
	return codex.InputLine(as.Options, text, imgs)
}
func (codexDriver) InterruptLine(as store.AgentSession) []byte {
	return codex.InterruptLine(as.Options)
}
func (codexDriver) ControlLines(as store.AgentSession, op BrowserOp) [][]byte {
	return codex.ControlLines(as.Options, codex.Op{Op: op.Op, ID: op.ID, Kind: op.Kind, Outcome: op.Outcome, Message: op.Message, Input: op.Input, Permissions: op.Permissions, Answers: op.Answers, Content: op.Content, Key: op.Key, Value: op.Value, Target: op.Target, DryRun: op.DryRun, Question: op.Question})
}
func (codexDriver) Notes(_, kind string, p json.RawMessage) map[string]string {
	return codex.Notes(kind, p)
}
func (codexDriver) Option(kind string, p json.RawMessage) (string, string, bool) {
	return codex.Option(kind, p)
}
func (codexDriver) BackgroundTask(string, json.RawMessage) (string, string, bool) {
	return "", "", false
}
func (codexDriver) TaskEnded(string, json.RawMessage) (string, bool) { return "", false }
func (codexDriver) ResumeFailed(int, string) bool                    { return false }
func (codexDriver) Rename(as store.AgentSession, title string) ([][]byte, string) {
	return [][]byte{codex.RenameLine(as.Options, title)}, ""
}
func (codexDriver) TitleRequest(string, string) []byte { return nil }
func (codexDriver) TitleAnswer(string, json.RawMessage) (string, string, bool) {
	return "", "", false
}
func (codexDriver) Transcript(store.AgentSession, []store.AgentSessionEvent) (Catchup, bool) {
	return Catchup{}, false
}
func (codexDriver) TranscriptRecords([]json.RawMessage) []TranscriptRecord { return nil }
func (codexDriver) Projector() model.Projector                             { return codex.New() }
