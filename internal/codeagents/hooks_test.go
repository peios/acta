package codeagents

import (
	"encoding/json"
	"reflect"
	"testing"

	"acta/internal/codethreads"
)

func claudeHookFixture(subtype string) map[string]any {
	return map[string]any{"type": "system", "subtype": subtype, "hook_id": "hook-a", "hook_name": "SessionStart:startup", "hook_event": "SessionStart", "stdout": "one\ntwo", "stderr": "warning", "output": "one\nwarning\ntwo", "outcome": "success", "exit_code": 0}
}
func hookSource(t *testing.T, provider string, data any) codethreads.Payload {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return codethreads.Payload{Type: "debug.unknown", Provider: provider, Stream: "stdout", Raw: string(raw)}
}
func TestClaudeHookSnapshotsAndOutcomes(t *testing.T) {
	c := (Claude{}).NewConverter()
	for _, stage := range []struct{ subtype, kind string }{{"hook_started", "hook.started"}, {"hook_progress", "hook.progress"}, {"hook_response", "hook.completed"}} {
		p := hookSource(t, "claude", claudeHookFixture(stage.subtype))
		got := c.Convert(p)
		if len(got) != 1 || got[0].Type != stage.kind || got[0].Raw != p.Raw || got[0].Hook.ID != "hook-a" || got[0].Hook.Event != "session_start" {
			t.Fatalf("bad conversion: %#v", got)
		}
		if stage.subtype != "hook_started" {
			if !reflect.DeepEqual(got[0].Hook.Outputs, []codethreads.HookOutput{{Kind: "stdout", Text: "one\ntwo"}, {Kind: "stderr", Text: "warning"}, {Kind: "output", Text: "one\nwarning\ntwo"}}) {
				t.Fatal("streams or snapshot duplicated", got[0].Hook)
			}
		}
		if again := c.Convert(p); !reflect.DeepEqual(again, got) {
			t.Fatal("snapshot depends on prior history")
		}
	}
	for outcome, status := range map[string]string{"success": "completed", "error": "failed", "cancelled": "cancelled"} {
		data := claudeHookFixture("hook_response")
		data["outcome"] = outcome
		got := c.Convert(hookSource(t, "claude", data))[0]
		if got.Hook.Status != status || got.Hook.ExitCode == nil || *got.Hook.ExitCode != 0 {
			t.Fatal("outcome/exit lost", got.Hook)
		}
	}
}

func TestCodexHookSnapshots(t *testing.T) {
	c := (Codex{}).NewConverter()
	for _, status := range []string{"running", "completed", "failed", "blocked", "stopped"} {
		method, kind := "hook/completed", "hook.completed"
		if status == "running" {
			method, kind = "hook/started", "hook.started"
		}
		p := hookSource(t, "codex", map[string]any{"method": method, "params": map[string]any{"threadId": "thread", "turnId": nil, "run": map[string]any{"id": "run", "eventName": "preToolUse", "status": status, "entries": []map[string]string{{"kind": "context", "text": "added context"}, {"kind": "warning", "text": "warning"}}, "startedAt": 123, "durationMs": 25, "completedAt": 148, "sourcePath": "/test/hooks.json", "source": "project", "executionMode": "sync", "handlerType": "command", "scope": "turn", "statusMessage": "status details"}}})
		got := c.Convert(p)[0]
		if got.Type != kind || got.Hook.Status != status || got.Hook.Event != "pre_tool_use" || got.Raw != p.Raw {
			t.Fatal("bad Codex hook", got)
		}
		h := got.Hook
		if h.Name != "" || h.Outputs[0].Kind != "context" || h.Outputs[1].Kind != "warning" || *h.DurationMS != 25 || *h.StartedAt != 123 || h.SourcePath != "/test/hooks.json" || h.StatusMessage != "status details" {
			t.Fatal("metadata lost or fabricated", h)
		}
	}
}

func TestHookConvertersPreserveUnrecognizedEvents(t *testing.T) {
	for _, edit := range []func(map[string]any){
		func(m map[string]any) { delete(m, "hook_id") },
		func(m map[string]any) { delete(m, "stdout") },
		func(m map[string]any) { m["exit_code"] = "zero" },
		func(m map[string]any) { m["outcome"] = "new-outcome" },
		func(m map[string]any) { m["subtype"] = "hook_future" },
	} {
		data := claudeHookFixture("hook_response")
		edit(data)
		p := hookSource(t, "claude", data)
		if got := (Claude{}).NewConverter().Convert(p); !reflect.DeepEqual(got, []codethreads.Payload{p}) {
			t.Fatal("unrecognized Claude hook lost", got)
		}
	}
	for _, raw := range []string{
		`{"method":"hook/completed","params":{"threadId":"t","run":{"id":"h","eventName":"stop","status":"future","entries":[]}}}`,
		`{"method":"hook/started","params":{"threadId":"t","run":{"id":"h","eventName":"stop","status":"completed","entries":[]}}}`,
		`{"method":"hook/completed","params":{"threadId":"t","run":{"id":"h","eventName":"stop","status":"completed","entries":[{"kind":"new-kind","text":"inspect"}]}}}`,
		`{"method":"hook/completed","params":{"run":{"id":"h","eventName":"stop","status":"completed","entries":[]}}}`,
		`{"method":"hook/completed"`,
		`{"method":"item/completed","params":{"item":{"type":"hookPrompt","id":"i","fragments":[{"hookRunId":"h","text":null}]}}}`,
	} {
		p := codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: raw}
		if got := (Codex{}).NewConverter().Convert(p); !reflect.DeepEqual(got, []codethreads.Payload{p}) {
			t.Fatal("unrecognized Codex hook lost", got)
		}
	}
	p := hookSource(t, "claude", claudeHookFixture("hook_started"))
	p.Stream = "stderr"
	if got := (Claude{}).NewConverter().Convert(p); !reflect.DeepEqual(got, []codethreads.Payload{p}) {
		t.Fatal("stderr interpreted as protocol")
	}
	p.Stream = "stdout"
	p.Provider = "codex"
	if got := (Codex{}).NewConverter().Convert(p); !reflect.DeepEqual(got, []codethreads.Payload{p}) {
		t.Fatal("cross-provider conversion")
	}
}
