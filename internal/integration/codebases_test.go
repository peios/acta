package integration

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"acta/internal/accounts"
	actaclient "acta/internal/client"
	"acta/internal/codebases"
	"acta/internal/codehost"
	"acta/internal/codethreads"
	"acta/internal/config"
	"acta/internal/httpapi"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestCodebaseRelayLifecycleAndPrivateOwnership(t *testing.T) {
	providerBin := t.TempDir()
	t.Setenv("PATH", providerBin)
	must(t, os.WriteFile(filepath.Join(providerBin, "claude"), []byte(`#!/bin/sh
if [ "$1" = '--print' ]; then
 printf '{"type":"system","subtype":"init"}\n'
 printf '{"type":"system","subtype":"hook_started","hook_id":"hook-test","hook_event":"SessionStart","hook_name":"Load context"}\n'
 IFS= read -r input
 request_id=${input#*\"request_id\":\"}
 request_id=${request_id%%\"*}
 printf '{"type":"control_response","response":{"subtype":"success","request_id":"%s","response":{}}}\n' "$request_id"
 while IFS= read -r input; do
  printf '%s\n' "$input"
  printf '{"type":"system","subtype":"hook_response","hook_id":"hook-test","hook_event":"SessionStart","hook_name":"Load context","stdout":"hello","stderr":"","output":"hello","outcome":"success","exit_code":0}\n'
  printf '{"type":"result","subtype":"success"}\n'
 done
else
 printf '%s' '{"loggedIn":true,"email":"private@example.test"}'
fi
`), 0700))
	must(t, os.WriteFile(filepath.Join(providerBin, "codex"), []byte(`#!/bin/sh
if [ "$1" = 'app-server' ]; then
 printf '{"method":"hook/started","params":{"threadId":"native-test-thread","run":{"id":"hook-test","eventName":"sessionStart","status":"running","entries":[]}}}\n'
 IFS= read -r input
 request_id=${input#*\"id\":\"}
 request_id=${request_id%%\"*}
 printf '{"id":"%s","result":{"userAgent":"test"}}\n' "$request_id"
 IFS= read -r initialized
 IFS= read -r input
 request_id=${input#*\"id\":\"}
 request_id=${request_id%%\"*}
 printf '{"id":"%s","result":{"thread":{"id":"native-test-thread"}}}\n' "$request_id"
 while IFS= read -r input; do
  request_id=${input#*\"id\":\"}
  request_id=${request_id%%\"*}
  client_id=${input#*\"clientUserMessageId\":\"}
  client_id=${client_id%%\"*}
  printf '{"id":"%s","result":{"turn":{"id":"turn-test"}}}\n' "$request_id"
  printf '{"method":"item/started","params":{"turnId":"turn-test","item":{"type":"userMessage","id":"item-test","clientId":"%s","content":[{"type":"text","text":"hello"}]}}}\n' "$client_id"
  printf '{"method":"turn/completed","params":{"turn":{"id":"turn-test"}}}\n'
  printf '{"method":"hook/completed","params":{"threadId":"native-test-thread","run":{"id":"hook-test","eventName":"sessionStart","status":"completed","entries":[{"kind":"context","text":"hello"}]}}}\n'
 done
else
 printf '%s' 'Not logged in' >&2
 exit 1
fi
`), 0700))
	f := securityDatabase(t)
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	cfg, err := config.Parse(server.URL, "")
	must(t, err)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	owner, err := f.service.Current(t.Context(), f.token)
	must(t, err)
	secret := codeHostLogin(t, f, owner.ID)
	c := actaclient.New(server.URL, secret)
	caller := actaclient.New(server.URL, codeHostLogin(t, f, owner.ID))
	dir := t.TempDir()
	catalogue, err := codehost.OpenCatalogue(dir)
	must(t, err)
	identity := codeHostInput()
	start := func(cat *codehost.Catalogue) func() {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- codehost.Run(ctx, c, identity, cat, io.Discard) }()
		return func() {
			cancel()
			select {
			case err := <-done:
				must(t, err)
			case <-time.After(3 * time.Second):
				t.Error("host did not stop")
			}
		}
	}
	stop := start(catalogue)
	defer func() { stop() }()
	waitConnected := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			hosts, err := caller.CodeHosts(t.Context())
			must(t, err)
			connected := len(hosts) == 1 && hosts[0].Connected
			if connected == want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("host connected never became %v", want)
	}
	waitConnected(true)
	deadline := time.Now().Add(5 * time.Second)
	for {
		hosts, err := caller.CodeHosts(t.Context())
		must(t, err)
		if len(hosts) == 1 && hosts[0].ProviderStatus != nil {
			status := hosts[0].ProviderStatus
			if !status.Valid() || status.CheckedAt.IsZero() || status.Providers[0].Auth != "signed_in" || status.Providers[1].Auth != "signed_out" {
				t.Fatal("incorrect relayed provider status", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider status never arrived")
		}
		time.Sleep(20 * time.Millisecond)
	}
	base := t.TempDir()
	second := t.TempDir()
	must(t, os.Mkdir(filepath.Join(base, "nested"), 0700))
	must(t, os.WriteFile(filepath.Join(base, "nested", "example.txt"), []byte("private file contents"), 0600))
	input := codebases.Add{ID: uuid.NewString(), Name: "peios", Paths: []string{base, second}}
	var added codebases.Codebase
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codebases.AddMethod, input, &added))
	if len(added.Roots) != 2 {
		t.Fatal("multi-root codebase missing")
	}
	var listing codebases.Directory
	query := codebases.DirectoryQuery{CodebaseID: added.ID, RootID: added.Roots[0].ID, Path: "nested"}
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codebases.DirectoryMethod, query, &listing))
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "example.txt" {
		t.Fatal("directory relay failed", listing)
	}
	startThread := codethreads.Start{Provider: "claude", ID: uuid.NewString(), CodebaseID: added.ID, RootID: added.Roots[1].ID}
	var thread codethreads.Thread
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.StartMethod, startThread, &thread))
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.StartMethod, startThread, &thread))
	if thread.Directory != second {
		t.Fatal("thread did not use chosen root")
	}
	var frames codethreads.Page
	frameQuery := codethreads.Read{CodebaseID: added.ID, ThreadID: thread.ID}
	deadline = time.Now().Add(3 * time.Second)
	for {
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, frameQuery, &frames))
		if len(frames.Frames) >= 2 && frames.Thread.State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CAT frame not relayed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if frames.Frames[0].Type != "debug.unknown" || frames.Frames[0].Raw != `{"type":"system","subtype":"init"}` {
		t.Fatal("provider output not preserved in CAT", frames)
	}
	var threadList struct {
		Threads []codethreads.Thread `json:"threads"`
	}
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ListMethod, codethreads.Query{CodebaseID: added.ID}, &threadList))
	if len(threadList.Threads) != 1 {
		t.Fatal("start retry spawned duplicate thread")
	}
	startThread.Provider = "codex"
	if err := caller.CodeHostRequest(t.Context(), identity.ID, codethreads.StartMethod, startThread, &thread); err == nil {
		t.Fatal("provider mismatch accepted for existing thread")
	}
	startThread.ID = uuid.NewString()
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.StartMethod, startThread, &thread))
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.StartMethod, startThread, &thread))
	frameQuery.ThreadID = thread.ID
	deadline = time.Now().Add(3 * time.Second)
	for {
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, frameQuery, &frames))
		if frames.Thread.State == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Codex never became ready", frames.Thread)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if thread.Provider != "codex" || thread.Directory != second || len(frames.Frames) != 3 {
		t.Fatal("Codex routing or capture failed", thread, frames)
	}
	for _, frame := range frames.Frames {
		if (frame.Type != "debug.unknown" && frame.Type != "hook.started") || frame.Provider != "codex" {
			t.Fatal("wrong Codex CAT mapping", frame)
		}
	}
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ListMethod, codethreads.Query{CodebaseID: added.ID}, &threadList))
	if len(threadList.Threads) != 2 {
		t.Fatal("both providers not listed or retry duplicated")
	}
	for _, target := range threadList.Threads {
		var initial codethreads.Page
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, codethreads.Read{CodebaseID: added.ID, ThreadID: target.ID}, &initial))
		input := codethreads.Send{Input: codethreads.Input{ID: uuid.NewString(), Text: "hello", Delivery: "start_turn"}, CodebaseID: added.ID, ThreadID: target.ID}
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.SendMethod, input, &thread))
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.SendMethod, input, &thread))
		deadline = time.Now().Add(3 * time.Second)
		for {
			must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, codethreads.Read{CodebaseID: added.ID, ThreadID: target.ID}, &frames))
			var sent, acked uint64
			count := 0
			hookComplete := false
			for _, f := range frames.Frames {
				if f.Type == "hook.completed" {
					hookComplete = true
				}
				if f.Type == "input.message.user" && f.ID == input.ID {
					sent = f.Seq
					count++
				}
				if f.Type == "message.user" && f.InputID == input.ID {
					acked = f.Seq
				}
			}
			if acked > sent && sent > 0 && count == 1 && !frames.Thread.Busy && hookComplete {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("send/echo/idle not relayed", frames.Thread)
			}
			time.Sleep(10 * time.Millisecond)
		}
		before := frames
		var updates codethreads.Page
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, codethreads.Read{CodebaseID: added.ID, ThreadID: target.ID, Revision: initial.Thread.Revision, After: initial.Next}, &updates))
		var hookStart, hookEnd codethreads.Frame
		for _, frame := range updates.Frames {
			if frame.Type == "hook.started" {
				hookStart = frame
			}
			if frame.Type == "hook.completed" {
				hookEnd = frame
			}
		}
		if hookStart.ID == "" || hookEnd.Supersedes != hookStart.ID || hookStart.SupersededBy != hookEnd.ID || hookStart.ChangeSeq <= initial.Next || hookEnd.Hook.Status != "completed" {
			t.Fatal("hook updates/supersession not relayed", hookStart, hookEnd)
		}
		request := codethreads.Reprocess{CodebaseID: added.ID, ThreadID: target.ID, Revision: frames.Thread.Revision}
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReprocessMethod, request, &thread))
		if thread.Revision != before.Thread.Revision+1 || thread.Busy || thread.State != "ready" {
			t.Fatal("replay changed runtime state", thread)
		}
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, codethreads.Read{CodebaseID: added.ID, ThreadID: target.ID, Revision: before.Thread.Revision, After: before.Next}, &frames))
		if !frames.Reset || !reflect.DeepEqual(before.Frames, frames.Frames) {
			t.Fatal("replay lost raw output/input/ack or failed to reset cursor")
		}
		if err := caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReprocessMethod, request, &thread); err == nil {
			t.Fatal("stale replay revision accepted")
		}
		// Reprocessing does not erase submission deduplication.
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.SendMethod, input, &thread))
		must(t, caller.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, codethreads.Read{CodebaseID: added.ID, ThreadID: target.ID, Revision: frames.Thread.Revision, After: frames.Next}, &frames))
		if len(frames.Frames) != 0 || frames.Thread.Busy {
			t.Fatal("replay caused input to be resent")
		}
	}
	other := uuid.NewString()
	_, err = f.conn.Exec(t.Context(), `INSERT INTO accounts(id,username,direct_permissions) VALUES($1,'other-admin',ARRAY['site.superuser'])`, other)
	must(t, err)
	outsider := actaclient.New(server.URL, codeHostLogin(t, f, other))
	privateHosts, err := outsider.CodeHosts(t.Context())
	must(t, err)
	if len(privateHosts) != 0 {
		t.Fatal("provider metadata leaked to other superuser")
	}
	err = outsider.CodeHostRequest(t.Context(), identity.ID, codebases.DirectoryMethod, query, &listing)
	var problem *actaclient.Error
	if !errors.As(err, &problem) || problem.Code != "host_disconnected" {
		t.Fatal("other superuser reached private host", err)
	}
	err = outsider.CodeHostRequest(t.Context(), identity.ID, codethreads.ReadMethod, frameQuery, &frames)
	if !errors.As(err, &problem) || problem.Code != "host_disconnected" {
		t.Fatal("other superuser reached private frames", err)
	}
	err = outsider.CodeHostRequest(t.Context(), identity.ID, codethreads.SendMethod, codethreads.Send{Input: codethreads.Input{ID: uuid.NewString(), Text: "hello", Delivery: "start_turn"}, CodebaseID: added.ID, ThreadID: thread.ID}, &thread)
	if !errors.As(err, &problem) || problem.Code != "host_disconnected" {
		t.Fatal("other superuser submitted input", err)
	}
	err = outsider.CodeHostRequest(t.Context(), identity.ID, codethreads.ReprocessMethod, codethreads.Reprocess{CodebaseID: added.ID, ThreadID: thread.ID, Revision: thread.Revision}, &thread)
	if !errors.As(err, &problem) || problem.Code != "host_disconnected" {
		t.Fatal("other superuser reprocessed private thread", err)
	}
	err = caller.CodeHostRequest(t.Context(), identity.ID, "threads.steer", map[string]string{"thread_id": thread.ID}, &frames)
	if !errors.As(err, &problem) || problem.Code != "unknown_method" {
		t.Fatal("steering operation unexpectedly enabled", err)
	}
	query.Path = "../"
	err = caller.CodeHostRequest(t.Context(), identity.ID, codebases.DirectoryMethod, query, &listing)
	if !errors.As(err, &problem) || problem.Code != "host_invalid_path" {
		t.Fatal("path traversal accepted", err)
	}
	// Reconnect a new process against the same host-owned catalogue.
	stop()
	stop = func() {}
	waitConnected(false)
	reopened, err := codehost.OpenCatalogue(dir)
	must(t, err)
	stop = start(reopened)
	waitConnected(true)
	var result struct {
		Codebases []codebases.Codebase `json:"codebases"`
	}
	must(t, caller.CodeHostRequest(t.Context(), identity.ID, codebases.ListMethod, struct{}{}, &result))
	if len(result.Codebases) != 1 || result.Codebases[0].ID != added.ID || result.Codebases[0].Roots[0].ID != added.Roots[0].ID {
		t.Fatal("catalogue lost on host restart")
	}
	// Revocation is rechecked before relay, without waiting for the next ping.
	must(t, c.Logout(t.Context()))
	revokedHosts, err := caller.CodeHosts(t.Context())
	must(t, err)
	if len(revokedHosts) != 1 || revokedHosts[0].ProviderStatus != nil {
		t.Fatal("revoked session retained provider status")
	}
	err = caller.CodeHostRequest(t.Context(), identity.ID, codebases.ListMethod, struct{}{}, &result)
	if !errors.As(err, &problem) || problem.Code != "host_disconnected" {
		t.Fatal("revoked native session retained host access", err)
	}
}

func TestCodeHostWebSocketRejectsBrowserOriginAndCookies(t *testing.T) {
	f := securityDatabase(t)
	var handler http.Handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer server.Close()
	cfg, err := config.Parse(server.URL, "")
	must(t, err)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), manager(f), cfg)
	owner, err := f.service.Current(t.Context(), f.token)
	must(t, err)
	secret := codeHostLogin(t, f, owner.ID)
	for _, headers := range []http.Header{
		{"Cookie": []string{"acta_session=" + f.token}},
		{"Authorization": []string{"Bearer " + secret}, "Origin": []string{server.URL}},
		{"Authorization": []string{"Bearer " + secret}, "Origin": []string{"https://outside.example"}},
	} {
		conn, response, err := websocket.Dial(t.Context(), server.URL+"/api/code/hosts/connect", &websocket.DialOptions{HTTPHeader: headers})
		if conn != nil {
			conn.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != 403 {
			t.Fatal("browser upgraded to native host", err)
		}
	}
}
