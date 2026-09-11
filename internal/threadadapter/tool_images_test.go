package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestClaudeCapturedImageResult(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-image-result.json")
	if err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const tool = "toolu_01VCe1D5f96NfXC94ymFm8qm"
	for _, scenario := range []string{"image", "mixed", "invalid-base64", "unsupported-media", "url", "unknown-part"} {
		t.Run(scenario, func(t *testing.T) {
			var m object
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			result := obj(list(obj(m["message"])["content"])[0])
			content := list(result["content"])
			source := obj(obj(content[0])["source"])
			switch scenario {
			case "mixed":
				result["content"] = append(content, object{"type": "text", "text": "A magenta square"})
			case "invalid-base64":
				source["data"] = "not base64!"
			case "unsupported-media":
				source["media_type"] = "image/svg+xml"
			case "url":
				source["type"] = "url"
			case "unknown-part":
				result["content"] = append(content, object{"type": "future-content"})
			}
			s := State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "turn", Name: "Read", Category: "read", Label: "Read image", Status: "running", Arguments: object{}}}}
			text, _ := json.Marshal(m)
			p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}
			next, frames, err := Map(s, p, "stdout", string(text))
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "image" && scenario != "mixed" {
				if len(frames) != 1 || frames[0].Kind != "debug/unknown" || next.Tools[tool].ResultReceived {
					t.Fatalf("partially consumed malformed image: %+v", frames)
				}
				return
			}
			if len(frames) != 2 || frames[0].Kind != "debug/resolved" || frames[1].Kind != "tool/call" || DataError(frames[0]) {
				t.Fatalf("image not mapped: %+v", frames)
			}
			if next.Tools[tool].Status != "completed" || len(next.Tools[tool].Images) != 1 || next.Tools[tool].Completed == nil {
				t.Fatalf("invalid completion: %+v", next.Tools[tool])
			}
			if scenario == "mixed" && *next.Tools[tool].Output != "A magenta square" {
				t.Fatal("lost text output")
			}
			checkpoint, _ := json.Marshal(next)
			if err := json.Unmarshal(checkpoint, &next); err != nil {
				t.Fatal(err)
			}
			p.Sequence++
			replayed, duplicate, err := Map(next, p, "stdout", string(text))
			if err != nil || len(duplicate) != 1 || len(replayed.Tools[tool].Images) != 1 {
				t.Fatalf("duplicated image after checkpoint: %v %+v", err, duplicate)
			}
		})
	}
}
