package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"acta/internal/accounts"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/hyperharness"
	"acta/internal/threads"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

func TestThreadDiscoveryCreatesRecordAndFramesCommitBeforeAck(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			f := securityDatabase(t)
			_, other := permissionMember(t, f, "other")
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			var handler http.Handler
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
			defer srv.Close()
			cfg, err := config.Parse(srv.URL, "")
			must(t, err)
			handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
			token := agentCLI(t, f, "")
			conn, _, err := websocket.Dial(ctx, srv.URL+"/api/harnesses/connect", &websocket.DialOptions{Subprotocols: []string{hyperharness.Protocol}, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
			must(t, err)
			defer conn.CloseNow()
			must(t, wsjson.Write(ctx, conn, hyperharness.Hello{Hostname: "thread-host"}))
			var ack struct{ Connection hyperharness.Connection }
			must(t, wsjson.Read(ctx, conn, &ack))
			id := uuid.NewString()
			body, _ := json.Marshal(map[string]string{"id": id, "connection_id": ack.Connection.ID, "provider": provider, "cwd": "/tmp"})
			request, err := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/threads/create", bytes.NewReader(body))
			must(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", srv.URL)
			request.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
			response, err := http.DefaultClient.Do(request)
			must(t, err)
			response.Body.Close()
			if response.StatusCode != 202 {
				t.Fatal(response.StatusCode)
			}

			account, err := f.service.Current(ctx, f.token)
			must(t, err)
			list, err := f.store.ListThreads(ctx, account.ID)
			must(t, err)
			if len(list) != 0 {
				t.Fatal("Create wrote a server thread record")
			}
			var control struct {
				Type    string
				Control threads.Control
			}
			control.Control = threads.Control{}
			must(t, wsjson.Read(ctx, conn, &control))
			if control.Type != "control" || control.Control.ThreadID != id {
				t.Fatal(control)
			}
			desc := threads.Descriptor{ID: id, RunID: id, Provider: provider, ProviderID: "native-session", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
			must(t, wsjson.Write(ctx, conn, hyperharness.ProviderUpdate{Type: "inventory", Threads: []threads.Descriptor{desc}}))
			frame, err := threads.Unknown(threads.ProviderFrame{ThreadID: id, RunID: id, Sequence: 1, Provider: provider, ReceivedAt: time.Now().UTC(), Raw: json.RawMessage(`{"method":"thread/started","unknown":{"number":9007199254740993}}`)})
			must(t, err)
			for range 2 {
				must(t, wsjson.Write(ctx, conn, hyperharness.ProviderUpdate{Type: "frames", ThreadID: id, Frames: []threads.Frame{frame}}))
				var received struct {
					Type     string
					Sequence int64
				}
				must(t, wsjson.Read(ctx, conn, &received))
				if received.Type != "frame_ack" || received.Sequence != 1 {
					t.Fatal(received)
				}
			}
			saved, err := f.store.ThreadFrames(ctx, account.ID, id, 0)
			must(t, err)
			if len(saved) != 1 || string(saved[0].Data) != string(frame.Data) {
				t.Fatal("frames duplicated or payload changed", saved)
			}

			browserRequest, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/threads/"+id+"/frames", nil)
			must(t, err)
			browserRequest.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
			browserResponse, err := http.DefaultClient.Do(browserRequest)
			must(t, err)
			var browserFrames struct {
				Frames []struct {
					RawJSON string `json:"raw_json"`
				}
			}
			must(t, json.NewDecoder(browserResponse.Body).Decode(&browserFrames))
			browserResponse.Body.Close()
			if len(browserFrames.Frames) != 1 || !strings.Contains(browserFrames.Frames[0].RawJSON, "9007199254740993") {
				t.Fatal("browser JSON lost numeric precision", browserFrames)
			}
			foreign, err := other.service.Current(ctx, other.token)
			must(t, err)
			if _, err = f.store.Thread(ctx, foreign.ID, id); err == nil {
				t.Fatal("cross-owner thread disclosed")
			}
			bad := frame
			bad.Sequence = 3
			if _, err = f.store.AppendThreadFrames(ctx, account.ID, id, []threads.Frame{bad}); err == nil {
				t.Fatal("sequence gap accepted")
			}
			bad = frame
			bad.Data = json.RawMessage(`{"raw":"changed","stream":"stdout","reason":"Changed replay"}`)
			if _, err = f.store.AppendThreadFrames(ctx, account.ID, id, []threads.Frame{bad}); err == nil {
				t.Fatal("conflicting replay accepted")
			}
			last, err := f.store.AppendThreadFrames(ctx, account.ID, id, []threads.Frame{frame})
			must(t, err)
			if last != 1 {
				t.Fatal("failed batch moved cursor")
			}

			// Interrupt uses the existing authenticated, durable control path.
			stopID := uuid.NewString()
			for _, tc := range []struct {
				run, token string
				status     int
			}{{"", f.token, 400}, {id, other.token, 409}, {id, f.token, 202}} {
				body, _ := json.Marshal(map[string]string{"id": stopID, "action": "interrupt", "run_id": tc.run})
				req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/threads/"+id+"/control", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", srv.URL)
				req.AddCookie(&http.Cookie{Name: "acta_session", Value: tc.token})
				res, e := http.DefaultClient.Do(req)
				must(t, e)
				res.Body.Close()
				if res.StatusCode != tc.status {
					t.Fatalf("interrupt status=%d want=%d", res.StatusCode, tc.status)
				}
			}
			control.Control = threads.Control{}
			must(t, wsjson.Read(ctx, conn, &control))
			if control.Control.Action != "interrupt" || control.Control.RunID != id {
				t.Fatal(control)
			}
			savedControl, e := f.store.ThreadControl(ctx, account.ID, id, stopID)
			must(t, e)
			if savedControl.Action != "interrupt" {
				t.Fatal(savedControl)
			}

			questionID := uuid.NewString()
			for _, tc := range []struct {
				answers map[string][]string
				token   string
				status  int
			}{
				{nil, f.token, 400}, {map[string][]string{"q1": {"Blue"}}, other.token, 409}, {map[string][]string{"q1": {"Blue"}}, f.token, 202},
			} {
				body, _ := json.Marshal(map[string]any{"id": questionID, "action": "answer", "run_id": id, "question_id": questionID, "answers": tc.answers})
				req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/threads/"+id+"/control", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", srv.URL)
				req.AddCookie(&http.Cookie{Name: "acta_session", Value: tc.token})
				res, e := http.DefaultClient.Do(req)
				must(t, e)
				res.Body.Close()
				if res.StatusCode != tc.status {
					t.Fatalf("answer status=%d want=%d", res.StatusCode, tc.status)
				}
			}
			control.Control = threads.Control{}
			must(t, wsjson.Read(ctx, conn, &control))
			if control.Control.Action != "answer" || control.Control.QuestionID != questionID || control.Control.Answers["q1"][0] != "Blue" {
				t.Fatal(control)
			}
			must(t, f.store.RequestThreadControl(ctx, account.ID, control.Control))
			changed := control.Control
			changed.Answers = map[string][]string{"q1": {"Green"}}
			if f.store.RequestThreadControl(ctx, account.ID, changed) == nil {
				t.Fatal("answer command mutated")
			}
			must(t, f.store.CompleteThreadControl(ctx, account.ID, threads.Result{ID: questionID, ThreadID: id, Outcome: "accepted", Decision: "answer", Answers: control.Control.Answers}))
			answerStatus, e := f.store.ThreadControl(ctx, account.ID, id, questionID)
			must(t, e)
			if answerStatus.Result == nil || answerStatus.Result.Answers["q1"][0] != "Blue" {
				t.Fatal("answer lost", answerStatus)
			}
		})
	}
}

func TestThreadControlDurableOutcomeAndOwnership(t *testing.T) {
	f := securityDatabase(t)
	_, other := permissionMember(t, f, "other")
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	foreign, err := other.service.Current(ctx, other.token)
	must(t, err)
	id := uuid.NewString()
	d := threads.Descriptor{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{d}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "kill"}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	pending, err := f.store.PendingThreadControls(ctx, owner.ID, id)
	must(t, err)
	if len(pending) != 1 || pending[0].ID != q.ID {
		t.Fatal(pending)
	}
	bad := q
	bad.Action = "resume"
	if f.store.RequestThreadControl(ctx, owner.ID, bad) == nil {
		t.Fatal("conflicting command accepted")
	}
	if f.store.RequestThreadControl(ctx, foreign.ID, q) == nil {
		t.Fatal("foreign command accepted")
	}
	if _, err = f.store.ThreadControl(ctx, foreign.ID, id, q.ID); err == nil {
		t.Fatal("foreign outcome disclosed")
	}
	must(t, f.store.CompleteThreadControl(ctx, foreign.ID, threads.Result{ID: q.ID, ThreadID: id}))
	status, err := f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if status.Result != nil {
		t.Fatal("foreign owner completed command")
	}
	result := threads.Result{ID: q.ID, ThreadID: id, Error: "provider unavailable"}
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, result))
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, threads.Result{ID: q.ID, ThreadID: id}))
	status, err = f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if status.Result == nil || !reflect.DeepEqual(*status.Result, result) {
		t.Fatal("outcome changed", status)
	}
	pending, err = f.store.PendingThreadControls(ctx, owner.ID, id)
	must(t, err)
	if len(pending) != 0 {
		t.Fatal("completed command remained pending")
	}
}

func TestFrameBundlesAreAtomicAndPaginationNeverSplitsCapture(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	desc := threads.Descriptor{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	bundle := func(seq int64) []threads.Frame {
		p := threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: seq, ReceivedAt: desc.CreatedAt}
		d := threads.NewFrame(p, "debug/resolved", map[string]any{"stream": "stdout", "raw": "{}", "reason": "Mapped status", "outputs": []threads.OutputReference{{OutputIndex: 1, Kind: "thread/status"}}})
		v := threads.NewFrame(p, "thread/status", map[string]any{"status": "idle", "waiting_for": []string{}})
		v.OutputIndex = 1
		return []threads.Frame{d, v}
	}
	if _, err = f.store.AppendThreadFrames(ctx, owner.ID, id, bundle(1)[:1]); err == nil {
		t.Fatal("accepted incomplete bundle")
	}
	rows, err := f.store.ThreadFrames(ctx, owner.ID, id, 0)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("partial bundle persisted")
	}
	all := []threads.Frame{}
	for i := int64(1); i <= 65; i++ {
		all = append(all, bundle(i)...)
	}
	last, err := f.store.AppendThreadFrames(ctx, owner.ID, id, all)
	must(t, err)
	if last != 65 {
		t.Fatal(last)
	}
	rows, err = f.store.ThreadFrames(ctx, owner.ID, id, 0)
	must(t, err)
	if len(rows) != 128 || rows[127].Sequence != 64 || rows[127].OutputIndex != 1 {
		t.Fatal("page split capture", len(rows))
	}
	rows, err = f.store.ThreadFrames(ctx, owner.ID, id, 64)
	must(t, err)
	if len(rows) != 2 {
		t.Fatal("lost page tail")
	}
	last, err = f.store.AppendThreadFrames(ctx, owner.ID, id, bundle(65))
	must(t, err)
	if last != 65 {
		t.Fatal(last)
	}
	conflicting := bundle(65)
	conflicting[1].Data = json.RawMessage(`{"status":"active","waiting_for":[]}`)
	if _, err = f.store.AppendThreadFrames(ctx, owner.ID, id, conflicting); err == nil {
		t.Fatal("accepted changed replay")
	}
	batch := append(bundle(66), bundle(68)...)
	if _, err = f.store.AppendThreadFrames(ctx, owner.ID, id, batch); err == nil {
		t.Fatal("accepted gap")
	}
	rows, err = f.store.ThreadFrames(ctx, owner.ID, id, 65)
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("failed transaction advanced stream")
	}
	desc.Committed = true
	desc.Revision++
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	desc.Committed = false
	desc.Revision++
	if err = f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}); err == nil {
		t.Fatal("commitment regressed")
	}
}

func TestSendCommandPayloadAndLateAcceptance(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "send", Text: "hello"}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	pending, err := f.store.PendingThreadControls(ctx, owner.ID, id)
	must(t, err)
	if len(pending) != 1 || !reflect.DeepEqual(pending[0], q) {
		t.Fatal("lost command payload", pending)
	}
	changed := q
	changed.Text = "different"
	if f.store.RequestThreadControl(ctx, owner.ID, changed) == nil {
		t.Fatal("changed text accepted")
	}
	changed = q
	changed.RunID = uuid.NewString()
	if f.store.RequestThreadControl(ctx, owner.ID, changed) == nil {
		t.Fatal("changed run accepted")
	}
	uncertain := threads.Result{ID: q.ID, ThreadID: id, Outcome: "uncertain", Error: "lost reply"}
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, uncertain))
	accepted := threads.Result{ID: q.ID, ThreadID: id, Outcome: "accepted"}
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, accepted))
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, uncertain))
	status, err := f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if status.Result == nil || !reflect.DeepEqual(*status.Result, accepted) {
		t.Fatal("late acknowledgement lost", status)
	}
}

func TestSettingsCommandPayload(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "configure", Settings: threads.ModelSettings{Model: "test-model", Effort: "high", FastMode: true}}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	pending, err := f.store.PendingThreadControls(ctx, owner.ID, id)
	must(t, err)
	if len(pending) != 1 || !reflect.DeepEqual(pending[0], q) {
		t.Fatal("settings lost", pending)
	}
	changed := q
	changed.Settings.FastMode = false
	if f.store.RequestThreadControl(ctx, owner.ID, changed) == nil {
		t.Fatal("settings command was mutable")
	}
	status, err := f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if !reflect.DeepEqual(status.Control, q) {
		t.Fatal("status lost settings", status)
	}
}
