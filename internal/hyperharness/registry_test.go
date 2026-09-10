package hyperharness

import (
	"acta2/internal/providers"
	"sync"
	"testing"
)

func TestConnectionOwnershipAndNotifications(t *testing.T) {
	r := NewRegistry()
	changed, stop := r.Subscribe("alice")
	defer stop()
	a, closeA := r.Connect("alice", "same-host")
	b, closeB := r.Connect("alice", "same-host")
	_, closeOther := r.Connect("bob", "private")
	defer closeOther()
	if a.ID == b.ID {
		t.Fatal("connections merged by hostname")
	}
	if len(r.Snapshot("alice").Connections) != 2 || len(r.Snapshot("stranger").Connections) != 0 {
		t.Fatal("owner isolation")
	}
	select {
	case <-changed:
	default:
		t.Fatal("missing notification")
	}
	closeA()
	closeA()
	got := r.Snapshot("alice").Connections
	if len(got) != 1 || got[0].ID != b.ID {
		t.Fatal("disconnect removed another connection")
	}
	closeB()
	stop()
	if _, ok := r.owners["alice"]; ok {
		t.Fatal("retained offline owner")
	}
}
func TestConcurrentConnectionChurn(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			ch, stop := r.Subscribe("owner")
			_, close := r.Connect("owner", "host")
			_ = r.Snapshot("owner")
			select {
			case <-ch:
			default:
			}
			stop()
			close()
		})
	}
	wg.Wait()
	if len(r.owners) != 0 {
		t.Fatal("registry leaked state")
	}
}
func TestHostnameValidation(t *testing.T) {
	for _, s := range []string{"", "host\nforged", "host name", "\x1b[31mhost", "host\u202e"} {
		if ValidHostname(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"jack-desktop", "host.local", "机器"} {
		if !ValidHostname(s) {
			t.Fatalf("rejected %q", s)
		}
	}
}

func TestProviderUpdatesAreIsolatedAndCannotReviveConnections(t *testing.T) {
	r := NewRegistry()
	c, disconnect := r.Connect("owner", "desktop")
	c.Providers[0].ID = "mutation"
	if r.Snapshot("owner").Connections[0].Providers[0].ID != "codex" {
		t.Fatal("Connect aliases registry")
	}
	changes, stop := r.Subscribe("owner")
	defer stop()
	states := providers.Initial()
	states[0] = providers.Status{ID: "codex", Installation: "installed", Version: "0.153.4", Authentication: "signed_in"}
	if r.UpdateProviders("other", c.ID, states) {
		t.Fatal("wrong owner accepted")
	}
	if !r.UpdateProviders("owner", c.ID, states) {
		t.Fatal("update rejected")
	}
	<-changes
	if !r.UpdateProviders("owner", c.ID, states) {
		t.Fatal("duplicate rejected")
	}
	select {
	case <-changes:
		t.Fatal("duplicate notified")
	default:
	}
	states[0].Version = "0.0.0"
	snapshot := r.Snapshot("owner")
	snapshot.Connections[0].Providers[0].Version = "9.9.9"
	if r.Snapshot("owner").Connections[0].Providers[0].Version != "0.153.4" {
		t.Fatal("update or snapshot aliases storage")
	}
	states[0].Issue = "raw secret"
	if r.UpdateProviders("owner", c.ID, states) {
		t.Fatal("invalid snapshot accepted")
	}
	disconnect()
	if r.UpdateProviders("owner", c.ID, providers.Initial()) || len(r.Snapshot("owner").Connections) != 0 {
		t.Fatal("disconnected connection revived")
	}
}
