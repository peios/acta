package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"acta2/internal/localstate"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

var ErrBusy = errors.New("an update is already in progress")

type Job struct {
	ID          string    `json:"id"`
	Actor       string    `json:"actor"`
	Phase       string    `json:"phase"`
	Previous    Signed    `json:"previous"`
	Target      Signed    `json:"target"`
	Started     time.Time `json:"started_at"`
	Updated     time.Time `json:"updated_at"`
	Error       string    `json:"error,omitempty"`
	Paused      bool      `json:"paused,omitempty"`
	RestoreData bool      `json:"restore_data"`
}
type State struct {
	Version    int        `json:"version"`
	Current    Signed     `json:"current"`
	Available  *Signed    `json:"available,omitempty"`
	Checked    *time.Time `json:"checked_at,omitempty"`
	CheckError string     `json:"check_error,omitempty"`
	Jobs       []Job      `json:"jobs"`
}
type Engine interface {
	Prepare(context.Context, Job) error
	Quiesce(context.Context, Job) error
	Snapshot(context.Context, Job) error
	Apply(context.Context, Job) error
	Validate(context.Context, Job) error
	Restore(context.Context, Job) error
	Publish(context.Context, Job, bool) error
}
type Service struct {
	c        Config
	key      []byte
	engine   Engine
	mu       sync.Mutex
	state    State
	lock     *flock.Flock
	wake     chan struct{}
	check    chan struct{}
	checking bool
}

func Open(c Config, e Engine) (*Service, error) {
	key, err := c.Key()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(c.StateDir, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(c.StateDir, "service.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBusy
	}
	s := &Service{c: c, key: key, engine: e, lock: l, wake: make(chan struct{}, 1), check: make(chan struct{}, 1)}
	if err = localstate.Read(filepath.Join(c.StateDir, "state.json"), &s.state); err != nil {
		l.Unlock()
		return nil, fmt.Errorf("load updater journal (bootstrap first): %w", err)
	}
	if s.state.Version != 1 {
		l.Unlock()
		return nil, errors.New("unsupported updater journal version")
	}
	if _, err = Verify(s.state.Current, key, c.Repository); err != nil {
		l.Unlock()
		return nil, err
	}
	for _, j := range s.state.Jobs {
		if _, err = Verify(j.Target, key, c.Repository); err != nil {
			l.Unlock()
			return nil, err
		}
		if _, err = Verify(j.Previous, key, c.Repository); err != nil {
			l.Unlock()
			return nil, err
		}
	}
	return s, nil
}
func (s *Service) Close() error { return s.lock.Unlock() }
func (s *Service) save() error {
	return localstate.Write(filepath.Join(s.c.StateDir, "state.json"), s.state)
}
func terminal(p string) bool  { return p == "succeeded" || p == "rolled_back" || p == "failed" }
func (s *Service) busy() bool { return len(s.state.Jobs) > 0 && !terminal(s.state.Jobs[0].Phase) }
func (s *Service) View() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var out State
	_ = json.Unmarshal(raw, &out)
	return out
}
func (s *Service) Install(id, actor string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() || s.checking {
		return Job{}, ErrBusy
	}
	if s.state.Available == nil || s.state.Available.ID() != id {
		return Job{}, errors.New("release selection is stale; check for updates again")
	}
	current, err := Verify(s.state.Current, s.key, s.c.Repository)
	if err != nil {
		return Job{}, err
	}
	next, err := Verify(*s.state.Available, s.key, s.c.Repository)
	if err != nil {
		return Job{}, err
	}
	if err = Compatible(current, next); err != nil {
		return Job{}, err
	}
	j := Job{ID: uuid.NewString(), Actor: actor, Phase: "preparing", Previous: s.state.Current, Target: *s.state.Available, Started: time.Now().UTC(), Updated: time.Now().UTC()}
	old := s.state.Jobs
	s.state.Jobs = append([]Job{j}, old...)
	if err = s.save(); err != nil {
		s.state.Jobs = old
		return Job{}, err
	}
	s.signal()
	return j, nil
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Retry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.busy() {
		return errors.New("no interrupted update to continue")
	}
	s.signal()
	return nil
}
func (s *Service) transition(phase string, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.Jobs[0]
	j := old
	j.Paused = cause != nil && old.Phase == phase
	j.Phase = phase
	j.Updated = time.Now().UTC()
	if cause != nil {
		j.Error = cause.Error()
	}
	if phase == "applying" {
		j.RestoreData = true
	}
	s.state.Jobs[0] = j
	current := s.state.Current
	available := s.state.Available
	if phase == "succeeded" {
		s.state.Current = j.Target
		s.state.Available = nil
	}
	if phase == "rolled_back" {
		s.state.Current = j.Previous
	}
	if err := s.save(); err != nil {
		s.state.Jobs[0] = old
		s.state.Current = current
		s.state.Available = available
		return err
	}
	return nil
}

// Run never infers completion from a missing process. The durable phase is the
// authority; ambiguous candidate execution is rolled back before admitting users.
func (s *Service) Run(ctx context.Context) {
	s.signal()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RequestCheck()
		case <-s.check:
			checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_ = s.Check(checkCtx)
			cancel()
		case <-s.wake:
			if err := s.advance(ctx); err != nil {
				fmt.Fprintln(os.Stderr, "Update paused:", err)
			} else if !s.busyView() {
				if d, ok := s.engine.(interface{ ReconcileUpdater(context.Context) error }); ok {
					if err := d.ReconcileUpdater(ctx); err != nil {
						fmt.Fprintln(os.Stderr, "Updater replacement pending:", err)
					}
				}
			}
		}
	}
}
func (s *Service) advance(parent context.Context) error {
	s.mu.Lock()
	busy := s.busy()
	s.mu.Unlock()
	if !busy {
		return nil
	}
	initial := s.View().Jobs[0]
	if initial.Phase == "applying" || initial.Phase == "validating" {
		if err := s.transition("restoring", errors.New("update interrupted before cutover; recovering previous release")); err != nil {
			return err
		}
	}
	for {
		if parent.Err() != nil {
			return parent.Err()
		}
		j := s.View().Jobs[0]
		if terminal(j.Phase) {
			return nil
		}
		ctx, cancel := context.WithTimeout(parent, time.Duration(s.c.TimeoutMinutes)*time.Minute)
		var err error
		next := ""
		switch j.Phase {
		case "preparing":
			err = s.engine.Prepare(ctx, j)
			next = "quiescing"
		case "quiescing":
			err = s.engine.Quiesce(ctx, j)
			next = "snapshotting"
		case "snapshotting":
			err = s.engine.Snapshot(ctx, j)
			next = "applying"
		case "applying":
			err = s.engine.Apply(ctx, j)
			next = "validating"
		case "validating":
			err = s.engine.Validate(ctx, j)
			next = "committing"
		case "committing":
			err = s.engine.Publish(ctx, j, false)
			next = "succeeded"
		case "restoring":
			err = s.engine.Restore(ctx, j)
			next = "rollback_committing"
		case "rollback_committing":
			err = s.engine.Publish(ctx, j, true)
			next = "rolled_back"
		default:
			err = errors.New("unrecognised update phase")
		}
		cancel()
		if parent.Err() != nil {
			return parent.Err()
		}
		if err != nil {
			switch j.Phase {
			case "preparing":
				return s.transition("failed", err)
			case "quiescing", "snapshotting", "applying", "validating":
				if e := s.transition("restoring", err); e != nil {
					return e
				}
				continue
			default:
				_ = s.transition(j.Phase, err)
				return err
			}
		}
		if err = s.transition(next, nil); err != nil {
			return err
		}
	}
}
func stringTrim(b []byte) string { return strings.TrimSpace(string(b)) }

func (s *Service) busyView() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.busy() }

func (s *Service) RequestCheck() {
	select {
	case s.check <- struct{}{}:
	default:
	}
}
