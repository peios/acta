package harnesspipe

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"acta2/internal/localstate"
	"github.com/google/uuid"
)

func TestRecoveryOfAbsentProviderAllowsExplicitNewRun(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if !processAbsent(pid) {
		t.Fatal("reaped process still exists")
	}
	spec := testSpec(t)
	dir := privateDir(t)
	path := filepath.Join(dir, spec.RunID+".json")
	saved := persisted{Process: Process{Spec: spec, State: "running", PID: pid}, Writes: map[string]string{"pending": ""}}
	if err := localstate.Write(path, saved); err != nil {
		t.Fatal(err)
	}
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	old, err := e.Spawn(t.Context(), spec)
	if err != nil || old.State != "exited" || old.Error == "" {
		t.Fatal("old run was relaunched or its unknown outcome lost", old, err)
	}
	var recovered persisted
	if err := localstate.Read(path, &recovered); err != nil {
		t.Fatal(err)
	}
	if value, ok := recovered.Writes["pending"]; !ok || value != "" || recovered.Process.State != "exited" {
		t.Fatal("recovery did not persist exit or changed uncertain input", recovered)
	}
	if err := e.Write(t.Context(), WriteRequest{RunID: spec.RunID, ID: "pending", Data: "{}\n"}); err == nil {
		t.Fatal("replayed pending input to dead run")
	}
	spec.RunID = uuid.NewString()
	if next, err := e.Spawn(t.Context(), spec); err != nil || next.State != "running" {
		t.Fatal("explicit new run blocked", next, err)
	}
}

func TestRecoveryKeepsLiveAndMissingPIDsUncertain(t *testing.T) {
	for _, pid := range []int{os.Getpid(), 0, -1} {
		spec := testSpec(t)
		dir := privateDir(t)
		if err := localstate.Write(filepath.Join(dir, spec.RunID+".json"), persisted{Process: Process{Spec: spec, State: "running", PID: pid}}); err != nil {
			t.Fatal(err)
		}
		e, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer e.Close()
		old, err := e.Spawn(t.Context(), spec)
		if err != nil || old.State != "uncertain" {
			t.Fatal("unconfirmed PID treated as exited", pid, old, err)
		}
		if err := e.Kill(t.Context(), spec.RunID); err == nil {
			t.Fatal("allowed signalling unowned PID", pid)
		}
		spec.RunID = uuid.NewString()
		if _, err := e.Spawn(t.Context(), spec); err == nil {
			t.Fatal("allowed duplicate provider", pid)
		}
	}
}
