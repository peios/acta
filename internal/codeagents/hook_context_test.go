package codeagents

import (
	"reflect"
	"testing"

	"acta/internal/codethreads"
)

func TestClaudeReportedContext(t *testing.T) {
	for _, tc := range []struct{ event, stdout, want string }{
		{"SessionStart", "project context", "project context"},
		{"UserPromptSubmit", "prompt context", "prompt context"},
		{"PostToolUse", "diagnostic output", ""},
		{"SessionStart", `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Use the local build."},"systemMessage":"UI only"}`, "Use the local build."},
		{"PreToolUse", `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"Context only","permissionDecision":"allow"}}`, "Context only"},
		{"PreToolUse", `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"deferred","permissionDecision":"defer"}}`, ""},
		{"SessionStart", `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":42}}`, ""},
		{"SessionStart", `{"systemMessage":"UI only"}`, ""},
		{"SessionStart", `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"wrong event"}}`, ""},
		{"SessionStart", `{"hookSpecificOutput":`, ""},
		{"SessionStart", `{"continue":false,"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"stopped"}}`, ""},
		{"SessionStart", `{"async":true,"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"later"}}`, ""},
		{"UserPromptSubmit", `{"decision":"block","hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"blocked"}}`, ""},
	} {
		got := claudeHookContext(tc.event, tc.stdout)
		if tc.want == "" {
			if got != nil {
				t.Fatalf("diagnostic/control output classified as context: %s", tc.stdout)
			}
			continue
		}
		if got == nil || got.Source != "hook_response" || !reflect.DeepEqual(got.Texts, []string{tc.want}) {
			t.Fatalf("wrong reported context: %#v", got)
		}
	}
	for _, stage := range []string{"hook_started", "hook_progress", "hook_response"} {
		data := claudeHookFixture(stage)
		p := hookSource(t, "claude", data)
		got := (Claude{}).NewConverter().Convert(p)[0]
		if (got.Hook.Context != nil) != (stage == "hook_response") {
			t.Fatal("premature context", stage)
		}
	}
	for _, outcome := range []string{"error", "cancelled"} {
		data := claudeHookFixture("hook_response")
		data["outcome"] = outcome
		if got := (Claude{}).NewConverter().Convert(hookSource(t, "claude", data))[0]; got.Hook.Context != nil {
			t.Fatal("failed hook marked injected")
		}
	}
}

func TestCodexHookContextFragments(t *testing.T) {
	p := codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: `{"method":"item/completed","params":{"turnId":"t","item":{"id":"context-1","type":"hookPrompt","fragments":[{"hookRunId":"a","text":"first"},{"hookRunId":"b","text":"second"},{"hookRunId":"a","text":"third"}]}}}`}
	got := (Codex{}).NewConverter().Convert(p)
	if len(got) != 2 || got[0].Type != "hook.context" || got[0].Hook.ID != "a" || got[1].Hook.ID != "b" || !reflect.DeepEqual(got[0].Hook.Context.Texts, []string{"first", "third"}) || got[0].Hook.Context.Source != "context_item" || got[0].TurnID != "t" || got[0].Raw != p.Raw {
		t.Fatal("context correlation/ordering lost", got)
	}
	p.Raw = `{"method":"item/started","params":{"item":{"id":"context-1","type":"hookPrompt","fragments":[{"hookRunId":"a","text":"first"}]}}}`
	if got := (Codex{}).NewConverter().Convert(p); len(got) != 0 {
		t.Fatal("context lifecycle duplicated")
	}
	p.Stream = "stderr"
	if got := (Codex{}).NewConverter().Convert(p); !reflect.DeepEqual(got, []codethreads.Payload{p}) {
		t.Fatal("stderr parsed as context")
	}
}
