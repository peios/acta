// Package threads defines the provider-independent thread stream.
package threads

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"acta2/learn/schemas"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

type ProviderFrame struct {
	ThreadID   string          `json:"thread_id"`
	RunID      string          `json:"run_id"`
	Sequence   int64           `json:"sequence"`
	Provider   string          `json:"provider"`
	ReceivedAt time.Time       `json:"received_at"`
	Raw        json.RawMessage `json:"-"` // Capture input only; wire data lives in the debug payload.
}

type Frame struct {
	ProviderFrame
	LaneID        string          `json:"lane_id,omitempty"`
	SchemaVersion int             `json:"schema_version"`
	OutputIndex   int             `json:"output_index"`
	OccurredAt    *time.Time      `json:"occurred_at"`
	Kind          string          `json:"kind"`
	Data          json.RawMessage `json:"data"`
}

type OutputReference struct {
	OutputIndex int    `json:"output_index"`
	Kind        string `json:"kind"`
}

var frameSchema = func() *jsonschema.Resolved {
	var s jsonschema.Schema
	if err := json.Unmarshal(schemas.ThreadFrame, &s); err != nil {
		panic(err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		panic(err)
	}
	return r
}()

func (f Frame) Validate() error {
	if _, err := uuid.Parse(f.ThreadID); err != nil {
		return err
	}
	if _, err := uuid.Parse(f.RunID); err != nil {
		return err
	}
	if f.ReceivedAt.IsZero() {
		return errors.New("missing capture time")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	var value any
	if err = json.Unmarshal(raw, &value); err != nil {
		return err
	}
	return frameSchema.Validate(value)
}

// ValidateBundle ensures acknowledgement can never split a captured input.
func ValidateBundle(bundle []Frame) error {
	if len(bundle) == 0 {
		return errors.New("empty frame bundle")
	}
	first := bundle[0]
	for i, f := range bundle {
		if f.OutputIndex != i || f.ThreadID != first.ThreadID || f.RunID != first.RunID || f.Sequence != first.Sequence || f.Provider != first.Provider || !f.ReceivedAt.Equal(first.ReceivedAt) || !sameTime(f.OccurredAt, first.OccurredAt) {
			return errors.New("inconsistent frame bundle")
		}
		if err := f.Validate(); err != nil {
			return fmt.Errorf("invalid %s: %w", f.Kind, err)
		}
		if (i == 0) != strings.HasPrefix(f.Kind, "debug/") {
			return errors.New("invalid debug output position")
		}
	}
	if len(bundle) == 1 {
		if first.Kind == "debug/resolved" {
			return errors.New("resolved bundle has no outputs")
		}
		return nil
	}
	if first.Kind != "debug/resolved" {
		return errors.New("derived outputs require resolved debug frame")
	}
	var d struct {
		Outputs []OutputReference `json:"outputs"`
	}
	_ = json.Unmarshal(first.Data, &d)
	if len(d.Outputs) != len(bundle)-1 {
		return errors.New("incomplete output references")
	}
	for i, r := range d.Outputs {
		if r.OutputIndex != i+1 || r.Kind != bundle[i+1].Kind {
			return errors.New("conflicting output reference")
		}
	}
	return nil
}
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func NewFrame(p ProviderFrame, kind string, data any) Frame {
	raw, err := json.Marshal(data)
	if err != nil {
		panic(err)
	} // Only JSON-compatible adapter values.
	return Frame{ProviderFrame: p, SchemaVersion: 1, Kind: kind, Data: raw}
}
func Unknown(p ProviderFrame) (Frame, error) {
	if p.ThreadID == "" || p.RunID == "" || p.Sequence < 1 || p.Provider == "" || p.ReceivedAt.IsZero() || !json.Valid(p.Raw) {
		return Frame{}, errors.New("invalid captured provider frame")
	}
	p.Raw = append(json.RawMessage(nil), p.Raw...)
	return NewFrame(p, "debug/unknown", map[string]any{"raw": string(p.Raw), "stream": "stdout", "reason": "Provider frame is not recognized."}), nil
}
