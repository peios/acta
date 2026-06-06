package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func TestAttrVocab(t *testing.T) {
	// Round-trip slug<->value across every option, including unset.
	for _, v := range []AttrVocab{Priorities, ItemTypes, Sizes} {
		for _, o := range v.Options() {
			if got, ok := v.Parse(o.Slug); !ok || got != o.Value {
				t.Errorf("Parse(%q) = %d,%v want %d", o.Slug, got, ok, o.Value)
			}
			if v.Label(o.Value) != o.Label {
				t.Errorf("Label(%d) = %q want %q", o.Value, v.Label(o.Value), o.Label)
			}
			if !v.Valid(o.Value) {
				t.Errorf("Valid(%d) = false", o.Value)
			}
		}
	}
	// Case/space-insensitive parse; unknown slug rejected.
	if got, ok := Priorities.Parse("  Urgent "); !ok || got != 4 {
		t.Errorf("Parse fuzzy urgent = %d,%v", got, ok)
	}
	if _, ok := Priorities.Parse("p0"); ok {
		t.Error("unknown slug should not parse")
	}
	// Out-of-range value is invalid; unknown value falls back to the unset option.
	if Priorities.Valid(9) {
		t.Error("9 should be invalid for priority")
	}
	if Priorities.Option(9).Slug != "none" {
		t.Error("unknown value should fall back to none")
	}
}

func TestParseDueAndOverdue(t *testing.T) {
	if d, err := ParseDue(""); err != nil || d != nil {
		t.Errorf("empty due = %v,%v want nil,nil", d, err)
	}
	if _, err := ParseDue("not-a-date"); err == nil {
		t.Error("malformed due should error")
	}
	d, err := ParseDue("2026-07-01")
	if err != nil || d == nil || DueString(d) != "2026-07-01" {
		t.Fatalf("ParseDue round-trip failed: %v %v", d, err)
	}

	past := time.Now().UTC().AddDate(0, 0, -2)
	future := time.Now().UTC().AddDate(0, 0, 2)
	if !Overdue(&past, false) {
		t.Error("past + not done should be overdue")
	}
	if Overdue(&past, true) {
		t.Error("done item is never overdue")
	}
	if Overdue(&future, false) {
		t.Error("future date is not overdue")
	}
	if Overdue(nil, false) {
		t.Error("no due date is not overdue")
	}
	// Due today is not yet overdue.
	today := time.Now().UTC()
	if Overdue(&today, false) {
		t.Error("due today should not be overdue")
	}
}

// newTestService wires a Service over a fresh memstore with one workspace, board,
// status and item, returning the service, store and the seeded item id.
func newAttrTestService(t *testing.T) (*Service, *memstore.Store, string) {
	t.Helper()
	ms := memstore.New()
	svc := New(ms)
	ctx := context.Background()
	ws, err := ms.CreateWorkspace(ctx, store.Workspace{Slug: "w", Name: "W"})
	if err != nil {
		t.Fatal(err)
	}
	bd, err := ms.CreateBoard(ctx, store.Board{WorkspaceID: ws.ID, Name: "Tasks", Slug: "tasks"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := ms.CreateStatus(ctx, store.Status{WorkspaceID: ws.ID, BoardID: bd.ID, Name: "To do"})
	if err != nil {
		t.Fatal(err)
	}
	it, err := ms.CreateItem(ctx, store.Item{WorkspaceID: ws.ID, StatusID: st.ID, Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, ms, it.ID
}

func TestServiceSetAttributes(t *testing.T) {
	svc, ms, id := newAttrTestService(t)
	ctx := context.Background()

	if err := svc.SetPriority(ctx, id, 4); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetType(ctx, id, 2); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetSize(ctx, id, 3); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := svc.SetDue(ctx, id, &due); err != nil {
		t.Fatal(err)
	}
	got, _ := ms.ItemByID(ctx, id)
	if got.Priority != 4 || got.Type != 2 || got.Size != 3 || DueString(got.DueDate) != "2026-07-01" {
		t.Fatalf("attrs not set: %+v", got)
	}

	// Out-of-range is rejected and leaves the value untouched.
	if err := svc.SetPriority(ctx, id, 99); !errors.Is(err, ErrInvalidAttribute) {
		t.Errorf("SetPriority(99) = %v, want ErrInvalidAttribute", err)
	}
	if got, _ := ms.ItemByID(ctx, id); got.Priority != 4 {
		t.Error("rejected set should not change the value")
	}

	// Clearing works.
	if err := svc.SetPriority(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDue(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = ms.ItemByID(ctx, id)
	if got.Priority != 0 || got.DueDate != nil {
		t.Errorf("clear failed: %+v", got)
	}

	// Unknown item id propagates the store's not-found.
	if err := svc.SetPriority(ctx, "nope", 1); !errors.Is(err, store.ErrItemNotFound) {
		t.Errorf("unknown id = %v, want ErrItemNotFound", err)
	}
}
