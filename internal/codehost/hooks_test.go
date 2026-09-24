package codehost

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"acta/internal/codeagents"
	"acta/internal/codethreads"
)

func appendHook(t *testing.T, threads *Threads, r *threadRecord, id, phase string) {
	t.Helper()
	raw := fmt.Sprintf(`{"type":"system","subtype":%q,"hook_id":%q,"hook_event":"SessionStart","hook_name":"Load context","stdout":"hello","stderr":"","output":"hello","outcome":"success","exit_code":0}`, phase, id)
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Provider: "claude", Stream: "stdout", Raw: raw})
}

func TestHookSupersessionChangeCursorAndReplay(t *testing.T) {
	threads, r, _ := replayFixture(t)
	threads.adapters["claude"] = codeagents.Claude{}
	r.converter = threads.adapters["claude"].NewConverter()
	appendHook(t, threads, r, "a", "hook_started")
	initial, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1})
	if err != nil || len(initial.Frames) != 1 {
		t.Fatal(initial, err)
	}
	start := initial.Frames[0]
	threads.append(r, codethreads.Payload{Type: "message.user", Text: "interleaved", ID: "input-ack"})
	appendHook(t, threads, r, "b", "hook_started")
	appendHook(t, threads, r, "a", "hook_progress")
	progress := r.frames[3]
	appendHook(t, threads, r, "a", "hook_response")
	completed := r.frames[4]
	if r.frames[0].SupersededBy != progress.ID || progress.Supersedes != start.ID || r.frames[3].SupersededBy != completed.ID || completed.Supersedes != progress.ID || r.frames[2].SupersededBy != "" {
		t.Fatal("incorrect supersession chain", r.frames)
	}
	if r.frames[0].Seq != start.Seq || !r.frames[0].At.Equal(start.At) || completed.Seq <= r.frames[1].Seq {
		t.Fatal("completion moved to start position")
	}
	page, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1, After: initial.Next})
	if err != nil || page.Reset || page.First != 1 || len(page.Frames) != 5 {
		t.Fatal("old frame update missing", page, err)
	}
	found := false
	for _, frame := range page.Frames {
		if frame.ID == start.ID {
			found = frame.SupersededBy != "" && frame.ChangeSeq > initial.Next
		}
	}
	if !found {
		t.Fatal("client that passed start never receives its mutation")
	}
	if initial.Frames[0].SupersededBy != "" {
		t.Fatal("previous read mutated through shared memory")
	}
	all, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range all.Frames {
		if frame.ID == start.ID && frame.SupersededBy == "" {
			t.Fatal("partial reader sees false running start")
		}
	}
	before := append([]codethreads.Frame(nil), r.frames...)
	sources := append([]codethreads.Frame(nil), r.sources...)
	_, err = threads.Reprocess(t.Context(), codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1})
	if err != nil || !reflect.DeepEqual(before, r.frames) || !reflect.DeepEqual(sources, r.sources) {
		t.Fatal("replay changed identities or raw history", err)
	}
	reset, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1, After: page.Next})
	if err != nil || !reset.Reset || len(reset.Frames) != 5 {
		t.Fatal("replay failed to reset upserts", err)
	}
	appendHook(t, threads, r, "b", "hook_response")
	if r.frames[2].SupersededBy != r.frames[5].ID {
		t.Fatal("live continuation lost correlation after replay")
	}
	bytes := 0
	for i, f := range r.frames {
		raw, _ := json.Marshal(f)
		bytes += len(raw)
		if len(raw) != r.sizes[i] {
			t.Fatal("mutation size not tracked")
		}
	}
	if r.bytes != bytes {
		t.Fatal("incorrect retained byte count")
	}
}

func TestHookPaginationAndEviction(t *testing.T) {
	threads, r, _ := replayFixture(t)
	r.converter = (codeagents.Claude{}).NewConverter()
	appendHook(t, threads, r, "a", "hook_started")
	for i := 0; i < 101; i++ {
		threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: fmt.Sprint(i)})
	}
	appendHook(t, threads, r, "a", "hook_response")
	seen := map[uint64]codethreads.Frame{}
	cursor := uint64(0)
	for {
		page, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1, After: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range page.Frames {
			if f.ChangeSeq <= cursor {
				t.Fatal("cursor regressed")
			}
			seen[f.Seq] = f
		}
		cursor = page.Next
		if !page.More {
			break
		}
	}
	if len(seen) != 103 || seen[1].SupersededBy == "" || seen[103].Hook.Status != "completed" {
		t.Fatal("pagination lost old mutation or result")
	}
	for i := 0; i < 1000; i++ {
		threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: "filler"})
	}
	appendHook(t, threads, r, "orphan", "hook_response")
	last := r.frames[len(r.frames)-1]
	if last.Supersedes != "" || last.Hook.Name != "Load context" || last.Hook.Status != "completed" || len(last.Hook.Outputs) == 0 {
		t.Fatal("standalone completion missing data", last)
	}
	page, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1, After: cursor})
	if err != nil || page.First <= 103 || !page.Thread.SourceTruncated || len(r.frames) > 1000 || r.bytes > threadHistoryBytes {
		t.Fatal("eviction/pruning contract broken", err)
	}
}
