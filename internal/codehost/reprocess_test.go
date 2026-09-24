package codehost

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"acta/internal/codeagents"
	"acta/internal/codethreads"
	"github.com/google/uuid"
)

type replayAdapter struct {
	mapEvents bool
	cancel    context.CancelFunc
}

func (*replayAdapter) Start(context.Context, codeagents.Launch, func(codethreads.Payload)) (codeagents.Session, error) {
	panic("replay must never start a process")
}
func (a *replayAdapter) NewConverter() codeagents.Converter {
	return &replayConverter{mapEvents: a.mapEvents, cancel: a.cancel}
}

type replayConverter struct {
	mapEvents bool
	pending   string
	count     int
	cancel    context.CancelFunc
}

func (c *replayConverter) Convert(p codethreads.Payload) []codethreads.Payload {
	c.count++
	if c.cancel != nil {
		c.cancel()
	}
	if !c.mapEvents {
		return []codethreads.Payload{p}
	}
	switch p.Raw {
	case "drop":
		return nil
	case "part1":
		c.pending = p.Raw
		return nil
	case "part2":
		return []codethreads.Payload{{Type: "test.joined", Text: c.pending + p.Raw}, {Type: "test.extra", Text: "second output"}}
	default:
		return []codethreads.Payload{{Type: "test.line", Text: p.Raw + ":" + strconv.Itoa(c.count)}}
	}
}

func replayFixture(t *testing.T) (*Threads, *threadRecord, *replayAdapter) {
	t.Helper()
	a := &replayAdapter{}
	threads := NewThreads(t.Context(), nil, map[string]codeagents.Adapter{"claude": a})
	t.Cleanup(threads.Close)
	r := &threadRecord{converter: a.NewConverter(), inputs: map[string][32]byte{}, info: codethreads.Thread{ID: uuid.NewString(), CodebaseID: uuid.NewString(), Provider: "claude", State: "ready", Revision: 1, CreatedAt: time.Now().UTC()}}
	threads.items[r.info.ID] = r
	return threads, r, a
}

func TestReprocessUsesOriginalSourcesAndPreservesInputs(t *testing.T) {
	threads, r, adapter := replayFixture(t)
	id := uuid.NewString()
	for _, p := range []codethreads.Payload{
		{Type: "input.message.user", ID: id, Text: "hello", Delivery: "start_turn"},
		{Type: "debug.unknown", Provider: "claude", Stream: "stdout", Raw: "part1"},
		{Type: "debug.unknown", Provider: "claude", Stream: "stdout", Raw: "part2"},
		{Type: "debug.unknown", Provider: "claude", Stream: "stderr", Raw: "drop"},
		{Type: "message.user", ID: "ack", InputID: id, Text: "hello"},
		{Type: "input.message.error", InputID: id, Error: "kept error"},
	} {
		threads.append(r, p)
	}
	sources := append([]codethreads.Frame(nil), r.sources...)
	original := append([]codethreads.Frame(nil), r.frames...)
	originalInfo := r.info
	adapter.mapEvents = true
	request := codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1}
	updated, err := threads.Reprocess(t.Context(), request)
	if err != nil || updated.Revision != 2 || len(r.frames) != 5 {
		t.Fatal("rebuild failed", updated, err, len(r.frames))
	}
	if !reflect.DeepEqual(r.sources, sources) {
		t.Fatal("raw source journal modified")
	}
	if r.frames[1].Type != "test.joined" || r.frames[1].Text != "part1part2" || r.frames[1].SourceSeq != 3 || r.frames[2].SourceSeq != 3 || !r.frames[1].At.Equal(sources[2].At) {
		t.Fatal("ordered many-to-many conversion/provenance lost", r.frames)
	}
	for i, j := range map[int]int{0: 0, 3: 4, 4: 5} {
		if !reflect.DeepEqual(r.frames[i].Payload, sources[j].Payload) {
			t.Fatal("input/ack/error changed")
		}
	}
	if updated.ID != originalInfo.ID || updated.State != originalInfo.State || updated.Busy || !updated.CreatedAt.Equal(originalInfo.CreatedAt) {
		t.Fatal("live identity/state changed")
	}
	page, err := threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, After: 999, Revision: 1})
	if err != nil || !page.Reset || page.Frames[0].Seq != 1 || page.Thread.Revision != 2 {
		t.Fatal("stale cursor not reset", page, err)
	}
	if _, err = threads.Read(codethreads.Read{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, After: 999, Revision: 2}); err == nil {
		t.Fatal("current invalid cursor accepted")
	}
	if _, err = threads.Reprocess(t.Context(), request); err == nil {
		t.Fatal("duplicate revision replay accepted")
	}
	once := append([]codethreads.Frame(nil), r.frames...)
	request.Revision = 2
	if _, err = threads.Reprocess(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(once, r.frames) {
		t.Fatal("repeated replay duplicated or reordered output")
	}
	// Reverting a mapping can restore dropped events: replay reads original input.
	adapter.mapEvents = false
	request.Revision = 3
	if _, err = threads.Reprocess(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, r.frames) {
		t.Fatal("replay consumed converted frames instead of original sources")
	}
	adapter.mapEvents = true
	request.Revision = 4
	if _, err = threads.Reprocess(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: "tail", Stream: "stdout"})
	if r.frames[len(r.frames)-1].Text != "tail:4" {
		t.Fatal("live conversion did not continue from replay state")
	}
}

func TestReprocessRejectsActiveForeignAndCancelledRequests(t *testing.T) {
	threads, r, adapter := replayFixture(t)
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: "one"})
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: "two"})
	request := codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1}
	before := append([]codethreads.Frame(nil), r.frames...)
	for _, state := range []string{"busy", "starting", "foreign", "cancelled"} {
		in := request
		ctx := t.Context()
		switch state {
		case "busy":
			r.info.Busy = true
		case "starting":
			r.info.State = "starting"
		case "foreign":
			in.CodebaseID = uuid.NewString()
		case "cancelled":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if _, err := threads.Reprocess(ctx, in); err == nil {
			t.Fatal("invalid replay accepted", state)
		}
		r.info.Busy = false
		r.info.State = "ready"
		if r.info.Revision != 1 || !reflect.DeepEqual(before, r.frames) {
			t.Fatal("failed replay changed frames")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	adapter.mapEvents = true
	adapter.cancel = cancel
	if _, err := threads.Reprocess(ctx, request); err == nil {
		t.Fatal("mid-conversion cancellation accepted")
	}
	if r.info.Revision != 1 || !reflect.DeepEqual(before, r.frames) {
		t.Fatal("partial rebuild published")
	}
}

func TestReprocessMarksTruncatedHistoryAndKeepsSourcesBounded(t *testing.T) {
	threads, r, adapter := replayFixture(t)
	for i := 0; i < 1100; i++ {
		threads.append(r, codethreads.Payload{Type: "debug.unknown", Raw: strconv.Itoa(i), Stream: "stdout"})
	}
	if len(r.sources) != 1000 || !r.info.SourceTruncated || r.sourceBytes > threadHistoryBytes {
		t.Fatal("unbounded sources or missing truncation marker")
	}
	adapter.mapEvents = true
	if _, err := threads.Reprocess(t.Context(), codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if r.frames[0].SourceSeq != 101 || r.frames[0].Text != "100:1" || !r.info.SourceTruncated {
		t.Fatal("truncated replay concealed missing context")
	}
}

func TestReprocessDropsCodexInitializationAndRetainsRawSource(t *testing.T) {
	threads, r, _ := replayFixture(t)
	r.info.Provider = "codex"
	threads.adapters["codex"] = codeagents.Codex{}
	// The fixture's initial converter represents the previous pass-through rule.
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: `{"id":"init","result":{"userAgent":"Codex","codexHome":"/test/.codex","platformFamily":"unix","platformOs":"linux"}}`})
	threads.append(r, codethreads.Payload{Type: "debug.unknown", Provider: "codex", Stream: "stdout", Raw: `{"method":"thread/started","params":{}}`})
	sources := append([]codethreads.Frame(nil), r.sources...)
	if _, err := threads.Reprocess(t.Context(), codethreads.Reprocess{CodebaseID: r.info.CodebaseID, ThreadID: r.info.ID, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if len(r.frames) != 1 || r.frames[0].SourceSeq != 2 {
		t.Fatal("initialization was not dropped selectively", r.frames)
	}
	if !reflect.DeepEqual(sources, r.sources) {
		t.Fatal("raw source was discarded")
	}
}
