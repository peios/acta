package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"acta/internal/localstate"
)

func testRelease(seq int64) Release {
	images := map[string]string{}
	for _, k := range []string{"app", "db", "backup", "updater"} {
		images[k] = "ghcr.io/peios/acta-" + k + "@sha256:" + strings.Repeat("a", 64)
	}
	images["app"] = "ghcr.io/peios/acta-server@sha256:" + strings.Repeat("a", 64)
	images["caddy"] = "caddy@sha256:" + strings.Repeat("b", 64)
	return Release{Version: "v0.1.0-test", Sequence: seq, Repository: "peios/acta", Protocol: Protocol, Layout: Layout, Postgres: 17, Schema: 40, MinimumSchema: 40, Images: images}
}

type fakeEngine struct {
	calls    []string
	fail     string
	cancel   func()
	crash    string
	restored bool
}

func (f *fakeEngine) step(name string) error {
	f.calls = append(f.calls, name)
	if name == f.crash {
		f.cancel()
		return context.Canceled
	}
	if name == f.fail {
		return errors.New(name + " failed")
	}
	return nil
}
func (f *fakeEngine) Prepare(context.Context, Job) error  { return f.step("prepare") }
func (f *fakeEngine) Quiesce(context.Context, Job) error  { return f.step("quiesce") }
func (f *fakeEngine) Snapshot(context.Context, Job) error { return f.step("snapshot") }
func (f *fakeEngine) Apply(context.Context, Job) error    { return f.step("apply") }
func (f *fakeEngine) Validate(context.Context, Job) error { return f.step("validate") }
func (f *fakeEngine) Restore(_ context.Context, j Job) error {
	f.restored = j.RestoreData
	return f.step("restore")
}
func (f *fakeEngine) Publish(_ context.Context, _ Job, rollback bool) error {
	if rollback {
		return f.step("publish-old")
	}
	return f.step("publish-new")
}
func openTest(t *testing.T, f Engine) *Service {
	t.Helper()
	dir := t.TempDir()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := filepath.Join(dir, "key")
	os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString(pub)), 0600)
	c := Config{StateDir: dir, PublicKeyFile: key, Repository: "peios/acta", TimeoutMinutes: 5}
	current, _ := Sign(testRelease(1), priv.Seed())
	next, _ := Sign(testRelease(2), priv.Seed())
	if err := localstate.Write(filepath.Join(dir, "state.json"), State{Version: 1, Current: current, Available: &next, Jobs: []Job{}}); err != nil {
		t.Fatal(err)
	}
	s, err := Open(c, f)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func installTest(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.Install(s.View().Available.ID(), "actor"); err != nil {
		t.Fatal(err)
	}
}
func TestUpdateFailureBoundaries(t *testing.T) {
	for _, failure := range []string{"", "prepare", "quiesce", "snapshot", "apply", "validate", "publish-new", "restore", "publish-old"} {
		t.Run(failure, func(t *testing.T) {
			f := &fakeEngine{fail: failure}
			s := openTest(t, f)
			installTest(t, s)
			if failure == "restore" || failure == "publish-old" {
				s.transition("restoring", nil)
			}
			_ = s.advance(context.Background())
			j := s.View().Jobs[0]
			want := "rolled_back"
			switch failure {
			case "":
				want = "succeeded"
			case "prepare":
				want = "failed"
			case "publish-new":
				want = "committing"
			case "restore":
				want = "restoring"
			case "publish-old":
				want = "rollback_committing"
			}
			if j.Paused != (want == "committing" || want == "restoring" || want == "rollback_committing") {
				t.Fatalf("unexpected paused state: %+v", j)
			}
			if j.Phase != want {
				t.Fatalf("phase %s, want %s; calls %v", j.Phase, want, f.calls)
			}
			if failure == "apply" || failure == "validate" {
				if !f.restored {
					t.Fatal("candidate may have written; missing database restore")
				}
			}
			if failure == "quiesce" || failure == "snapshot" {
				if f.restored {
					t.Fatal("restoring incomplete snapshot")
				}
			}
			if failure == "publish-new" && f.restored {
				t.Fatal("rolled back after accepting user writes")
			}
		})
	}
}
func TestCrashAtEveryBoundary(t *testing.T) {
	for _, crash := range []string{"prepare", "quiesce", "snapshot", "apply", "validate", "publish-new", "restore", "publish-old"} {
		t.Run(crash, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &fakeEngine{crash: crash, cancel: cancel}
			s := openTest(t, f)
			installTest(t, s)
			if crash == "restore" || crash == "publish-old" {
				s.transition("restoring", nil)
			}
			if err := s.advance(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected interruption: %v", err)
			}
			c := s.c
			s.Close()
			resumed := &fakeEngine{}
			s2, err := Open(c, resumed)
			if err != nil {
				t.Fatal(err)
			}
			defer s2.Close()
			if err = s2.advance(context.Background()); err != nil {
				t.Fatal(err)
			}
			j := s2.View().Jobs[0]
			want := "succeeded"
			if crash == "apply" || crash == "validate" || crash == "restore" || crash == "publish-old" {
				want = "rolled_back"
			}
			if j.Phase != want {
				t.Fatalf("got %s, want %s", j.Phase, want)
			}
			if crash == "publish-new" && !reflect.DeepEqual(resumed.calls, []string{"publish-new"}) {
				t.Fatalf("unsafe replay after cutover: %v", resumed.calls)
			}
			if crash == "publish-old" && !reflect.DeepEqual(resumed.calls, []string{"publish-old"}) {
				t.Fatalf("unsafe replay after rollback cutover: %v", resumed.calls)
			}
		})
	}
}
func TestInstallOwnershipAndStaleSelection(t *testing.T) {
	s := openTest(t, &fakeEngine{})
	selection := s.View().Available.ID()
	if _, e := Open(s.c, &fakeEngine{}); !errors.Is(e, ErrBusy) {
		t.Fatalf("second service acquired journal: %v", e)
	}
	if _, e := s.Install("stale", "actor"); e == nil {
		t.Fatal("stale selection accepted")
	}
	installTest(t, s)
	if _, e := s.Install(selection, "actor"); !errors.Is(e, ErrBusy) {
		t.Fatal("concurrent update accepted")
	}
	if e := s.advance(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Install(selection, "actor"); e == nil {
		t.Fatal("replayed installed release")
	}
}
func TestSignatureAndCompatibility(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	r := testRelease(1)
	s, err := Sign(r, priv.Seed())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(s, pub, "peios/acta"); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(s, pub, "peios/acta-legacy"); err == nil {
		t.Fatal("wrong repository accepted")
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = Verify(s, other, "peios/acta"); err == nil {
		t.Fatal("wrong key accepted")
	}
	raw, _ := base64.StdEncoding.DecodeString(s.Payload)
	raw[10] ^= 1
	s.Payload = base64.StdEncoding.EncodeToString(raw)
	if _, err = Verify(s, pub, "peios/acta"); err == nil {
		t.Fatal("tampered release accepted")
	}
	next := testRelease(2)
	next.MinimumSchema = 41
	next.Schema = 41
	if Compatible(r, next) == nil {
		t.Fatal("unsupported schema jump accepted")
	}
	r.Images["app"] = "ghcr.io/peios/acta-unrelated@sha256:" + strings.Repeat("a", 64)
	if r.Validate() == nil {
		t.Fatal("unrelated image package accepted")
	}
}

func TestSuccessfulUpdateClearsOfferedRelease(t *testing.T) {
	s := openTest(t, &fakeEngine{})
	installTest(t, s)
	if err := s.advance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.View().Available != nil {
		t.Fatal("installed release is still offered")
	}
	var saved State
	if err := localstate.Read(filepath.Join(s.c.StateDir, "state.json"), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Available != nil {
		t.Fatal("installed release persisted as available")
	}
}

func TestDevelopmentLayoutRequiresFreshInstallation(t *testing.T) {
	r := testRelease(1)
	r.Layout = 1
	if err := r.Validate(); err == nil {
		t.Fatal("development layout accepted under the renamed database and executable paths")
	}
}
