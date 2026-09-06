package memstore

import (
	"context"
	"testing"

	"github.com/peios/acta/internal/store"
)

// TestAgentSessionSizes checks sizes are summed per session for one owner
// only, and that a session with no frames is absent.
func TestAgentSessionSizes(t *testing.T) {
	s := New()
	ctx := context.Background()
	u1, _ := s.CreateUser(ctx, store.NewUser{Username: "a", Display: "A", PasswordHash: "x"})
	u2, _ := s.CreateUser(ctx, store.NewUser{Username: "b", Display: "B", PasswordHash: "x"})
	mk := func(id, owner string) {
		if _, err := s.CreateAgentSession(ctx, store.AgentSession{ID: id, OwnerID: owner, Backend: "claude-code"}); err != nil {
			t.Fatal(err)
		}
	}
	mk("s1", u1.ID)
	mk("s2", u1.ID)
	mk("empty", u1.ID)
	mk("other", u2.ID)
	add := func(id string, payload string) {
		if _, err := s.AppendAgentSessionEvent(ctx, store.AgentSessionEvent{SessionID: id, Kind: "event", Payload: []byte(payload)}); err != nil {
			t.Fatal(err)
		}
	}
	add("s1", `{"a":1}`)
	add("s1", `{"b":22}`)
	add("s2", `{}`)
	add("other", `{"c":333}`)

	got, err := s.AgentSessionSizes(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got["s1"] != (store.AgentSessionSize{Frames: 2, Bytes: 15}) {
		t.Errorf("s1 = %+v", got["s1"])
	}
	if got["s2"] != (store.AgentSessionSize{Frames: 1, Bytes: 2}) {
		t.Errorf("s2 = %+v", got["s2"])
	}
	if _, ok := got["empty"]; ok {
		t.Error("a session with no frames should be absent")
	}
	if _, ok := got["other"]; ok {
		t.Error("another owner's session should be absent")
	}
}
