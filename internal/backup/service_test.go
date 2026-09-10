package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testEngine struct {
	repository             Repository
	failBackup, failVerify bool
	expired                bool
	calls                  int
	lastID                 string
}

func (e *testEngine) Inspect(context.Context, Policy) (Repository, error) { return e.repository, nil }
func (e *testEngine) Backup(_ context.Context, _ Policy, id string, _ bool) (Point, error) {
	e.calls++
	e.lastID = id
	if e.failBackup {
		return Point{}, errors.New("failed upload")
	}
	return Point{Label: "20260910-120000F", Complete: true}, nil
}
func (e *testEngine) Verify(context.Context, Policy, string) error {
	if e.failVerify {
		return errors.New("corrupt backup")
	}
	return nil
}
func (e *testEngine) Drill(context.Context, Policy, string) error { return nil }
func (e *testEngine) Expire(context.Context, Policy) error        { e.expired = true; return nil }
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{StateDir: t.TempDir(), Destinations: []Destination{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}, MinRetainFull: 2, MaxRetainFull: 30, JobTimeoutMinutes: 1, RecoveryIdentityFile: "/operator/identity"}
}

func TestPolicyAndSchedule(t *testing.T) {
	c := testConfig(t)
	s, err := Open(c, &testEngine{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := s.View().Policy
	p.Enabled = true
	p.BackupMinutes = 60
	p.FullMinutes = 1440
	p.MaxAgeMinutes = 120
	if _, err = s.Update(p); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(p); !errors.Is(err, ErrConflict) {
		t.Fatal("stale policy accepted", err)
	}
	p = s.View().Policy
	p.RetainFull = 1
	if _, err = s.Update(p); err == nil {
		t.Fatal("unsafe retention accepted")
	}
	p = s.View().Policy
	p.Destination = "arbitrary/path"
	if _, err = s.Update(p); err == nil {
		t.Fatal("unprovisioned destination accepted")
	}
	anchor := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if Due(anchor.Add(-time.Second), anchor, 60, time.Time{}) {
		t.Fatal("ran before anchor")
	}
	if !Due(anchor.Add(72*time.Hour+30*time.Minute), anchor, 60, anchor) {
		t.Fatal("missed slots did not coalesce")
	}
	if Due(anchor.Add(72*time.Hour+30*time.Minute), anchor, 60, anchor.Add(72*time.Hour+time.Minute)) {
		t.Fatal("repeated current slot")
	}
}

func TestFailedBackupNeverExpires(t *testing.T) {
	for _, phase := range []string{"upload", "verify"} {
		t.Run(phase, func(t *testing.T) {
			e := &testEngine{failBackup: phase == "upload", failVerify: phase == "verify"}
			s, err := Open(testConfig(t), e)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			j, err := s.Enqueue("full", "review")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Enqueue("full", "other"); !errors.Is(err, ErrBusy) {
				t.Fatal("overlap accepted")
			}
			if _, err = s.Update(s.View().Policy); !errors.Is(err, ErrBusy) {
				t.Fatal("in-flight policy changed")
			}
			if err = s.execute(context.Background()); err != nil {
				t.Fatal(err)
			}
			if e.expired || s.View().Jobs[0].State != "failed" || s.View().Jobs[0].ID != j.ID {
				t.Fatal("failure lost or retention ran")
			}
			if strings.Contains(s.View().Jobs[0].Error, "upload") {
				t.Fatal("driver details leaked")
			}
		})
	}
}

func TestQueueAndPolicySurviveRestart(t *testing.T) {
	c := testConfig(t)
	e := &testEngine{}
	s, err := Open(c, e)
	if err != nil {
		t.Fatal(err)
	}
	p := s.View().Policy
	p.BackupMinutes = 30
	p.Destination = "two"
	if _, err = s.Update(p); err != nil {
		t.Fatal(err)
	}
	j, err := s.Enqueue("backup", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(c, e); err == nil {
		t.Fatal("two workers acquired one state directory")
	}
	s.Close()
	s, err = Open(c, e)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.View().Policy.Destination != "two" || s.View().Jobs[0].ID != j.ID {
		t.Fatal("durable state was lost")
	}
	if err = s.execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.lastID != j.ID || s.View().Jobs[0].State != "succeeded" {
		t.Fatal("persisted job not executed")
	}
}

func TestCorruptStateFailsClosed(t *testing.T) {
	c := testConfig(t)
	if err := os.WriteFile(filepath.Join(c.StateDir, "state.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(c, &testEngine{}); err == nil {
		t.Fatal("corruption silently replaced with defaults")
	}
}

func TestSealedMetadataNeverReachesStatus(t *testing.T) {
	s, err := Open(testConfig(t), &testEngine{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.state.Repository.Points = []Point{{Label: "review", SealedRecovery: "PRIVATE", JobID: "internal"}}
	if s.View().Repository.Points[0].SealedRecovery != "" {
		t.Fatal("sealed key leaked to status")
	}
}

func TestRepositoryParsingRejectsUnencryptedAndMissing(t *testing.T) {
	for _, raw := range []string{`[]`, `[{"name":"acta","status":{"code":0},"cipher":"none"}]`, `[{"name":"acta","status":{"code":99},"cipher":"aes-256-cbc"}]`} {
		if _, err := parseInfo([]byte(raw), "acta"); err == nil {
			t.Fatal("invalid repository accepted", raw)
		}
	}
	r, err := parseInfo([]byte(`[{"name":"acta","status":{"code":0},"cipher":"aes-256-cbc","backup":[{"label":"set","type":"full","timestamp":{"start":100,"stop":200},"annotation":{"acta-complete":"1","acta-release":"v1","acta-recovery":"sealed","acta-integrity":"2026-09-10T12:00:00Z"}}]}]`), "acta")
	if err != nil || len(r.Points) != 1 || !r.Points[0].Complete || r.Points[0].RestoredAt != nil {
		t.Fatal("integrity mistaken for restore evidence", r, err)
	}
}

func TestStateWriteFailureDoesNotAcknowledgeMutation(t *testing.T) {
	c := testConfig(t)
	s, err := Open(c, &testEngine{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state := filepath.Join(c.StateDir, "state.json")
	if err = os.Remove(state); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	old := s.View().Policy
	p := old
	p.Enabled = true
	if _, err = s.Update(p); err == nil {
		t.Fatal("acknowledged unsaved policy")
	}
	if s.View().Policy != old {
		t.Fatal("unsaved policy leaked into current state")
	}
	if _, err = s.Enqueue("full", "operator"); err == nil {
		t.Fatal("acknowledged unsaved job")
	}
	if len(s.View().Jobs) != 0 {
		t.Fatal("unsaved job remained queued")
	}
}

func TestVerifyReportFailsClosed(t *testing.T) {
	valid := "stanza: acta\nstatus: ok\n archiveId: 17-1, total WAL checked: 1, total valid WAL: 1\n missing: 0, checksum invalid: 0, size invalid: 0, other: 0\n backup: set, status: valid, total files checked: 10, total valid files: 10\n missing: 0, checksum invalid: 0, size invalid: 0, other: 0\n"
	if err := verifyReport([]byte(valid), "acta", "set"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", strings.ReplaceAll(valid, "status: ok", "status: error"), strings.ReplaceAll(valid, "checksum invalid: 0", "checksum invalid: 1"), strings.ReplaceAll(valid, "valid files: 10", "valid files: 9"), strings.ReplaceAll(valid, "backup: set", "backup: other")} {
		if err := verifyReport([]byte(bad), "acta", "set"); err == nil {
			t.Fatal("accepted incomplete/corrupt report")
		}
	}
}

func TestLargeCatalogDiscardsUnneededRecoveryPayloads(t *testing.T) {
	// This catalog exceeds the normal 16 MiB subprocess output limit; emit one
	// record at a time without constructing the whole input in memory.
	r, w := io.Pipe()
	go func() {
		fmt.Fprint(w, `[{"name":"acta","status":{"code":0},"cipher":"aes-256-cbc","backup":[`)
		payload := strings.Repeat("a", 20<<10)
		for i := 0; i < 1000; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"label":"set-%d","type":"incr","annotation":{"acta-complete":"1","acta-release":"v1","acta-recovery":"%s"}}`, i, payload)
		}
		fmt.Fprint(w, `]}]`)
		w.Close()
	}()
	defer r.Close()
	repo, err := parseInfoReader(r, "acta", "set-500")
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.Points) != 1000 || repo.PointCount != 1000 {
		t.Fatal("lost catalog records")
	}
	for _, p := range repo.Points {
		if !p.Complete {
			t.Fatal("lost completeness marker")
		}
		if p.Label == "set-500" {
			if p.SealedRecovery == "" {
				t.Fatal("lost selected recovery record")
			}
		} else if p.SealedRecovery != "" {
			t.Fatal("retained unrelated sealed record")
		}
	}
}

func TestCompactCatalogPreservesSchedulingEvidence(t *testing.T) {
	now := time.Now()
	r := Repository{PointCount: 1000}
	for i := 0; i < 1000; i++ {
		r.Points = append(r.Points, Point{Label: fmt.Sprint(i), Type: "incr", Complete: true, IntegrityAt: &now})
	}
	r.Points[900].Type = "full"
	r.Points[950].RestoredAt = &now
	c := compactRepository(r)
	if len(c.Points) != 502 || c.PointCount != 1000 || c.Points[500].Label != "900" || c.Points[501].Label != "950" {
		t.Fatal("lost full/restore evidence outside recent catalog")
	}
}
