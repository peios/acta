package harnesspipe

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"acta/internal/localstate"
	"github.com/google/uuid"
)

func TestPipeProviderHelper(t *testing.T) {
	if os.Getenv("ACTA_PIPE_HELPER") != "1" {
		return
	}
	fmt.Println(`{"ready":true}`)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), MaxFrame)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	os.Exit(0)
}
func testSpec(t *testing.T) Spec {
	t.Helper()
	t.Setenv("ACTA_PIPE_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{ThreadID: uuid.NewString(), RunID: uuid.NewString(), Executable: path, CWD: t.TempDir(), Args: []string{"-test.run=^TestPipeProviderHelper$"}}
}
func awaitFrames(t *testing.T, api API, id string, count int) []RawFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		frames, err := api.Read(ctx, ReadRequest{ThreadID: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(frames) >= count {
			return frames
		}
		select {
		case <-ctx.Done():
			t.Fatal("missing frames")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func TestSpawnAndInputRedeliveryCannotDuplicate(t *testing.T) {
	spec := testSpec(t)
	e, err := Open(privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var wg sync.WaitGroup
	pids := make(chan int, 8)
	for range 8 {
		wg.Go(func() {
			p, err := e.Spawn(t.Context(), spec)
			if err != nil {
				t.Error(err)
				return
			}
			pids <- p.PID
		})
	}
	wg.Wait()
	close(pids)
	var pid int
	for p := range pids {
		if pid != 0 && p != pid {
			t.Fatal("duplicate process")
		}
		pid = p
	}
	request := WriteRequest{RunID: spec.RunID, ID: "once", Data: "{\"hello\":true}\n"}
	for range 3 {
		if err = e.Write(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}
	frames := awaitFrames(t, e, spec.ThreadID, 2)
	if len(frames) != 2 {
		t.Fatal("duplicate input", len(frames))
	}
	other := spec
	other.RunID = uuid.NewString()
	if _, err = e.Spawn(t.Context(), other); err == nil {
		t.Fatal("parallel process for same thread")
	}
	if err = e.Kill(t.Context(), spec.RunID); err != nil {
		t.Fatal(err)
	}
	p, err := e.Spawn(t.Context(), spec)
	if err != nil || p.State != "exited" {
		t.Fatal("redelivery respawned terminated process", p, err)
	}
}
func TestJournalReplayAndTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames")
	j, err := openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Append("run", "stdout", []byte(`{"value":9007199254740993}`)); err != nil {
		t.Fatal(err)
	}
	expected, _ := j.Read(0)
	j.Close()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"sequence":2`)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	j, err = openJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	actual, err := j.Read(0)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	if string(a) != string(b) {
		t.Fatal("replay changed capture")
	}
	if err = j.Append("next", "stdout", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	frames, _ := j.Read(1)
	if len(frames) != 1 || frames[0].Sequence != 2 {
		t.Fatal(frames)
	}
}
func TestUncertainSpawnIsNotRetried(t *testing.T) {
	spec := testSpec(t)
	dir := privateDir(t)
	if err := localstate.Write(filepath.Join(dir, spec.RunID+".json"), persisted{Process: Process{Spec: spec, State: "starting"}, Writes: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	e, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p, err := e.Spawn(t.Context(), spec)
	if err != nil || p.State != "uncertain" || p.PID != 0 {
		t.Fatal(p, err)
	}
	if err = e.Kill(t.Context(), spec.RunID); err == nil {
		t.Fatal("signalled an unowned process")
	}
	if _, err = Open(dir); err == nil {
		t.Fatal("second pipe acquired ownership")
	}
}
func TestPersistedInputIntentIsNotReplayed(t *testing.T) {
	spec := testSpec(t)
	e, err := Open(privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.Spawn(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	c := e.children[spec.RunID]
	c.data.Writes["lost"] = ""
	err = e.save(c)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	err = e.Write(t.Context(), WriteRequest{RunID: spec.RunID, ID: "lost", Data: "{}\n"})
	if err == nil {
		t.Fatal("uncertain input resent")
	}
}
func TestCaptureFailureKillsOnlyItsProvider(t *testing.T) {
	spec := testSpec(t)
	e, err := Open(privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.Spawn(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	other := spec
	other.ThreadID = uuid.NewString()
	other.RunID = uuid.NewString()
	if _, err = e.Spawn(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	awaitFrames(t, e, spec.ThreadID, 1)
	_ = e.Write(t.Context(), WriteRequest{RunID: spec.RunID, ID: "invalid", Data: "not JSON\n"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		ps, _ := e.Processes(t.Context())
		stopped := false
		for _, p := range ps {
			if p.Spec.RunID == other.RunID && p.State != "running" {
				t.Fatal("unrelated provider affected")
			}
			if p.Spec.RunID == spec.RunID && p.State == "exited" {
				stopped = true
			}
		}
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("capture failure did not terminate process")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestCompleteCorruptJournalFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frames")
	if err := os.WriteFile(path, []byte("invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if j, err := openJournal(path); err == nil {
		j.Close()
		t.Fatal("corruption ignored")
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestJournalPreservesOriginalWhitespaceAndDiagnosticText(t *testing.T) {
	j, err := openJournal(filepath.Join(t.TempDir(), "frames"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	raw := []byte(`{ "huge": 9007199254740993, "text": "a < b" }`)
	if err = j.Append("run", "stdout", raw); err != nil {
		t.Fatal(err)
	}
	diagnostic := `{"level":"WARN","message":"exact"}`
	encoded, _ := json.Marshal(diagnostic)
	if err = j.Append("run", "stderr", encoded); err != nil {
		t.Fatal(err)
	}
	frames, err := j.Read(0)
	if err != nil {
		t.Fatal(err)
	}
	if frames[0].Text() != string(raw) || frames[1].Text() != diagnostic {
		t.Fatal("changed captured text", frames)
	}
}

func TestImageSizedInputOverDetachedTransportIsIdempotent(t *testing.T) {
	spec := testSpec(t)
	e, err := Open(privateDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.Spawn(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	handler := Handler(e)
	data := `{"image":"` + strings.Repeat("a", 6*1024*1024) + "\"}\n"
	payload, _ := json.Marshal(WriteRequest{RunID: spec.RunID, ID: "image-once", Data: data})
	for range 2 {
		request := httptest.NewRequest("POST", "/write", strings.NewReader(string(payload)))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("X-Acta-Pipe-Max-Input") != "8388608" {
			t.Fatal("missing input capability")
		}
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
	}

	frames := awaitFrames(t, e, spec.ThreadID, 2)
	if len(frames) != 2 || len(frames[1].Data) != len(data)-1 {
		t.Fatal("image input truncated or duplicated", len(frames))
	}
}
