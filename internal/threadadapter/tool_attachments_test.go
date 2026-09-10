package threadadapter

import (
	"acta2/internal/threads"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCapturedPDFResult(t *testing.T) {
	raw, err := os.ReadFile("testdata/claude-pdf-result.json")
	if err != nil {
		t.Fatal(err)
	}
	const id = "db799357-e3f3-4f43-9960-df8e8e3bd330"
	const tool = "toolu_01VepBc2zK2rrQDpUGEDv8c4"
	for _, valid := range []bool{true, false} {
		var m object
		_ = json.Unmarshal(raw, &m)
		result := obj(list(obj(m["message"])["content"])[0])
		if !valid {
			obj(obj(list(result["content"])[1])["source"])["media_type"] = "text/html"
		}
		s := State{RunID: id, NativeID: id, Claude: ClaudeState{Turn: "turn"}, Tools: map[string]*ToolCall{tool: {ID: tool, Turn: "turn", Name: "Read", Category: "read", Label: "Read PDF", Status: "running", Arguments: object{"file_path": "/tmp/qa-document.pdf"}}}}
		text, _ := json.Marshal(m)
		next, frames, err := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(text))
		if err != nil {
			t.Fatal(err)
		}
		if !valid {
			if len(frames) != 1 || frames[0].Kind != "debug/unknown" || next.Tools[tool].ResultReceived {
				t.Fatal("unsupported document partially consumed")
			}
			continue
		}
		if len(frames) != 2 || frames[1].Kind != "tool/call" || DataError(frames[0]) {
			t.Fatalf("unmapped PDF: %+v", frames)
		}
		call := next.Tools[tool]
		if call.Status != "completed" || len(call.Attachments) != 1 || call.Attachments[0].Name != "qa-document.pdf" || call.Output == nil || *call.Output == "" {
			t.Fatalf("lost result: %+v", call)
		}
	}
	for _, path := range []string{"", "/", `C:\qa\document.pdf`} {
		file, ok := toolPDF("application/pdf", "JVBERg==", path)
		if !ok || file.Name != "document.pdf" {
			t.Fatalf("bad filename: %+v", file)
		}
	}
	if _, ok := toolPDF("application/pdf", "not base64!", ""); ok {
		t.Fatal("accepted malformed bytes")
	}
}
