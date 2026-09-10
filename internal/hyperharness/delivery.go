package hyperharness

import (
	"acta2/internal/harnesspipe"
	"acta2/internal/threadadapter"
	"acta2/internal/threads"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
)

// Frames persists interpretation and its reducer state before exposing any output.
// An adapter upgrade can therefore never reinterpret a delivered capture on retry.
func (c *Controller) Frames(ctx context.Context, id string) ([]threads.Frame, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.records[id]
	if r == nil || !r.Discovered {
		return nil, errors.New("thread not discovered")
	}
	if len(r.Pending) > 0 {
		c.offered[id] = r.Pending[len(r.Pending)-1].Sequence
		return append([]threads.Frame(nil), r.Pending...), nil
	}
	copy := *r
	copy.MappedThrough = max(copy.MappedThrough, copy.Ack)
	raw, err := c.pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: id, After: copy.MappedThrough})
	if err != nil {
		return nil, err
	}
	encoded, e := json.Marshal(r.Adapter)
	if e != nil {
		return nil, e
	}
	copy.Adapter, e = threadadapter.DecodeState(encoded)
	if e != nil {
		return nil, e
	}
	copy.Adapter.NativeID = r.Thread.ProviderID
	for _, l := range copy.Adapter.Lanes {
		l.State.Submissions = map[string]string{}
		l.State.SubmissionTexts = map[string]string{}
		l.State.SubmissionImages = map[string][]threads.InputImage{}
	}
	copy.Adapter.Submissions = map[string]string{}
	copy.Adapter.SubmissionTexts = map[string]string{}
	copy.Adapter.SubmissionImages = map[string][]threads.InputImage{}
	for command, request := range r.Requests {
		if request.Action == "send" {
			target := &copy.Adapter
			if request.LaneID != "" {
				l := copy.Adapter.Lanes[request.LaneID]
				if l == nil {
					continue
				}
				target = &l.State
			}
			target.Submissions[command] = request.RunID
			target.SubmissionTexts[command] = request.Text
			target.SubmissionImages[command] = request.Images
		}
	}
	copy.Commands = maps.Clone(r.Commands)
	if copy.Commands == nil {
		copy.Commands = map[string]threads.Result{}
	}
	for _, f := range raw {
		if f.Sequence != copy.MappedThrough+1 {
			return nil, errors.New("capture sequence gap")
		}
		state, bundle, err := threadadapter.Map(copy.Adapter, threads.ProviderFrame{ThreadID: id, RunID: f.RunID, Sequence: f.Sequence, Provider: r.Thread.Provider, ReceivedAt: f.ReceivedAt}, f.Stream, f.Text())
		if err != nil {
			return nil, err
		}
		copy.Adapter = state
		copy.reconcileCapture(f, bundle)
		copy.Pending = append(copy.Pending, bundle...)
		copy.MappedThrough = f.Sequence
	}
	if len(copy.Pending) > 0 {
		if err = c.save(&copy); err != nil {
			return nil, err
		}
		c.records[id] = &copy
		c.offered[id] = copy.MappedThrough
	}
	return append([]threads.Frame(nil), copy.Pending...), nil
}
func (c *Controller) Acknowledge(id string, sequence int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.records[id]
	if r == nil {
		return errors.New("unknown thread")
	}
	if sequence <= r.Ack {
		return nil
	}
	if sequence > c.offered[id] {
		return errors.New("acknowledgement exceeds delivered frames")
	}
	copy := *r
	copy.Ack = sequence
	copy.Pending = nil
	for _, f := range r.Pending {
		if f.Sequence > sequence {
			copy.Pending = append(copy.Pending, f)
		}
	}
	if err := c.save(&copy); err != nil {
		return err
	}
	c.records[id] = &copy
	return nil
}

// Reconcile only evidence for a persisted command in this exact provider run.
// Called on the unpublished delivery copy, before its checkpoint is saved.
func (r *localThread) reconcileCapture(f harnesspipe.RawFrame, bundle []threads.Frame) {
	if f.RunID != r.Thread.RunID {
		return
	}
	if r.Thread.Provider == "codex" && f.Stream == "stdout" {
		var reply struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		prefix := f.RunID + "/thread/settings/update/"
		if json.Unmarshal(f.Data, &reply) == nil && strings.HasPrefix(reply.ID, prefix) && len(reply.Result) > 0 && string(reply.Result) != "null" && (len(reply.Error) == 0 || string(reply.Error) == "null") {
			command := strings.TrimPrefix(reply.ID, prefix)
			if q, ok := r.Requests[command]; ok && q.Action == "configure" && q.RunID == f.RunID {
				r.Commands[q.ID] = threads.Result{ID: q.ID, ThreadID: r.Thread.ID, Outcome: "accepted"}
			}
		}
	}
	for _, output := range bundle {
		switch output.Kind {
		case "thread/configuration":
			if output.LaneID != "" {
				continue
			}
			var config threads.ModelSettings
			if json.Unmarshal(output.Data, &config) == nil && config.Model != "" {
				r.Configuration = &config
			}
		case "message/user":
			var message struct {
				SubmissionID string `json:"submission_id"`
			}
			if json.Unmarshal(output.Data, &message) != nil {
				continue
			}
			if q, ok := r.Requests[message.SubmissionID]; ok && q.Action == "send" && q.RunID == f.RunID {
				r.Commands[q.ID] = threads.Result{ID: q.ID, ThreadID: r.Thread.ID, Outcome: "accepted"}
			}
		}
	}
}
