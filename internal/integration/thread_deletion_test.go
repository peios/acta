package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"acta2/internal/accounts"
	"acta2/internal/config"
	"acta2/internal/conversation"
	"acta2/internal/httpapi"
	"acta2/internal/hyperharness"
	"acta2/internal/postgres"
	"acta2/internal/threads"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

func TestThreadDeletionIsActaOnlyAndSurvivesDiscovery(t *testing.T) {
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
	account, err := f.service.Current(ctx, f.token)
	must(t, err)
	conn, _, err := websocket.Dial(ctx, srv.URL+"/api/harnesses/connect", &websocket.DialOptions{Subprotocols: []string{hyperharness.Protocol}, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + agentCLI(t, f, "")}}})
	must(t, err)
	defer conn.CloseNow()
	must(t, wsjson.Write(ctx, conn, hyperharness.Hello{Hostname: "deletion-test"}))
	var hello any
	must(t, wsjson.Read(ctx, conn, &hello))
	id := uuid.NewString()
	desc := threads.Descriptor{ID: id, RunID: id, Provider: "claude", ProviderID: "retained-provider-session", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	inventory := func() {
		t.Helper()
		must(t, wsjson.Write(ctx, conn, hyperharness.ProviderUpdate{Type: "inventory", Threads: []threads.Descriptor{desc}}))
	}
	sendFrame := func(seq int64) {
		t.Helper()
		frame, err := threads.Unknown(threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: seq, ReceivedAt: time.Now().UTC(), Raw: json.RawMessage(`{"type":"unmapped"}`)})
		must(t, err)
		must(t, wsjson.Write(ctx, conn, hyperharness.ProviderUpdate{Type: "frames", ThreadID: id, Frames: []threads.Frame{frame}}))
		var ack struct {
			Type     string
			Sequence int64
		}
		must(t, wsjson.Read(ctx, conn, &ack))
		if ack.Type != "frame_ack" || ack.Sequence != seq {
			t.Fatalf("expected acknowledgement, not a provider control: %+v", ack)
		}
	}
	request := func(method, path, token string) int {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, srv.URL+"/api/threads/"+path, bytes.NewBufferString(`{}`))
		must(t, err)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", srv.URL)
		r.AddCookie(&http.Cookie{Name: "acta_session", Value: token})
		res, err := http.DefaultClient.Do(r)
		must(t, err)
		defer res.Body.Close()
		return res.StatusCode
	}
	inventory()
	sendFrame(1)
	if code := request("POST", id+"/delete", other.token); code != 404 {
		t.Fatal("cross-owner deletion", code)
	}
	if _, err := f.store.Thread(ctx, account.ID, id); err != nil {
		t.Fatal("other owner removed thread", err)
	}
	// Deletion also clears accepted commands which have not been dispatched.
	must(t, f.store.RequestThreadControl(ctx, account.ID, threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"}))
	for range 2 {
		if code := request("POST", id+"/delete", f.token); code != 200 {
			t.Fatal("delete", code)
		}
	}
	if code := request("POST", uuid.NewString()+"/delete", f.token); code != 404 {
		t.Fatal("missing", code)
	}
	desc.Revision++
	inventory()
	sendFrame(2)
	inventory()
	sendFrame(3)
	list, err := f.store.ListThreads(ctx, account.ID)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("rediscovered deleted thread", list)
	}
	for _, path := range []string{id + "/frames", id + "/conversation"} {
		if code := request("GET", path, f.token); code != 404 {
			t.Fatal("deleted history exposed", path, code)
		}
	}
	if _, err := f.store.Conversation(ctx, account.ID, id, conversation.Query{}); err == nil {
		t.Fatal("deleted conversation readable")
	}
	if err := f.store.RequestThreadControl(ctx, account.ID, threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "resume"}); err == nil {
		t.Fatal("accepted deleted thread control")
	}
	for _, table := range []string{"provider_thread_frames", "provider_thread_commands", "thread_conversation_items"} {
		var count int
		must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE thread_id=$1`, id).Scan(&count))
		if count != 0 {
			t.Fatal("retained deleted content", table, count)
		}
	}
	var descriptor, state string
	must(t, f.conn.QueryRow(ctx, `SELECT descriptor::text,conversation_state::text FROM provider_threads WHERE id=$1 AND deleted_at IS NOT NULL`, id).Scan(&descriptor, &state))
	if descriptor != `{"provider": "claude"}` || state != "{}" {
		t.Fatal("tombstone retained metadata", descriptor, state)
	}
	// Startup must skip tombstones rather than rebuild or fail on them.
	reopened, err := postgres.Open(ctx, f.url)
	must(t, err)
	defer reopened.Close()
	list, err = reopened.ListThreads(ctx, account.ID)
	must(t, err)
	if len(list) != 0 {
		t.Fatal("restart restored thread")
	}
}
