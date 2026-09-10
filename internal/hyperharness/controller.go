package hyperharness

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"
	"time"

	"acta2/internal/harnesspipe"
	"acta2/internal/localstate"
	"acta2/internal/threadadapter"
	"acta2/internal/threads"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

var ErrBusy = errors.New("a thread operation is already pending; try again")

type localThread struct {
	ApprovalReply  string                     `json:"approval_reply,omitempty"`
	PermissionMode string                     `json:"permission_mode,omitempty"`
	Catalogue      *modelCatalogue            `json:"-"`
	Thread         threads.Descriptor         `json:"thread"`
	Discovered     bool                       `json:"discovered"`
	Path           string                     `json:"path,omitempty"`
	Action         string                     `json:"action"`
	CommandID      string                     `json:"command_id"`
	Commands       map[string]threads.Result  `json:"commands"`
	Ack            int64                      `json:"ack"`
	MappedThrough  int64                      `json:"mapped_through"`
	Adapter        threadadapter.State        `json:"adapter"`
	Pending        []threads.Frame            `json:"pending"`
	ProbeAttempt   uint64                     `json:"probe_attempt"`
	Requests       map[string]threads.Control `json:"requests,omitempty"`
	Configuration  *threads.ModelSettings     `json:"configuration,omitempty"`
	SendAttempted  bool                       `json:"send_attempted"`
}
type Controller struct {
	probing   map[string]bool
	failure   error
	offered   map[string]int64
	persist   func(string, any) error
	spawnSpec func(threads.Descriptor) (harnesspipe.Spec, error)
	mu        sync.Mutex
	dir       string
	pipe      harnesspipe.API
	lock      *flock.Flock
	records   map[string]*localThread
	active    map[string]bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewController(ctx context.Context, dir string, pipe harnesspipe.API) (*Controller, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("controller directory must be private (0700)")
	}
	lock := flock.New(filepath.Join(dir, "controller.lock"))
	ok, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("another hyperharness controls this server/account on this machine")
	}
	ctx, cancel := context.WithCancel(ctx)
	c := &Controller{dir: dir, pipe: pipe, lock: lock, spawnSpec: providerSpec, records: map[string]*localThread{}, active: map[string]bool{}, probing: map[string]bool{}, offered: map[string]int64{}, persist: localstate.Write, ctx: ctx, cancel: cancel}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		c.Close()
		return nil, err
	}
	for _, path := range files {
		var r localThread
		if err = localstate.Read(path, &r); err != nil {
			c.Close()
			return nil, err
		}
		if _, err = uuid.Parse(r.Thread.ID); err != nil {
			c.Close()
			return nil, err
		}
		c.records[r.Thread.ID] = &r
	}

	return c, nil
}
func (c *Controller) save(r *localThread) error {
	err := c.persist(filepath.Join(c.dir, r.Thread.ID+".json"), r)
	if err != nil {
		c.failure = fmt.Errorf("cannot persist thread state: %w", err)
		c.cancel() // Do not advertise or act on state whose durable outcome is uncertain.
	}
	return err
}
func (c *Controller) launch(id string) {
	if c.active[id] {
		return
	}
	c.active[id] = true
	c.wg.Go(func() { defer func() { c.mu.Lock(); delete(c.active, id); c.mu.Unlock() }(); c.execute(id) })
}
func (c *Controller) Control(q threads.Control) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if _, err := uuid.Parse(q.ThreadID); err != nil {
		return errors.New("invalid thread id")
	}
	if _, err := uuid.Parse(q.ID); err != nil {
		return errors.New("invalid command id")
	}
	r := c.records[q.ThreadID]
	if r != nil {
		if prior, ok := r.Requests[q.ID]; ok && !reflect.DeepEqual(prior, q) {
			return errors.New("command ID reused with different input")
		}
		if old, ok := r.Commands[q.ID]; ok {
			if q.Action == "send" || q.Action == "models" || q.Action == "configure" || q.Action == "permissions" || q.Action == "approval" || q.Action == "answer" || q.Action == "interrupt" {
				return nil
			} // Replay the durable result, not another provider write.
			if old.Error != "" {
				return errors.New(old.Error)
			}
			return nil
		}
		if q.Action == "start" {
			if q.Provider != r.Thread.Provider || q.CWD != r.Thread.CWD {
				return errors.New("thread UUID reused with different settings")
			}
			return nil
		}
		if c.active[q.ThreadID] && r.Action == "" {
			return ErrBusy
		}
		if r.Action != "" {
			if r.CommandID == q.ID && r.Action == q.Action {
				return nil
			}
			return ErrBusy
		}
	} else {
		if q.Action != "start" {
			return errors.New("thread is not on this machine")
		}
		if !threads.SupportedProvider(q.Provider) {
			return errors.New("unsupported provider")
		}
		if !filepath.IsAbs(q.CWD) {
			return errors.New("working directory must be absolute")
		}
		r = &localThread{Thread: threads.Descriptor{ID: q.ThreadID, Provider: q.Provider, CWD: q.CWD, CreatedAt: time.Now().UTC(), State: "starting", Revision: 1}, Commands: map[string]threads.Result{}}
	}
	copy := *r
	copy.Commands = maps.Clone(r.Commands)
	copy.Requests = maps.Clone(r.Requests)
	if copy.Requests == nil {
		copy.Requests = map[string]threads.Control{}
	}
	r = &copy
	if q.LaneID != "" && (len(q.LaneID) > 256 || q.Action != "interrupt") {
		return errors.New("unsupported subagent command")
	}
	if q.Action == "start" && q.ID != q.ThreadID {
		return errors.New("creation command must use its thread UUID")
	}
	if q.Action != "start" && q.Action != "resume" && q.Action != "kill" && q.Action != "send" && q.Action != "models" && q.Action != "configure" && q.Action != "permissions" && q.Action != "approval" && q.Action != "answer" && q.Action != "interrupt" && q.Action != "rename" {
		return errors.New("unsupported lifecycle action")
	}
	if (q.Action == "rename" && !threads.ValidName(q.Name)) || (q.Action != "rename" && q.Name != "") {
		return errors.New("invalid thread name")
	}
	if q.Action != "send" && len(q.Images) != 0 {
		return errors.New("images require a send command")
	}
	if q.Action == "send" && (r.Thread.State != "running" || q.RunID != r.Thread.RunID || threads.ValidateMessage(q.Text, q.Images) != nil) {
		return errors.New("message requires the current running provider and non-empty text of up to 64 KiB")
	}
	if (q.Action == "models" || q.Action == "configure" || q.Action == "permissions" || q.Action == "approval" || q.Action == "answer" || q.Action == "interrupt") && (r.Thread.State != "running" || q.RunID != r.Thread.RunID) {
		return errors.New("settings require the current running provider")
	}
	if q.Action == "permissions" && !threads.ValidPermissionMode(q.PermissionMode) {
		return errors.New("invalid permission mode")
	}
	if q.Action == "answer" && (q.ID != q.QuestionID || !threads.ValidAnswers(q.Answers)) {
		return errors.New("invalid question answer")
	}
	if q.Action == "approval" && (q.ID != q.ApprovalID || (q.Decision != "approve" && q.Decision != "deny")) {
		return errors.New("invalid approval decision")
	}
	if q.Action == "resume" && r.Thread.ProviderID == "" {
		return errors.New("no provider session is available for resumption")
	}
	if q.Action == "resume" {
		processes, err := c.pipe.Processes(c.ctx)
		if err != nil {
			return err
		}
		for _, p := range processes {
			if p.Spec.ThreadID == q.ThreadID && p.State != "exited" {
				return errors.New("this thread already has a running or uncertain provider process")
			}
		}
	}

	if q.Action == "start" || q.Action == "resume" {
		r.Thread.RunID = q.ID
		r.Thread.State = "starting"
	} else if q.Action == "kill" {
		r.Thread.State = "killing"
	}
	if q.Action != "rename" {
		r.Thread.Error = ""
	}
	r.Thread.Revision++
	r.Action = q.Action
	r.CommandID = q.ID
	q.Answers = maps.Clone(q.Answers)
	for id, values := range q.Answers {
		q.Answers[id] = slices.Clone(values)
	}
	r.Requests[q.ID] = q
	r.SendAttempted = false
	r.ApprovalReply = ""
	if err := c.save(r); err != nil {
		return err
	}
	c.records[q.ThreadID] = r
	c.launch(q.ThreadID)
	return nil
}
func (c *Controller) execute(id string) {
	c.mu.Lock()
	r := *c.records[id]
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()
	var err error
	if r.Action == "rename" {
		c.executeRename(ctx, &r)
		return
	}
	if r.Action == "interrupt" {
		c.executeInterrupt(ctx, &r)
		return
	}
	if r.Action == "permissions" {
		c.executePermissions(ctx, &r)
		return
	}
	if r.Action == "approval" || r.Action == "answer" {
		c.executeApproval(ctx, &r)
		return
	}
	if r.Action == "models" {
		c.executeModels(ctx, &r)
		return
	}
	if r.Action == "configure" {
		c.executeConfigure(ctx, &r)
		return
	}
	if r.Action == "send" {
		c.executeSend(ctx, &r)
		return
	}
	if r.Action == "kill" {
		err = c.pipe.Kill(ctx, r.Thread.RunID)
	} else {
		err = c.start(ctx, &r)
		if err == nil && r.Thread.Name != "" {
			c.restoreNativeName(ctx, &r)
		}
	}
	if err != nil && r.Action != "kill" && c.ctx.Err() == nil {
		cleanup, stop := context.WithTimeout(c.ctx, 12*time.Second)
		killErr := c.pipe.Kill(cleanup, r.Thread.RunID)
		stop()
		if killErr != nil {
			err = fmt.Errorf("%w; provider cleanup: %v", err, killErr)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.records[id]
	if c.ctx.Err() != nil {
		return
	} // Detached reattachment reconciles the durable operation and pipe journal.
	current.Thread.ProviderID = r.Thread.ProviderID
	current.Path = r.Path
	current.Thread.Runtime = r.Thread.Runtime
	current.Thread.NameSyncPending = r.Thread.NameSyncPending
	current.Thread.NameSyncError = r.Thread.NameSyncError
	current.Thread.Committed = current.Thread.Committed || r.Thread.Committed
	if err == nil {
		if r.Action == "kill" {
			current.Thread.State = "exited"
		} else {
			current.Thread.State = "running"
			current.Discovered = true
		}
	} else {
		current.Thread.State = "error"
		current.Thread.Error = err.Error()
	}
	current.Thread.Revision++
	current.Action = ""
	result := threads.Result{ID: r.CommandID, ThreadID: id}
	if err != nil {
		result.Error = err.Error()
	}
	current.Commands[r.CommandID] = result
	if e := c.save(current); e != nil {
		current.Thread.State = "uncertain"
		current.Thread.Error = "Cannot persist lifecycle outcome: " + e.Error()
	}
}
func (c *Controller) start(ctx context.Context, r *localThread) error {
	if r.Thread.Provider == "claude" {
		return c.startClaude(ctx, r)
	}
	return c.startCodex(ctx, r)
}
func (c *Controller) Inventory(ctx context.Context) ([]threads.Descriptor, error) {
	if err := c.Err(); err != nil {
		return nil, err
	}
	ps, err := c.pipe.Processes(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []threads.Descriptor{}
	for _, r := range c.records {
		if r.Action == "" {
			for _, p := range ps {
				if p.Spec.RunID == r.Thread.RunID && p.State != "running" && r.Thread.State == "running" {
					r.Thread.State = p.State
					r.Thread.Error = p.Error
					r.Thread.Revision++
					if err := c.save(r); err != nil {
						return nil, err
					}
				}
			}
		}
		if r.Discovered {
			c.probeLaneMetadata(r)
			if r.Action == "" && r.Thread.State == "running" && (!r.Thread.Committed || r.Thread.Provider == "claude") && !c.probing[r.Thread.ID] {
				id := r.Thread.ID
				c.probing[id] = true
				c.wg.Go(func() {
					defer func() { c.mu.Lock(); delete(c.probing, id); c.mu.Unlock() }()
					if r.Thread.Provider == "claude" {
						c.observeClaude(id)
					} else {
						c.probeCommitment(id)
					}
				})
			}
			out = append(out, r.Thread)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (c *Controller) Results() []threads.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []threads.Result{}
	for _, r := range c.records {
		for _, v := range r.Commands {
			out = append(out, v)
		}
	}
	return out
}

func (c *Controller) Close() error { c.cancel(); c.wg.Wait(); return c.lock.Close() }

func (c *Controller) Recover() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.records {
		if r.Action != "" {
			c.launch(r.Thread.ID)
		}
	}
}

func (c *Controller) Err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.failure }
