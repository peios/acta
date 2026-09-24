package codeagents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"acta/internal/codethreads"
	"github.com/google/uuid"
)

// One outstanding turn per process. Provider protocols stay here; the host only
// sees user acknowledgements and the completion of Send.
type messages struct {
	mu          sync.Mutex
	active      *messageTurn
	stdin       io.Writer
	processDone <-chan struct{}
	cancel      context.CancelFunc
	emit        func(codethreads.Payload)
	provider    string
}
type messageTurn struct {
	input             codethreads.Input
	requestID, turnID string
	acked             bool
	ack               chan struct{}
	done              chan error
}

func (m *messages) send(ctx context.Context, input codethreads.Input, nativeThread string) error {
	if input.Delivery != "start_turn" {
		return errors.New("Only start_turn is supported.")
	}
	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		return errors.New("A turn is already active.")
	}
	t := &messageTurn{input: input, requestID: uuid.NewString(), ack: make(chan struct{}), done: make(chan error, 1)}
	m.active = t
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.active = nil; m.mu.Unlock() }()
	var request any
	if m.provider == "claude" {
		request = map[string]any{"type": "user", "uuid": input.ID, "session_id": "", "parent_tool_use_id": nil, "message": map[string]string{"role": "user", "content": input.Text}}
	} else {
		request = map[string]any{"id": t.requestID, "method": "turn/start", "params": map[string]any{"threadId": nativeThread, "clientUserMessageId": input.ID, "input": []any{map[string]string{"type": "text", "text": input.Text}}}}
	}
	written := make(chan error, 1)
	go func() { written <- json.NewEncoder(m.stdin).Encode(request) }()
	writeTimer := time.NewTimer(5 * time.Second)
	defer writeTimer.Stop()
	select {
	case err := <-written:
		if err != nil {
			m.cancel()
			return errors.New("Provider input write failed; delivery is uncertain. This input will not be resent.")
		}
	case <-writeTimer.C:
		m.cancel()
		<-written
		return errors.New("Provider input write timed out; delivery is uncertain. This input will not be resent.")
	case <-ctx.Done():
		m.cancel()
		<-written
		return errors.New("Host stopped during submission; delivery is uncertain.")
	}
	ackTimer := time.NewTimer(60 * time.Second)
	defer ackTimer.Stop()
	ack := t.ack
	var deadline <-chan time.Time = ackTimer.C
	for {
		select {
		case err := <-t.done:
			return err
		case <-ack:
			ack = nil
			deadline = nil
		case <-deadline:
			m.cancel()
			return errors.New("Provider acknowledgement timed out; delivery is uncertain. This input will not be resent.")
		case <-m.processDone:
			select {
			case err := <-t.done:
				return err
			default:
			}
			return errors.New("Provider exited before the turn completed. Check its debug output.")
		case <-ctx.Done():
			return errors.New("Host stopped before the turn completed.")
		}
	}
}

func (m *messages) acknowledge(t *messageTurn, id, text, turnID string) {
	if t.acked {
		return
	}
	t.acked = true
	m.emit(codethreads.Payload{Type: "message.user", Provider: m.provider, ID: id, InputID: t.input.ID, Text: text, TurnID: turnID})
	close(t.ack)
}
func (m *messages) complete(t *messageTurn, err error) {
	if err == nil && !t.acked {
		err = errors.New("The turn ended without a matching user acknowledgement; delivery is uncertain.")
	}
	select {
	case t.done <- err:
	default:
	}
}
func (m *messages) observe(raw []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.active
	if t == nil {
		return
	}
	if m.provider == "claude" {
		var event struct {
			Type    string  `json:"type"`
			UUID    string  `json:"uuid"`
			Parent  *string `json:"parent_tool_use_id"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return
		}
		if event.Type == "user" && event.Parent == nil && event.Message.Role == "user" {
			text, ok := messageText(event.Message.Content)
			if ok && text == t.input.Text && (event.UUID == t.input.ID || event.UUID == "") {
				id := event.UUID
				if id == "" {
					id = uuid.NewString()
				}
				m.acknowledge(t, id, text, "")
			}
		}
		if event.Type == "result" {
			m.complete(t, nil)
		}
		return
	}
	var event struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Error  json.RawMessage `json:"error"`
		Result struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"result"`
		Params struct {
			TurnID string `json:"turnId"`
			Turn   struct {
				ID string `json:"id"`
			} `json:"turn"`
			Item struct {
				Type     string          `json:"type"`
				ID       string          `json:"id"`
				ClientID string          `json:"clientId"`
				Content  json.RawMessage `json:"content"`
			} `json:"item"`
		} `json:"params"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	if event.Method == "" && event.ID == t.requestID {
		if len(event.Error) > 0 && string(event.Error) != "null" {
			m.complete(t, errors.New("Codex rejected the turn. Check its debug output."))
			return
		}
		if event.Result.Turn.ID == "" {
			m.cancel()
			m.complete(t, errors.New("Codex returned an invalid turn response; delivery is uncertain."))
			return
		}
		t.turnID = event.Result.Turn.ID
	}
	if event.Method == "turn/started" && t.turnID == "" {
		t.turnID = event.Params.Turn.ID
	}
	if event.Method == "item/started" || event.Method == "item/completed" {
		item := event.Params.Item
		text, ok := messageText(item.Content)
		if item.Type == "userMessage" && item.ID != "" && ok && text == t.input.Text && (t.turnID == "" || event.Params.TurnID == t.turnID) && (item.ClientID == t.input.ID || item.ClientID == "") {
			t.turnID = event.Params.TurnID
			m.acknowledge(t, item.ID, text, t.turnID)
		}
	}
	if event.Method == "turn/completed" && t.turnID != "" && event.Params.Turn.ID == t.turnID {
		m.complete(t, nil)
	}
}

// Only plain user text participates in fallback matching. Tool results, images
// and other content cannot accidentally acknowledge a pending text submission.
func messageText(raw json.RawMessage) (string, bool) {
	var text string
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return "", false
	}
	var out strings.Builder
	for _, b := range blocks {
		if b.Type != "text" {
			return "", false
		}
		out.WriteString(b.Text)
	}
	return out.String(), true
}
