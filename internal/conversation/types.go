// Package conversation assembles provider-independent frames into durable UI items.
// It has no database or browser dependencies; persistence owns transaction boundaries.
package conversation

import (
	"acta2/internal/threads"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Object = map[string]any

// Item has separate stable history position and mutable revision. Payload is the
// rendered item, including lifecycle metadata; Internal is reducer-only state.
type Item struct {
	ID          string `json:"id"`
	LaneID      string `json:"lane_id,omitempty"`
	Sequence    int64  `json:"sequence"`
	OutputIndex int    `json:"output_index"`
	Revision    int64  `json:"revision"`
	RunID       string `json:"run_id"`
	TurnID      string `json:"turn_id"`
	Visibility  string `json:"visibility"`
	Deleted     bool   `json:"deleted"`
	Payload     Object `json:"payload"`
	Internal    Object `json:"-"`
}
type Current struct {
	Lanes       map[string]*Current      `json:"lanes,omitempty"`
	Agents      map[string]threads.Frame `json:"agents,omitempty"`
	Background  map[string]threads.Frame `json:"background,omitempty"`
	RunID       string                   `json:"run_id"`
	Frames      map[string]threads.Frame `json:"frames"`
	Pending     map[string]threads.Frame `json:"pending"`
	MCP         map[string]string        `json:"mcp,omitempty"`
	OpenMCP     string                   `json:"open_mcp,omitempty"`
	TerminalMCP map[string]string        `json:"terminal_mcp,omitempty"`
}
type Page struct {
	ProjectionVersion int     `json:"projection_version"`
	LaneID            string  `json:"lane_id"`
	ThreadID          string  `json:"thread_id"`
	Items             []*Item `json:"items"`
	Current           Current `json:"current"`
	Revision          int64   `json:"revision"`
	CursorRevision    int64   `json:"cursor_revision"`
	Next              string  `json:"next"`
	HasMore           bool    `json:"has_more"`
}
type Query struct {
	LaneID string
	Before string
	After  *int64
	Debug  bool
	Limit  int
}
type Repository interface {
	Get(string) (*Item, error)
	Save(*Item) error
	InTurn(string, string) ([]*Item, error)
}
type Reducer struct {
	Store   Repository
	Current *Current
}

func decode(raw []byte, dest any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	return d.Decode(dest)
}
func Data(f threads.Frame) Object {
	var d Object
	_ = decode(f.Data, &d)
	if d == nil {
		d = Object{}
	}
	return d
}
func obj(v any) Object {
	d, _ := v.(map[string]any)
	if d == nil {
		return Object{}
	}
	return d
}
func str(v any) string           { s, _ := v.(string); return s }
func yes(v any) bool             { b, _ := v.(bool); return b }
func list(v any) []any           { a, _ := v.([]any); return a }
func key(parts ...string) string { b, _ := json.Marshal(parts); return string(b) }
func frameKey(f threads.Frame) string {
	return fmt.Sprintf("%s:%d:%d", f.ThreadID, f.Sequence, f.OutputIndex)
}
func frameObject(f threads.Frame) Object {
	raw, _ := json.Marshal(f)
	var d Object
	_ = decode(raw, &d)
	return d
}
func pretty(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}
func terminal(v string) bool {
	switch v {
	case "completed", "failed", "interrupted", "declined", "permission_denied":
		return true
	}
	return false
}
func (r *Reducer) item(f threads.Frame, id, kind string) (*Item, error) {
	i, e := r.Store.Get(id)
	if e != nil {
		return nil, e
	}
	if i != nil {
		return i, nil
	}
	return &Item{ID: id, Sequence: f.Sequence, OutputIndex: f.OutputIndex, RunID: f.RunID, TurnID: str(Data(f)["turn_id"]), Visibility: "normal", Payload: Object{"kind": kind, "id": id, "frame": frameObject(f), "started_at": f.ReceivedAt, "completed_at": nil}, Internal: Object{}}, nil
}
func (r *Reducer) save(i *Item, f threads.Frame) error {
	i.Revision = f.Sequence
	return r.Store.Save(i)
}
func (r *Reducer) event(f threads.Frame, visibility string) error {
	i, e := r.item(f, frameKey(f), "frame")
	if e != nil {
		return e
	}
	i.Visibility = visibility
	return r.save(i, f)
}

// ErrCursor identifies malformed or out-of-range pagination input.
var ErrCursor = errors.New("invalid conversation cursor")
