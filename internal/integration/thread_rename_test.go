package integration

import (
	"acta/internal/threads"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestThreadNameDiscoveryAndCommandOwnership(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	owner, err := f.service.Current(ctx, f.token)
	must(t, err)
	id := uuid.NewString()
	d := threads.Descriptor{ID: id, RunID: id, Provider: "claude", ProviderID: id, CWD: "/tmp", State: "exited", CreatedAt: time.Now(), Revision: 1}
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{d}))
	q := threads.Control{ID: uuid.NewString(), ThreadID: id, Action: "rename", Name: "Research"}
	if f.store.RequestThreadControl(ctx, uuid.NewString(), q) == nil {
		t.Fatal("foreign owner renamed thread")
	}
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	must(t, f.store.RequestThreadControl(ctx, owner.ID, q))
	q.Name = "Conflicting name"
	if f.store.RequestThreadControl(ctx, owner.ID, q) == nil {
		t.Fatal("accepted conflicting rename replay")
	}
	stale := d
	d.Name, d.Revision = "Research", 2
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{d}))
	must(t, f.store.DiscoverThreads(ctx, owner.ID, []threads.Descriptor{stale}))
	got, err := f.store.Thread(ctx, owner.ID, id)
	must(t, err)
	if got.Name != "Research" {
		t.Fatal("stale discovery lost name", got)
	}
}
