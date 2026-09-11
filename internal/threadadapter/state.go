package threadadapter

import (
	"acta/internal/threads"
	"bytes"
	"encoding/json"
	"fmt"
)

type Message struct {
	Turn       string
	Kind       string
	Started    any
	Completed  bool
	Submission string
}
type State struct {
	InteractionLanes  map[string]string               `json:"interaction_lanes,omitempty"`
	Lanes             map[string]*Lane                `json:"lanes,omitempty"`
	BackgroundHistory map[string]*BackgroundRecord    `json:"background_history,omitempty"`
	Compactions       map[string]*Compaction          `json:"compactions,omitempty"`
	QuestionRequests  map[string]bool                 `json:"question_requests,omitempty"`
	Tools             map[string]*ToolCall            `json:"tools,omitempty"`
	Thinking          map[string]*Thinking            `json:"thinking,omitempty"`
	Claude            ClaudeState                     `json:"claude"`
	RunID             string                          `json:"run_id"`
	NativeID          string                          `json:"native_id"`
	Configuration     map[string]any                  `json:"configuration"`
	Turns             map[string]string               `json:"turns"`
	Messages          map[string]Message              `json:"messages"`
	Submissions       map[string]string               `json:"submissions,omitempty"` // Acta submission UUID -> provider run
	SubmissionImages  map[string][]threads.InputImage `json:"submission_images,omitempty"`
	SubmissionTexts   map[string]string               `json:"submission_texts,omitempty"` // Persisted Acta send text, never inferred from a receipt
}

// DecodeState uses the same number semantics as live provider parsing.
func DecodeState(raw []byte) (State, error) {
	var s State
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&s)
	return s, err
}

func (s State) clone() (State, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return State{}, fmt.Errorf("encode adapter checkpoint: %w", err)
	}
	next, err := DecodeState(raw)
	if err != nil {
		return State{}, fmt.Errorf("decode adapter checkpoint: %w", err)
	}
	return next, nil
}
