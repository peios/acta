package codehost

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/codeagents"
	"acta/internal/codebases"
	"acta/internal/codethreads"
	"github.com/google/uuid"
)

type testAdapter struct {
	calls    atomic.Int32
	launched chan string
	burst    bool
}

func (a *testAdapter) NewConverter() codeagents.Converter {
	return (codeagents.Claude{}).NewConverter()
}

func (a *testAdapter) Start(ctx context.Context, in codeagents.Launch, emit func(codethreads.Payload)) (codeagents.Session, error) {
	a.calls.Add(1)
	a.launched <- in.Directory
	emit(codethreads.Payload{Type: "debug.unknown", Provider: "claude", Stream: "stdout", Raw: `{"type":"startup"}`})
	if a.burst {
		for i := 0; i < 40; i++ {
			emit(codethreads.Payload{Type: "debug.unknown", Provider: "claude", Stream: "stderr", Raw: fmt.Sprint(i) + strings.Repeat("x", 32000)})
		}
	}
	return testSession{ctx: ctx}, nil
}

type testSession struct{ ctx context.Context }

func (s testSession) Wait() error { <-s.ctx.Done(); return s.ctx.Err() }
func (s testSession) Send(ctx context.Context, _ codethreads.Input) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestCodeThreadsRootIdentityLifetimeAndCursor(t *testing.T) {
	cat, err := OpenCatalogue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, b := t.TempDir(), t.TempDir()
	base, err := cat.Add(t.Context(), codebases.Add{ID: uuid.NewString(), Name: "multi repo", Paths: []string{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &testAdapter{launched: make(chan string, 8), burst: true}
	threads := NewThreads(t.Context(), cat, map[string]codeagents.Adapter{"claude": adapter, "codex": adapter})
	defer threads.Close()
	in := codethreads.Start{Provider: "claude", ID: uuid.NewString(), CodebaseID: base.ID, RootID: base.Roots[1].ID}
	for _, provider := range []string{"", "other", "/bin/sh"} {
		invalid := in
		invalid.Provider = provider
		if _, err := threads.Start(t.Context(), invalid); err == nil {
			t.Fatal("unsupported provider accepted", provider)
		}
	}
	request, cancel := context.WithCancel(t.Context())
	first, err := threads.Start(request, in)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	again, err := threads.Start(t.Context(), in)
	if err != nil || again.ID != first.ID {
		t.Fatal("start retry failed", err)
	}
	select {
	case dir := <-adapter.launched:
		if dir != b {
			t.Fatal("wrong working root", dir)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter not started")
	}
	var page codethreads.Page
	deadline := time.Now().Add(time.Second)
	for {
		page, err = threads.Read(codethreads.Read{CodebaseID: base.ID, ThreadID: first.ID})
		if err != nil {
			t.Fatal(err)
		}
		if page.Thread.State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("request cancellation killed thread")
		}
		time.Sleep(time.Millisecond)
	}
	if adapter.calls.Load() != 1 {
		t.Fatal("retry spawned duplicate")
	}
	input := codethreads.Send{Input: codethreads.Input{ID: uuid.NewString(), Text: "hello", Delivery: "start_turn"}, CodebaseID: base.ID, ThreadID: first.ID}
	accepted, err := threads.Send(t.Context(), input)
	if err != nil || !accepted.Busy {
		t.Fatal("send not accepted", err)
	}
	if _, err = threads.Send(t.Context(), input); err != nil {
		t.Fatal("unchanged retry rejected", err)
	}
	conflict := input
	conflict.Text = "different"
	if _, err = threads.Send(t.Context(), conflict); err == nil {
		t.Fatal("input ID conflict accepted")
	}
	conflict = input
	conflict.ID = uuid.NewString()
	if _, err = threads.Send(t.Context(), conflict); err == nil {
		t.Fatal("second active turn accepted")
	}
	conflict = input
	conflict.CodebaseID = uuid.NewString()
	if _, err = threads.Send(t.Context(), conflict); err == nil {
		t.Fatal("cross-codebase send accepted")
	}
	conflict = input
	conflict.Delivery = "steer"
	if _, err = threads.Send(t.Context(), conflict); err == nil {
		t.Fatal("steering accepted")
	}
	if page.First <= 1 || !page.More || len(page.Frames) == 0 {
		t.Fatal("retention gap or pagination missing", page.First, page.More)
	}
	next, err := threads.Read(codethreads.Read{CodebaseID: base.ID, ThreadID: first.ID, After: page.Next, Revision: page.Thread.Revision})
	if err != nil || len(next.Frames) == 0 || next.Frames[0].Seq <= page.Next {
		t.Fatal("bad cursor continuation", err)
	}
	if _, err = threads.Read(codethreads.Read{CodebaseID: uuid.NewString(), ThreadID: first.ID}); err == nil {
		t.Fatal("wrong codebase read accepted")
	}
	in.Provider = "codex"
	if _, err = threads.Start(t.Context(), in); err == nil {
		t.Fatal("conflicting provider retry accepted")
	}
	in.Provider = "claude"
	in.RootID = base.Roots[0].ID
	if _, err = threads.Start(t.Context(), in); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	in.ID = uuid.NewString()
	in.RootID = uuid.NewString()
	if _, err = threads.Start(t.Context(), in); err == nil {
		t.Fatal("arbitrary working root accepted")
	}
	threads.Close()
	page, err = threads.Read(codethreads.Read{CodebaseID: base.ID, ThreadID: first.ID})
	if err != nil || page.Thread.State != "stopped" {
		t.Fatal("shutdown not reflected", err)
	}
}
