package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRunner struct {
	missing error
	results []Result
	calls   [][]string
}

func (r *fakeRunner) LookPath(name string) (string, error) { return name, r.missing }
func (r *fakeRunner) Run(_ context.Context, _ string, args ...string) Result {
	r.calls = append(r.calls, args)
	if len(r.results) == 0 {
		return Result{Err: errors.New("unexpected command")}
	}
	value := r.results[0]
	r.results = r.results[1:]
	return value
}
func TestAdapterDiscovery(t *testing.T) {
	for _, tc := range []struct {
		id, name, output string
		code             int
		auth             string
		err              error
	}{
		{"codex", "signed in", "Logged in using ChatGPT\n", 0, "signed_in", nil},
		{"codex", "signed out", "Not logged in\n", 1, "signed_out", nil},
		{"codex", "unexpected", "new output format", 0, "unknown", nil},
		{"codex", "wrong exit", "Logged in using ChatGPT", 1, "unknown", nil},
		{"claude", "signed in", `{"loggedIn":true,"email":"private@example.org","orgId":"secret","projectsDirectory":"/private"}`, 0, "signed_in", nil},
		{"claude", "signed out", `{"loggedIn":false}`, 1, "signed_out", nil},
		{"claude", "missing bool", `{"email":"private@example.org"}`, 0, "unknown", nil},
		{"claude", "malformed", `{"loggedIn":`, 0, "unknown", nil},
		{"claude", "wrong exit", `{"loggedIn":false}`, 0, "unknown", nil},
		{"claude", "truncated output", `{"loggedIn":false}`, 1, "unknown", errOutputLimit},
	} {
		t.Run(tc.id+"/"+tc.name, func(t *testing.T) {
			version, usage := "codex-cli 0.153.4", "Usage: codex login status [OPTIONS]"
			if tc.id == "claude" {
				version = "2.1.263 (Claude Code)"
				usage = "Usage: claude auth status [options]"
			}
			r := &fakeRunner{results: []Result{{Stdout: version}, {Stdout: usage}, {Stdout: tc.output, Code: tc.code, Err: tc.err}}}
			s := (cliAdapter{tc.id, r}).Discover(t.Context())
			if s.Installation != "installed" || s.Authentication != tc.auth || s.Version == "" {
				t.Fatal(s)
			}
			encoded, _ := json.Marshal(s)
			for _, secret := range []string{"private", "secret", "email", "orgId", "projectsDirectory"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("private data in advertisement")
				}
			}
			if tc.auth == "unknown" && s.Issue != "auth_unavailable" {
				t.Fatal(s)
			}
		})
	}
}
func TestMissingUnsupportedAndTimedOutProviders(t *testing.T) {
	for _, tc := range []struct {
		name                string
		runner              fakeRunner
		installation, issue string
		calls               int
	}{
		{"missing", fakeRunner{missing: exec.ErrNotFound}, "not_installed", "", 0},
		{"lookup failed", fakeRunner{missing: os.ErrPermission}, "unknown", "probe_failed", 0},
		{"version failed", fakeRunner{results: []Result{{Err: errors.New("failure")}}}, "installed", "probe_failed", 1},
		{"unknown version", fakeRunner{results: []Result{{Stdout: "private output"}}}, "installed", "version_unknown", 1},
		{"unsupported auth", fakeRunner{results: []Result{{Stdout: "2.1.263 (Claude Code)"}, {Stdout: "Usage: claude [prompt]"}}}, "installed", "auth_unavailable", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := (cliAdapter{"claude", &tc.runner}).Discover(t.Context())
			if s.Installation != tc.installation || s.Issue != tc.issue || len(tc.runner.calls) != tc.calls {
				t.Fatal(s, tc.runner.calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := &fakeRunner{results: []Result{{Err: ctx.Err()}}}
	if s := (cliAdapter{"codex", r}).Discover(ctx); s.Issue != "timeout" {
		t.Fatal(s)
	}
}

func TestProbeHelper(t *testing.T) {
	if os.Getenv("ACTA_PROVIDER_TEST_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "output":
		fmt.Print(strings.Repeat("x", outputLimit*4))
	case "wait":
		time.Sleep(10 * time.Second)
	case "token":
		fmt.Print(os.Getenv("ACTA_TOKEN"))
	case "exit":
		os.Exit(1)
	}
	os.Exit(0)
}
func TestLocalRunnerBoundsAndEnvironment(t *testing.T) {
	t.Setenv("ACTA_PROVIDER_TEST_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	t.Setenv("ACTA_TOKEN", "must-not-reach-provider")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string) Result {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		return (localRunner{}).Run(ctx, path, "-test.run=^TestProbeHelper$", mode)
	}
	if r := run("token"); r.Err != nil || r.Stdout != "" {
		t.Fatal("Acta credential reached provider or probe failed")
	}
	if r := run("output"); !errors.Is(r.Err, errOutputLimit) || len(r.Stdout) > outputLimit {
		t.Fatal("unbounded output", r.Err, len(r.Stdout))
	}
	start := time.Now()
	r := run("wait")
	if !errors.Is(r.Err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal("probe did not cancel promptly", r.Err)
	}
	if r := run("exit"); r.Code != 1 || r.Err != nil {
		t.Fatal(r)
	}
}

type testAdapter struct {
	id    string
	probe func(context.Context) Status
}

func (a testAdapter) ID() string                          { return a.id }
func (a testAdapter) Discover(ctx context.Context) Status { return a.probe(ctx) }
func TestDiscoveryIndependenceRefreshAndCancellation(t *testing.T) {
	d := NewDiscovery()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	slow := testAdapter{"codex", func(ctx context.Context) Status {
		<-ctx.Done()
		return Status{ID: "codex", Installation: "installed", Authentication: "unknown", Issue: "timeout"}
	}}
	fast := testAdapter{"claude", func(context.Context) Status {
		n := calls.Add(1)
		s := Status{ID: "claude", Installation: "not_installed", Authentication: "unknown"}
		if n > 1 {
			s.Installation = "installed"
			s.Authentication = "signed_in"
			s.Version = "2.1.263"
		}
		return s
	}}
	changes, unsubscribe := d.Subscribe()
	defer unsubscribe()
	done := make(chan struct{})
	go func() { defer close(done); d.run(ctx, []Adapter{slow, fast}, 5*time.Millisecond, 500*time.Millisecond) }()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-changes:
			if d.Snapshot()[1].Authentication == "signed_in" {
				goto updated
			}
		case <-timeout:
			t.Fatal("fast provider blocked")
		}
	}
updated:
	if d.Snapshot()[0].Installation != "checking" {
		t.Fatal("fast updates waited for slow probe")
	}
	s := d.Snapshot()
	s[1].Version = "mutated"
	if d.Snapshot()[1].Version == "mutated" {
		t.Fatal("snapshot aliases discovery")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("discovery did not stop")
	}
}

func TestPublicSnapshotValidation(t *testing.T) {
	if !ValidSnapshot(Initial()) {
		t.Fatal("initial snapshot invalid")
	}
	for _, mutate := range []func([]Status){
		func(s []Status) { s[0].ID = "other" }, func(s []Status) { s[0] = s[1] },
		func(s []Status) { s[0].Version = "/private/path" }, func(s []Status) { s[0].Issue = "secret error" },
		func(s []Status) { s[0].Authentication = "signed_in" },
	} {
		s := Initial()
		mutate(s)
		if ValidSnapshot(s) {
			t.Fatal("accepted invalid snapshot", s)
		}
	}
}
