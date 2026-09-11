package backup

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"acta/internal/localstate"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

type Service struct {
	c      Config
	engine Engine
	mu     sync.Mutex
	state  State
	lock   *flock.Flock
	wake   chan struct{}
}

func Open(c Config, engine Engine) (*Service, error) {
	if err := os.MkdirAll(c.StateDir, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(c.StateDir, "service.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("another backup service owns this state directory")
	}
	s := &Service{c: c, engine: engine, lock: l, wake: make(chan struct{}, 1)}
	err = ReadJSON(filepath.Join(c.StateDir, "state.json"), &s.state)
	if errors.Is(err, os.ErrNotExist) {
		s.state = State{Version: 1, Jobs: []Job{}, Policy: Policy{Revision: 1, Destination: c.Destinations[0].ID, Mode: "snapshot", Anchor: time.Now().UTC().Truncate(time.Minute), BackupMinutes: 1440, FullMinutes: 10080, DrillMinutes: 0, MaxAgeMinutes: 2880, ArchiveAgeMinutes: 10, RetainFull: max(2, c.MinRetainFull)}}
		err = s.save()
	} else if err == nil && s.state.Version != 1 {
		err = errors.New("unsupported backup state version")
	}
	if err == nil {
		err = s.state.Policy.Validate(c)
	}
	if err != nil {
		_ = l.Unlock()
		return nil, err
	}
	return s, nil
}
func (s *Service) Close() error { return s.lock.Unlock() }
func (s *Service) save() error {
	return localstate.Write(filepath.Join(s.c.StateDir, "state.json"), s.state)
}
func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) busy() bool {
	for _, j := range s.state.Jobs {
		if j.State == "queued" || j.State == "running" {
			return true
		}
	}
	return false
}

func (s *Service) Update(p Policy) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() {
		return Policy{}, ErrBusy
	}
	if p.Revision != s.state.Policy.Revision {
		return Policy{}, ErrConflict
	}
	if err := p.Validate(s.c); err != nil {
		return Policy{}, err
	}
	old := s.state.Policy
	oldRepo := s.state.Repository
	p.Revision++
	p.Anchor = p.Anchor.UTC()
	s.state.Policy = p
	if p.Destination != old.Destination {
		s.state.Repository = Repository{}
	}
	if err := s.save(); err != nil {
		s.state.Policy = old
		s.state.Repository = oldRepo
		return Policy{}, err
	}
	s.signal()
	return p, nil
}

func (s *Service) Enqueue(kind, actor string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enqueue(kind, actor, time.Now().UTC())
}
func (s *Service) enqueue(kind, actor string, now time.Time) (Job, error) {
	if s.busy() {
		return Job{}, ErrBusy
	}
	if kind != "backup" && kind != "full" && kind != "drill" {
		return Job{}, errors.New("choose backup, full or drill")
	}
	if kind == "drill" && s.c.RecoveryIdentityFile == "" {
		return Job{}, errors.New("the operator has not enabled restore drills")
	}
	j := Job{ID: uuid.NewString(), Kind: kind, State: "queued", Actor: actor, Policy: s.state.Policy, RequestedAt: now}
	old := s.state.Jobs
	s.state.Jobs = append([]Job{j}, s.state.Jobs...)
	if len(s.state.Jobs) > 100 {
		s.state.Jobs = s.state.Jobs[:100]
	}
	if err := s.save(); err != nil {
		s.state.Jobs = old
		return Job{}, err
	}
	s.signal()
	return j, nil
}

func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	// JSON clone excludes engine-only sealed recovery payloads and avoids
	// races with later state updates after releasing the lock.
	v := View{MinRetainFull: s.c.MinRetainFull, MaxRetainFull: s.c.MaxRetainFull, Configured: true, Driver: "pgbackrest", Policy: s.state.Policy, CanDrill: s.c.RecoveryIdentityFile != "", Jobs: s.state.Jobs, Repository: s.state.Repository, Warnings: s.warnings(time.Now().UTC()), AlertError: s.state.AlertError}
	for _, d := range s.c.Destinations {
		v.Destinations = append(v.Destinations, Choice{d.ID, d.Name})
	}
	raw, _ := json.Marshal(v)
	var copy View
	_ = json.Unmarshal(raw, &copy)
	return copy
}

func (s *Service) warnings(now time.Time) []string {
	out := []string{}
	p := s.state.Policy
	r := s.state.Repository
	if !p.Enabled {
		out = append(out, "Automatic backups are disabled.")
	}
	if r.Error != "" {
		out = append(out, "Repository inspection failed. Recovery coverage is unknown.")
	}
	if r.ObservedAt.IsZero() || now.Sub(r.ObservedAt) > 2*time.Minute {
		out = append(out, "Repository status is stale.")
	}
	var last, restored time.Time
	for _, point := range r.Points {
		if point.Complete && point.IntegrityAt != nil {
			if point.FinishedAt.After(last) {
				last = point.FinishedAt
			}
		}
		if point.RestoredAt != nil && point.RestoredAt.After(restored) {
			restored = *point.RestoredAt
		}
	}
	if last.IsZero() {
		out = append(out, "No complete integrity-checked installation backup is available.")
	} else if now.Sub(last) > time.Duration(p.MaxAgeMinutes)*time.Minute {
		out = append(out, "The latest integrity-checked backup exceeds the configured age limit.")
	}
	if restored.IsZero() {
		out = append(out, "No backup has passed a restore drill.")
	} else if p.DrillMinutes > 0 && now.Sub(restored) > time.Duration(p.DrillMinutes+p.BackupMinutes)*time.Minute {
		out = append(out, "A restore drill is overdue.")
	}
	if p.Mode == "continuous" {
		if !r.ArchiveMode || r.ArchiveTimeoutSeconds <= 0 || r.ArchiveTimeoutSeconds > p.ArchiveAgeMinutes*60 {
			out = append(out, "PostgreSQL archive settings do not satisfy the configured recovery policy.")
		}
		if r.ArchiveLast == nil || now.Sub(*r.ArchiveLast) > time.Duration(p.ArchiveAgeMinutes)*time.Minute {
			out = append(out, "Transaction-log archive evidence is overdue; the current recovery gap is unknown.")
		}
		if r.ArchiveFailure != nil && (r.ArchiveLast == nil || r.ArchiveFailure.After(*r.ArchiveLast)) {
			out = append(out, "The latest PostgreSQL archive attempt failed.")
		}
	}
	if len(s.state.Jobs) > 0 && s.state.Jobs[0].State == "failed" {
		out = append(out, "The latest backup operation failed.")
	}
	return out
}

func (s *Service) refresh(ctx context.Context) error {
	s.mu.Lock()
	p := s.state.Policy
	s.mu.Unlock()
	r, err := s.engine.Inspect(ctx, p)
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Revision != s.state.Policy.Revision {
		return nil
	}
	if err != nil {
		s.state.Repository.Error = "Cannot inspect repository or source database."
	} else {
		s.state.Repository = compactRepository(r)
	}
	return s.save()
}

func (s *Service) Run(ctx context.Context) error {
	// A running backup may have reached the repository before a crash. Its
	// persisted job ID is reused by the engine to reconcile that outcome.
	s.mu.Lock()
	for i := range s.state.Jobs {
		if s.state.Jobs[i].State == "running" {
			if s.state.Jobs[i].Kind == "drill" {
				s.state.Jobs[i].State = "failed"
				s.state.Jobs[i].Error = "Restore drill was interrupted; inspect the retained isolated target."
				now := time.Now().UTC()
				s.state.Jobs[i].FinishedAt = &now
			} else {
				s.state.Jobs[i].State = "queued"
			}
		}
	}
	err := s.save()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err = s.execute(ctx); err != nil {
			return err
		}
		inspect, cancel := context.WithTimeout(ctx, 25*time.Second)
		err = s.refresh(inspect)
		cancel()
		if err != nil {
			return err
		}
		if err = s.schedule(time.Now().UTC()); err != nil {
			return err
		}
		if err = s.alert(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func (s *Service) schedule(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.state.Policy
	if !p.Enabled || s.busy() {
		return nil
	}
	var lastBackup, lastFull, lastDrill time.Time
	for _, point := range s.state.Repository.Points {
		if point.IntegrityAt != nil && point.Complete {
			if point.FinishedAt.After(lastBackup) {
				lastBackup = point.FinishedAt
			}
			if point.Type == "full" && point.FinishedAt.After(lastFull) {
				lastFull = point.FinishedAt
			}
		}
		if point.RestoredAt != nil && point.RestoredAt.After(lastDrill) {
			lastDrill = *point.RestoredAt
		}
	}
	// Back off failed jobs instead of retrying every health poll. The queue
	// and requested time survive a restart. Independent destinations do not
	// inherit another destination's successful schedule slots.
	if len(s.state.Jobs) > 0 {
		j := s.state.Jobs[0]
		if j.State == "failed" && j.FinishedAt != nil && now.Sub(*j.FinishedAt) < 5*time.Minute {
			return nil
		}
	}
	kind := ""
	if Due(now, p.Anchor, p.FullMinutes, lastFull) {
		kind = "full"
	} else if Due(now, p.Anchor, p.BackupMinutes, lastBackup) {
		kind = "backup"
	} else if !lastBackup.IsZero() && Due(now, p.Anchor, p.DrillMinutes, lastDrill) {
		kind = "drill"
	}
	if kind != "" {
		_, err := s.enqueue(kind, "scheduler", now)
		return err
	}
	return nil
}

func (s *Service) execute(ctx context.Context) error {
	s.mu.Lock()
	idx := -1
	for i, j := range s.state.Jobs {
		if j.State == "queued" {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return nil
	}
	j := s.state.Jobs[idx]
	now := time.Now().UTC()
	j.StartedAt = &now
	j.State = "running"
	s.state.Jobs[idx] = j
	err := s.save()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithTimeout(ctx, time.Duration(s.c.JobTimeoutMinutes)*time.Minute)
	defer cancel()
	if j.Kind == "drill" {
		var r Repository
		r, err = s.engine.Inspect(jobCtx, j.Policy)
		if err == nil {
			// Drill the latest full chain first, allowing safe retention to make
			// progress. Explicit standalone restores can verify any selected set.
			for _, p := range r.Points {
				if p.Type == "full" && p.Complete && p.IntegrityAt != nil {
					j.Backup = p.Label
					break
				}
			}
			if j.Backup == "" {
				err = errors.New("no integrity-checked full backup is available")
			} else {
				err = s.engine.Drill(jobCtx, j.Policy, j.Backup)
			}
		}
	} else {
		var point Point
		point, err = s.engine.Backup(jobCtx, j.Policy, j.ID, j.Kind == "full")
		j.Backup = point.Label
		if err == nil {
			err = s.engine.Verify(jobCtx, j.Policy, point.Label)
		}
	}
	if err == nil {
		err = s.engine.Expire(jobCtx, j.Policy)
	}
	finished := time.Now().UTC()
	j.FinishedAt = &finished
	j.State = "succeeded"
	if err != nil {
		slog.Error("backup job failed", "job", j.ID, "error", err)
		j.State = "failed"
		j.Error = "Operation failed. Inspect the backup service and protected pgBackRest logs."
	}
	if ctx.Err() != nil && j.Kind != "drill" {
		j.State = "running"
		j.FinishedAt = nil
	}
	s.mu.Lock()
	s.state.Jobs[idx] = j
	saveErr := s.save()
	s.mu.Unlock()
	return saveErr
}

func (s *Service) alert(ctx context.Context) error {
	if s.c.AlertCommand == "" {
		return nil
	}
	s.mu.Lock()
	warnings := s.warnings(time.Now().UTC())
	key := strings.Join(warnings, "\n")
	if key == s.state.AlertKey && s.state.AlertError == "" {
		s.mu.Unlock()
		return nil
	}
	if s.state.AlertSentAt != nil && time.Since(*s.state.AlertSentAt) < 5*time.Minute {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"kind": "acta.backup.health", "warnings": warnings, "at": time.Now().UTC()})
	alert, cancel := context.WithTimeout(ctx, 30*time.Second)
	_, err := command(alert, s.c.AlertCommand, nil, raw)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.state.AlertSentAt = &now
	if err == nil {
		s.state.AlertKey = key
		s.state.AlertError = ""
	} else {
		s.state.AlertError = "External alert delivery failed."
	}
	return s.save()
}

// Keep bounded web/state history plus the full/restore evidence needed for
// scheduling. The engine always uses the complete catalog for retention.
func compactRepository(r Repository) Repository {
	if len(r.Points) <= 500 {
		return r
	}
	selected := append([]Point(nil), r.Points[:500]...)
	full, restored, valid := "", "", ""
	latestRestore := time.Time{}
	for _, p := range r.Points {
		if valid == "" && p.Complete && p.IntegrityAt != nil {
			valid = p.Label
		}
		if full == "" && p.Type == "full" && p.Complete && p.IntegrityAt != nil {
			full = p.Label
		}
		if p.RestoredAt != nil && p.RestoredAt.After(latestRestore) {
			latestRestore = *p.RestoredAt
			restored = p.Label
		}
	}
	for _, p := range r.Points[500:] {
		if p.Label == full || p.Label == restored || p.Label == valid {
			selected = append(selected, p)
		}
	}
	r.Points = selected
	return r
}
