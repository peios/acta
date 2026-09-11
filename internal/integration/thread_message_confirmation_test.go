package integration

import (
	"acta/internal/conversation"
	"acta/internal/postgres"
	"acta/internal/threadadapter"
	"acta/internal/threads"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestMessageConfirmationOutsideLatestPage(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{{ID: id, RunID: id, Provider: "codex", ProviderID: "native", CWD: "/tmp", State: "running", CreatedAt: time.Now(), Revision: 1}}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, RunID: id, Action: "send", Text: "same text"}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.CompleteThreadControl(ctx, owner.ID, threads.Result{ID: q.ID, ThreadID: id, Outcome: "accepted"}))
	status, err := f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if status.MessageConfirmed {
		t.Fatal("acceptance without an echo confirmed the message")
	}
	state := threadadapter.State{NativeID: "native", Submissions: map[string]string{q.ID: id}}
	for n := 0; n < 76; n++ {
		client := ""
		if n == 0 {
			client = q.ID
		}
		raw, _ := json.Marshal(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "native", "turnId": "turn", "item": map[string]any{"type": "userMessage", "id": uuid.NewString(), "clientId": client, "content": []any{map[string]any{"type": "text", "text": "same text"}}}}})
		next, bundle, err := threadadapter.Map(state, threads.ProviderFrame{ThreadID: id, RunID: id, Provider: "codex", Sequence: int64(n + 1), ReceivedAt: time.Now().UTC()}, "stdout", string(raw))
		must(t, err)
		_, err = f.store.AppendThreadFrames(ctx, owner.ID, id, bundle)
		must(t, err)
		state = next
	}
	page, err := f.store.Conversation(ctx, owner.ID, id, conversation.Query{})
	must(t, err)
	if len(page.Items) != 50 || !page.HasMore {
		t.Fatal("not testing an off-page echo")
	}
	for _, item := range page.Items {
		if item.Payload["id"] == "submission:"+q.ID {
			t.Fatal("echo unexpectedly on newest page")
		}
	}
	status, err = f.store.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if !status.MessageConfirmed {
		t.Fatal("off-page echo did not confirm message")
	}
	other := q
	other.ID = uuid.NewString()
	must(t, f.store.RequestThreadControl(ctx, owner.ID, other))
	status, err = f.store.ThreadControl(ctx, owner.ID, id, other.ID)
	must(t, err)
	if status.MessageConfirmed {
		t.Fatal("same text confirmed another command")
	}
	if _, err = f.store.ThreadControl(ctx, uuid.NewString(), id, q.ID); err == nil {
		t.Fatal("foreign owner read confirmation")
	}
	if _, err = f.store.ThreadControl(ctx, owner.ID, uuid.NewString(), q.ID); err == nil {
		t.Fatal("another thread read confirmation")
	}
	// Confirmation is derived from the recoverable projection, not its page cursor.
	_, err = f.conn.Exec(ctx, `UPDATE provider_threads SET conversation_version=0 WHERE id=$1`, id)
	must(t, err)
	reopened, err := postgres.Open(ctx, f.url)
	must(t, err)
	defer reopened.Close()
	status, err = reopened.ThreadControl(ctx, owner.ID, id, q.ID)
	must(t, err)
	if !status.MessageConfirmed {
		t.Fatal("rebuild lost confirmation")
	}
}
