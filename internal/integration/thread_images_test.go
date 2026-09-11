package integration

import (
	"acta/internal/conversation"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestImageCommandAndConversationDurability(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "claude", ProviderID: id, CWD: "/tmp", State: "running", CreatedAt: time.Now(), Revision: 1}}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "send", Images: []threads.InputImage{{Name: "pixel.png", MediaType: "image/png", Base64: "YWJj"}}}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	changed := q
	changed.Images = append([]threads.InputImage{}, q.Images...)
	changed.Images[0].Base64 = "ZGVm"
	if f.store.RequestThreadControl(ctx, owner.ID, changed) == nil {
		t.Fatal("image mutation reused command identity")
	}
	pending, err := f.store.PendingThreadControls(ctx, owner.ID, id)
	must(t, err)
	if len(pending) != 1 || pending[0].Images[0].Base64 != "YWJj" {
		t.Fatal("lost immutable image")
	}
	state := threadadapter.State{RunID: id, NativeID: id, Submissions: map[string]string{q.ID: id}, SubmissionTexts: map[string]string{q.ID: ""}, SubmissionImages: map[string][]threads.InputImage{q.ID: q.Images}}
	raw, _ := json.Marshal(map[string]any{"type": "command_lifecycle", "session_id": id, "command_uuid": q.ID, "state": "started"})
	_, frames, err := threadadapter.Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "claude", Sequence: 1, ReceivedAt: time.Now()}, "stdout", string(raw))
	must(t, err)
	_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, frames)
	must(t, err)
	page, err := f.store.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, err)
	found := false
	for _, item := range page.Items {
		if item.Payload["kind"] == "user-message" {
			encoded, _ := json.Marshal(item.Payload["images"])
			if string(encoded) == "null" || string(encoded) == "[]" {
				t.Fatal("missing image")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no user message")
	}
	receipt, err := f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if !receipt.MessageConfirmed {
		t.Fatal("image-only message not confirmed")
	}
	if _, err = f.store.Conversation(ctx, uuid.NewString(), id, conversation.Query{}); err == nil {
		t.Fatal("foreign owner accessed image history")
	}
}
