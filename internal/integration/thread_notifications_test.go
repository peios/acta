package integration

import (
	"acta2/internal/config"
	"acta2/internal/httpapi"
	"acta2/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"testing"
	"time"
)

func TestThreadNotificationsLifecycleIsolationAndReplay(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	other, _ := permissionMember(t, f, "other")
	id := uuid.NewString()
	desc := threads.Descriptor{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", Name: "Review", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	var seq int64
	bundle := func(n int64, lane, kind string, data map[string]any) []threads.Frame {
		switch kind {
		case "approval/request":
			data["title"] = "Read file"
			data["reason"] = "Permission"
			data["details"] = map[string]any{}
			data["tool_id"] = nil
		case "question/request":
			data["tool_id"] = nil
			if _, ok := data["blocking"]; !ok {
				data["blocking"] = true
			}
			data["questions"] = []any{map[string]any{"id": "q", "header": "Choice", "text": "Continue?", "multiple": false, "options": []any{}}}
		case "turn/completed":
			data["error"] = nil
			data["started_at"] = nil
			data["completed_at"] = nil
			data["duration_ms"] = nil
		}
		p := threads.ProviderFrame{ThreadID: id, RunID: desc.RunID, Provider: "codex", Sequence: n, ReceivedAt: desc.CreatedAt}
		debug := threads.NewFrame(p, "debug/resolved", map[string]any{"stream": "stdout", "raw": "{}", "reason": "Mapped", "outputs": []threads.OutputReference{{OutputIndex: 1, Kind: kind}}})
		value := threads.NewFrame(p, kind, data)
		value.OutputIndex = 1
		value.LaneID = lane
		return []threads.Frame{debug, value}
	}
	appendFrame := func(lane, kind string, data map[string]any) []threads.Frame {
		t.Helper()
		seq++
		frames := bundle(seq, lane, kind, data)
		_, e := f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
		must(t, e)
		return frames
	}
	list := func(want int) threads.NotificationPage {
		t.Helper()
		page, e := f.store.ThreadNotifications(ctx, owner.ID)
		must(t, e)
		if page.Total != want || len(page.Items) != want {
			t.Fatalf("want %d notices, got %+v", want, page)
		}
		return page
	}
	request := uuid.NewString()
	frames := appendFrame("child", "approval/request", map[string]any{"approval_id": request, "turn_id": "child-turn"})
	page := list(1)
	n := page.Items[0]
	if n.LaneID != "child" || n.Title != "Approval needed" || n.ThreadName != "Review" {
		t.Fatal(n)
	}
	_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
	must(t, err)
	list(1)
	foreign, err := f.store.ThreadNotifications(ctx, other.ID)
	must(t, err)
	if foreign.Total != 0 {
		t.Fatal("leaked notifications")
	}
	must(t, f.store.ReadThreadNotifications(ctx, other.ID, []threads.NotificationRead{{ID: n.ID, Revision: n.Revision}}))
	list(1)
	must(t, f.store.RequestThreadControl(ctx, owner.ID, threads.Control{ID: request, ThreadID: id, RunID: id, LaneID: "child", Action: "approval", ApprovalID: request, Decision: "approve"}))
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, threads.Result{ID: request, ThreadID: id, Outcome: "accepted"}))
	list(0)
	// Semantic retransmission after acceptance must not reopen attention.
	appendFrame("child", "approval/request", map[string]any{"approval_id": request, "turn_id": "child-turn"})
	list(0)
	appendFrame("child", "turn/completed", map[string]any{"turn_id": "child-turn", "outcome": "completed"})
	list(0)
	frames = appendFrame("", "turn/completed", map[string]any{"turn_id": "main", "outcome": "completed"})
	page = list(1)
	n = page.Items[0]
	must(t, f.store.ReadThreadNotifications(ctx, owner.ID, []threads.NotificationRead{{ID: n.ID, Revision: n.Revision}}))
	list(0)
	_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
	must(t, err)
	list(0)
	appendFrame("", "turn/completed", map[string]any{"turn_id": "main", "outcome": "failed"})
	page = list(1)
	if page.Items[0].ID != n.ID || page.Items[0].Revision <= n.Revision || page.Items[0].Kind != "failed" {
		t.Fatal(page)
	}
	// A stale acknowledgement cannot hide the newly corrected failure.
	must(t, f.store.ReadThreadNotifications(ctx, owner.ID, []threads.NotificationRead{{ID: n.ID, Revision: n.Revision}}))
	list(1)
	appendFrame("", "turn/completed", map[string]any{"turn_id": "main", "outcome": "interrupted"})
	list(0)
	appendFrame("", "question/request", map[string]any{"question_id": uuid.NewString(), "turn_id": "pending", "blocking": true})
	list(1)
	appendFrame("", "turn/completed", map[string]any{"turn_id": "pending", "outcome": "interrupted"})
	list(0)
	// A gap rolls back both the notification and conversation writes.
	first := bundle(seq+1, "", "turn/completed", map[string]any{"turn_id": "rollback", "outcome": "completed"})
	gap := bundle(seq+3, "", "turn/completed", map[string]any{"turn_id": "gap", "outcome": "completed"})
	if _, err = f.store.AppendThreadFrames(ctx, owner.ID, id, append(first, gap...)); err == nil {
		t.Fatal("accepted gap")
	}
	list(0)
	appendFrame("", "question/request", map[string]any{"question_id": uuid.NewString(), "turn_id": "late"})
	list(1)
	desc.Revision++
	desc.RunID = uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	list(0)
	desc.Revision++
	desc.State = "exited"
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	page = list(1)
	if page.Items[0].Title != "Provider exited unexpectedly" {
		t.Fatal(page)
	}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	list(1)
	must(t, f.store.DeleteThread(ctx, owner.ID, id))
	list(0)
}

func TestThreadNotificationsDeliberateKill(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	desc := threads.Descriptor{ID: id, RunID: id, Provider: "claude", CWD: "/tmp", State: "running", CreatedAt: time.Now().UTC(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "kill"}))
	desc.Revision++
	desc.State = "exited"
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{desc}))
	page, err := f.store.ThreadNotifications(ctx, owner.ID)
	must(t, err)
	if page.Total != 0 {
		t.Fatal(page)
	}
}

func TestThreadNotificationsHTTP(t *testing.T) {
	f := securityDatabase(t)
	cfg, err := config.Parse("http://localhost:8081", "")
	must(t, err)
	h := httpapi.New(f.service, f.security, nil, nil, cfg)
	c := client{t, h, map[string]*http.Cookie{}}
	c.request("GET", "notifications", "", 401)
	c.request("POST", "notifications/read", `{"items":[]}`, 401)
	c.cookies["acta_session"] = &http.Cookie{Name: "acta_session", Value: f.token}
	var page threads.NotificationPage
	must(t, json.Unmarshal(c.request("GET", "notifications", "", 200).Body.Bytes(), &page))
	if page.Items == nil || page.Counts == nil || page.Total != 0 {
		t.Fatal(page)
	}
	c.request("POST", "notifications/read", `{"items":[]}`, 400)
	c.request("POST", "notifications/read", `{"items":[{"id":"not-a-uuid","revision":1}]}`, 400)
	c.request("POST", "notifications/read", body(map[string]any{"items": []threads.NotificationRead{{ID: uuid.NewString(), Revision: 0}}}), 400)
	c.request("POST", "notifications/read", body(map[string]any{"items": []threads.NotificationRead{{ID: uuid.NewString(), Revision: 1}}}), 200)
}
