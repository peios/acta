package codehost

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"acta/internal/codeagents"
	"acta/internal/codehosts"
	"acta/internal/codethreads"
)

const threadHistoryBytes = 1024 * 1024
const maxCodeThreads = 64
const maxRunningCodeThreads = 8

type threadRecord struct {
	converter   codeagents.Converter
	sources     []codethreads.Frame
	sourceSizes []int
	sourceBytes int
	sourceSeq   uint64
	session     codeagents.Session
	inputs      map[string][32]byte
	info        codethreads.Thread
	frames      []codethreads.Frame
	sizes       []int
	bytes       int
	seq         uint64
	changeSeq   uint64
}
type Threads struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
	adapters  map[string]codeagents.Adapter
	catalogue *Catalogue
	items     map[string]*threadRecord
	order     []string
}

func NewThreads(parent context.Context, catalogue *Catalogue, adapters map[string]codeagents.Adapter) *Threads {
	ctx, cancel := context.WithCancel(parent)
	owned := make(map[string]codeagents.Adapter, len(adapters))
	for id, adapter := range adapters {
		owned[id] = adapter
	}
	return &Threads{ctx: ctx, cancel: cancel, catalogue: catalogue, adapters: owned, items: make(map[string]*threadRecord)}
}
func (t *Threads) Close() {
	t.mu.Lock()
	t.closed = true
	t.cancel()
	t.mu.Unlock()
	t.wg.Wait()
}

func (t *Threads) Start(ctx context.Context, in codethreads.Start) (codethreads.Thread, error) {
	var empty codethreads.Thread
	providerName := map[string]string{"claude": "Claude Code", "codex": "Codex"}[in.Provider]
	adapter := t.adapters[in.Provider]
	if providerName == "" || adapter == nil {
		return empty, problem("invalid_provider", "Select Claude Code or Codex.")
	}
	if !validID(in.ID) || !validID(in.CodebaseID) || !validID(in.RootID) {
		return empty, problem("invalid_thread", "Select a codebase and working folder.")
	}
	var dir, name string
	for _, item := range t.catalogue.List() {
		if item.ID == in.CodebaseID {
			for _, root := range item.Roots {
				if root.ID == in.RootID {
					dir, name = root.Path, item.Name
				}
			}
		}
	}
	if dir == "" {
		return empty, problem("not_found", "This codebase root does not exist on the host.")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return empty, problem("folder_unavailable", "The working folder is no longer accessible.")
	}
	root.Close()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.ctx.Err() != nil {
		return empty, problem("host_stopping", "The host is stopping.")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if old := t.items[in.ID]; old != nil {
		if old.info.CodebaseID != in.CodebaseID || old.info.RootID != in.RootID || old.info.Provider != in.Provider {
			return empty, problem("thread_conflict", "This thread ID was already used for another folder or provider.")
		}
		return old.info, nil
	}
	if len(t.items) >= maxCodeThreads {
		return empty, problem("thread_limit", "This host has reached its limit of 64 threads for this run.")
	}
	running := 0
	for _, item := range t.items {
		if item.info.State == "starting" || item.info.State == "ready" {
			running++
		}
	}
	if running >= maxRunningCodeThreads {
		return empty, problem("thread_limit", "This host already has eight active threads.")
	}
	record := &threadRecord{converter: adapter.NewConverter(), inputs: make(map[string][32]byte), info: codethreads.Thread{Revision: 1, ID: in.ID, CodebaseID: in.CodebaseID, RootID: in.RootID, Provider: in.Provider, Name: providerName + " · " + name, Directory: dir, State: "starting", CreatedAt: time.Now().UTC()}}
	t.items[in.ID] = record
	t.order = append(t.order, in.ID)
	t.wg.Add(1)
	go t.run(record, adapter)
	return record.info, nil
}

func (t *Threads) run(record *threadRecord, adapter codeagents.Adapter) {
	defer t.wg.Done()
	session, err := adapter.Start(t.ctx, codeagents.Launch{Directory: record.info.Directory}, func(payload codethreads.Payload) {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.append(record, payload)
	})
	if err == nil {
		t.mu.Lock()
		record.info.State = "ready"
		record.session = session
		t.mu.Unlock()
		err = session.Wait()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	record.info.State = "exited"
	if t.ctx.Err() != nil {
		record.info.State = "stopped"
	} else if err != nil {
		record.info.State = "failed"
		record.info.Error = err.Error()
	}
}

// append requires t.mu, so accepted inputs precede their provider echoes in CAT.
func (t *Threads) append(record *threadRecord, payload codethreads.Payload) {
	record.sourceSeq++
	source := codethreads.Frame{Payload: payload, SourceSeq: record.sourceSeq, At: time.Now().UTC()}
	raw, _ := json.Marshal(source)
	record.sources = append(record.sources, source)
	record.sourceSizes = append(record.sourceSizes, len(raw))
	record.sourceBytes += len(raw)
	for record.sourceBytes > threadHistoryBytes || len(record.sources) > 1000 {
		record.sourceBytes -= record.sourceSizes[0]
		record.sources[0] = codethreads.Frame{}
		record.sources, record.sourceSizes = record.sources[1:], record.sourceSizes[1:]
		record.info.SourceTruncated = true
	}
	t.convert(record, source)
}

func (t *Threads) convert(record *threadRecord, source codethreads.Frame) {
	if source.Type != "debug.unknown" {
		t.project(record, source, source.Payload, 0)
		return
	}
	for ordinal, payload := range record.converter.Convert(source.Payload) {
		t.project(record, source, payload, ordinal)
	}
}

func (t *Threads) project(record *threadRecord, source codethreads.Frame, payload codethreads.Payload, ordinal int) {
	record.seq++
	frame := codethreads.Frame{Payload: payload, Seq: record.seq, SourceSeq: source.SourceSeq, At: source.At}
	if frame.Hook != nil {
		frame.ID = fmt.Sprintf("cat:%d:%d", source.SourceSeq, ordinal)
		// Link only retained records. A response without its start is still a
		// complete snapshot; no unbounded operation index is needed.
		for i := len(record.frames) - 1; i >= 0; i-- {
			previous := &record.frames[i]
			if previous.Hook == nil || previous.Provider != frame.Provider || previous.Hook.ID != frame.Hook.ID {
				continue
			}
			if frame.Type == "hook.context" {
				// Materialize a standalone context snapshot while retaining the
				// lifecycle facts. Never mutate the prior snapshot through pointers.
				context := frame.Hook.Context
				hook := *previous.Hook
				hook.Context = context
				frame.Hook = &hook
				// Distinct context insertions stay at their respective positions.
				if previous.Type == "hook.context" {
					break
				}
			}
			frame.Supersedes = previous.ID
			previous.SupersededBy = frame.ID
			record.changeSeq++
			previous.ChangeSeq = record.changeSeq
			raw, _ := json.Marshal(previous)
			record.bytes += len(raw) - record.sizes[i]
			record.sizes[i] = len(raw)
			break
		}
	}
	record.changeSeq++
	frame.ChangeSeq = record.changeSeq
	raw, _ := json.Marshal(frame)
	record.frames = append(record.frames, frame)
	record.sizes = append(record.sizes, len(raw))
	record.bytes += len(raw)
	for record.bytes > threadHistoryBytes || len(record.frames) > 1000 {
		record.bytes -= record.sizes[0]
		record.frames[0] = codethreads.Frame{}
		record.frames, record.sizes = record.frames[1:], record.sizes[1:]
	}
}

// Reprocess replaces only the projection. It never calls Adapter.Start, Send,
// or the live lifecycle parser. The lock serializes publication with incoming
// background output and new sends, so neither can be lost during replacement.
func (t *Threads) Reprocess(ctx context.Context, in codethreads.Reprocess) (codethreads.Thread, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var empty codethreads.Thread
	if t.closed || t.ctx.Err() != nil {
		return empty, problem("host_stopping", "The host is stopping.")
	}
	r := t.items[in.ThreadID]
	if r == nil || r.info.CodebaseID != in.CodebaseID {
		return empty, problem("not_found", "This thread does not exist in the selected codebase.")
	}
	if r.info.Busy || r.info.State == "starting" {
		return empty, problem("turn_active", "Wait until the thread is idle before reprocessing.")
	}
	if in.Revision != r.info.Revision {
		return empty, problem("revision_conflict", "The thread was already reprocessed. Refresh its frames before trying again.")
	}
	adapter := t.adapters[r.info.Provider]
	if adapter == nil {
		return empty, problem("invalid_provider", "The thread's adapter is unavailable.")
	}
	rebuilt := &threadRecord{converter: adapter.NewConverter()}
	for _, source := range r.sources {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		t.convert(rebuilt, source)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	r.frames, r.sizes, r.bytes, r.seq = rebuilt.frames, rebuilt.sizes, rebuilt.bytes, rebuilt.seq
	r.changeSeq = rebuilt.changeSeq
	r.converter = rebuilt.converter
	r.info.Revision++
	return r.info, nil
}

func (t *Threads) Send(ctx context.Context, in codethreads.Send) (codethreads.Thread, error) {
	var empty codethreads.Thread
	if !validID(in.ID) || !validID(in.CodebaseID) || !validID(in.ThreadID) || in.Delivery != "start_turn" || strings.TrimSpace(in.Text) == "" || len(in.Text) > codethreads.MaxMessageBytes {
		return empty, problem("invalid_message", "Send a nonempty text message of at most 4 KiB with start_turn delivery.")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.ctx.Err() != nil {
		return empty, problem("host_stopping", "The host is stopping.")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	r := t.items[in.ThreadID]
	if r == nil || r.info.CodebaseID != in.CodebaseID {
		return empty, problem("not_found", "This thread does not exist in the selected codebase.")
	}
	digest := sha256.Sum256([]byte(in.Text))
	if old, ok := r.inputs[in.ID]; ok {
		if old != digest {
			return empty, problem("input_conflict", "This input ID was already used for another message.")
		}
		return r.info, nil
	}
	if r.info.State != "ready" || r.session == nil {
		return empty, problem("thread_not_ready", "Wait for the thread to become ready.")
	}
	if r.info.Busy {
		return empty, problem("turn_active", "Wait for the current turn to finish.")
	}
	if len(r.inputs) >= 1000 {
		return empty, problem("input_limit", "This thread has reached its limit of 1,000 inputs for this host run.")
	}
	r.inputs[in.ID] = digest
	r.info.Busy = true
	t.append(r, codethreads.Payload{Type: "input.message.user", Provider: r.info.Provider, ID: in.ID, Text: in.Text, Delivery: in.Delivery})
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		err := r.session.Send(t.ctx, in.Input)
		t.mu.Lock()
		defer t.mu.Unlock()
		if err != nil {
			t.append(r, codethreads.Payload{Type: "input.message.error", Provider: r.info.Provider, InputID: in.ID, Error: err.Error()})
		}
		r.info.Busy = false
	}()
	return r.info, nil
}

func (t *Threads) List(codebase string) []codethreads.Thread {
	t.mu.Lock()
	defer t.mu.Unlock()
	items := []codethreads.Thread{}
	for _, id := range t.order {
		if item := t.items[id]; item.info.CodebaseID == codebase {
			items = append(items, item.info)
		}
	}
	slices.Reverse(items)
	return items
}
func (t *Threads) Read(in codethreads.Read) (codethreads.Page, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := codethreads.Page{Frames: []codethreads.Frame{}}
	r := t.items[in.ThreadID]
	if r == nil || r.info.CodebaseID != in.CodebaseID {
		return out, problem("not_found", "This thread does not exist in the selected codebase.")
	}
	if in.Revision != r.info.Revision {
		in.After = 0
		out.Reset = true
	}
	if in.After > r.changeSeq {
		return out, problem("invalid_cursor", "The thread cursor is ahead of the host. Reopen the thread.")
	}
	out.Thread, out.First, out.Next = r.info, r.seq+1, in.After
	if len(r.frames) > 0 {
		out.First = r.frames[0].Seq
	}
	bytes := 0
	// Current-state upserts coalesce repeated edits. A separate change cursor
	// delivers mutations to older frames without altering their thread position.
	indices := make([]int, len(r.frames))
	for i := range indices {
		indices[i] = i
	}
	slices.SortFunc(indices, func(a, b int) int { return cmp.Compare(r.frames[a].ChangeSeq, r.frames[b].ChangeSeq) })
	for _, i := range indices {
		frame := r.frames[i]
		if frame.ChangeSeq <= in.After {
			continue
		}
		if bytes+r.sizes[i] > 512*1024 || len(out.Frames) >= 100 {
			out.More = true
			break
		}
		out.Frames = append(out.Frames, frame)
		out.Next = frame.ChangeSeq
		bytes += r.sizes[i]
	}
	if !out.More {
		out.Next = r.changeSeq
	}
	return out, nil
}

func (t *Threads) Handle(ctx context.Context, in codehosts.Request) codehosts.Response {
	out := codehosts.Response{ID: in.ID}
	var result any
	var err error
	switch in.Method {
	case codethreads.ReprocessMethod:
		var params codethreads.Reprocess
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = t.Reprocess(ctx, params)
		}
	case codethreads.SendMethod:
		var params codethreads.Send
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = t.Send(ctx, params)
		}
	case codethreads.StartMethod:
		var params codethreads.Start
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = t.Start(ctx, params)
		}
	case codethreads.ListMethod:
		var params codethreads.Query
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result = map[string]any{"threads": t.List(params.CodebaseID)}
		}
	case codethreads.ReadMethod:
		var params codethreads.Read
		err = codehosts.Decode(in.Params, &params)
		if err == nil {
			result, err = t.Read(params)
		}
	default:
		return t.catalogue.Handle(ctx, in)
	}
	if err != nil {
		var p *codehosts.Problem
		if errors.As(err, &p) {
			out.Error = p
		} else {
			out.Error = &codehosts.Problem{Code: "invalid_request", Message: "The host could not complete the thread request."}
		}
	} else {
		out.Result, _ = json.Marshal(result)
	}
	return out
}
