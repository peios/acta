package threadadapter

import (
	"acta/internal/threads"
	"encoding/json"
	"testing"
	"time"
)

func TestImageMessageReplayAndReceipts(t *testing.T) {
	const id = "5b24dfdc-d229-42e4-9b43-af1f3de85723"
	const command = "0e33f0fc-455b-4cba-bcb7-14dff21e3f1b"
	image := threads.InputImage{Name: "pixel.png", MediaType: "image/png", Base64: "YWJj"}
	for _, provider := range []string{"codex", "claude", "claude-receipt"} {
		t.Run(provider, func(t *testing.T) {
			s := State{RunID: id, NativeID: id, Submissions: map[string]string{command: id}, SubmissionTexts: map[string]string{command: ""}, SubmissionImages: map[string][]threads.InputImage{command: {image}}}
			var native object
			if provider == "codex" {
				native = object{"method": "item/completed", "params": object{"threadId": id, "turnId": command, "item": object{"type": "userMessage", "id": "msg", "clientId": command, "content": []any{object{"type": "localImage", "path": "/provider/private.png"}}}}}
			} else if provider == "claude" {
				native = object{"type": "user", "uuid": command, "session_id": id, "isReplay": true, "parent_tool_use_id": nil, "message": object{"content": []any{object{"type": "image", "source": object{"type": "base64", "media_type": "image/png", "data": "YWJj"}}}}}
			} else {
				native = object{"type": "command_lifecycle", "session_id": id, "command_uuid": command, "state": "started"}
			}
			mappedProvider := provider
			if provider == "claude-receipt" {
				mappedProvider = "claude"
			}
			users := 0
			for n := 0; n < 2; n++ {
				checkpoint, _ := json.Marshal(s)
				s, _ = DecodeState(checkpoint)
				raw, _ := json.Marshal(native)
				next, frames, e := Map(s, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: mappedProvider, Sequence: int64(n + 1), ReceivedAt: time.Now()}, "stdout", string(raw))
				if e != nil {
					t.Fatal(e)
				}
				for _, f := range frames {
					if f.Kind == "debug/unknown" || DataError(f) {
						t.Fatalf("%s %s", f.Kind, f.Data)
					}
					if f.Kind == "message/user" {
						users++
						var d object
						json.Unmarshal(f.Data, &d)
						parts := list(d["content"])
						if len(parts) != 1 || obj(parts[0])["type"] != "image" || d["submission_id"] != command {
							t.Fatal(string(f.Data))
						}
					}
				}
				s = next
			}
			if users != 1 {
				t.Fatalf("users=%d", users)
			}
		})
	}
}
func TestInputImageURLsAreNotFetched(t *testing.T) {
	for _, url := range []string{"https://example.org/a.png", "file:///etc/passwd", "data:image/svg+xml;base64,YWJj"} {
		if _, ok := inputImage(object{"url": url}, "codex"); ok {
			t.Fatal(url)
		}
	}
}
