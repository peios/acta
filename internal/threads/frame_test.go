package threads

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestUnknownFrameRetainsProviderPayloadAndReplayIdentity(t *testing.T) {
	raw := json.RawMessage(`{ "id": 17, "result": {"futureField": [1, true, null], "text": "hello"} }`)
	p := ProviderFrame{ThreadID: "acta-thread", RunID: "provider-run", Sequence: 42, Provider: "codex", ReceivedAt: time.Date(2026, 9, 7, 1, 2, 3, 0, time.UTC), Raw: raw}
	first, err := Unknown(p)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := Unknown(p)
	if err != nil {
		t.Fatal(err)
	}
	if first.Kind != "debug/unknown" || first.Sequence != 42 || first.RunID != p.RunID || first.ThreadID != p.ThreadID || first.ReceivedAt != p.ReceivedAt || string(first.Raw) != string(raw) {
		t.Fatal("adapter altered frame", first)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) {
		t.Fatal("replay changed frame identity or content")
	}
	raw[0] = '!'
	if !json.Valid(first.Raw) {
		t.Fatal("captured payload aliases mutable transport buffer")
	}
}
func TestUnknownRejectsInvalidCapture(t *testing.T) {
	p := ProviderFrame{ThreadID: "thread", RunID: "run", Sequence: 1, Provider: "codex", ReceivedAt: time.Now(), Raw: json.RawMessage(`{"partial":`)}
	if _, err := Unknown(p); err == nil {
		t.Fatal("accepted partial JSON")
	}
	p.Raw = json.RawMessage(`{}`)
	p.Sequence = 0
	if _, err := Unknown(p); err == nil {
		t.Fatal("accepted unsequenced capture")
	}
}

func TestDocumentedExamplesValidate(t *testing.T) {
	raw, err := os.ReadFile("../../learn/schemas/thread-frame.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var frames []Frame
	if err = json.Unmarshal(raw, &frames); err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if err = f.Validate(); err != nil {
			t.Fatal(f.Kind, err)
		}
	}
}
