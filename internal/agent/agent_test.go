package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func human(t *testing.T, ms *memstore.Store, username string) store.User {
	t.Helper()
	u, err := ms.CreateUser(context.Background(), store.NewUser{Username: username, Display: username})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateComposesHandle(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	svc := New(ms)

	a, err := svc.Create(context.Background(), jack.ID, "Deploy-Bot", "Deploy Bot")
	if err != nil {
		t.Fatal(err)
	}
	if a.Username != "jack/deploy-bot" { // lowercased
		t.Fatalf("handle = %q, want jack/deploy-bot", a.Username)
	}
	if a.AgentOfID != jack.ID {
		t.Fatalf("AgentOfID = %q, want %q", a.AgentOfID, jack.ID)
	}
	if a.Display != "Deploy Bot" {
		t.Fatalf("display = %q", a.Display)
	}
	if a.PasswordHash != "" {
		t.Fatal("an agent must have no password")
	}
}

func TestCreateDefaultsDisplayToHandle(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	a, _ := New(ms).Create(context.Background(), jack.ID, "triage", "")
	if a.Display != "jack/triage" {
		t.Fatalf("display = %q, want jack/triage", a.Display)
	}
}

func TestCreateRejectsBadNames(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	svc := New(ms)
	bad := []string{"", "has space", "slash/name", "bang!", "trailing-", "-leading", strings.Repeat("a", 41)}
	for _, name := range bad {
		if _, err := svc.Create(context.Background(), jack.ID, name, ""); err != ErrInvalidName {
			t.Fatalf("Create(%q): want ErrInvalidName, got %v", name, err)
		}
	}
}

func TestCreateRejectsAgentOwningAgent(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	svc := New(ms)
	a, _ := svc.Create(context.Background(), jack.ID, "bot", "")
	if _, err := svc.Create(context.Background(), a.ID, "subbot", ""); err != ErrOwnerIsAgent {
		t.Fatalf("want ErrOwnerIsAgent, got %v", err)
	}
}

func TestCreateRejectsDuplicate(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	svc := New(ms)
	if _, err := svc.Create(context.Background(), jack.ID, "bot", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(context.Background(), jack.ID, "bot", ""); err != ErrNameTaken {
		t.Fatalf("want ErrNameTaken, got %v", err)
	}
}

func TestGetOwnershipGuard(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	sam := human(t, ms, "sam")
	svc := New(ms)
	a, _ := svc.Create(context.Background(), jack.ID, "bot", "")

	if _, err := svc.Get(context.Background(), a.ID, sam.ID); err != ErrNotOwned {
		t.Fatalf("cross-owner Get: want ErrNotOwned, got %v", err)
	}
	if _, err := svc.Get(context.Background(), jack.ID, jack.ID); err != ErrNotOwned {
		t.Fatalf("a human is not an agent: want ErrNotOwned, got %v", err)
	}
	if _, err := svc.Get(context.Background(), a.ID, jack.ID); err != nil {
		t.Fatalf("owner Get: %v", err)
	}
}

func TestListAndDelete(t *testing.T) {
	ms := memstore.New()
	jack := human(t, ms, "jack")
	sam := human(t, ms, "sam")
	svc := New(ms)
	a, _ := svc.Create(context.Background(), jack.ID, "bot", "")
	if _, err := svc.Create(context.Background(), jack.ID, "ci", ""); err != nil {
		t.Fatal(err)
	}

	if list, _ := svc.List(context.Background(), jack.ID); len(list) != 2 {
		t.Fatalf("want 2 agents, got %d", len(list))
	}
	if err := svc.Delete(context.Background(), a.ID, sam.ID); err != ErrNotOwned {
		t.Fatalf("cross-owner delete: want ErrNotOwned, got %v", err)
	}
	if err := svc.Delete(context.Background(), a.ID, jack.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.List(context.Background(), jack.ID); len(list) != 1 {
		t.Fatalf("after delete want 1, got %d", len(list))
	}
}
