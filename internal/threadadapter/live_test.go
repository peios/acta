package threadadapter

import (
	"encoding/json"
	"os"
	"testing"
)

// Opt-in fixture replay: capture only the agreed Test Message in a disposable
// provider process. No provider access or model call happens in this test.
func TestCapturedProviderFrames(t *testing.T) {
	path := os.Getenv("ACTA_FRAME_CAPTURE")
	if path == "" {
		t.Skip("set ACTA_FRAME_CAPTURE for captured-provider replay")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var frames []struct {
		Run    string `json:"run_id"`
		Stream string `json:"stream"`
		Raw    string `json:"raw"`
	}
	if err = json.Unmarshal(raw, &frames); err != nil {
		t.Fatal(err)
	}
	state := State{}
	counts := map[string]int{}
	for i, f := range frames {
		p := capture(int64(i + 1))
		p.RunID = f.Run
		next, bundle, err := Map(state, p, f.Stream, f.Raw)
		if err != nil {
			t.Fatal(i, err)
		}
		state = next
		for _, out := range bundle {
			counts[out.Kind]++
			var data object
			_ = json.Unmarshal(out.Data, &data)
			if out.Kind == "debug/local" && obj(data["processing_error"])["code"] == "mapping_failed" {
				t.Errorf("capture %d: %s", i, out.Data)
			}
		}
	}
	for _, kind := range []string{"thread/configuration", "message/user", "message/assistant", "message/assistant/delta", "usage/context", "usage/account", "turn/started", "turn/completed", "mcp/server/status"} {
		if counts[kind] == 0 {
			t.Errorf("missing mapped %s", kind)
		}
	}
	t.Logf("frames by kind: %v", counts)
}
