package hyperharness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"acta2/internal/harnesspipe"
	"acta2/internal/threads"
	"github.com/google/uuid"
)

func TestCodexLifecycleHelper(t *testing.T) {
	if os.Getenv("ACTA_CODEX_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var q struct {
			ID     string         `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &q) != nil {
			os.Exit(2)
		}
		if q.ID == "" {
			continue
		}
		var result any = map[string]any{}
		if q.Method == "thread/start" || q.Method == "thread/resume" {
			id := "native-" + q.ID
			if q.Method == "thread/resume" {
				id = q.Params["threadId"].(string)
			}
			path := os.Getenv("ACTA_CODEX_ROLLOUT")
			// No rollout is created. Provider acknowledgement alone is enough
			// for discovery; resumability is the provider's separate contract.
			if q.Method == "thread/resume" && os.Getenv("ACTA_CODEX_REJECT_RESUME") == "1" {
				raw, _ := json.Marshal(map[string]any{"id": q.ID, "error": map[string]any{"message": "no rollout found"}})
				fmt.Println(string(raw))
				continue
			}
			result = map[string]any{"thread": map[string]string{"id": id, "path": path}}
		}
		if q.Method == "thread/name/set" {
			if q.Params["name"] == "Rejected" {
				raw, _ := json.Marshal(map[string]any{"id": q.ID, "error": map[string]string{"message": "rename rejected"}})
				fmt.Println(string(raw))
				continue
			}
			result = q.Params
		}
		if q.Method == "model/list" {
			result = map[string]any{"data": []any{
				map[string]any{"model": "test-model", "displayName": "Test Model", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "high", "description": "High"}, map[string]string{"reasoningEffort": "low", "description": "Low"}}, "defaultReasoningEffort": "low", "serviceTiers": []any{map[string]string{"id": "priority", "description": "Faster, increased usage"}}},
				map[string]any{"model": "small-model", "displayName": "Small Model", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "low", "description": "Low"}}, "defaultReasoningEffort": "low", "serviceTiers": []any{}},
			}, "nextCursor": nil}
		}
		if q.Method == "thread/settings/update" {
			settings := map[string]any{"model": q.Params["model"], "effort": q.Params["effort"], "serviceTier": q.Params["serviceTier"], "cwd": "/tmp", "approvalPolicy": "on-request", "approvalsReviewer": "user", "sandboxPolicy": map[string]string{"type": "readOnly"}}
			notice, _ := json.Marshal(map[string]any{"method": "thread/settings/updated", "params": map[string]any{"threadId": q.Params["threadId"], "threadSettings": settings}})
			fmt.Println(string(notice))
		}
		if q.Method == "thread/read" {
			result = map[string]any{"thread": map[string]any{"id": q.Params["threadId"], "status": map[string]string{"type": "idle"}}}
		}
		if q.Method == "turn/start" && q.Params["clientUserMessageId"] != nil {
			input := q.Params["input"].([]any)[0].(map[string]any)["text"].(string)
			if input == "reject" {
				fmt.Printf(`{"id":%q,"error":{"message":"rejected for test"}}`+"\n", q.ID)
				continue
			}
			if input == "uncertain" {
				fmt.Printf(`{"id":%q,"result":{}}`+"\n", q.ID)
				continue
			}
			fmt.Printf(`{"id":%q,"result":{"turn":{"id":"sent-turn"}}}`+"\n", q.ID)
			frame := map[string]any{"method": "item/started", "params": map[string]any{"threadId": q.Params["threadId"], "turnId": "sent-turn", "item": map[string]any{"type": "userMessage", "id": q.Params["clientUserMessageId"], "clientId": q.Params["clientUserMessageId"], "content": q.Params["input"]}}}
			raw, _ := json.Marshal(frame)
			fmt.Println(string(raw))
			continue
		}
		if q.Method == "turn/start" {
			// Every model turn must originate from an explicit send command.
			os.Exit(4)
		}
		raw, _ := json.Marshal(map[string]any{"id": q.ID, "result": result})
		fmt.Println(string(raw))
	}
	os.Exit(0)
}
func TestControllerDetachedReattachmentAndResume(t *testing.T) {
	t.Setenv("ACTA_CODEX_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("ACTA_CODEX_ROLLOUT", filepath.Join(t.TempDir(), "rollout"))
	root := t.TempDir()
	pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	spec := func(d threads.Descriptor) (harnesspipe.Spec, error) {
		spawns.Add(1)
		return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: d.CWD, Executable: executable, Args: []string{"-test.run=^TestCodexLifecycleHelper$"}}, nil
	}
	open := func() *Controller {
		t.Helper()
		c, err := NewController(t.Context(), filepath.Join(root, "threads"), pipe)
		if err != nil {
			t.Fatal(err)
		}
		c.spawnSpec = spec
		c.Recover()
		return c
	}
	c := open()
	defer func() { c.Close() }()
	id := uuid.NewString()
	q := threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "codex", CWD: root}
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	if err = c.Control(q); err != nil {
		t.Fatal(err)
	}
	wait := func(state string) threads.Descriptor {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()
		for {
			list, err := c.Inventory(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(list) == 1 && list[0].State == state && len(c.Results()) > 0 && !controllerPending(c, id) {
				return list[0]
			}
			select {
			case <-ctx.Done():
				t.Fatalf("thread did not reach %s: %#v", state, list)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	first := wait("running")
	assertEmptyThread(t, c, id)
	frames, err := c.Frames(t.Context(), id)
	if err != nil || len(frames) < 2 {
		t.Fatal(frames, err)
	}
	last := frames[len(frames)-1].Sequence
	if err = c.Acknowledge(id, last+100); err == nil {
		t.Fatal("accepted an acknowledgement beyond delivered frames")
	}
	if err = c.Acknowledge(id, last); err != nil {
		t.Fatal(err)
	}
	ps, _ := pipe.Processes(t.Context())
	pid := ps[0].PID
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	c = open()
	second := wait("running")
	ps, _ = pipe.Processes(t.Context())
	if first.ProviderID != second.ProviderID || ps[0].PID != pid || spawns.Load() != 1 {
		t.Fatal("reattachment restarted provider")
	}
	frames, err = c.Frames(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if f.Sequence <= last {
			t.Fatal("acknowledged frames replayed")
		}
	}
	kill := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"}
	if err = c.Control(kill); err != nil {
		t.Fatal(err)
	}
	if err = c.Control(kill); err != nil {
		t.Fatal("redelivery while Kill pending", err)
	}
	wait("exited")
	if err = c.Control(kill); err != nil {
		t.Fatal("duplicate Kill", err)
	}
	resume := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"}
	if err = c.Control(resume); err != nil {
		t.Fatal(err)
	}
	resumed := wait("running")
	if resumed.ProviderID != first.ProviderID || resumed.RunID == first.RunID || spawns.Load() != 2 {
		t.Fatal("resume lost identity", resumed)
	}
	t.Setenv("ACTA_CODEX_REJECT_RESUME", "1")
	if err = c.Control(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"}); err != nil {
		t.Fatal(err)
	}
	wait("exited")
	if err = c.Control(threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"}); err != nil {
		t.Fatal(err)
	}
	failed := wait("error")
	if failed.ProviderID != first.ProviderID || failed.Error != "no rollout found" || spawns.Load() != 3 {
		t.Fatal("missing session was replaced", failed)
	}
	raw, err := pipe.Read(t.Context(), harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	starts, turns := 0, 0
	for _, f := range raw {
		var response struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(f.Data, &response)
		if strings.HasSuffix(response.ID, "/turn/start") {
			turns++
		}
		if strings.HasSuffix(response.ID, "/thread/start") {
			starts++
		}
	}
	if turns != 0 {
		t.Fatal("creation or recovery sent an unsolicited turn", turns)
	}
	if starts != 1 {
		t.Fatal("unexpected second native Start", starts)
	}
}

func TestControllerFailsClosedOnPersistenceFailure(t *testing.T) {
	pipe, err := harnesspipe.Open(filepath.Join(t.TempDir(), "pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	c, err := NewController(t.Context(), filepath.Join(t.TempDir(), "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.persist = func(string, any) error { return errors.New("disk full") }
	id := uuid.NewString()
	if err = c.Control(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "codex", CWD: t.TempDir()}); err == nil {
		t.Fatal("failed save accepted")
	}
	if c.Err() == nil {
		t.Fatal("controller remained healthy after failed persistence")
	}
	if len(c.records) != 0 {
		t.Fatal("uncommitted intent became live")
	}
	ps, err := pipe.Processes(t.Context())
	if err != nil || len(ps) != 0 {
		t.Fatal("spawned after failed intent", ps, err)
	}
}

// Simulates losing the adapter's input acknowledgement after the pipe accepted
// thread/start. The process and raw journal remain with the detached pipe.
type lostStartReply struct {
	harnesspipe.API
	accepted chan struct{}
	method   string
}

func (p *lostStartReply) Write(ctx context.Context, q harnesspipe.WriteRequest) error {
	if err := p.API.Write(ctx, q); err != nil {
		return err
	}
	if strings.HasSuffix(q.ID, "/"+p.method) {
		close(p.accepted)
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func TestControllerRecoversPendingStartWithoutSecondNativeThread(t *testing.T) {
	for _, method := range []string{"initialize", "thread/start"} {
		t.Run(method, func(t *testing.T) { testRecoverPendingCreation(t, method) })
	}
}
func testRecoverPendingCreation(t *testing.T, method string) {
	t.Setenv("ACTA_CODEX_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	root := t.TempDir()
	pipe, err := harnesspipe.Open(filepath.Join(root, "pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()
	wrapped := &lostStartReply{API: pipe, accepted: make(chan struct{}), method: method}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := NewController(ctx, filepath.Join(root, "controller"), wrapped)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := func(d threads.Descriptor) (harnesspipe.Spec, error) {
		return harnesspipe.Spec{ThreadID: d.ID, RunID: d.RunID, CWD: d.CWD, Executable: executable, Args: []string{"-test.run=^TestCodexLifecycleHelper$"}}, nil
	}
	c.spawnSpec = spec
	id := uuid.NewString()
	if err = c.Control(threads.Control{ID: id, ThreadID: id, Action: "start", Provider: "codex", CWD: root}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wrapped.accepted:
	case <-ctx.Done():
		t.Fatal("Start not accepted")
	}
	ps, err := pipe.Processes(ctx)
	if err != nil || len(ps) != 1 {
		t.Fatal(ps, err)
	}
	pid := ps[0].PID
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewController(ctx, filepath.Join(root, "controller"), pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	recovered.spawnSpec = spec
	recovered.Recover()
	for {
		list, err := recovered.Inventory(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 && list[0].State == "running" && !controllerPending(recovered, id) {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("pending Start did not reconcile", list, recovered.Results())
		case <-time.After(10 * time.Millisecond):
		}
	}
	ps, err = pipe.Processes(ctx)
	if err != nil || len(ps) != 1 || ps[0].PID != pid {
		t.Fatal("provider replaced", ps, err)
	}
	raw, err := pipe.Read(ctx, harnesspipe.ReadRequest{ThreadID: id})
	if err != nil {
		t.Fatal(err)
	}
	starts, turns := 0, 0
	for _, f := range raw {
		var response struct{ ID string }
		_ = json.Unmarshal(f.Data, &response)
		if strings.HasSuffix(response.ID, "/turn/start") {
			turns++
		}
		if strings.HasSuffix(response.ID, "/thread/start") {
			starts++
		}
	}
	if turns != 0 {
		t.Fatal("creation or recovery sent an unsolicited turn", turns)
	}
	if starts != 1 {
		t.Fatal("native Start repeated", starts)
	}
}

func controllerPending(c *Controller, id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records[id].Action != "" || c.active[id]
}

// Provider readiness must not require sending content or starting a model turn.
func assertEmptyThread(t *testing.T, c *Controller, id string) {
	t.Helper()
	inventory, err := c.Inventory(t.Context())
	if err != nil || len(inventory) != 1 || inventory[0].ID != id || inventory[0].State != "running" || inventory[0].Committed {
		t.Fatalf("expected a discovered, running, uncommitted thread: %#v, %v", inventory, err)
	}
	frames, err := c.Frames(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range frames {
		if strings.HasPrefix(frame.Kind, "message/") || strings.HasPrefix(frame.Kind, "turn/") {
			t.Fatalf("creation generated unsolicited conversation content: %s %s", frame.Kind, frame.Data)
		}
	}
}
