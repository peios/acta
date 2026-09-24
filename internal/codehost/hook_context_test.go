package codehost

import (
	"reflect"
	"testing"

	"acta/internal/codeagents"
	"acta/internal/codethreads"
)

func TestHookContextInsertionAndReplay(t *testing.T) {
	threads, r, _ := replayFixture(t)
	r.info.Provider = "codex"
	threads.adapters["codex"] = codeagents.Codex{}
	r.converter = threads.adapters["codex"].NewConverter()
	appendRaw := func(raw string) {
		threads.append(r, codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: raw})
	}
	appendRaw(`{"method":"hook/completed","params":{"threadId":"t","run":{"id":"a","eventName":"sessionStart","status":"completed","entries":[{"kind":"warning","text":"not context"}]}}}`)
	threads.append(r, codethreads.Payload{Type: "message.user", Text: "interleaved"})
	appendRaw(`{"method":"item/completed","params":{"item":{"id":"i","type":"hookPrompt","fragments":[{"hookRunId":"a","text":"injected"}]}}}`)
	context := r.frames[2]
	if context.Type != "hook.context" || context.Supersedes != r.frames[0].ID || r.frames[0].SupersededBy != context.ID || context.Hook.Event != "session_start" || context.Hook.Status != "completed" || context.Hook.Context.Texts[0] != "injected" || r.frames[0].Hook.Context != nil {
		t.Fatal("context snapshot/correlation incorrect", r.frames)
	}
	appendRaw(`{"method":"item/completed","params":{"item":{"id":"j","type":"hookPrompt","fragments":[{"hookRunId":"a","text":"another insertion"}]}}}`)
	if r.frames[2].SupersededBy != "" || r.frames[3].Supersedes != "" {
		t.Fatal("distinct context insertions hidden")
	}
	appendRaw(`{"method":"item/completed","params":{"item":{"id":"k","type":"hookPrompt","fragments":[{"hookRunId":"missing","text":"standalone"}]}}}`)
	if r.frames[4].Hook.Status != "unknown" || r.frames[4].Hook.Context.Texts[0] != "standalone" {
		t.Fatal("orphan injection fabricated execution status")
	}
	before := append([]codethreads.Frame(nil), r.frames...)
	_, err := threads.Reprocess(t.Context(), codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1})
	if err != nil || !reflect.DeepEqual(before, r.frames) {
		t.Fatal("context replay changed projection", err)
	}
}
